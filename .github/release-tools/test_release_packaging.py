"""Independent publication-boundary, immutable pricing and OCI integrity regressions."""
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import yaml

sys.path.insert(0, str(Path(__file__).resolve().parent))
import release_job as worker
import release_oci as oci
import release_pricing as pricing

ROOT = Path(__file__).resolve().parents[2]
TAG = 'v9.8.7+custom.009'
PLAN = {'version': TAG[1:], 'sha': 'a' * 40, 'mode': 'branch-rehearsal'}


class PricingTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        for name in pricing.ASSETS:
            (self.directory / name).write_bytes(b'generated pricing')

    def metadata(self, names):
        return {'draft': False, 'prerelease': False, 'assets': [{'name': name} for name in names]}

    def download(self, command):
        if command[1:3] == ['release', 'download']:
            name = command[command.index('--pattern') + 1]
            (self.directory / 'existing' / name).write_bytes(b'generated pricing')
        return ''

    def test_read_only_modes_compare_bytes_without_uploads(self):
        for mode in ('tag-rehearsal', 'branch-rehearsal'):
            with patch.object(pricing, 'release_metadata', return_value=self.metadata(pricing.ASSETS)), \
                 patch.object(pricing, 'command', side_effect=self.download) as command:
                outcomes = pricing.compare('owner/repo', TAG, mode, self.directory)
            self.assertEqual(set(outcomes.values()), {'matched'})
            self.assertFalse(any('upload' in call.args[0] for call in command.call_args_list))
            for path in (self.directory / 'existing').iterdir():
                path.unlink()

    def test_absence_is_untested_on_branch_and_failure_for_historical_tag(self):
        with patch.object(pricing, 'release_metadata', return_value=None), \
             patch.object(pricing, 'command') as command:
            outcomes = pricing.compare('owner/repo', TAG, 'branch-rehearsal', self.directory)
            self.assertEqual(set(outcomes.values()), {'absent-untested'})
            with self.assertRaisesRegex(ValueError, 'requires both'):
                pricing.compare('owner/repo', TAG, 'tag-rehearsal', self.directory)
        command.assert_not_called()

    def test_existing_changed_pricing_fails_before_upload(self):
        def changed(command):
            name = command[command.index('--pattern') + 1]
            (self.directory / 'existing' / name).write_bytes(b'old immutable bytes')
        with patch.object(pricing, 'release_metadata', return_value=self.metadata(pricing.ASSETS)), \
             patch.object(pricing, 'command', side_effect=changed) as command:
            with self.assertRaisesRegex(ValueError, 'Refusing to replace'):
                pricing.compare('owner/repo', TAG, 'publish', self.directory)
        self.assertEqual(command.call_count, 1)

    def test_publication_preflight_never_uploads_and_final_upload_never_clobbers(self):
        with patch.object(pricing, 'release_metadata', return_value=self.metadata([])), \
             patch.object(pricing, 'command') as command, \
             patch.dict(os.environ, {'RELEASE_MODE': 'publish'}):
            pricing.compare('owner/repo', TAG, 'publish', self.directory, upload=False)
            command.assert_not_called()
            pricing.compare('owner/repo', TAG, 'publish', self.directory)
        args = command.call_args.args[0]
        self.assertEqual(args[:3], ['gh', 'release', 'upload'])
        self.assertNotIn('--clobber', args)

    def test_api_error_is_not_treated_as_absent_release(self):
        with patch.object(subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, '', 'HTTP 503')), \
             patch.object(pricing, 'command') as command:
            with self.assertRaisesRegex(RuntimeError, 'reliably read'):
                pricing.release_metadata('owner/repo', TAG, allow_missing=True)
        command.assert_not_called()

    def test_inaccessible_repository_404_is_not_absence(self):
        with patch.object(subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, '', 'HTTP 404')), \
             patch.object(pricing, 'command', side_effect=RuntimeError('inaccessible')):
            with self.assertRaisesRegex(RuntimeError, 'inaccessible'):
                pricing.release_metadata('owner/repo', TAG, allow_missing=True)


class OciTests(unittest.TestCase):
    def fixture(self, root, *, arch='arm64', corrupt=False, binary=b'binary', executable=True, delete=False):
        context = root / 'context'
        files = {
            'app/sub2api': ('linux/arm64/sub2api', b'binary', 0o755),
            'app/docker-entrypoint.sh': ('deploy/docker-entrypoint.sh', b'#!/bin/sh\n', 0o755),
            'app/resources/model-pricing/model_prices_and_context_window.json': (
                'backend/resources/model-pricing/model_prices_and_context_window.json', b'{"model":{}}', 0o644),
        }
        layer = io.BytesIO()
        with tarfile.open(fileobj=layer, mode='w:gz') as archive:
            for name, (relative, data, mode) in files.items():
                source = context / relative
                source.parent.mkdir(parents=True, exist_ok=True)
                source.write_bytes(data)
                if name == 'app/sub2api':
                    data = binary
                    mode = mode if executable else 0o644
                member = tarfile.TarInfo(name)
                member.size, member.mode = len(data), mode
                archive.addfile(member, io.BytesIO(data))
            if delete:
                archive.addfile(tarfile.TarInfo('app/.wh.sub2api'))
        blobs = {}
        def add(data):
            digest = hashlib.sha256(data).hexdigest()
            blobs['blobs/sha256/' + digest] = data
            return {'digest': 'sha256:' + digest, 'size': len(data)}
        config = {'architecture': arch, 'os': 'linux', 'config': {
            'Entrypoint': ['/app/docker-entrypoint.sh'], 'Cmd': ['/app/sub2api'], 'Labels': {
                'org.opencontainers.image.version': PLAN['version'],
                'org.opencontainers.image.revision': PLAN['sha'],
                'org.opencontainers.image.source': 'https://github.com/owner/repo',
            }}}
        layer_desc = add(layer.getvalue())
        if corrupt:
            blobs['blobs/sha256/' + layer_desc['digest'].split(':')[1]] = b'corruption'
        manifest = {'config': add(json.dumps(config).encode()), 'layers': [layer_desc]}
        index = {'manifests': [add(json.dumps(manifest).encode())]}
        blobs['index.json'] = json.dumps(index).encode()
        path = root / 'image.tar'
        with tarfile.open(path, mode='w') as archive:
            for name, data in blobs.items():
                member = tarfile.TarInfo(name)
                member.size = len(data)
                archive.addfile(member, io.BytesIO(data))
        return path, context

    def test_actual_layer_bytes_platform_labels_and_executable_files(self):
        with tempfile.TemporaryDirectory() as temp:
            path, context = self.fixture(Path(temp))
            result = oci.inspect_image(path, context, 'arm64', PLAN, 'owner/repo')
            self.assertEqual(result['runtime_files'], ['app/docker-entrypoint.sh',
                             'app/resources/model-pricing/model_prices_and_context_window.json', 'app/sub2api'])

    def test_corrupt_blob_wrong_arch_binary_mode_and_whiteout_are_rejected(self):
        for options in ({'corrupt': True}, {'arch': 'amd64'}, {'binary': b'wrong binary'},
                        {'executable': False}, {'delete': True}):
            with self.subTest(options=options), tempfile.TemporaryDirectory() as temp:
                path, context = self.fixture(Path(temp), **options)
                with self.assertRaises(ValueError):
                    oci.inspect_image(path, context, 'arm64', PLAN, 'owner/repo')


class WorkflowTests(unittest.TestCase):
    def test_only_real_publisher_has_write_permissions_and_environment(self):
        jobs = yaml.safe_load((ROOT / '.github/workflows/release.yml').read_text())['jobs']
        for name, job in jobs.items():
            if name == 'release':
                self.assertEqual(job['environment']['name'], 'release')
                self.assertEqual(job['permissions'], {'contents': 'write', 'packages': 'write'})
                self.assertIn("needs.verify.result == 'success'", job['if'])
                self.assertIn("needs.build-binaries.result == 'success'", job['if'])
                self.assertIn("needs.prepare.outputs.mode == 'publish'", job['if'])
            else:
                self.assertNotIn('environment', job)
                self.assertNotIn('write', job.get('permissions', {}).values())
                self.assertFalse(any('login-action' in step.get('uses', '') for step in job['steps']))
        self.assertIn("needs.verify-rehearsal.result == 'success'", jobs['build-frontend']['if'])
        self.assertIn("needs.verify.result == 'success'", jobs['build-frontend']['if'])
        self.assertIn("needs.prepare.result == 'success'", jobs['build-frontend']['if'])
        self.assertEqual(jobs['build-binaries']['strategy']['fail-fast'], False)

    def test_failed_pricing_preflight_cannot_build_or_publish_images(self):
        plan = {**PLAN, 'mode': 'publish', 'date': '2026-10-09T01:02:03Z', 'tag': TAG}
        with patch.object(worker.matrix, 'contexts'), patch.object(worker.matrix, 'git', return_value='notes'), \
             patch.object(worker.pricing, 'generate'), \
             patch.object(worker.pricing, 'compare', side_effect=ValueError('immutable mismatch')), \
             patch.object(worker, 'run') as run, patch.dict(os.environ, {'GITHUB_REPOSITORY': 'owner/repo'}):
            with self.assertRaisesRegex(ValueError, 'immutable mismatch'):
                worker.package(plan, False)
        run.assert_not_called()

    def test_branch_gate_cannot_authorize_publisher_or_strict_tag_verifier(self):
        with patch.object(worker, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'cannot consume rehearsal'):
                worker.package(PLAN, False)
            with self.assertRaisesRegex(ValueError, 'incorrect verification gate'):
                worker.verify(PLAN, True)
        run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
