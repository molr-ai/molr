#!/usr/bin/env python3
"""Remove one Linux installation; preserve configuration and data unless --purge."""
import argparse
import json
import os
from pathlib import Path
import pwd
import re
import shutil
import stat
import subprocess
import sys

from install_layout import (InstallError, check_path, check_managed, layout,
                            validate_receipt, PROGRAM_FILES, CONFIG_FILES)


def run(command):
    return subprocess.run(command, check=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)


def service_state(control, name):
    try:
        result = run(control + ['show', name+'.service', '--property=LoadState,ActiveState,MainPID,ControlGroup,FragmentPath'])
        output = result.stdout
    except subprocess.CalledProcessError as error:
        if 'LoadState=not-found' not in (error.stdout or '').splitlines():
            raise
        output = error.stdout
    state = dict(line.split('=', 1) for line in output.splitlines() if '=' in line)
    if not state.get('LoadState') or not state.get('ActiveState'):
        raise InstallError('Cannot determine service state; no files removed.')
    return state


def check_tree(path, uid):
    check_path(path)
    if not path.exists():
        return
    info = path.stat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != uid:
        raise InstallError(f'Directory ownership changed; refusing removal: {path}')
    # rmtree does not follow symlinks, but does cross mounts, including bind mounts.
    for line in Path('/proc/self/mountinfo').read_text().splitlines():
        mount = Path(re.sub(r'\\([0-7]{3})', lambda m: chr(int(m[1], 8)), line.split()[4]))
        if path == mount or path in mount.parents:
            raise InstallError(f'Unmount filesystem before removing directory: {mount}')


def remove_tree(path):
    if not shutil.rmtree.avoids_symlink_attacks:
        raise InstallError('This Python does not support safe directory removal.')
    # Pin every ancestor without following links; rmtree pins descendants itself.
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in path.parent.parts[1:]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            os.close(fd)
            fd = child
        try:
            shutil.rmtree(path.name, dir_fd=fd)
        except FileNotFoundError:
            pass
    finally:
        os.close(fd)


def remove_empty(path):
    if path.exists():
        try:
            path.rmdir()
        except OSError:
            print(f'Preserved non-empty directory: {path}')


def uninstall(args):
    if sys.version_info < (3, 11):
        raise InstallError('Python 3.11 or newer is required for safe directory removal.')
    if sys.platform != 'linux':
        raise InstallError('Linux is required.')
    uid = os.geteuid()
    account = pwd.getpwuid(uid)
    if not args.record and ((uid == 0) != args.system):
        raise InstallError('Use --system as root; run user uninstallation as its original user.')
    if args.record:
        marker = check_path(Path(args.record))
    else:
        paths = layout(args.name, account.pw_dir, args.system)
        marker = paths['config']/'installation.json'
    check_managed(marker, uid)
    if not marker.exists():
        if args.record:
            print(f'Installation record absent; no files removed: {marker}')
            return
        if any(paths[k].exists() for k in ('prefix', 'config', 'data', 'cache', 'unit')):
            raise InstallError(f'No installation record; refusing to remove unmanaged files: {marker}')
        print('Instance is not installed; nothing to remove.')
        return
    info = marker.stat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != uid or info.st_mode & 0o077:
        raise InstallError('Installation record must be private and owned by the installer.')
    record = json.loads(marker.read_text())
    paths = validate_receipt(record, account.pw_dir, uid)
    if marker != paths['config']/'installation.json':
        raise InstallError('Installation record location does not match its contents.')
    if args.record and ((args.name != 'molr' and args.name != record['name']) or (args.system and not record['system'])):
        raise InstallError('Options do not match the recorded instance.')
    control = ['systemctl'] + ([] if record['system'] else ['--user'])
    state = service_state(control, record['name'])
    fragment = state.get('FragmentPath')
    # systemctl enable registers custom XDG unit paths through a symlink in
    # the manager's unit search directory; compare the resolved source file.
    if fragment and Path(fragment).resolve() != paths['unit']:
        raise InstallError('Service belongs to another installation; no files removed.')
    files = [paths['prefix']/p for p in PROGRAM_FILES]
    files.append(paths['unit'])
    if args.purge:
        files += [paths['config']/p for p in CONFIG_FILES if p != 'installation.json']
    for path in files:
        check_managed(path, uid)
        if path.exists() and not path.is_file():
            raise InstallError(f'Expected a managed regular file: {path}')
    trees = [paths['cache']]
    if args.purge and record['managed_data']:
        trees.append(paths['data'])
    for path in trees:
        check_tree(path, record['uid'])
    # The configuration may have been edited since installation. Custom data and
    # external work roots are reported, never added to the removal plan.
    preserved = set()
    actual_data = Path(record['data_dir'])
    config = paths['config']/'server.json'
    if config.exists():
        check_managed(config, uid)
        settings = json.loads(config.read_text())
        actual_data = Path(settings.get('data_dir', record['data_dir']))
        work = settings.get('sandbox', {}).get('work_root')
        if work:
            preserved.add(work)
    for data in (Path(record['data_dir']), actual_data):
        if data != paths['data']:
            preserved.add(str(data))
    # A later config edit must not put persistent data underneath removable paths.
    for value in preserved:
        path = check_path(Path(value))
        for target in [paths['prefix'], *trees]:
            if path == target or target in path.parents or path in target.parents:
                raise InstallError(f'Persistent path overlaps removal target: {path}, {target}')
    print(f"Instance: {record['name']} ({'system' if record['system'] else 'user'})")
    print('Stop and remove service: '+str(paths['unit']))
    for path in files:
        if path.exists():
            print('Remove file: '+str(path))
    for path in trees:
        if path.exists():
            print('Remove directory: '+str(path))
    if not args.purge:
        preserved.update((str(paths['config']), str(paths['data'])))
    for path in sorted(preserved):
        print('Preserve: '+path)
    if args.dry_run:
        print('Dry run: no changes made.')
        return
    if args.purge and not args.yes:
        try:
            with open('/dev/tty', 'r+') as terminal:
                terminal.write(f"Permanently delete this instance's managed configuration and data? Type {record['name']}: ")
                terminal.flush()
                accepted = terminal.readline().strip() == record['name']
        except OSError:
            accepted = False
        if not accepted:
            raise InstallError('Purge cancelled; use --purge --yes for explicit non-interactive deletion.')
    if state['LoadState'] != 'not-found':
        run(control + ['stop', record['name']+'.service'])
        stopped = service_state(control, record['name'])
        if stopped['ActiveState'] not in ('inactive', 'failed') or stopped.get('MainPID') != '0':
            raise InstallError('Service has not stopped; no files removed.')
        group = state.get('ControlGroup')
        if group:
            if not group.startswith('/') or '..' in Path(group).parts:
                raise InstallError('Invalid service cgroup; no files removed.')
            events = Path('/sys/fs/cgroup'+group)/'cgroup.events'
            if events.exists() and 'populated 1' in events.read_text().splitlines():
                raise InstallError('Service still has running tasks; no files removed.')
        run(control + ['disable', record['name']+'.service'])
    # Check again after stopping; failure leaves the receipt available for retry.
    for path in trees:
        check_tree(path, record['uid'])
    for path in files:
        check_managed(path, uid)
        path.unlink(missing_ok=True)
    run(control + ['daemon-reload'])
    for path in trees:
        if path.exists():
            remove_tree(path)
    for path in (paths['prefix']/'bin', paths['prefix']/'scripts', paths['prefix']):
        remove_empty(path)
    if args.purge:
        marker.unlink()
        remove_empty(paths['config'])
    print('Uninstalled. '+('Managed data removed.' if args.purge else 'Configuration and data preserved.'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--name', default='molr', help='service instance, default molr')
    parser.add_argument('--system', action='store_true', help='remove a system installation; requires root')
    parser.add_argument('--record', help='installation record; supplied by the installed uninstall.sh')
    parser.add_argument('--dry-run', action='store_true', help='show the removal plan without changing anything')
    parser.add_argument('--purge', action='store_true', help='also remove managed configuration and data')
    parser.add_argument('--yes', action='store_true', help='explicitly confirm --purge without a prompt')
    args = parser.parse_args()
    os.environ['PATH'] = '/usr/sbin:/usr/bin:/sbin:/bin'
    try:
        uninstall(args)
    except (InstallError, OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        print(f'Uninstallation failed: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
