"""Release requirements: exact source, five archives, Plus names and immutable publication."""
import argparse
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import stat
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
PRICING_BODY = b'{"fixture":{"input_cost_per_token":1}}'


def write_fixture_archive(path, target, scenario='regular'):
    # Expected runtime layout is specified independently of the release helper.
    name = 'resources/model-pricing/model_prices_and_context_window.json'
    if scenario == 'wrong-path':
        # Exact layout observed in the broken published archive: dst became a file.
        name = 'resources/model-pricing'
    body = b'{"fixture":{"input_cost_per_token":9}}' if scenario == 'wrong-bytes' else PRICING_BODY
    if path.suffix == '.zip':
        with zipfile.ZipFile(path, 'w') as archive:
            archive.writestr('sub2api.exe', b'binary fixture')
            if scenario == 'missing':
                return
            info = zipfile.ZipInfo(name)
            info.create_system = 3
            mode = {'directory': stat.S_IFDIR, 'symlink': stat.S_IFLNK}.get(scenario, stat.S_IFREG)
            info.external_attr = (mode | 0o644) << 16
            # No trailing slash: a regular-file check must inspect the ZIP type.
            archive.writestr(info, b'other.json' if scenario == 'symlink' else body)
            if scenario == 'duplicate':
                archive.writestr('./' + name, body)
        return
    with tarfile.open(path, 'w:gz') as archive:
        binary = tarfile.TarInfo('sub2api')
        binary.size, binary.mode = 14, 0o755
        archive.addfile(binary, io.BytesIO(b'binary fixture'))
        if scenario == 'missing':
            return
        info = tarfile.TarInfo(name)
        info.mode = 0o644
        if scenario == 'directory':
            info.type = tarfile.DIRTYPE
        elif scenario in ('symlink', 'hardlink'):
            info.type = tarfile.SYMTYPE if scenario == 'symlink' else tarfile.LNKTYPE
            info.linkname = 'other.json'
        else:
            info.size = len(body)
        archive.addfile(info, io.BytesIO(body) if info.isfile() else None)
        if scenario == 'duplicate':
            duplicate = tarfile.TarInfo('./' + name)
            duplicate.mode, duplicate.size = 0o644, len(body)
            archive.addfile(duplicate, io.BytesIO(body))


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
        pricing_source = Path('backend/resources/model-pricing/model_prices_and_context_window.json')
        pricing_source.parent.mkdir(parents=True)
        pricing_source.write_bytes(PRICING_BODY)
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
            write_fixture_archive(archive, target)
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
        self.assertEqual(release.archive_name(VERSION, {'goos': 'linux', 'goarch': 'amd64'}),
                         'sub2api_9.8.7+custom.009_linux_amd64.tar.gz')
        self.assertEqual(release.archive_name(VERSION, {'goos': 'windows', 'goarch': 'amd64'}),
                         'sub2api_9.8.7+custom.009_windows_amd64.zip')

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
                   GITHUB_REPO_OWNER_LOWER='owner',
                   DOCKER_TAG_VERSION='v9.8.7-custom.009', TAG_MESSAGE='Configuration test')
        for mode in ('build', 'publish'):
            release.generate_config(argparse.Namespace(mode=mode, goos='linux', goarch='arm64', output='check.yaml'))
            subprocess.run(['goreleaser', 'check', '--config', 'check.yaml'], env=env, check=True,
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def test_pinned_goreleaser_packages_runtime_fallback_in_real_tar_and_zip(self):
        # Build only a dependency-free fixture. The archive file mapping is the
        # actual repository configuration, not a reconstructed expected mapping.
        Path('backend/go.mod').write_text('module example.com/release-archive-fixture\n\ngo 1.27.0\n')
        Path('backend/cmd/server/main.go').write_text(
            'package main\nimport _ "embed"\n//go:embed VERSION\nvar version string\n'
            'var Commit, Date, BuildType string\nfunc main() { println(version) }\n')
        self.git('remote', 'add', 'origin', 'https://github.com/owner/repo.git')
        release.generate_config(argparse.Namespace(mode='build', goos='linux', goarch='amd64', output='package.yaml'))
        configuration = yaml.safe_load(Path('package.yaml').read_text())
        configuration['builds'][0]['goos'] = ['linux', 'windows']
        Path('package.yaml').write_text(yaml.safe_dump(configuration, sort_keys=False))
        env = {key: value for key, value in os.environ.items() if key not in ('GH_TOKEN', 'GITHUB_TOKEN')}
        env.update(RELEASE_VERSION=VERSION, RELEASE_DATE=DATE, GITHUB_REPO_OWNER='owner',
                   GITHUB_REPO_NAME='repo', GITHUB_REPO_OWNER_LOWER='owner',
                   DOCKER_TAG_VERSION='v9.8.7-custom.009', TAG_MESSAGE='Read-only fixture',
                   GOWORK='off', GOPROXY='off')
        subprocess.run(['goreleaser', 'release', '--snapshot', '--clean', '--config', 'package.yaml',
                        '--skip=validate,before,publish,announce,docker'], env=env, check=True,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
        expected = 'resources/model-pricing/model_prices_and_context_window.json'
        tar_path = Path('dist/sub2api_9.8.7+custom.009_linux_amd64.tar.gz')
        with tarfile.open(tar_path) as archive:
            members = [member for member in archive.getmembers() if member.name == expected]
            self.assertEqual(len(members), 1)
            self.assertTrue(members[0].isfile())
            self.assertEqual(archive.extractfile(members[0]).read(), PRICING_BODY)
        zip_path = Path('dist/sub2api_9.8.7+custom.009_windows_amd64.zip')
        with zipfile.ZipFile(zip_path) as archive:
            members = [member for member in archive.infolist() if member.filename == expected]
            self.assertEqual(len(members), 1)
            self.assertEqual(stat.S_IFMT(members[0].external_attr >> 16), stat.S_IFREG)
            self.assertEqual(archive.read(members[0]), PRICING_BODY)
        release.verify_archive_pricing(tar_path, self.sha)
        release.verify_archive_pricing(zip_path, self.sha)

    def test_collect_checks_runtime_fallback_before_emitting_leaf_artifacts(self):
        Path('dist').mkdir()
        for target in ({'goos': 'linux', 'goarch': 'amd64'}, {'goos': 'windows', 'goarch': 'amd64'}):
            name = release.archive_name(VERSION, target)
            source = Path('dist') / name
            scenarios = ['missing', 'wrong-path', 'directory', 'symlink', 'duplicate', 'wrong-bytes']
            if target['goos'] == 'linux':
                scenarios.append('hardlink')
            for scenario in scenarios:
                with self.subTest(target=target, scenario=scenario):
                    write_fixture_archive(source, target, scenario)
                    Path('dist/checksums.txt').write_text(release.sha256(source) + '  ' + name + '\n')
                    output = 'leaf-' + target['goos'] + '-' + scenario
                    args = argparse.Namespace(version=VERSION, sha=self.sha, date=DATE,
                                              output=output, **target)
                    with self.assertRaisesRegex(ValueError, 'fallback pricing'):
                        release.collect(args)
                    self.assertFalse(Path(output).exists(), 'invalid archive cannot become a leaf artifact')
            write_fixture_archive(source, target)
            Path('dist/checksums.txt').write_text(release.sha256(source) + '  ' + name + '\n')
            output = 'leaf-' + target['goos'] + '-valid'
            release.collect(argparse.Namespace(version=VERSION, sha=self.sha, date=DATE, output=output, **target))
            self.assertEqual((Path(output) / name).read_bytes(), source.read_bytes())
            self.assertTrue((Path(output) / f"manifest-{target['goos']}-{target['goarch']}.json").is_file())

    def test_consumer_rechecks_real_archive_fallback_even_with_valid_provenance_and_hash(self):
        args = self.fixture()
        for target in ({'goos': 'linux', 'goarch': 'amd64'}, {'goos': 'windows', 'goarch': 'amd64'}):
            name = release.archive_name(VERSION, target)
            source = Path(args.input) / name
            manifest_path = Path(args.input) / f"manifest-{target['goos']}-{target['goarch']}.json"
            scenarios = ['missing', 'wrong-path', 'directory', 'symlink', 'duplicate', 'wrong-bytes']
            if target['goos'] == 'linux':
                scenarios.append('hardlink')
            for scenario in scenarios:
                with self.subTest(target=target, scenario=scenario):
                    write_fixture_archive(source, target, scenario)
                    manifest = json.loads(manifest_path.read_text())
                    manifest['sha256'] = release.sha256(source)
                    manifest_path.write_text(json.dumps(manifest))
                    with self.assertRaisesRegex(ValueError, 'fallback pricing'):
                        release.verify(args)
            write_fixture_archive(source, target)
            manifest = json.loads(manifest_path.read_text())
            manifest['sha256'] = release.sha256(source)
            manifest_path.write_text(json.dumps(manifest))
        release.verify(args)

    def test_archive_fallback_compares_captured_blob_instead_of_dirty_worktree(self):
        wrong = b'{"fixture":{"input_cost_per_token":9}}'
        Path('backend/resources/model-pricing/model_prices_and_context_window.json').write_bytes(wrong)
        for target in ({'goos': 'linux', 'goarch': 'amd64'}, {'goos': 'windows', 'goarch': 'amd64'}):
            with self.subTest(target=target):
                archive = Path(release.archive_name(VERSION, target))
                write_fixture_archive(archive, target, 'wrong-bytes')
                with self.assertRaisesRegex(ValueError, 'fallback pricing differs from captured source bytes'):
                    release.verify_archive_pricing(archive, self.sha)

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
        pricing_path = Path('backend/resources/model-pricing/model_prices_and_context_window.json')
        release.contexts(args)
        for arch in ('amd64', 'arm64'):
            context = Path('contexts') / arch
            binary = context / 'linux' / arch / 'sub2api'
            self.assertEqual(binary.read_bytes(), b'binary fixture')
            self.assertEqual(binary.stat().st_mode & 0o777, 0o755)
            self.assertEqual((context / 'deploy/docker-entrypoint.sh').read_bytes(), b'entrypoint fixture')
            self.assertEqual((context / pricing_path).read_bytes(), b'{"fixture":{"input_cost_per_token":1}}')

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
