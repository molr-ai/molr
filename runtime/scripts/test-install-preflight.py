#!/usr/bin/env python3
"""System preparation uses artificial policies and never changes host AppArmor."""
import argparse
from contextlib import ExitStack, redirect_stdout
import importlib.util
import io
import os
from pathlib import Path
import pwd
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import install_layout as layout
spec = importlib.util.spec_from_file_location('installer', Path(__file__).with_name('install-linux.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class AppArmorTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory()))
        self.directory = self.root/'profiles'; self.directory.mkdir()
        self.profile = self.directory/'bwrap-userns-restrict'
        self.extra = self.root/'extra'; self.extra.write_text('distribution policy')
        self.flag = self.root/'enabled'; self.flag.write_text('1\n')
        for name, value in [('APPARMOR_USERNS', self.flag), ('APPARMOR_DIRECTORY', self.directory),
                            ('APPARMOR_PROFILE', self.profile), ('APPARMOR_EXTRA_PROFILE', self.extra)]:
            self.stack.enter_context(patch.object(m, name, value))
        self.stack.enter_context(patch.object(m.shutil, 'which', side_effect=lambda name: '/usr/bin/'+name))
        self.stack.enter_context(redirect_stdout(io.StringIO()))
        self.args = argparse.Namespace(skip_dependencies=False, install_deps=True, yes=True)
        self.account = pwd.getpwuid(os.getuid())
        self.probe = self.stack.enter_context(patch.object(m, 'probe_bubblewrap', side_effect=[(False, 'bwrap: Operation not permitted'), (True, '')]))
        self.commands = []
        self.stack.enter_context(patch.object(m, 'run', side_effect=self.fake_run))

    def fake_run(self, command):
        self.commands.append(command)
        if command[0] == 'sudo': command = command[1:]
        if command[0] == 'install': shutil.copyfile(command[-2], command[-1])
        if command[:3] == ['apt-get', 'install', '-y']: self.extra.write_text('distribution policy')

    def test_missing_profile_is_installed_loaded_and_probed_twice(self):
        self.extra.unlink()
        m.prepare_apparmor(self.args, self.account)
        self.assertEqual(self.profile.read_text(), 'distribution policy')
        self.assertEqual(self.probe.call_count, 2)
        self.assertTrue(any('apparmor-profiles' in c for c in self.commands))
        self.assertTrue(any('apparmor_parser' in c for c in self.commands))

    def test_existing_profile_is_loaded_without_overwrite_or_package_install(self):
        self.profile.write_text('administrator policy')
        m.prepare_apparmor(self.args, self.account)
        self.assertEqual(self.profile.read_text(), 'administrator policy')
        self.assertEqual(len(self.commands), 1)
        self.assertIn('apparmor_parser', self.commands[0])

    def test_working_sandbox_leaves_policies_untouched(self):
        self.probe.side_effect = [(True, '')]
        m.prepare_apparmor(self.args, self.account)
        self.assertFalse(self.profile.exists())
        self.assertEqual(self.commands, [])

    def test_other_vendor_profile_stops_before_any_system_change(self):
        other = self.directory/'vendor'; other.write_text('profile vendor /usr/bin/bwrap {}')
        with self.assertRaisesRegex(m.InstallError, 'Existing Bubblewrap policy'):
            m.prepare_apparmor(self.args, self.account)
        self.assertEqual(self.commands, [])
        self.assertFalse(self.profile.exists())

    def test_declined_or_disabled_preparation_does_not_modify_system(self):
        with patch.object(m, 'approve_system_change', return_value=False):
            with self.assertRaisesRegex(m.InstallError, 'declined'):
                m.prepare_apparmor(self.args, self.account)
        self.args.install_deps = False
        self.probe.side_effect = [(False, 'bwrap denied')]
        with self.assertRaisesRegex(m.InstallError, '--install-deps'):
            m.prepare_apparmor(self.args, self.account)
        self.assertEqual(self.commands, [])

    def test_restriction_disabled_and_skip_do_not_even_probe(self):
        self.flag.write_text('0\n')
        m.prepare_apparmor(self.args, self.account)
        self.flag.write_text('1\n'); self.args.skip_dependencies = True
        m.prepare_apparmor(self.args, self.account)
        self.probe.assert_not_called()
        self.assertEqual(self.commands, [])

    def test_failure_after_loading_exposes_bwrap_error(self):
        self.probe.side_effect = [(False, 'initial'), (False, 'bwrap: namespace denied')]
        with self.assertRaisesRegex(m.InstallError, 'bwrap: namespace denied'):
            m.prepare_apparmor(self.args, self.account)
        self.assertFalse(any('systemctl' in c for c in self.commands))

    def test_symlink_policy_is_not_overwritten(self):
        victim = self.root/'victim'; victim.write_text('keep')
        self.profile.symlink_to(victim)
        with self.assertRaisesRegex(m.InstallError, 'symlink'):
            m.prepare_apparmor(self.args, self.account)
        self.assertEqual(victim.read_text(), 'keep')
        self.assertEqual(self.commands, [])


class PermissionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)/'shared'; self.directory.mkdir(mode=0o775); self.directory.chmod(0o775)
        self.child = self.directory/'child'; self.child.write_text('keep'); self.child.chmod(0o664)
        self.out = io.StringIO()
        self.redirect = redirect_stdout(self.out); self.redirect.__enter__()
        self.addCleanup(self.redirect.__exit__, None, None, None)

    def check(self, policy):
        policy.check(self.directory, self.directory.stat())

    def test_interactive_fix_changes_only_directory_and_preserves_other_bits(self):
        self.directory.chmod(0o2775)
        policy = layout.DirectoryPermissions()
        with patch('builtins.open', return_value=io.StringIO('f\n')):
            self.check(policy)
        self.assertEqual(self.directory.stat().st_mode & 0o7777, 0o2755)
        self.assertEqual(self.child.stat().st_mode & 0o777, 0o664)

    def test_interactive_ignore_is_remembered_for_same_directory(self):
        policy = layout.DirectoryPermissions()
        with patch('builtins.open', return_value=io.StringIO('i\n')) as terminal:
            self.check(policy); self.check(policy)
        self.assertEqual(terminal.call_count, 1)
        self.assertEqual(self.directory.stat().st_mode & 0o777, 0o775)

    def test_noninteractive_choices_and_dry_run(self):
        for mode in ('ignore', 'fix'):
            self.directory.chmod(0o775)
            with patch('builtins.open', side_effect=AssertionError('no terminal')):
                self.check(layout.DirectoryPermissions(mode))
            self.assertEqual(self.directory.stat().st_mode & 0o777, 0o775 if mode == 'ignore' else 0o755)
        self.directory.chmod(0o775)
        self.check(layout.DirectoryPermissions('fix', dry_run=True))
        self.assertEqual(self.directory.stat().st_mode & 0o777, 0o775)

    def test_missing_terminal_requires_explicit_choice(self):
        with patch('builtins.open', side_effect=OSError('no tty')):
            with self.assertRaisesRegex(layout.InstallError, '--directory-permissions'):
                self.check(layout.DirectoryPermissions())
        self.assertEqual(self.directory.stat().st_mode & 0o777, 0o775)

    def test_quit_does_not_change_directory(self):
        with patch('builtins.open', return_value=io.StringIO('q\n')):
            with self.assertRaisesRegex(layout.InstallError, 'Stopped'):
                self.check(layout.DirectoryPermissions())
        self.assertEqual(self.directory.stat().st_mode & 0o777, 0o775)

    def test_ignoring_directories_does_not_allow_writable_files_or_symlinks(self):
        policy = layout.DirectoryPermissions('ignore')
        with self.assertRaisesRegex(layout.InstallError, 'must not be group/other writable'):
            layout.check_managed(self.child, os.getuid(), policy)
        link = Path(self.temp.name)/'link'; link.symlink_to(self.directory)
        with self.assertRaisesRegex(layout.InstallError, 'symlink'):
            layout.check_managed(link/'child', os.getuid(), policy)
        info = self.directory.stat()
        fake = os.stat_result((info.st_mode, info.st_ino, info.st_dev, info.st_nlink, os.getuid()+1, info.st_gid, info.st_size, info.st_atime, info.st_mtime, info.st_ctime))
        with patch.object(Path, 'stat', return_value=fake), patch.object(Path, 'is_symlink', return_value=False):
            with self.assertRaisesRegex(layout.InstallError, 'owner UID'):
                layout.check_managed(self.directory, os.getuid(), policy)

    def test_replaced_directory_cannot_be_chmod_after_prompt(self):
        policy = layout.DirectoryPermissions('fix')
        original = self.directory.stat()
        replacement = Path(self.temp.name)/'replacement'; replacement.mkdir()
        self.directory.rename(Path(self.temp.name)/'old'); replacement.rename(self.directory)
        with self.assertRaisesRegex(layout.InstallError, 'changed during'):
            policy.check(self.directory, original)

    def test_path_failure_precedes_dependency_installation(self):
        args = argparse.Namespace(user=None, name='molr', config=None)
        with patch.object(m, 'check_managed', side_effect=layout.InstallError('owner mismatch')), \
             patch.object(m, 'prepare_dependencies') as dependencies, patch.object(m, 'run') as run:
            with self.assertRaisesRegex(layout.InstallError, 'owner mismatch'):
                m.install(args)
            dependencies.assert_not_called(); run.assert_not_called()


class ProbeTests(unittest.TestCase):
    def test_probe_uses_target_account_and_no_new_privileges(self):
        account = pwd.struct_passwd(('fixture', 'x', 1234, 1234, '', '/home/fixture', '/bin/sh'))
        for uid in (0, 1234):
            with patch.object(m.os, 'geteuid', return_value=uid), \
                 patch.object(m.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, '', 'bwrap denied')) as run:
                self.assertEqual(m.probe_bubblewrap(account), (False, 'bwrap denied'))
                command = run.call_args.args[0]
                self.assertIn('--no-new-privs', command)
                self.assertEqual('runuser' in command, uid == 0)
                self.assertEqual(run.call_args.kwargs['timeout'], 15)


if __name__ == '__main__':
    unittest.main(verbosity=2)
