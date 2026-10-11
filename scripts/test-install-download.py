#!/usr/bin/env python3
"""Download bootstrap rejects damaged packages before executing an installer."""
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
import http.server
import threading
import platform
from contextlib import contextmanager
ROOT=Path(__file__).resolve().parents[1]
class BootstrapTests(unittest.TestCase):
    def test_checksum_rejected_before_extract(self):
        with tempfile.TemporaryDirectory() as d:
            archive=Path(d)/'bad.tar.gz'
            archive.write_bytes(b'not an archive')
            Path(str(archive)+'.sha256').write_text('0'*64+'  bad.tar.gz\n')
            result=subprocess.run(['bash',str(ROOT/'scripts/install.sh'),'--archive',str(archive)],capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0)
            self.assertIn('Checksum mismatch',result.stderr)
    def test_verified_archive_cannot_escape_destination(self):
        with tempfile.TemporaryDirectory() as d:
            archive=Path(d)/'bad.tar.gz'
            with tarfile.open(archive,'w:gz') as t:
                content=b'escaped';item=tarfile.TarInfo('../escaped');item.size=len(content);t.addfile(item,io.BytesIO(content))
            Path(str(archive)+'.sha256').write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  bad.tar.gz\n')
            result=subprocess.run(['bash',str(ROOT/'scripts/install.sh'),'--archive',str(archive)],capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0)
            self.assertIn('Unsafe archive member',result.stderr)

@contextmanager
def release_server(directory):
    class Handler(http.server.SimpleHTTPRequestHandler):
        def __init__(self, *args, **kwargs):
            super().__init__(*args, directory=str(directory), **kwargs)
        def log_message(self, *_):
            pass
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f'http://127.0.0.1:{server.server_port}'
    finally:
        server.shutdown(); server.server_close(); thread.join()

class ChannelDownloadTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.arch = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}[platform.machine()]
        self.version = '0.2.0-rc.1'
        self.commit = 'a' * 40
        self.archive = self.root / f'molr_{self.version}_linux_{self.arch}.tar.gz'
        with tarfile.open(self.archive, 'w:gz') as package:
            members = {
                'molr/release.json': json.dumps({'version': self.version, 'commit': self.commit, 'os': 'linux', 'arch': self.arch}).encode(),
                'molr/scripts/install-linux.sh': b'#!/bin/bash\nprintf "%s\\n" "$@" > "$(dirname "$0")/../../../executed"\n',
                'molr/bin/molr-server': b'artificial binary',
            }
            # The mock installer records its arguments outside the temporary extraction directory.
            members['molr/scripts/install-linux.sh'] = ('#!/bin/bash\nprintf "%s\\n" "$@" > '+str(self.root/'executed')+'\n').encode()
            for name, content in members.items():
                item = tarfile.TarInfo(name); item.size = len(content); package.addfile(item, io.BytesIO(content))

    def tearDown(self):
        self.temp.cleanup()

    def manifest(self, origin):
        return {'schema': 1, 'version': self.version, 'channel': 'preview', 'commit': self.commit, 'platforms': [
            {'os': 'linux', 'arch': self.arch, 'aliases': [platform.machine()], 'file': self.archive.name,
             'sha256': hashlib.sha256(self.archive.read_bytes()).hexdigest(), 'url': origin+'/'+self.archive.name}]}

    def write_manifest(self, path, manifest):
        target = self.root/path; target.parent.mkdir(parents=True, exist_ok=True); target.write_text(json.dumps(manifest))

    def run_installer(self, *args):
        return subprocess.run(['bash', str(ROOT/'scripts/install.sh'), *args], capture_output=True, text=True)

    def test_channel_source_and_fixed_version_download(self):
        with release_server(self.root) as origin:
            manifest = self.manifest(origin)
            self.write_manifest('preview/manifest.json', manifest)
            self.write_manifest('versions/'+self.version+'.json', manifest)
            for args in [('--source', origin, '--channel', 'preview'), ('--source', origin+'/preview'), ('--source', origin, '--channel', 'preview', '--version', self.version)]:
                result = self.run_installer(*args, '--name', 'molr-fixture')
                self.assertEqual(result.returncode, 0, result.stderr)
                forwarded = (self.root/'executed').read_text()
                self.assertIn(origin+'/preview', forwarded)
                self.assertIn('molr-fixture', forwarded)

    def test_invalid_release_never_executes_installer(self):
        with release_server(self.root) as origin:
            for mutate in [lambda m: m.update(channel='nightly'), lambda m: m.update(commit='b'*40), lambda m: m.update(platforms=[]), lambda m: m['platforms'][0].update(sha256='0'*64), lambda m: m['platforms'][0].update(file='../bad')]:
                manifest = self.manifest(origin); mutate(manifest)
                self.write_manifest('preview/manifest.json', manifest)
                result = self.run_installer('--source', origin, '--channel', 'preview')
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.root/'executed').exists())

    def test_stable_never_falls_back_to_preview(self):
        with release_server(self.root) as origin:
            self.write_manifest('preview/manifest.json', self.manifest(origin))
            self.assertNotEqual(self.run_installer('--source', origin).returncode, 0)
            self.assertFalse((self.root/'executed').exists())

    def test_fixed_version_mismatch_is_rejected(self):
        with release_server(self.root) as origin:
            self.write_manifest('versions/0.1.0.json', self.manifest(origin))
            self.assertNotEqual(self.run_installer('--source', origin, '--version', '0.1.0').returncode, 0)
            self.assertFalse((self.root/'executed').exists())

if __name__=='__main__': unittest.main()
