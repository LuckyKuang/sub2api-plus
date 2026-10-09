#!/usr/bin/env python3
"""Inspect exported OCI blobs and the final layered runtime files without executing them."""
import hashlib
import io
import json
from pathlib import Path
import tarfile


def inspect_image(path, context, arch, plan, repository):
    wanted = {
        'app/sub2api': (context / 'linux' / arch / 'sub2api', True),
        'app/docker-entrypoint.sh': (context / 'deploy/docker-entrypoint.sh', True),
        'app/resources/model-pricing/model_prices_and_context_window.json': (
            context / 'backend/resources/model-pricing/model_prices_and_context_window.json', False),
    }
    with tarfile.open(path) as archive:
        def blob(descriptor):
            algorithm, digest = descriptor['digest'].split(':', 1)
            if algorithm != 'sha256':
                raise ValueError('unsupported OCI digest')
            data = archive.extractfile('blobs/sha256/' + digest).read()
            if len(data) != descriptor['size'] or hashlib.sha256(data).hexdigest() != digest:
                raise ValueError('OCI blob digest or size mismatch')
            return data
        index = json.load(archive.extractfile('index.json'))
        if len(index['manifests']) != 1:
            raise ValueError('expected one architecture manifest')
        descriptor = index['manifests'][0]
        manifest = json.loads(blob(descriptor))
        config = json.loads(blob(manifest['config']))
        if config['architecture'] != arch or config['os'] != 'linux':
            raise ValueError('OCI platform mismatch')
        runtime = config['config']
        expected_labels = {
            'org.opencontainers.image.version': plan['version'],
            'org.opencontainers.image.revision': plan['sha'],
            'org.opencontainers.image.source': 'https://github.com/' + repository,
        }
        if any(runtime.get('Labels', {}).get(k) != v for k, v in expected_labels.items()):
            raise ValueError('OCI source/version label mismatch')
        if runtime.get('Entrypoint') != ['/app/docker-entrypoint.sh'] or runtime.get('Cmd') != ['/app/sub2api']:
            raise ValueError('OCI runtime command mismatch')
        files = {}
        for layer in manifest['layers']:
            with tarfile.open(fileobj=io.BytesIO(blob(layer)), mode='r:*') as contents:
                for member in contents:
                    name = member.name.removeprefix('./').lstrip('/')
                    parts = Path(name).parts
                    if '..' in parts:
                        raise ValueError('invalid OCI layer path')
                    basename = Path(name).name
                    parent = str(Path(name).parent)
                    if basename == '.wh..wh..opq':
                        files = {k: v for k, v in files.items() if not k.startswith(parent + '/')}
                    elif basename.startswith('.wh.'):
                        deleted = str(Path(parent) / basename[4:])
                        files = {k: v for k, v in files.items() if k != deleted and not k.startswith(deleted + '/')}
                    elif name in wanted:
                        if not member.isfile():
                            raise ValueError('runtime artifact must be a regular file')
                        files[name] = (contents.extractfile(member).read(), member.mode)
        for name, (source, executable) in wanted.items():
            if name not in files or files[name][0] != source.read_bytes():
                raise ValueError('OCI runtime artifact mismatch: ' + name)
            if executable and files[name][1] & 0o111 != 0o111:
                raise ValueError('OCI runtime artifact is not executable: ' + name)
        with path.open('rb') as stream:
            digest = hashlib.file_digest(stream, 'sha256').hexdigest()
        return {'architecture': arch, 'manifest': descriptor['digest'],
                'archive_sha256': digest,
                'runtime_files': sorted(files)}


def inspect_images(plan, output, contexts, repository):
    return [inspect_image(output / f'sub2api-{arch}.oci.tar', contexts / arch, arch, plan, repository)
            for arch in ('amd64', 'arm64')]
