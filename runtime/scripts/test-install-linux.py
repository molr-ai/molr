#!/usr/bin/env python3
"""Unit tests run without root; container tests exercise real account/file operations."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import pwd
import subprocess
import tempfile
import unittest
import sys
sys.path.insert(0, str(Path(__file__).parent))
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('installer', Path(__file__).with_name('install-linux.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class InstallerTests(unittest.TestCase):
    def test_system_busybox_must_be_static_executable_and_not_writable_by_others(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory)/'busybox'
            binary.write_bytes(b'fixture')
            binary.chmod(0o755)
            with patch.object(m.shutil, 'which', return_value=str(binary)):
                with patch.object(m, 'output', return_value='ELF dynamically linked'):
                    self.assertIsNone(m.find_static_busybox())
                describe = lambda command: 'ELF statically linked' if command[-1] == str(binary) else 'ELF dynamically linked'
                with patch.object(m, 'output', side_effect=describe):
                    self.assertEqual(m.find_static_busybox(), binary)
                    binary.chmod(0o775)
                    self.assertIsNone(m.find_static_busybox())
                    binary.chmod(0o644)
                    self.assertIsNone(m.find_static_busybox())

    def test_dynamic_busybox_triggers_system_package_install(self):
        args = argparse.Namespace(skip_dependencies=False, install_deps=True, yes=True)
        with patch.object(m, 'find_static_busybox', return_value=None), \
                patch.object(m.shutil, 'which', side_effect=lambda name: '/usr/bin/'+name), \
                patch.object(m, 'run') as run:
            m.prepare_dependencies(args)
        self.assertEqual(run.call_count, 2)
        self.assertIn('busybox-static', run.call_args.args[0])
        args.skip_dependencies = True
        with patch.object(m, 'find_static_busybox') as find, patch.object(m, 'run') as run:
            m.prepare_dependencies(args)
            find.assert_not_called()
            run.assert_not_called()

    def test_release_license_materials_are_complete_and_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/'bin').mkdir()
            (root/'scripts').mkdir()
            binary = root/'bin/molr-server'
            # Keep the fallback source checkout out of this fixture.
            with patch.object(m, '__file__', str(root/'other/runtime/scripts/install-linux.py')):
                (root/'LICENSE').write_text('MIT fixture\n')
                with self.assertRaises(m.InstallError):
                    m.license_materials(binary)
                (root/'THIRD_PARTY_NOTICES.md').write_text('All original notices\n')
                notices = m.license_materials(binary)
                installed = root/'installed'
                installed.mkdir()
                for name, data in notices.items():
                    m.atomic_write(installed/name, data, 0o644, os.getuid(), os.getgid())
                    self.assertEqual((installed/name).read_bytes(), (root/name).read_bytes())
                    self.assertEqual((installed/name).stat().st_mode & 0o777, 0o644)

    def test_quick_start_defaults_preserve_selected_port_and_private_hosts(self):
        with patch.object(m, 'output', return_value='[{"addr_info":[{"local":"192.168.1.10"}]}]'):
            http = m.default_http(18877)
        self.assertEqual(http['public_url'], 'http://192.168.1.10:18877')
        self.assertEqual(http['listen'], '0.0.0.0:18877')
        self.assertEqual(http['tls_mode'], 'lan')
        self.assertIn('http://127.0.0.1:18877', http['allowed_origins'])
        for host in ('8.8.8.8', '0.0.0.0', '169.254.169.254'):
            with self.assertRaises(m.InstallError):
                with patch.object(m, 'output', return_value='[]'):
                    m.default_http(18877, host)

    def test_update_source_preserves_preferences_and_offline_configuration(self):
        config = {'updates': {'source': 'https://old.example/releases', 'auto_check': False}}
        m.configure_updates(config, None)
        self.assertEqual(config['updates']['source'], 'https://old.example/releases')
        m.configure_updates(config, 'https://new.example/releases/')
        self.assertEqual(config['updates'], {'source': 'https://new.example/releases', 'auto_check': False})
        fresh = {}
        m.configure_updates(fresh, 'http://192.168.1.10/releases')
        self.assertTrue(fresh['updates']['auto_check'])
        for source in ('http://example.com/releases', 'https://user:pass@example.com', 'https://example.com/?token=secret', 'file:///tmp/releases', 'http://169.254.169.254/'):
            with self.assertRaises(m.InstallError):
                m.configure_updates({}, source)

    def test_upgrade_verification_distinguishes_pending_and_failure(self):
        expected = {'version': '0.2.0', 'commit': 'a'*40}
        status = {'Runtime': {'build': expected}, 'Sandbox': {'Enabled': False}}
        good = subprocess.CompletedProcess([], 0, json.dumps(status), '')
        self.assertTrue(m.verify_health(good, expected))
        with self.assertRaises(m.InstallError):
            m.verify_health(good, {'version': '0.1.0', 'commit': 'b'*40})
        for reason in ('HTTP 401', 'MFA required', 'HTTP 429'):
            self.assertFalse(m.verify_health(subprocess.CompletedProcess([], 1, '', reason), expected))
        with self.assertRaises(m.InstallError):
            m.verify_health(subprocess.CompletedProcess([], 1, '', 'sandbox unavailable'), expected)

    def test_identity_authorization(self):
        ordinary = pwd.struct_passwd(('existing', 'x', 1234, 2345, '', '/home/existing', '/bin/bash'))
        with patch.object(m.pwd, 'getpwuid', return_value=ordinary):
            self.assertEqual(m.account_request(None, 1234), ordinary)
        for name, uid in [(None, 0), ('existing', 1234), ('bad/name', 0), ('', 0)]:
            with self.assertRaises(m.InstallError):
                m.account_request(name, uid)
        with patch.object(m.pwd, 'getpwnam', return_value=pwd.struct_passwd(('alias', 'x', 0, 0, '', '/', '/bin/bash'))):
            with self.assertRaises(m.InstallError):
                m.account_request('alias', 0)
        with patch.object(m.pwd, 'getpwnam', return_value=ordinary):
            self.assertEqual(m.account_request('existing', 0), ordinary)
        with patch.object(m.pwd, 'getpwnam', side_effect=KeyError):
            self.assertIsNone(m.account_request('missing', 0))

    def test_symlink_and_hardlink_destinations(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            victim = base/'victim'
            victim.write_text('preserve')
            destination = base/'destination'
            destination.symlink_to(victim)
            with self.assertRaises(m.InstallError):
                m.atomic_write(destination, b'changed', 0o600, os.getuid(), os.getgid())
            destination.unlink()
            os.link(victim, destination)
            m.atomic_write(destination, b'new', 0o600, os.getuid(), os.getgid())
            self.assertEqual(victim.read_text(), 'preserve')
            self.assertEqual(destination.read_text(), 'new')
            destination.unlink()
            destination.symlink_to(base/'missing')
            with self.assertRaises(m.InstallError):
                m.check_path(destination/'child')

    def test_config_preserves_policy_and_handles_legacy(self):
        for label in ('current', 'molr-sandbox'):
            result = m.normalize_config({'sandbox': {'host_tools_user': label, 'enabled': False}}, '/data')
            self.assertEqual(result, {'data_dir':'/data', 'sandbox':{'host_tools':True, 'enabled':False}})
        self.assertFalse(m.normalize_config({'sandbox':{'rootfs':'/custom'}}, '/data')['sandbox']['host_tools'])
        self.assertFalse(m.normalize_config({'sandbox':{'host_tools':False}}, '/data')['sandbox']['host_tools'])
        for sandbox in ({'host_tools':'yes'}, {'host_tools':True,'rootfs':'/custom'}, {'host_tools_user':'other'}, {'host_tools':False,'host_tools_user':'current'}):
            with self.assertRaises(m.InstallError):
                m.normalize_config({'sandbox':sandbox}, '/data')

    def test_full_access_option_is_independent_of_isolation(self):
        config = m.normalize_config({'sandbox': {'allow_full_access': True}}, '/data')
        self.assertTrue(config['sandbox']['allow_full_access'])
        self.assertTrue(config['sandbox']['host_tools'])
        self.assertNotIn('mode', config['sandbox'])
        with self.assertRaises(m.InstallError):
            m.normalize_config({'sandbox': {'allow_full_access': 'yes'}}, '/data')
        self.assertNotIn('Delegate=', m.render_unit('/bin/server', '/config', delegate=False))
        self.assertIn('Delegate=yes', m.render_unit('/bin/server', '/config'))

    def test_unit_identity_and_argument_escaping(self):
        account = pwd.struct_passwd(('existing', 'x', 1234, 2345, '', '/home/existing', '/bin/bash'))
        user_unit = m.render_unit('/opt/molr/bin/server', '/etc/molr/server.json')
        self.assertNotIn('User=', user_unit)
        system_unit = m.render_unit('/opt/molr/bin/server', '/etc/a % $ " \\/server.json', account)
        for expected in ('User=existing\n', 'Group=2345\n', 'WantedBy=multi-user.target', 'Delegate=yes', 'DelegateSubgroup=supervisor', 'NoNewPrivileges=yes', '%%', '$$', '\\"'):
            self.assertIn(expected, system_unit)


@unittest.skipUnless(os.environ.get('MOLR_INSTALL_CONTAINER_TEST') == '1', 'only in disposable installer test container')
class ContainerTests(unittest.TestCase):
    def test_real_root_install_and_update(self):
        self.assertEqual(os.getuid(), 0)
        config = Path('/tmp/template.json')
        config.write_text(json.dumps({'http':{'listen':'127.0.0.1:18550','public_url':'http://127.0.0.1:18550','tls_mode':'local'}}))
        args = argparse.Namespace(user='molr-review', name='molr-review', binary='/src/.cache/acceptance/molr-server', config=str(config), no_start=True, init_owner=None)
        with self.assertRaises(KeyError): pwd.getpwnam(args.user)
        m.install(args)
        account = pwd.getpwnam(args.user)
        self.assertEqual(account.pw_shell, '/usr/sbin/nologin')
        self.assertEqual(account.pw_dir, '/nonexistent')
        self.assertEqual(os.getgrouplist(account.pw_name, account.pw_gid), [account.pw_gid])
        data = Path('/var/lib/molr/review')
        self.assertEqual(data.stat().st_uid, account.pw_uid)
        installed = Path('/opt/molr/review/bin/molr-server')
        cfg = Path('/etc/molr/review/server.json')
        self.assertEqual(installed.stat().st_uid, 0)
        self.assertEqual(installed.stat().st_mode & 0o777, 0o755)
        self.assertEqual(cfg.stat().st_mode & 0o777, 0o640)
        subprocess.run(['runuser','-u',account.pw_name,'--',str(installed),'init','-config',str(cfg),'-username','owner','-password-stdin'], input='installer-test-password-678!\n',text=True,check=True,stdout=subprocess.DEVNULL)
        self.assertEqual((data/'runtime.db').stat().st_uid, account.pw_uid)
        cfg_data = json.loads(cfg.read_text());cfg_data['sandbox']['network_connections'] = 7;cfg.write_text(json.dumps(cfg_data))
        sentinel = data/'preserve';sentinel.write_text('preserve');os.chown(sentinel, account.pw_uid, account.pw_gid)
        before = (Path('/etc/passwd').read_bytes(),Path('/etc/group').read_bytes())
        args.no_start = False
        m.install(args)
        self.assertEqual(before, (Path('/etc/passwd').read_bytes(),Path('/etc/group').read_bytes()))
        self.assertEqual(json.loads(cfg.read_text())['sandbox']['network_connections'],7)
        self.assertEqual(sentinel.read_text(),'preserve')
        self.assertIn('User=molr-review\n',Path('/etc/systemd/system/molr-review.service').read_text())
        # Initialization runs as the target user; a failed first password is retryable.
        command = ['bash','/src/runtime/scripts/install-linux.sh','--user','molr-initial-review','--name','molr-initial-review','--init-owner','owner','--no-start',args.binary,str(config)]
        failed = subprocess.run(command,input='short\n',text=True,capture_output=True)
        self.assertNotEqual(failed.returncode,0)
        good = subprocess.run(command,input='installer-initial-password-678!\n',text=True,capture_output=True)
        self.assertEqual(good.returncode,0,good.stderr)
        self.assertEqual(Path('/var/lib/molr/initial-review/runtime.db').stat().st_uid,pwd.getpwnam('molr-initial-review').pw_uid)
        repeated = subprocess.run(command,input='another-password-678!\n',text=True,capture_output=True)
        self.assertNotEqual(repeated.returncode,0)
        # Another existing ordinary login user also works; no shell/group changes.
        subprocess.run(['useradd','-m','-s','/bin/bash','existing-review'],check=True)
        subprocess.run(['usermod','-aG','users','existing-review'],check=True)
        before = (Path('/etc/passwd').read_bytes(),Path('/etc/group').read_bytes())
        args.user='existing-review';args.name='molr-existing-review';args.no_start=True
        m.install(args)
        self.assertEqual(before,(Path('/etc/passwd').read_bytes(),Path('/etc/group').read_bytes()))
        # Refuse accidental identity migration before writing account or installation files.
        args.name='molr-review'
        with self.assertRaises(m.InstallError):m.install(args)
        self.assertEqual(before,(Path('/etc/passwd').read_bytes(),Path('/etc/group').read_bytes()))


if __name__ == '__main__':
    unittest.main(verbosity=2)
