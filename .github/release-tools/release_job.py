#!/usr/bin/env python3
"""Container worker for the shared publication/rehearsal build graph."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

HERE = Path(__file__).resolve().parent
TOOLS = HERE.parent / 'tools'
if not TOOLS.is_dir():
    TOOLS = HERE.parents[1] / 'tools'
sys.path.insert(0, str(TOOLS))
import validation_runtime
import release_matrix as matrix
import release_pricing as pricing


def run(*command):
    subprocess.run(command, check=True)


def configure():
    plan = json.loads(Path('.release-plan/metadata.json').read_text())
    matrix.require_head(plan['sha'])
    if Path('backend/cmd/server/VERSION').read_text().strip() != plan['version']:
        raise ValueError('source VERSION differs from the prepared version')
    repository = os.environ['GITHUB_REPOSITORY']
    owner, name = repository.split('/')
    os.environ.update(RELEASE_VERSION=plan['version'], RELEASE_SHA=plan['sha'],
                      RELEASE_DATE=plan['date'], RELEASE_TAG=plan['tag'],
                      RELEASE_MODE=plan['mode'], GORELEASER_CURRENT_TAG=plan['tag'],
                      GITHUB_REPO_OWNER=owner, GITHUB_REPO_OWNER_LOWER=owner.lower(),
                      GITHUB_REPO_NAME=name,
                      DOCKER_TAG_VERSION=plan['tag'].replace('+', '-'),
                      DRY_RUN=plan['dry_run'], RUNNER_TEMP=str(Path('.release-output').resolve()))
    Path('.release-output').mkdir(exist_ok=True)
    return plan


def verify(plan, strict):
    if strict != (plan['mode'] != 'branch-rehearsal'):
        raise ValueError('incorrect verification gate for release mode')
    command = [sys.executable, str(TOOLS / 'check_release.py'), '--repo-root', str(Path.cwd())]
    if strict:
        tag = plan['tag']
        ref = 'refs/tags/' + tag
        if matrix.git('cat-file', '-t', ref) != 'tag':
            raise ValueError('release tag must be annotated')
        if matrix.git('rev-parse', ref + '^{}') != plan['sha']:
            raise ValueError('annotated tag does not match captured source')
        if matrix.git('for-each-ref', '--format=%(contents:subject)', ref) != 'Sub2API Plus ' + tag:
            raise ValueError('unexpected release tag subject')
        branch = os.environ['DEFAULT_BRANCH']
        run('git', 'fetch', '--no-tags', 'origin',
            f'+refs/heads/{branch}:refs/remotes/origin/{branch}')
        run('git', 'merge-base', '--is-ancestor', plan['sha'], 'origin/' + branch)
        notes = Path('.release-plan/notes.md')
        notes.write_text(matrix.git('for-each-ref', '--format=%(contents)', ref) + '\n')
        command += ['--tag', tag, '--notes-file', str(notes), '--require-status', 'planned']
        run(sys.executable, str(TOOLS / 'workflow_provenance.py'), '--repository',
            os.environ['GITHUB_REPOSITORY'], '--branch', branch, '--sha', plan['sha'])
    run(*command)


def build_binary(plan, args):
    matrix.generate_config(argparse.Namespace(mode='build', goos=args.goos,
                           goarch=args.goarch, output='.release-build.yaml'))
    run('goreleaser', 'release', '--snapshot', '--clean', '--config', '.release-build.yaml',
        '--skip=validate,before,publish,announce,docker')
    matrix.collect(argparse.Namespace(version=plan['version'], sha=plan['sha'],
                   date=plan['date'], goos=args.goos, goarch=args.goarch, output='release-leaf'))


def verify_checksums():
    expected = {p.name: matrix.sha256(p) for p in Path('release-input').glob('sub2api_*')}
    actual = {}
    for line in Path('dist/checksums.txt').read_text().splitlines():
        digest, name = line.split(maxsplit=1)
        name = Path(name.lstrip('*')).name
        if name in actual:
            raise ValueError('duplicate publisher checksum')
        actual[name] = digest
    if actual != expected:
        raise ValueError('publisher checksums do not match the verified complete artifact set')
    return expected


def package(plan, dry_run):
    if dry_run != (plan['mode'] != 'publish'):
        raise ValueError('publication job cannot consume rehearsal evidence')
    args = argparse.Namespace(input='release-input', version=plan['version'],
                              sha=plan['sha'], date=plan['date'], output='.release-context')
    matrix.contexts(args)
    if plan['mode'] == 'branch-rehearsal':
        os.environ['TAG_MESSAGE'] = 'Branch packaging rehearsal; no publication provenance.'
    else:
        os.environ['TAG_MESSAGE'] = matrix.git(
            'for-each-ref', '--format=%(contents:body)', 'refs/tags/' + plan['tag'])
    repository = os.environ['GITHUB_REPOSITORY']
    directory = Path('.release-pricing').resolve()
    pricing.generate(repository, plan['tag'], directory)
    # Compare before any writes. Publish mode uploads only after GoReleaser below.
    pricing.compare(repository, plan['tag'], plan['mode'], directory, upload=False)
    matrix.generate_config(argparse.Namespace(mode='publish', output='.release-publish.yaml'))
    # Materialize and verify the upload checksums before registry or Release writes.
    run('goreleaser', 'release', '--snapshot', '--clean', '--config', '.release-publish.yaml',
        '--skip=validate,before,docker,publish,announce')
    expected = verify_checksums()
    exists = None if dry_run else pricing.release_metadata(repository, plan['tag'], allow_missing=True)
    run('bash', str(HERE / 'release-images.sh'))
    if dry_run:
        from release_oci import inspect_images
        oci = inspect_images(plan, Path('.release-output'), Path('.release-context'), repository)
    else:
        oci = None
        flags = '--skip=validate,before,docker,announce' if exists else '--skip=validate,before,docker'
        run('goreleaser', 'release', '--clean', '--config', '.release-publish.yaml', flags)
        verify_checksums()
        pricing.compare(repository, plan['tag'], 'publish', directory)
    report = {**plan, 'tooling_sha': Path('.release-tooling/revision').read_text().strip(),
              'oci': oci, 'pricing': json.loads((directory / 'comparison.json').read_text()),
              'archives': expected, 'publication_permissions_tested': not dry_run}
    Path('.release-output/report.json').write_text(json.dumps(report, indent=2) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('verify', 'frontend', 'binary', 'package'))
    parser.add_argument('--strict', action='store_true')
    parser.add_argument('--dry-run', action='store_true')
    parser.add_argument('--goos')
    parser.add_argument('--goarch')
    args = parser.parse_args()
    validation_runtime.require_in_validation(tool='release worker')
    plan = configure()
    if args.action == 'verify':
        verify(plan, args.strict)
    elif args.action == 'frontend':
        run('pnpm', '--dir', 'frontend', 'install', '--frozen-lockfile')
        run('pnpm', '--dir', 'frontend', 'run', 'build')
    elif args.action == 'binary':
        build_binary(plan, args)
    else:
        package(plan, args.dry_run)


if __name__ == '__main__':
    main()
