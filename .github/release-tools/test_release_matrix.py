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
import sys
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
sys.path.insert(0, str(HERE))
import release_job as worker
VERSION = '9.8.7+custom.009'
DATE = '2026-10-09T01:02:03Z'
PRICING_BODY = b'{"fixture":{"input_cost_per_token":1}}'


def write_fixture_archive(path, target, scenario='regular', binary_body=b'binary fixture'):
    # Expected runtime layout is specified independently of the release helper.
    name = 'resources/model-pricing/model_prices_and_context_window.json'
    if scenario == 'wrong-path':
        # Exact layout observed in the broken published archive: dst became a file.
        name = 'resources/model-pricing'
    body = b'{"fixture":{"input_cost_per_token":9}}' if scenario == 'wrong-bytes' else PRICING_BODY
    if path.suffix == '.zip':
        with zipfile.ZipFile(path, 'w') as archive:
            archive.writestr('sub2api.exe', binary_body)
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
        binary.size, binary.mode = len(binary_body), 0o755
        archive.addfile(binary, io.BytesIO(binary_body))
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


def stage_release_runtime():
    # Literal inventory from the workflow's business contract, independent of
    # ignore parsing and the production worker's implementation.
    directories = (
        '.release-tooling', '.release-plan', '.release-output', '.release-context',
        '.release-pricing', '.release-docker-config', 'release-input', 'release-leaf',
        'dist', 'frontend/node_modules', 'backend/internal/web/dist',
    )
    for directory in directories:
        path = Path(directory) / 'fixture'
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b'generated runtime staging')
    for name in ('.release-build.yaml', '.release-publish.yaml'):
        Path(name).write_text('# generated runtime configuration\n')


def go_build_settings(binary):
    # Inspect actual compiler output without executing a target-platform binary.
    with tempfile.TemporaryDirectory() as temporary:
        path = Path(temporary) / 'sub2api'
        path.write_bytes(binary)
        metadata = subprocess.check_output(['go', 'version', '-m', str(path)], text=True)
    return dict(line.removeprefix('\tbuild\t').split('=', 1)
                for line in metadata.splitlines() if line.startswith('\tbuild\t') and '=' in line)


class ReleaseMatrixTest(unittest.TestCase):
    def setUp(self):
        self.previous = Path.cwd()
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        os.chdir(temporary.name)
        self.addCleanup(os.chdir, self.previous)
        shutil.copyfile(ROOT / '.goreleaser.yaml', '.goreleaser.yaml')
        shutil.copyfile(ROOT / '.gitignore', '.gitignore')
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

    def prepare_go_fixture(self):
        Path('backend/go.mod').write_text('module example.com/release-archive-fixture\n\ngo 1.27.0\n')
        Path('backend/cmd/server/main.go').write_text(
            'package main\nimport _ "embed"\n//go:embed VERSION\nvar version string\n'
            'var Commit, Date, BuildType string\n'
            'func main() { println(version, Commit, Date, BuildType) }\n')
        self.git('add', 'backend/go.mod', 'backend/cmd/server/main.go')
        self.git('commit', '-qm', 'dependency-free Go build fixture')
        self.sha = self.git('rev-parse', 'HEAD')

    def build_environment(self):
        env = {key: value for key, value in os.environ.items() if key not in ('GH_TOKEN', 'GITHUB_TOKEN')}
        env.update(RELEASE_VERSION=VERSION, RELEASE_DATE=DATE, GITHUB_REPO_OWNER='owner',
                   GITHUB_REPO_NAME='repo', GITHUB_REPO_OWNER_LOWER='owner',
                   DOCKER_TAG_VERSION='v9.8.7-custom.009', TAG_MESSAGE='Read-only fixture',
                   CGO_ENABLED='0', GOWORK='off', GOPROXY='off')
        return env

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
        self.binary_check.stop()
        self.prepare_go_fixture()
        stage_release_runtime()
        self.assertEqual(self.git('status', '--porcelain=v1', '--untracked-files=all'), '')
        self.git('remote', 'add', 'origin', 'https://github.com/owner/repo.git')
        release.generate_config(argparse.Namespace(mode='build', goos='linux', goarch='amd64', output='.release-build.yaml'))
        configuration = yaml.safe_load(Path('.release-build.yaml').read_text())
        configuration['builds'][0]['goos'] = ['linux', 'windows']
        Path('.release-build.yaml').write_text(yaml.safe_dump(configuration, sort_keys=False))
        subprocess.run(['goreleaser', 'release', '--snapshot', '--clean', '--config', '.release-build.yaml',
                        '--skip=validate,before,publish,announce,docker'], env=self.build_environment(), check=True,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
        expected = 'resources/model-pricing/model_prices_and_context_window.json'
        tar_path = Path('dist/sub2api_9.8.7+custom.009_linux_amd64.tar.gz')
        with tarfile.open(tar_path) as archive:
            members = [member for member in archive.getmembers() if member.name == expected]
            self.assertEqual(len(members), 1)
            self.assertTrue(members[0].isfile())
            self.assertEqual(archive.extractfile(members[0]).read(), PRICING_BODY)
            settings = go_build_settings(archive.extractfile('sub2api').read())
            self.assertEqual(settings['vcs'], 'git')
            self.assertEqual(settings['vcs.revision'], self.sha)
            self.assertEqual(settings['vcs.modified'], 'false')
        zip_path = Path('dist/sub2api_9.8.7+custom.009_windows_amd64.zip')
        with zipfile.ZipFile(zip_path) as archive:
            members = [member for member in archive.infolist() if member.filename == expected]
            self.assertEqual(len(members), 1)
            self.assertEqual(stat.S_IFMT(members[0].external_attr >> 16), stat.S_IFREG)
            self.assertEqual(archive.read(members[0]), PRICING_BODY)
            settings = go_build_settings(archive.read('sub2api.exe'))
            self.assertEqual(settings['vcs'], 'git')
            self.assertEqual(settings['vcs.revision'], self.sha)
            self.assertEqual(settings['vcs.modified'], 'false')
        release.verify_archive_pricing(tar_path, self.sha)
        release.verify_archive_pricing(zip_path, self.sha)
        release.verify_binary(tar_path, {'goos': 'linux', 'goarch': 'amd64'}, VERSION, self.sha, DATE)
        release.verify_binary(zip_path, {'goos': 'windows', 'goarch': 'amd64'}, VERSION, self.sha, DATE)
        self.assertEqual(self.git('status', '--porcelain=v1', '--untracked-files=all'), '')

    def test_binary_worker_accepts_only_exact_runtime_staging(self):
        stage_release_runtime()
        plan = {'sha': self.sha, 'version': VERSION, 'date': DATE}
        with patch.object(worker, 'run') as run, patch.object(worker.matrix, 'collect') as collect:
            worker.build_binary(plan, argparse.Namespace(goos='linux', goarch='amd64'))
        run.assert_called_once()
        collect.assert_called_once()
        self.assertEqual(self.git('status', '--porcelain=v1', '--untracked-files=all'), '')

    def test_binary_worker_rejects_source_changes_before_compiling(self):
        plan = {'sha': self.sha, 'version': VERSION, 'date': DATE}
        for scenario in ('modified', 'staged', 'deleted', 'untracked', 'unknown-staging'):
            with self.subTest(scenario=scenario):
                tracked = Path('backend/cmd/server/VERSION')
                if scenario in ('modified', 'staged'):
                    tracked.write_text('changed version\n')
                    if scenario == 'staged':
                        self.git('add', str(tracked))
                elif scenario == 'deleted':
                    tracked.unlink()
                elif scenario == 'untracked':
                    Path('backend/new-source.go').write_text('package backend\n')
                else:
                    Path('.release-unclassified.yaml').write_text('unexpected: staging\n')
                with patch.object(worker, 'run') as run, patch.object(worker.matrix, 'collect') as collect:
                    with self.assertRaisesRegex(ValueError, 'clean captured source'):
                        worker.build_binary(plan, argparse.Namespace(goos='linux', goarch='amd64'))
                run.assert_not_called()
                collect.assert_not_called()
                self.git('reset', '--hard', self.sha)
                Path('backend/new-source.go').unlink(missing_ok=True)
                Path('.release-unclassified.yaml').unlink(missing_ok=True)
        with patch.object(worker, 'run') as run, patch.object(worker.matrix, 'collect') as collect:
            with self.assertRaisesRegex(ValueError, 'prepared application SHA'):
                worker.build_binary({**plan, 'sha': 'b' * 40}, argparse.Namespace(goos='linux', goarch='amd64'))
        run.assert_not_called()
        collect.assert_not_called()

    def test_binary_worker_rejects_changes_during_compilation_before_collection(self):
        plan = {'sha': self.sha, 'version': VERSION, 'date': DATE}
        for scenario in ('tracked', 'untracked'):
            with self.subTest(scenario=scenario):
                def change_during_build(*command):
                    path = 'backend/cmd/server/VERSION' if scenario == 'tracked' else 'backend/new-source.go'
                    Path(path).write_text('unexpected build side effect\n')
                with patch.object(worker, 'run', side_effect=change_during_build) as run, \
                     patch.object(worker.matrix, 'collect') as collect:
                    with self.assertRaisesRegex(ValueError, 'clean captured source'):
                        worker.build_binary(plan, argparse.Namespace(goos='linux', goarch='amd64'))
                run.assert_called_once()
                collect.assert_not_called()
                self.git('reset', '--hard', self.sha)
                Path('backend/new-source.go').unlink(missing_ok=True)

    def test_real_dirty_and_disabled_vcs_builds_are_rejected_by_both_archive_gates(self):
        self.binary_check.stop()
        self.prepare_go_fixture()
        stage_release_runtime()
        target = {'goos': 'linux', 'goarch': 'amd64'}
        name = 'sub2api_9.8.7+custom.009_linux_amd64.tar.gz'
        binary = Path('.release-output/sub2api').resolve()
        source = Path('backend/cmd/server/main.go')
        original = source.read_text()
        env = dict(self.build_environment(), GOOS='linux', GOARCH='amd64')
        flags = f'-s -w -X main.Commit={self.sha} -X main.Date={DATE} -X main.BuildType=release'
        for scenario in ('dirty', 'disabled'):
            with self.subTest(scenario=scenario):
                source.write_text(original + '// tracked source changed\n' if scenario == 'dirty' else original)
                command = ['go', '-C', 'backend', 'build', '-tags=embed', '-ldflags', flags,
                           '-o', str(binary)]
                if scenario == 'disabled':
                    command.append('-buildvcs=false')
                command.append('./cmd/server')
                subprocess.run(command, env=env, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               timeout=120)
                settings = go_build_settings(binary.read_bytes())
                if scenario == 'dirty':
                    self.assertEqual(settings['vcs.revision'], self.sha)
                    self.assertEqual(settings['vcs.modified'], 'true')
                else:
                    self.assertNotIn('vcs', settings)
                    self.assertNotIn('vcs.revision', settings)
                    self.assertNotIn('vcs.modified', settings)
                archive = Path('dist') / name
                write_fixture_archive(archive, target, binary_body=binary.read_bytes())
                Path('dist/checksums.txt').write_text(release.sha256(archive) + '  ' + name + '\n')
                leaf = 'release-leaf/' + scenario
                with self.subTest(gate='producer'):
                    with self.assertRaisesRegex(ValueError, 'binary VCS build metadata'):
                        release.collect(argparse.Namespace(version=VERSION, sha=self.sha, date=DATE,
                                                          output=leaf, **target))
                    self.assertFalse(Path(leaf).exists())
                destination = Path('release-input') / name
                shutil.copyfile(archive, destination)
                manifest = {'sha': self.sha, 'version': VERSION, 'date': DATE, 'target': target,
                            'archive': name, 'sha256': release.sha256(destination)}
                Path('release-input/manifest-linux-amd64.json').write_text(json.dumps(manifest))
                with self.subTest(gate='consumer'):
                    with self.assertRaisesRegex(ValueError, 'binary VCS build metadata'):
                        release.verify(argparse.Namespace(input='release-input', version=VERSION,
                                                          sha=self.sha, date=DATE))

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
        metadata += f'\tbuild\tvcs=git\n\tbuild\tvcs.revision={self.sha}\n\tbuild\tvcs.modified=false\n'
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

    def test_binary_vcs_metadata_is_exact_complete_unique_and_clean(self):
        self.binary_check.stop()
        target = {'goos': 'linux', 'goarch': 'arm64'}
        metadata = '\tbuild\tGOOS=linux\n\tbuild\tGOARCH=arm64\n\tbuild\tCGO_ENABLED=0\n'
        metadata += f'\tbuild\t-tags=embed\n\tbuild\t-ldflags="-X main.Commit={self.sha} -X main.Date={DATE} -X main.BuildType=release"\n'
        vcs = f'\tbuild\tvcs=git\n\tbuild\tvcs.revision={self.sha}\n\tbuild\tvcs.modified=false\n'
        cases = {
            'missing-all': '',
            'missing-vcs': vcs.replace('\tbuild\tvcs=git\n', ''),
            'missing-revision': vcs.replace(f'\tbuild\tvcs.revision={self.sha}\n', ''),
            'missing-modified': vcs.replace('\tbuild\tvcs.modified=false\n', ''),
            'wrong-vcs': vcs.replace('vcs=git', 'vcs=hg'),
            'wrong-revision': vcs.replace(self.sha, 'b' * 40),
            'dirty': vcs.replace('vcs.modified=false', 'vcs.modified=true'),
            'false-prefix': vcs.replace('vcs.modified=false', 'vcs.modified=false-unverified'),
            'revision-prefix': vcs.replace(self.sha, self.sha + '0'),
            'duplicate-vcs': vcs + '\tbuild\tvcs=git\n',
            'duplicate-revision': vcs + f'\tbuild\tvcs.revision={self.sha}\n',
            'duplicate-modified': vcs + '\tbuild\tvcs.modified=false\n',
            'contradictory-modified': vcs + '\tbuild\tvcs.modified=true\n',
            'bare-modified': vcs.replace('vcs.modified=false', 'vcs.modified'),
        }
        with patch.object(release, 'binary_bytes', return_value=VERSION.encode()):
            with patch.object(subprocess, 'check_output', return_value=metadata + vcs):
                release.verify_binary(Path('unused'), target, VERSION, self.sha, DATE)
            for name, settings in cases.items():
                with self.subTest(scenario=name), \
                     patch.object(subprocess, 'check_output', return_value=metadata + settings):
                    with self.assertRaisesRegex(ValueError, 'binary VCS build metadata'):
                        release.verify_binary(Path('unused'), target, VERSION, self.sha, DATE)

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
