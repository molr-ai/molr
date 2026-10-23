#!/usr/bin/env python3
"""Test the POSIX downloader with artificial local archives; Python is a test tool only."""
import hashlib
import json
import http.server
import ssl
import threading
import time
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
        if platform.system() == 'Darwin':
            code = '#include <stdio.h>\nint main(int n,char **a){FILE *f=fopen('+json.dumps(str(self.marker))+',"w");if(!f)return 1;for(int i=1;i<n;i++)fprintf(f,"%s\\n",a[i]);return fclose(f);}'
            source = self.root/'fixture.c'; source.write_text(code)
            binary = self.root/'fixture'
            subprocess.run(['/usr/bin/cc', str(source), '-o', str(binary)], check=True)
            subprocess.run(['/usr/bin/codesign', '--force', '--sign', '-', str(binary)], check=True, capture_output=True)
            files['bin/molr'] = files['bin/molr-setup'] = binary.read_bytes()
            with tarfile.open(self.archive, 'w:gz') as archive:
                for name, content in files.items():
                    item=tarfile.TarInfo('molr/'+name); item.size=len(content); item.mode=0o755
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
        owner = self
        self.requests = []
        class Handler(http.server.SimpleHTTPRequestHandler):
            def __init__(self, *args, **kwargs): super().__init__(*args, directory=str(directory), **kwargs)
            def do_GET(self):
                owner.requests.append(self.path)
                if self.path == '/slow':
                    self.send_response(200); self.send_header('Content-Length', '999999'); self.end_headers()
                    try:
                        for _ in range(25):
                            self.wfile.write(b'x'); self.wfile.flush(); time.sleep(1)
                    except (BrokenPipeError, ConnectionResetError, ssl.SSLError): pass
                else: super().do_GET()
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
        system = 'darwin' if platform.system() == 'Darwin' else 'linux'
        with self.mirror() as (origin, env):
            descriptor = '\n'.join(['molr-download-v1', version, 'a'*40, system+'-'+arch, f'molr_{version}_{system}_{arch}.tar.gz', origin+'/package.tar.gz', hashlib.sha256(self.archive.read_bytes()).hexdigest(), ''])
            for prefix in ['preview', 'versions/'+version]:
                target=self.root/prefix/f'download-{system}-{arch}.txt'; target.parent.mkdir(parents=True, exist_ok=True); target.write_text(descriptor)
            for args in [[], ['--version', version]]:
                result=subprocess.run(['sh', str(ROOT/'scripts/install.sh'), '--source', origin, '--channel', 'preview', *args, '--non-interactive'], env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode,0,result.stderr); self.assertIn('Version: '+version,result.stdout); self.assertIn('--source\n'+origin+'\n--channel\npreview',self.marker.read_text())
            self.marker.unlink()
            (self.root/'preview'/f'download-{system}-{arch}.txt').write_text(descriptor.replace('molr-download-v1','invalid-schema'))
            result=subprocess.run(['sh',str(ROOT/'scripts/install.sh'),'--source',origin,'--channel','preview'],env=env,capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0);self.assertFalse(self.marker.exists())
    def test_official_mirror_fallback_and_checksum(self):
        self.package()
        arch = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}[platform.machine()]
        system = 'darwin' if platform.system() == 'Darwin' else 'linux'
        with self.mirror() as (origin, env):
            script = (ROOT/'scripts/install.sh').read_text()
            for old, new in [('https://github.com/molr-ai/molr', origin+'/github'), ('https://cdn.molr.ai', origin+'/cdn'), ('https://molr.ai', origin+'/website')]:
                script = script.replace(old, new)
            path = self.root/'install.sh'; path.write_text(script)
            for version, custom, corrupt, metadata_fallback in [('0.2.0', False, False, False), ('0.2.0', False, False, True), ('0.2.0', False, True, False), ('0.2.0-rc.1', False, False, False), ('0.2.0', True, False, False)]:
                self.marker.unlink(missing_ok=True); self.requests.clear()
                filename = f'molr_{version}_{system}_{arch}.tar.gz'
                url = origin+('/custom' if custom else '/github')+f'/releases/download/v{version}/{filename}'
                descriptor = '\n'.join(['molr-download-v1', version, 'a'*40, system+'-'+arch, filename, url, hashlib.sha256(self.archive.read_bytes()).hexdigest(), ''])
                target = self.root/f'website/releases/stable/download-{system}-{arch}.txt'
                target.parent.mkdir(parents=True, exist_ok=True); target.write_text(descriptor)
                mirrored = self.root/f'cdn/releases/stable/download-{system}-{arch}.txt'
                mirrored.parent.mkdir(parents=True, exist_ok=True); mirrored.write_text(descriptor)
                if metadata_fallback: target.unlink()
                package = self.root/f'cdn/releases/download/v{version}/{filename}'
                package.parent.mkdir(parents=True, exist_ok=True); package.write_bytes(b'corrupt' if corrupt else self.archive.read_bytes())
                result = subprocess.run(['sh', str(path), '--non-interactive'], env=env, capture_output=True, text=True, timeout=30)
                success = not (custom or corrupt or '-' in version)
                self.assertEqual(result.returncode == 0, success, result.stderr)
                self.assertEqual(self.marker.exists(), success)
                if corrupt: self.assertIn('Checksum mismatch', result.stderr)
                if custom or '-' in version: self.assertFalse(any('/cdn/' in request for request in self.requests))
                else: self.assertIn('Switching to official mirror', result.stdout)

    def test_low_speed_download_aborts_with_curl_and_wget(self):
        with self.mirror() as (origin, env):
            wgetrc = self.root/'wgetrc'; wgetrc.write_text('ca_certificate = '+str(self.root/'certificate.pem')+'\n')
            env = {**env, 'WGETRC': str(wgetrc)}
            (self.root/'healthy').write_bytes(b'healthy transfer')
            for wget in [False, True]:
                script = (ROOT/'scripts/install.sh').read_text()
                # Exercise the actual transfer/watchdog functions with only a short test driver.
                script = script[:script.index('if [ -n "$archive" ]; then')]
                if wget: script = script.replace('if command -v curl >/dev/null 2>&1; then', 'if false; then')
                healthy = script + '\nfetch "'+origin+'/healthy" "$temporary/healthy"\n'
                result = subprocess.run(['sh'], input=healthy, env=env, capture_output=True, text=True, timeout=5)
                self.assertEqual(result.returncode, 0, result.stderr)
                script += '\nfetch "'+origin+'/slow" "$temporary/slow"\n'
                start = time.monotonic()
                result = subprocess.run(['sh'], input=script, env=env, capture_output=True, text=True, timeout=25)
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertLess(time.monotonic()-start, 23)

    def test_http_downgrade_source_rejected(self):
        result=subprocess.run(['sh',str(ROOT/'scripts/install.sh'),'--source','http://example.com/releases'],capture_output=True,text=True)
        self.assertNotEqual(result.returncode,0);self.assertIn('HTTPS',result.stderr)
if __name__=='__main__':unittest.main()
