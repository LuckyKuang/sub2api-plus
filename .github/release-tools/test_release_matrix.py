"""Release requirements: exact source, five archives, Plus names and immutable publication."""
import argparse
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile
import yaml

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('release_matrix', HERE / 'release_matrix.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
VERSION = '9.8.7+custom.009'
DATE = '2026-10-09T01:02:03Z'


class ReleaseMatrixTest(unittest.TestCase):
    def setUp(self):
        self.previous = Path.cwd()
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        os.chdir(temporary.name)
        self.addCleanup(os.chdir, self.previous)
        shutil.copyfile(ROOT / '.goreleaser.yaml', '.goreleaser.yaml')
        Path('backend/cmd/server').mkdir(parents=True)
        release.VERSION_FILE.write_text(VERSION + '\n')
        self.git('init', '-q')
        self.git('config', 'user.name', 'Release Test')
        self.git('config', 'user.email', 'release@example.invalid')
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')
        self.sha = self.git('rev-parse', 'HEAD')
        self.binary_check = patch.object(release, 'verify_binary')
        self.binary_check.start()
        self.addCleanup(self.binary_check.stop)

    def git(self, *args):
        return subprocess.check_output(['git', *args], text=True).strip()

    def fixture(self):
        directory = Path('release-input')
        directory.mkdir()
        for target in release.targets():
            archive = directory / release.archive_name(VERSION, target)
            if target['goos'] == 'windows':
                with zipfile.ZipFile(archive, 'w') as stream:
                    stream.writestr('sub2api.exe', b'binary fixture')
            else:
                with tarfile.open(archive, 'w:gz') as stream:
                    info = tarfile.TarInfo('sub2api')
                    info.size, info.mode = 14, 0o755
                    stream.addfile(info, io.BytesIO(b'binary fixture'))
            manifest = dict(sha=self.sha, version=VERSION, date=DATE, target=target,
                            archive=archive.name, sha256=release.sha256(archive))
            (directory / f"manifest-{target['goos']}-{target['goarch']}.json").write_text(json.dumps(manifest))
        return argparse.Namespace(input='release-input', version=VERSION, sha=self.sha, date=DATE, output='contexts')

    def plan(self, ref, dry_run=True, env=None):
        with patch.dict(os.environ, {'GITHUB_REPOSITORY_OWNER': 'ExampleOwner', **(env or {})}):
            release.plan(argparse.Namespace(ref=ref, dry_run=dry_run, output='.release-plan',
                                            github_output='.release-plan/outputs'))
        return dict(line.split('=', 1) for line in Path('.release-plan/outputs').read_text().splitlines())

    def test_five_targets_and_supported_version_syntax(self):
        self.assertEqual(set((t['goos'], t['goarch']) for t in release.targets()),
                         {('linux', 'amd64'), ('linux', 'arm64'), ('darwin', 'amd64'),
                          ('darwin', 'arm64'), ('windows', 'amd64')})
        for version in ('1.2.3', VERSION, '1.2.3-rc.1+build.7'):
            self.assertTrue(release.valid_version(version), version)
        for version in ('01.2.3', '1.2.3+custom.', '1.2.3-01', '1.2', '1.2.3+bad space'):
            self.assertFalse(release.valid_version(version), version)
        self.assertIsNone(release.PLUS_TAG_RE.fullmatch('v1.2.3+custom.000'))

    def test_leaf_and_publisher_disable_all_image_owners(self):
        for mode in ('build', 'publish'):
            release.generate_config(argparse.Namespace(mode=mode, goos='darwin', goarch='arm64', output='config.yaml'))
            config = yaml.safe_load(Path('config.yaml').read_text())
            for key in ('dockers', 'docker_manifests', 'dockers_v2'):
                self.assertEqual(config[key], [])
            self.assertEqual(config['release']['header'], release.config()['release']['header'])
            if mode == 'build':
                self.assertEqual(config['builds'][0]['goos'], ['darwin'])
                self.assertEqual(config['builds'][0]['goarch'], ['arm64'])
                self.assertEqual(config['archives'], release.config()['archives'])
                self.assertIn('{{ .Env.RELEASE_DATE }}', '\n'.join(config['builds'][0]['ldflags']))
            else:
                self.assertEqual(config['before']['hooks'], [])
                self.assertTrue(config['builds'][0]['skip'])
                self.assertEqual(config['archives'], [])
                self.assertEqual(config['checksum']['extra_files'], config['release']['extra_files'])

    def test_pinned_goreleaser_accepts_both_generated_configs(self):
        env = dict(os.environ, GITHUB_REPO_OWNER='owner', GITHUB_REPO_NAME='repo',
                   GITHUB_REPO_OWNER_LOWER='owner', DOCKERHUB_USERNAME='skip',
                   DOCKER_TAG_VERSION='v9.8.7-custom.009', TAG_MESSAGE='Configuration test')
        for mode in ('build', 'publish'):
            release.generate_config(argparse.Namespace(mode=mode, goos='linux', goarch='arm64', output='check.yaml'))
            subprocess.run(['goreleaser', 'check', '--config', 'check.yaml'], env=env, check=True,
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def test_branch_plan_records_captured_sha_version_and_real_newlines(self):
        plan = self.plan('feature/build')
        self.assertEqual(plan['sha'], self.sha)
        self.assertEqual(plan['version'], VERSION)
        self.assertEqual(plan['mode'], 'branch-rehearsal')
        self.assertEqual(plan['owner_lower'], 'exampleowner')
        self.assertEqual(len(json.loads(plan['matrix'])['include']), 5)
        self.assertEqual(json.loads(Path('.release-plan/metadata.json').read_text())['sha'], self.sha)
        self.assertEqual(Path('.release-plan/VERSION').read_text(), VERSION + '\n')

    def test_publication_cannot_be_dispatched_from_branch(self):
        tag = 'v' + VERSION
        self.git('tag', '-a', tag, '-m', 'Sub2API Plus ' + tag)
        with self.assertRaisesRegex(ValueError, 'eligible tag ref'):
            self.plan(tag, False, {'GITHUB_REF': 'refs/heads/feature/build'})
        result = self.plan(tag, False, {'GITHUB_REF': 'refs/tags/' + tag})
        self.assertEqual(result['mode'], 'publish')

    def test_tag_rehearsal_rejects_missing_tag_and_pristine_version_mismatch(self):
        with self.assertRaisesRegex(ValueError, 'absent rehearsal tag'):
            self.plan('refs/tags/v9.8.7+custom.008')
        self.git('tag', 'v9.8.7+custom.008')
        with self.assertRaisesRegex(ValueError, 'pristine VERSION'):
            self.plan('v9.8.7+custom.008')
        with self.assertRaisesRegex(ValueError, 'tag pushes'):
            self.plan('feature/build', True, {'GITHUB_EVENT_NAME': 'push'})

    def test_complete_set_is_bound_to_head_and_hash(self):
        args = self.fixture()
        release.verify(args)
        args.sha = 'a' * 40
        with self.assertRaisesRegex(ValueError, 'checkout'):
            release.verify(args)
        args.sha = self.sha
        archive = next(Path(args.input).glob('*.tar.gz'))
        archive.write_bytes(b'corrupt')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            release.verify(args)

    def test_missing_extra_symlink_and_wrong_manifest_are_rejected(self):
        args = self.fixture()
        extra = Path(args.input) / 'unexpected'
        extra.write_text('invalid')
        with self.assertRaisesRegex(ValueError, 'unexpected'):
            release.verify(args)
        extra.unlink()
        archive = next(Path(args.input).glob('*.tar.gz'))
        original = archive.read_bytes()
        archive.unlink()
        with self.assertRaisesRegex(ValueError, 'regular files'):
            release.verify(args)
        archive.symlink_to('/dev/null')
        with self.assertRaisesRegex(ValueError, 'regular files'):
            release.verify(args)
        archive.unlink()
        archive.write_bytes(original)
        manifest = next(Path(args.input).glob('manifest-*.json'))
        data = json.loads(manifest.read_text())
        data['sha'] = 'b' * 40
        manifest.write_text(json.dumps(data))
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            release.verify(args)

    def test_linux_context_uses_targetplatform_and_same_source_resources(self):
        args = self.fixture()
        shutil.copyfile(ROOT / 'Dockerfile.goreleaser', 'Dockerfile.goreleaser')
        Path('deploy').mkdir()
        Path('deploy/docker-entrypoint.sh').write_bytes(b'entrypoint fixture')
        Path('backend/resources').mkdir()
        Path('backend/resources/data').write_bytes(b'resource fixture')
        release.contexts(args)
        for arch in ('amd64', 'arm64'):
            context = Path('contexts') / arch
            binary = context / 'linux' / arch / 'sub2api'
            self.assertEqual(binary.read_bytes(), b'binary fixture')
            self.assertEqual(binary.stat().st_mode & 0o777, 0o755)
            self.assertEqual((context / 'deploy/docker-entrypoint.sh').read_bytes(), b'entrypoint fixture')
            self.assertEqual((context / 'backend/resources/data').read_bytes(), b'resource fixture')

    def test_binary_metadata_rejects_wrong_target_sha_date_and_version(self):
        self.binary_check.stop()
        # Independent wire fixture: metadata is the format emitted by Go, not generated by the checker.
        metadata = '\tbuild\tGOOS=linux\n\tbuild\tGOARCH=arm64\n\tbuild\tCGO_ENABLED=0\n'
        metadata += f'\tbuild\t-tags=embed\n\tbuild\t-ldflags="-X main.Commit={self.sha} -X main.Date={DATE} -X main.BuildType=release"\n'
        with patch.object(release, 'binary_bytes', return_value=VERSION.encode()), \
             patch.object(subprocess, 'check_output', return_value=metadata):
            release.verify_binary(Path('unused'), {'goos': 'linux', 'goarch': 'arm64'}, VERSION, self.sha, DATE)
            for target, version, sha, date in [
                ({'goos': 'linux', 'goarch': 'amd64'}, VERSION, self.sha, DATE),
                ({'goos': 'linux', 'goarch': 'arm64'}, VERSION, 'b' * 40, DATE),
                ({'goos': 'linux', 'goarch': 'arm64'}, VERSION, self.sha, 'different-date'),
                ({'goos': 'linux', 'goarch': 'arm64'}, '1.0.0+custom.001', self.sha, DATE),
            ]:
                with self.assertRaisesRegex(ValueError, 'build metadata'):
                    release.verify_binary(Path('unused'), target, version, sha, date)

    def test_image_commands_preserve_plus_tags_and_never_write_in_rehearsal(self):
        Path('bin').mkdir()
        fake = Path('bin/docker')
        fake.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$DOCKER_LOG"\n')
        fake.chmod(0o755)
        for dry in ('true', 'false'):
            log = Path('docker-' + dry).resolve()
            env = dict(os.environ, PATH=str(Path('bin').resolve()) + os.pathsep + os.environ['PATH'],
                       DOCKER_LOG=str(log), RUNNER_TEMP=str(Path.cwd()), RELEASE_VERSION=VERSION,
                       RELEASE_SHA=self.sha, GITHUB_REPOSITORY='ExampleOwner/sub2api-plus',
                       DRY_RUN=dry, RELEASE_MODE='publish')
            subprocess.run(['bash', str(HERE / 'release-images.sh')], env=env, check=True)
            commands = log.read_text()
            self.assertEqual(commands.count('buildx build'), 2)
            self.assertIn('ghcr.io/exampleowner/sub2api-plus:v9.8.7-custom.009-amd64', commands)
            self.assertIn('org.opencontainers.image.version=9.8.7+custom.009', commands)
            self.assertNotIn('docker.io', commands)
            if dry == 'true':
                self.assertNotIn('--push', commands)
                self.assertNotIn('imagetools', commands)
                self.assertIn('type=oci,compression=gzip', commands)
            else:
                self.assertEqual(commands.count('imagetools create'), 1)
                self.assertIn('sub2api-plus:9.8 ', commands)
                self.assertIn('sub2api-plus:9 ', commands)
                self.assertIn('sub2api-plus:latest ', commands)

if __name__ == '__main__':
    unittest.main()
