#!/usr/bin/env python3
"""Test the POSIX downloader with artificial local archives; Python is a test tool only."""
import hashlib
import http.server
import ssl
import threading
import os
import platform
from contextlib import contextmanager
import io
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
ROOT = Path(__file__).resolve().parents[1]
class BootstrapTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.archive = self.root/'package.tar.gz'
        self.marker = self.root/'executed'
    def tearDown(self): self.temp.cleanup()
    def package(self, extra=None):
        files = {'LICENSE': b'fixture', 'THIRD_PARTY_NOTICES.md': b'fixture', 'release.json': b'{}', 'bin/molr': b'fixture', 'bin/molr-setup': ("#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+str(self.marker)+"'\n").encode()}
        with tarfile.open(self.archive, 'w:gz') as archive:
            for name, content in files.items():
                item = tarfile.TarInfo('molr/'+name); item.size = len(content); item.mode = 0o755
                archive.addfile(item, io.BytesIO(content))
            if extra: archive.addfile(extra)
        self.checksum()
    def checksum(self): Path(str(self.archive)+'.sha256').write_text(hashlib.sha256(self.archive.read_bytes()).hexdigest()+'  package.tar.gz\n')
    def run_setup(self, *args): return subprocess.run(['sh', str(ROOT/'scripts/install.sh'), '--archive', str(self.archive), *args], capture_output=True, text=True)
    def test_modes_and_quoted_arguments_forwarded(self):
        self.package();result=self.run_setup('--name', 'molr-fixture', '--non-interactive', '--program-root', "/tmp/a ' b", '--dry-run')
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(self.marker.read_text().splitlines(),['--name','molr-fixture','--non-interactive','--program-root',"/tmp/a ' b",'--dry-run'])
        self.assertIn('[Download] Verify checksum',result.stdout)
    def test_selected_architecture_checked_before_execution(self):
        self.package()
        arch = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}[platform.machine()]
        result = self.run_setup('--arch', arch, '--dry-run')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.marker.read_text(), '--dry-run\n')
        self.marker.unlink()
        for wrong in ['arm64' if arch == 'amd64' else 'amd64', 'invalid']:
            result = self.run_setup('--arch', wrong)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('does not match this device', result.stderr)
            self.assertFalse(self.marker.exists())
    def test_missing_architecture_value_rejected(self):
        result = self.run_setup('--arch')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Missing value for --arch', result.stderr)
    def test_checksum_rejected_before_execution(self):
        self.package();Path(str(self.archive)+'.sha256').write_text('0'*64+'  package.tar.gz\n')
        result=self.run_setup();self.assertNotEqual(result.returncode,0);self.assertIn('Checksum mismatch',result.stderr);self.assertFalse(self.marker.exists())
    def test_empty_checksum_has_stage(self):
        self.package();Path(str(self.archive)+'.sha256').write_text('')
        result=self.run_setup();self.assertNotEqual(result.returncode,0);self.assertIn('Read local archive',result.stderr)
    def test_traversal_links_and_duplicates_rejected(self):
        for name,kind in [('../escape',tarfile.REGTYPE),('molr/bin/molr-setup',tarfile.SYMTYPE),('molr/bin/molr-setup',tarfile.LNKTYPE),('molr/bin/molr-setup',tarfile.REGTYPE)]:
            item=tarfile.TarInfo(name);item.type=kind;item.linkname='/tmp/escape'
            self.package(item);result=self.run_setup();self.assertNotEqual(result.returncode,0);self.assertFalse(self.marker.exists())
    def test_piped_script_can_forward_silent_options(self):
        self.package();result=subprocess.run(['sh','-s','--','--archive',str(self.archive),'--non-interactive','--dry-run'],input=(ROOT/'scripts/install.sh').read_text(),capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(self.marker.read_text(),'--non-interactive\n--dry-run\n')
    @contextmanager
    def mirror(self):
        certificate, key = self.root/'certificate.pem', self.root/'private.key'
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', str(key), '-out', str(certificate), '-days', '1', '-subj', '/CN=127.0.0.1', '-addext', 'subjectAltName=IP:127.0.0.1'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        directory = self.root
        class Handler(http.server.SimpleHTTPRequestHandler):
            def __init__(self, *args, **kwargs): super().__init__(*args, directory=str(directory), **kwargs)
            def log_message(self, *_): pass
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER); context.load_cert_chain(certificate, key)
        server.socket = context.wrap_socket(server.socket, server_side=True)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        try: yield f'https://127.0.0.1:{server.server_port}', {**os.environ, 'CURL_CA_BUNDLE': str(certificate), 'SSL_CERT_FILE': str(certificate), 'NO_PROXY': '127.0.0.1'}
        finally: server.shutdown(); server.server_close(); thread.join()
    def test_https_channel_and_fixed_version_download(self):
        self.package()
        arch = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}[platform.machine()]
        version = '0.2.0-rc.1'
        with self.mirror() as (origin, env):
            descriptor = '\n'.join(['molr-download-v1', version, 'a'*40, 'linux-'+arch, f'molr_{version}_linux_{arch}.tar.gz', origin+'/package.tar.gz', hashlib.sha256(self.archive.read_bytes()).hexdigest(), ''])
            for prefix in ['preview', 'versions/'+version]:
                target=self.root/prefix/f'download-linux-{arch}.txt'; target.parent.mkdir(parents=True, exist_ok=True); target.write_text(descriptor)
            for args in [[], ['--version', version]]:
                result=subprocess.run(['sh', str(ROOT/'scripts/install.sh'), '--source', origin, '--channel', 'preview', *args, '--non-interactive'], env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode,0,result.stderr); self.assertIn('Version: '+version,result.stdout); self.assertIn('--source\n'+origin+'\n--channel\npreview',self.marker.read_text())
            self.marker.unlink()
            (self.root/'preview'/f'download-linux-{arch}.txt').write_text(descriptor.replace('molr-download-v1','invalid-schema'))
            result=subprocess.run(['sh',str(ROOT/'scripts/install.sh'),'--source',origin,'--channel','preview'],env=env,capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0);self.assertFalse(self.marker.exists())
    def test_http_downgrade_source_rejected(self):
        result=subprocess.run(['sh',str(ROOT/'scripts/install.sh'),'--source','http://example.com/releases'],capture_output=True,text=True)
        self.assertNotEqual(result.returncode,0);self.assertIn('HTTPS',result.stderr)
if __name__=='__main__':unittest.main()
