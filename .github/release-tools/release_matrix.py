#!/usr/bin/env python3
"""Split GoReleaser builds across runners without requiring GoReleaser Pro."""
import argparse
import hashlib
import itertools
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile
from datetime import datetime, timezone

import yaml

FULL_CONFIG = Path('.goreleaser.yaml')
VERSION_FILE = Path('backend/cmd/server/VERSION')
VERSION_RE = re.compile(
    r'(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)'
    r'(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?'
    r'(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?'
)
PLUS_TAG_RE = re.compile(r'v\d+\.\d+\.\d+\+custom\.(?:00[1-9]|0[1-9]\d|[1-9]\d{2})')
SHA_RE = re.compile(r'[0-9a-f]{40}')


def config():
    return yaml.safe_load(FULL_CONFIG.read_text())


def targets():
    build = config()['builds'][0]
    result = []
    for goos, goarch in itertools.product(build['goos'], build['goarch']):
        item = {'goos': goos, 'goarch': goarch}
        if any(all(item.get(k) == v for k, v in rule.items()) for rule in build.get('ignore', [])):
            continue
        result.append(item)
    if not result:
        raise ValueError('empty release target matrix')
    return result


def archive_name(version, target):
    if not valid_version(version) or target not in targets():
        raise ValueError('invalid release version or target')
    suffix = 'zip' if target['goos'] == 'windows' else 'tar.gz'
    return f"sub2api_{version}_{target['goos']}_{target['goarch']}.{suffix}"


def sha256(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def git(*args):
    return subprocess.check_output(['git', *args], text=True).strip()


def require_head(sha):
    if SHA_RE.fullmatch(sha) is None or git('rev-parse', 'HEAD') != sha:
        raise ValueError('checkout does not match the prepared application SHA')


def valid_version(version):
    if VERSION_RE.fullmatch(version) is None:
        return False
    prerelease = version.split('+', 1)[0].partition('-')[2]
    return not any(part.isdigit() and len(part) > 1 and part.startswith('0')
                   for part in prerelease.split('.'))


def plan(args):
    sha = git('rev-parse', 'HEAD')
    requested = args.ref.removeprefix('refs/tags/')
    explicit_tag = args.ref.startswith('refs/tags/')
    tag_exists = subprocess.run(
        ['git', 'show-ref', '--verify', '--quiet', f'refs/tags/{requested}'],
        check=False,
    ).returncode == 0
    if not args.dry_run:
        if PLUS_TAG_RE.fullmatch(requested) is None:
            raise ValueError('publishing requires a Plus version tag')
        if os.environ.get('GITHUB_REF') != f'refs/tags/{requested}':
            raise ValueError('real publication must execute from its eligible tag ref')
        mode = 'publish'
    elif tag_exists or explicit_tag or PLUS_TAG_RE.fullmatch(requested):
        if not tag_exists or PLUS_TAG_RE.fullmatch(requested) is None:
            raise ValueError('invalid or absent rehearsal tag')
        mode = 'tag-rehearsal'
    else:
        mode = 'branch-rehearsal'
    version = VERSION_FILE.read_text().strip()
    if not valid_version(version):
        raise ValueError('invalid VERSION')
    tag = 'v' + version
    if mode != 'branch-rehearsal':
        if git('rev-parse', '--verify', f'refs/tags/{requested}^{{commit}}') != sha:
            raise ValueError('checkout does not match the selected release tag')
        if version != requested.removeprefix('v'):
            raise ValueError('pristine VERSION does not match the selected release tag')
        tag = requested
    if PLUS_TAG_RE.fullmatch(tag) is None:
        raise ValueError('Plus packaging requires a valid custom version')
    if os.environ.get('GITHUB_EVENT_NAME') == 'push' and args.dry_run:
        raise ValueError('tag pushes cannot select rehearsal mode')
    result = {'sha': sha, 'tag': tag, 'version': version,
              'owner_lower': os.environ.get('GITHUB_REPOSITORY_OWNER', '').lower(),
              'dry_run': str(args.dry_run).lower(), 'mode': mode,
              'date': datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
              'matrix': json.dumps({'include': [dict(target, runner=(
                  'ubuntu-24.04-arm' if target['goarch'] == 'arm64' else 'ubuntu-24.04'))
                  for target in targets()]}, separators=(',', ':'))}
    output_dir = Path(args.output)
    output_dir.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(VERSION_FILE, output_dir / 'VERSION')
    (output_dir / 'metadata.json').write_text(json.dumps(result, indent=2) + '\n')
    with Path(args.github_output).open('a') as output:
        for key, value in result.items():
            output.write(f'{key}={value}\n')


def generate_config(args):
    data = config()
    data['snapshot'] = {'version_template': '{{ .Env.RELEASE_VERSION }}'}
    data['dockers'] = []
    data['docker_manifests'] = []
    data['dockers_v2'] = []
    if args.mode == 'build':
        target = {'goos': args.goos, 'goarch': args.goarch}
        if target not in targets():
            raise ValueError('unsupported build target')
        for build in data['builds']:
            build['goos'], build['goarch'], build['ignore'] = [args.goos], [args.goarch], []
            build['ldflags'] = [re.sub(r'{{\s*\.Date\s*}}', '{{ .Env.RELEASE_DATE }}', flag)
                                for flag in build.get('ldflags', [])]
    else:
        # Artifacts are supplied through the OSS extra_files mechanism. No build
        # is repeated on the publishing runner, and release templates stay intact.
        data['before'] = {'hooks': []}
        data['builds'] = [{'id': 'sub2api', 'skip': True}]
        data['archives'] = []
        extra = [{'glob': 'release-input/sub2api_*.tar.gz'}, {'glob': 'release-input/sub2api_*.zip'}]
        data['release']['extra_files'] = extra
        data['checksum'] = {'name_template': 'checksums.txt', 'algorithm': 'sha256', 'extra_files': extra}
    Path(args.output).write_text(yaml.safe_dump(data, sort_keys=False, allow_unicode=True))


def collect(args):
    require_head(args.sha)
    target = {'goos': args.goos, 'goarch': args.goarch}
    name = archive_name(args.version, target)
    source = Path('dist') / name
    checksums = {line.split()[1].lstrip('*'): line.split()[0] for line in Path('dist/checksums.txt').read_text().splitlines()}
    digest = sha256(source)
    if checksums.get(name) != digest:
        raise ValueError('archive does not match the build checksum')
    verify_binary(source, target, args.version, args.sha, args.date)
    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, output / name)
    manifest = {'sha': args.sha, 'version': args.version, 'date': args.date,
                'target': target, 'archive': name, 'sha256': digest}
    (output / f"manifest-{args.goos}-{args.goarch}.json").write_text(json.dumps(manifest) + '\n')


def verify(args):
    require_head(args.sha)
    directory = Path(args.input)
    expected = set()
    for target in targets():
        name = archive_name(args.version, target)
        manifest_name = f"manifest-{target['goos']}-{target['goarch']}.json"
        expected.update((name, manifest_name))
        for path in (directory / name, directory / manifest_name):
            if path.is_symlink() or not path.is_file():
                raise ValueError('release inputs must be regular files')
        manifest = json.loads((directory / manifest_name).read_text())
        if manifest != {'sha': args.sha, 'version': args.version, 'date': args.date, 'target': target,
                        'archive': name, 'sha256': sha256(directory / name)}:
            raise ValueError(f'build provenance or checksum mismatch: {name}')
        verify_binary(directory / name, target, args.version, args.sha, args.date)
    if {p.name for p in directory.iterdir()} != expected:
        raise ValueError('missing or unexpected release artifacts')


def binary_bytes(path, target):
    name = 'sub2api.exe' if target['goos'] == 'windows' else 'sub2api'
    if path.suffix == '.zip':
        with zipfile.ZipFile(path) as archive:
            matches = [item for item in archive.infolist() if item.filename in (name, './' + name)]
            if len(matches) != 1 or matches[0].is_dir():
                raise ValueError('archive must contain one regular binary')
            return archive.read(matches[0])
    with tarfile.open(path, 'r:gz') as archive:
        matches = [item for item in archive.getmembers() if item.name in (name, './' + name)]
        if len(matches) != 1 or not matches[0].isfile():
            raise ValueError('archive must contain one regular binary')
        return archive.extractfile(matches[0]).read()


def verify_binary(path, target, version, sha, date):
    binary = binary_bytes(path, target)
    with tempfile.TemporaryDirectory() as temporary:
        staged = Path(temporary) / 'sub2api'
        staged.write_bytes(binary)
        metadata = subprocess.check_output(['go', 'version', '-m', str(staged)], text=True)
    required = [f'GOOS={target["goos"]}', f'GOARCH={target["goarch"]}',
                'CGO_ENABLED=0', '-tags=embed', f'main.Commit={sha}',
                f'main.Date={date}', 'main.BuildType=release']
    if any(value not in metadata for value in required) or version.encode() not in binary:
        raise ValueError('binary build metadata does not match the release plan')


def contexts(args):
    verify(args)
    for target in targets():
        if target['goos'] != 'linux':
            continue
        dest = Path(args.output) / target['goarch']
        (dest / 'linux' / target['goarch']).mkdir(parents=True, exist_ok=True)
        with tarfile.open(Path(args.input) / archive_name(args.version, target), 'r:gz') as archive:
            members = [member for member in archive.getmembers() if member.name in ('sub2api', './sub2api')]
            if len(members) != 1 or not members[0].isfile():
                raise ValueError('archive must contain one regular sub2api binary')
            with archive.extractfile(members[0]) as source, (dest / 'linux' / target['goarch'] / 'sub2api').open('wb') as output:
                shutil.copyfileobj(source, output)
        (dest / 'linux' / target['goarch'] / 'sub2api').chmod(0o755)
        shutil.copy2('Dockerfile.goreleaser', dest / 'Dockerfile')
        (dest / 'deploy').mkdir(exist_ok=True)
        shutil.copy2('deploy/docker-entrypoint.sh', dest / 'deploy/docker-entrypoint.sh')
        shutil.copytree('backend/resources', dest / 'backend/resources', dirs_exist_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    p = commands.add_parser('plan')
    p.add_argument('--ref', required=True)
    p.add_argument('--dry-run', action='store_true')
    p.add_argument('--output', default='.release-plan')
    p.add_argument('--github-output', default='.release-plan/outputs')
    p.set_defaults(run=plan)
    p = commands.add_parser('config')
    p.add_argument('mode', choices=['build', 'publish'])
    p.add_argument('--goos')
    p.add_argument('--goarch')
    p.add_argument('--output', required=True)
    p.set_defaults(run=generate_config)
    p = commands.add_parser('collect')
    for arg in ('version', 'sha', 'date', 'goos', 'goarch', 'output'):
        p.add_argument('--' + arg, required=True)
    p.set_defaults(run=collect)
    for command, handler in [('verify', verify), ('contexts', contexts)]:
        p = commands.add_parser(command)
        for arg in ('version', 'sha', 'date', 'input'):
            p.add_argument('--' + arg, required=True)
        if command == 'contexts':
            p.add_argument('--output', required=True)
        p.set_defaults(run=handler)
    args = parser.parse_args()
    args.run(args)


if __name__ == '__main__':
    main()
