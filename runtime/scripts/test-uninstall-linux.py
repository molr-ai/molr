#!/usr/bin/env python3
"""Exercise real temporary files; mock host commands, dependency probes and identity."""
import argparse
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import pwd
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).parent))
import install_layout as paths


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(file))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


u = load('uninstaller', 'uninstall-linux.py')
i = load('installer', 'install-linux.py')


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        previous_umask = os.umask(0o077)
        self.addCleanup(os.umask, previous_umask)
        self.temp = tempfile.TemporaryDirectory(prefix='molr-uninstall-test-')
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        self.account = pwd.struct_passwd(('fixture', 'x', os.getuid(), os.getgid(), '', str(self.home), '/bin/bash'))
        self.env = {'XDG_DATA_HOME': str(self.home/'state'), 'XDG_CONFIG_HOME': str(self.home/'config'), 'XDG_CACHE_HOME': str(self.home/'cache')}
        self.layout = paths.layout('molr-review', self.home, False, self.env)
        self.marker = self.layout['config']/'installation.json'
        for key in ('prefix', 'config', 'data', 'cache'):
            self.layout[key].mkdir(parents=True)
        for name in paths.PROGRAM_FILES:
            file = self.layout['prefix']/name
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_text('program fixture')
        self.layout['unit'].parent.mkdir(parents=True)
        self.layout['unit'].write_text('managed service')
        (self.layout['config']/'server.json').write_text(json.dumps({'data_dir': str(self.layout['data'])}))
        (self.layout['data']/'sentinel').write_text('persistent data')
        (self.layout['cache']/'transient').write_text('discard')
        self.record = paths.receipt('molr-review', self.account, False, self.layout, self.layout['data'])
        self.save_record()
        self.running = True
        self.calls = []

    def save_record(self):
        self.marker.write_text(json.dumps(self.record))
        self.marker.chmod(0o600)

    def service(self, command):
        self.calls.append(command)
        if 'show' in command:
            present = self.layout['unit'].exists()
            text = f"LoadState={'loaded' if present else 'not-found'}\nActiveState={'active' if self.running and present else 'inactive'}\nMainPID={'42' if self.running and present else '0'}\nControlGroup=\nFragmentPath={self.layout['unit'] if present else ''}\n"
            return subprocess.CompletedProcess(command, 0, stdout=text)
        if 'stop' in command:
            self.running = False
        return subprocess.CompletedProcess(command, 0, stdout='')

    def uninstall(self, **kwargs):
        args = argparse.Namespace(record=str(self.marker), name='molr', system=False, dry_run=False, purge=False, yes=False)
        for key, value in kwargs.items(): setattr(args, key, value)
        with patch.object(u.pwd, 'getpwuid', return_value=self.account), patch.object(u, 'run', side_effect=self.service), contextlib.redirect_stdout(io.StringIO()):
            u.uninstall(args)

    def test_default_removes_program_and_cache_preserves_data_and_config(self):
        self.uninstall()
        self.assertFalse(self.layout['prefix'].exists())
        self.assertFalse(self.layout['unit'].exists())
        self.assertFalse(self.layout['cache'].exists())
        self.assertEqual((self.layout['data']/'sentinel').read_text(), 'persistent data')
        self.assertTrue(self.marker.exists())
        self.assertTrue((self.layout['config']/'server.json').exists())
        self.uninstall()  # receipt keeps a repeated uninstall safe
        self.uninstall(purge=True, yes=True)
        self.assertFalse(self.layout['data'].exists())
        self.assertFalse(self.layout['config'].exists())
        self.uninstall(purge=True, yes=True)

    def test_dry_run_purge_does_not_stop_or_delete(self):
        self.uninstall(dry_run=True, purge=True)
        self.assertTrue(self.running)
        self.assertTrue(self.layout['prefix'].exists())
        self.assertTrue(self.marker.exists())
        self.assertTrue(all('show' in command for command in self.calls))

    def test_custom_data_and_external_work_are_not_purged(self):
        custom = self.home/'external'; custom.mkdir(); (custom/'sentinel').write_text('keep')
        (self.layout['config']/'server.json').write_text(json.dumps({'data_dir':str(custom), 'sandbox':{'work_root':str(custom/'work')}}))
        self.uninstall(purge=True, yes=True)
        self.assertEqual((custom/'sentinel').read_text(), 'keep')

    def test_other_instance_survives(self):
        other = paths.layout('molr-other', self.home, False, self.env)
        other['data'].mkdir(); (other['data']/'sentinel').write_text('other')
        self.uninstall(purge=True, yes=True)
        self.assertEqual((other['data']/'sentinel').read_text(), 'other')
        self.assertFalse(any('molr-other.service' in c for c in self.calls))

    def test_stop_failure_preserves_everything(self):
        original = self.service
        def failing(command):
            if 'stop' in command: raise subprocess.CalledProcessError(1, command)
            return original(command)
        self.service = failing
        with self.assertRaises(subprocess.CalledProcessError): self.uninstall(purge=True, yes=True)
        self.assertTrue(self.marker.exists())
        self.assertTrue((self.layout['data']/'sentinel').exists())
        self.assertTrue(self.layout['prefix'].exists())

    def test_unrelated_service_refused(self):
        self.service = lambda c: subprocess.CompletedProcess(c, 0, stdout='LoadState=loaded\nActiveState=active\nFragmentPath=/other/service\n')
        with self.assertRaises(paths.InstallError): self.uninstall()
        self.assertTrue(self.layout['prefix'].exists())

    def test_systemd_registration_symlink_resolves_to_managed_unit(self):
        registration = self.home/'registered.service'
        registration.symlink_to(self.layout['unit'])
        original = self.service
        def service(command):
            result = original(command)
            result.stdout = result.stdout.replace('FragmentPath='+str(self.layout['unit']), 'FragmentPath='+str(registration))
            return result
        self.service = service
        self.uninstall()
        self.assertFalse(self.layout['prefix'].exists())

    def test_tampered_record_cannot_delete_another_path(self):
        self.record['paths']['prefix'] = str(self.home/'victim'); self.save_record()
        with self.assertRaises(paths.InstallError): self.uninstall(purge=True, yes=True)
        self.assertTrue(self.layout['prefix'].exists())

    def test_symlink_root_refused_and_descendant_symlink_not_followed(self):
        victim = self.home/'victim'; victim.mkdir(); (victim/'keep').write_text('keep')
        cache = self.layout['cache']; (cache/'transient').unlink(); cache.rmdir(); cache.symlink_to(victim, target_is_directory=True)
        with self.assertRaises(paths.InstallError): self.uninstall()
        cache.unlink(); cache.mkdir()
        (self.layout['data']/'link').symlink_to(victim, target_is_directory=True)
        self.uninstall(purge=True, yes=True)
        self.assertEqual((victim/'keep').read_text(), 'keep')

    def test_mount_refused(self):
        actual = Path.read_text
        def read(path, *args, **kwargs):
            if str(path) == '/proc/self/mountinfo': return f"1 0 0:1 / {self.layout['cache']}/bind rw - tmpfs tmpfs rw\n"
            return actual(path, *args, **kwargs)
        with patch.object(Path, 'read_text', read):
            with self.assertRaises(paths.InstallError): self.uninstall()
        self.assertTrue(self.running)

    def test_unknown_program_and_config_files_preserved(self):
        unknown = self.layout['prefix']/'personal'; unknown.write_text('keep')
        config = self.layout['config']/'private-note'; config.write_text('keep')
        self.uninstall(purge=True, yes=True)
        self.assertEqual(unknown.read_text(),'keep')
        self.assertEqual(config.read_text(),'keep')

    def test_data_moved_into_cache_is_refused(self):
        (self.layout['config']/'server.json').write_text(json.dumps({'data_dir':str(self.layout['cache']/'important')}))
        with self.assertRaises(paths.InstallError): self.uninstall()
        self.assertTrue(self.running)

    def install(self, missing_dependency=None, directory_permissions='ask'):
        binary = self.home/'new-server'; binary.write_bytes(b'new program'); binary.chmod(0o755)
        args = argparse.Namespace(user=None, name='molr-review', binary=str(binary),
            config=str(self.layout['config']/'server.json'), no_start=True, init_owner=None,
            skip_dependencies=True, install_deps=False, directory_permissions=directory_permissions)
        def output(command):
            if command[0] == 'file': return 'ELF statically linked'
            if command[-2:] == ['version', '--json']: return json.dumps({'version':'0.2.0','commit':'a'*40})
            if command[:2] == ['systemctl', '--version']: return 'systemd 255'
            return 'yes'
        commands = {name: '/usr/bin/'+name for name in ('systemctl', 'file', 'bwrap', 'nsenter')}
        commands.pop(missing_dependency, None)
        busybox = None if missing_dependency == 'busybox-static' else self.home/'busybox'
        # Filesystem lifecycle checks do not require real sandbox executables.
        # Native release acceptance exercises the actual host dependencies.
        with patch.object(i, 'account_request', return_value=self.account), patch.dict(os.environ, self.env), \
                patch.object(i, 'output', side_effect=output), patch.object(i, 'run'), \
                patch.object(i.shutil, 'which', side_effect=commands.get), \
                patch.object(i, 'find_static_busybox', return_value=busybox), \
                contextlib.redirect_stdout(io.StringIO()):
            i.install(args)

    def test_upgrade_and_reinstall_preserve_data_and_restore_offline_uninstaller(self):
        self.install()
        self.assertEqual((self.layout['prefix']/'bin/molr-server').read_bytes(), b'new program')
        self.assertEqual((self.layout['prefix']/'bin/molr-server.previous').read_text(), 'program fixture')
        self.assertIn(str(self.layout['cache']/'tmp'), self.layout['unit'].read_text())
        self.uninstall()
        self.install()
        self.assertEqual((self.layout['data']/'sentinel').read_text(), 'persistent data')
        self.assertIn('--record', (self.layout['prefix']/'uninstall.sh').read_text())
        self.assertTrue((self.layout['prefix']/'scripts/install_layout.py').is_file())
        self.uninstall(purge=True, yes=True)

    def test_ignored_directory_permissions_survive_install_and_uninstall(self):
        shared = self.home/'.local'
        shared.chmod(0o775)
        self.layout['prefix'].chmod(0o775)
        self.layout['config'].chmod(0o775)
        self.install(directory_permissions='ignore')
        for directory in (shared, self.layout['prefix'], self.layout['config']):
            self.assertEqual(directory.stat().st_mode & 0o777, 0o775)
        self.assertEqual((self.layout['prefix']/'bin/molr-server').stat().st_mode & 0o777, 0o755)
        self.uninstall(directory_permissions='ignore')
        self.assertEqual(shared.stat().st_mode & 0o777, 0o775)
        self.assertEqual(self.layout['config'].stat().st_mode & 0o777, 0o775)
        self.assertEqual((self.layout['data']/'sentinel').read_text(), 'persistent data')

    def test_dry_run_keeps_writable_directory_even_with_fix_option(self):
        self.layout['config'].chmod(0o775)
        self.uninstall(dry_run=True, directory_permissions='fix')
        self.assertEqual(self.layout['config'].stat().st_mode & 0o777, 0o775)
        self.assertTrue(self.running)

    def test_missing_sandbox_dependencies_preserve_existing_installation(self):
        def snapshot():
            return {p.relative_to(self.home): p.read_bytes() for p in self.home.rglob('*')
                    if p.is_file() and p != self.home/'new-server'}
        before = snapshot()
        for dependency, message in (('bwrap', 'Missing host dependency: bwrap'),
                                    ('nsenter', 'Missing host dependency: nsenter'),
                                    ('busybox-static', 'Static BusyBox required')):
            with self.subTest(dependency=dependency):
                with self.assertRaisesRegex(paths.InstallError, message):
                    self.install(missing_dependency=dependency)
                self.assertEqual(snapshot(), before)

    def test_unowned_default_data_not_adopted_by_custom_installation(self):
        custom = self.home/'custom'; custom.mkdir()
        self.record['data_dir'] = str(custom)
        self.record['managed_data'] = False
        self.save_record()
        (self.layout['config']/'server.json').write_text(json.dumps({'data_dir':str(custom)}))
        self.uninstall(purge=True, yes=True)
        self.assertTrue((self.layout['data']/'sentinel').exists())
        self.assertTrue(custom.exists())

    def test_purge_without_confirmation_leaves_service_and_files(self):
        real_open = open
        def no_terminal(path, *args, **kwargs):
            if path == '/dev/tty': raise OSError('no terminal')
            return real_open(path, *args, **kwargs)
        with patch('builtins.open', side_effect=no_terminal):
            with self.assertRaisesRegex(paths.InstallError, 'cancelled'): self.uninstall(purge=True)
        self.assertTrue(self.running)
        self.assertTrue(self.marker.exists())

    def test_layout_separates_program_and_xdg_data(self):
        self.assertEqual(self.layout['prefix'], self.home/'.local/lib/molr/review')
        self.assertEqual(self.layout['data'], self.home/'state/molr/review')
        system = paths.layout('molr', '/', True, self.env)
        self.assertEqual(system['prefix'], Path('/opt/molr/default'))
        self.assertEqual(system['data'], Path('/var/lib/molr/default'))
        self.assertEqual(system['config'], Path('/etc/molr/default'))
        self.assertEqual(system['cache'], Path('/var/cache/molr/default'))
        for name in ('molr-default', 'molr-../escape'):
            with self.assertRaises(paths.InstallError): paths.layout(name, self.home, False, self.env)
        with self.assertRaises(paths.InstallError): paths.layout('molr', self.home, False, {'XDG_DATA_HOME':str(self.home/'.local/lib')})


if __name__ == '__main__': unittest.main(verbosity=2)
