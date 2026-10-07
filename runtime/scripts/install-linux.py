#!/usr/bin/env python3
"""Install one Runtime. Its systemd identity also owns every local sandbox."""
import argparse
import json
import os
from pathlib import Path
import pwd
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
import socket
import ipaddress
import time


class InstallError(Exception):
    pass


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def output(args):
    return subprocess.check_output(args, text=True).strip()


def check_path(path):
    """Never write through symlinks, including dangling ones and ancestors."""
    path = Path(path)
    if not path.is_absolute() or path == Path('/') or '..' in path.parts or any(c in str(path) for c in '\n\r\x00'):
        raise InstallError(f'Dedicated absolute path required: {path}')
    for part in [*reversed(path.parents), path]:
        if part.is_symlink():
            raise InstallError(f'Refusing symlinked path: {part}')
    return path


def check_managed(path, uid):
    check_path(path)
    for part in [path, *path.parents]:
        if part.exists():
            info = part.stat()
            if info.st_uid == 0 and stat.S_ISDIR(info.st_mode) and info.st_mode & stat.S_ISVTX:
                continue  # e.g. /tmp; later components must still be installer-owned
            if info.st_uid not in (0, uid) or info.st_mode & 0o022:
                raise InstallError(f'Installation path must be owned by the installer and not group/other writable: {part}')


def atomic_write(path, data, mode, uid, gid):
    check_path(path)
    fd, temporary = tempfile.mkstemp(prefix='.molr-install-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(data)
            stream.flush()
            os.fchmod(stream.fileno(), mode)
            if os.geteuid() == 0:
                os.fchown(stream.fileno(), uid, gid)
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def unit_quote(value):
    return '"' + str(value).replace('\\', '\\\\').replace('"', '\\"').replace('%', '%%').replace('$', '$$') + '"'


def render_unit(binary, config, account=None, delegate=True):
    identity = '' if account is None else f'User={account.pw_name}\nGroup={account.pw_gid}\n'
    delegation = 'Delegate=yes\nDelegateSubgroup=supervisor\n' if delegate else ''
    target = 'default.target' if account is None else 'multi-user.target'
    return f'''[Unit]
Description=Molr Runtime

[Service]
Type=exec
{identity}ExecStart={unit_quote(binary)} serve -config {unit_quote(config)}
{delegation}KillMode=control-group
UMask=0077
NoNewPrivileges=yes
Restart=on-failure
TimeoutStopSec=30

[Install]
WantedBy={target}
'''


def account_request(name, uid):
    if name is None:
        if uid == 0:
            raise InstallError('Root installation requires --user USER; Runtime must not run as root.')
        return pwd.getpwuid(uid)
    if uid != 0:
        raise InstallError('--user requires root; omit it to install as the current account.')
    if not re.fullmatch(r'[a-z_][a-z0-9_.-]{0,31}', name):
        raise InstallError('Invalid account name.')
    try:
        account = pwd.getpwnam(name)
    except KeyError:
        return None
    if account.pw_uid == 0:
        raise InstallError('Runtime must not run as root (including UID 0 aliases).')
    return account


def normalize_config(config, default_data):
    if not isinstance(config, dict):
        raise InstallError('Configuration must be a JSON object.')
    config.setdefault('data_dir', str(default_data))
    sandbox = config.setdefault('sandbox', {})
    if not isinstance(sandbox, dict):
        raise InstallError('sandbox must be a JSON object.')
    if not isinstance(sandbox.get('allow_full_access', False), bool):
        raise InstallError('allow_full_access must be boolean.')
    # Old names were deployment labels, never authority to switch identities.
    legacy = sandbox.pop('host_tools_user', '')
    if legacy not in ('', 'current', 'molr-sandbox'):
        raise InstallError('Invalid legacy host_tools_user; use host_tools instead.')
    if legacy:
        if sandbox.get('host_tools') is False:
            raise InstallError('Conflicting host_tools and legacy host_tools_user settings.')
        sandbox['host_tools'] = True
    sandbox.setdefault('host_tools', not bool(sandbox.get('rootfs')))
    if not isinstance(sandbox['host_tools'], bool):
        raise InstallError('host_tools must be boolean.')
    if sandbox['host_tools'] and sandbox.get('rootfs'):
        raise InstallError('host_tools and custom rootfs are incompatible.')
    return config


def default_http(port, public_host=None):
    addresses = ['127.0.0.1']
    try:
        records = json.loads(output(['ip', '-j', '-4', 'address', 'show']))
        for record in records:
            for item in record.get('addr_info', []):
                address = item.get('local', '')
                if ipaddress.ip_address(address).is_private and not ipaddress.ip_address(address).is_link_local:
                    addresses.append(address)
    except (OSError, ValueError, subprocess.CalledProcessError):
        pass
    addresses = list(dict.fromkeys(addresses))
    if public_host:
        address = ipaddress.ip_address(public_host)
        if not (address.is_private or address.is_loopback) or address.is_unspecified or address.is_link_local:
            raise InstallError('--public-host requires a private or loopback IP')
        addresses.append(str(address))
    if port is None:
        for candidate in range(8787, 8888):
            with socket.socket() as probe:
                try:
                    probe.bind(('0.0.0.0', candidate))
                except OSError:
                    continue
                port = candidate
                break
        else:
            raise InstallError('No free default port; specify --port')
    if not 1 <= port <= 65535:
        raise InstallError('Port must be between 1 and 65535')
    host = public_host or next((a for a in addresses if a != '127.0.0.1'), '127.0.0.1')
    return {'listen': f'0.0.0.0:{port}', 'public_url': f'http://{host}:{port}', 'tls_mode': 'lan',
            'allowed_origins': [f'http://{a}:{port}' for a in addresses]}


def find_static_busybox():
    """Use the host package, never a BusyBox binary from a Molr archive."""
    for value in dict.fromkeys(filter(None, ('/usr/bin/busybox', '/bin/busybox', shutil.which('busybox')))):
        try:
            path = Path(value).resolve(strict=True)
            if (path.is_file() and os.access(path, os.X_OK) and not path.stat().st_mode & 0o022
                    and 'statically linked' in output(['file', str(path)])):
                return path
        except (OSError, subprocess.CalledProcessError):
            continue
    return None


def license_materials(binary):
    """Fixed filenames only; support release archives and source-tree installs."""
    sources = (binary.parent.parent, Path(__file__).resolve().parents[2])
    for root in sources:
        if all((root / name).is_file() for name in ('LICENSE', 'THIRD_PARTY_NOTICES.md')):
            return {name: (root / name).read_bytes() for name in ('LICENSE', 'THIRD_PARTY_NOTICES.md')}
    raise InstallError('Missing LICENSE or THIRD_PARTY_NOTICES.md; use the complete Molr archive or source tree.')


def prepare_dependencies(args):
    if args.skip_dependencies:
        return
    missing = [name for name in ('bwrap', 'nsenter', 'file', 'ip') if not shutil.which(name)]
    if find_static_busybox() is None:
        missing.append('busybox-static')
    if not Path('/etc/ssl/certs/ca-certificates.crt').is_file():
        missing.append('ca-certificates')
    if not missing:
        return
    print('Missing dependencies: ' + ', '.join(missing))
    if not args.install_deps or not shutil.which('apt-get'):
        print('Install bubblewrap busybox-static ca-certificates file util-linux iproute2 to enable isolated commands. Continuing with available capabilities.')
        return
    command = ([] if os.geteuid() == 0 else ['sudo']) + ['apt-get']
    print('Installing: bubblewrap busybox-static ca-certificates file util-linux iproute2')
    if not args.yes:
        with open('/dev/tty') as terminal:
            print('Continue with system dependency installation? [y/N] ', end='', flush=True)
            if terminal.readline().strip().lower() not in ('y', 'yes'):
                print('Continuing without installing dependencies; isolated commands may be unavailable.')
                return
    run(command + ['update'])
    run(command + ['install', '-y', 'bubblewrap', 'busybox-static', 'ca-certificates', 'file', 'util-linux', 'iproute2'])


def install(args):
    uid = os.geteuid()
    account = account_request(args.user, uid)
    system = args.user is not None
    if not re.fullmatch(r'molr(?:-[a-z0-9][a-z0-9-]{0,47})?', args.name):
        raise InstallError('Service name must be molr or molr-<lowercase name>.')
    if sys.platform != 'linux':
        raise InstallError('Linux is required.')
    if args.config is None or getattr(args, 'install_deps', False):
        prepare_dependencies(args)
    for command in ('systemctl', 'file') + (('useradd', 'runuser') if system else ()):
        if not shutil.which(command):
            raise InstallError(f'Missing host dependency: {command}. Install dependencies before running this script.')
    if not Path('/etc/ssl/certs/ca-certificates.crt').is_file():
        raise InstallError('Missing CA bundle; install ca-certificates.')
    binary = Path(args.binary).resolve(strict=True)
    source_config = Path(args.config).resolve(strict=True) if args.config else None
    notices = license_materials(binary)
    for executable in (binary,):
        if not executable.is_file() or not os.access(executable, os.X_OK) or 'statically linked' not in output(['file', str(executable)]):
            raise InstallError(f'Statically linked executable required: {executable}')
    # Root must not trust inherited HOME/XDG paths for system-wide destinations.
    if system:
        prefix = Path('/opt') / args.name
        config_dir = Path('/etc') / args.name
        unit_dir = Path('/etc/systemd/system')
        default_data = Path('/var/lib') / args.name / 'data'
    else:
        prefix = Path(os.environ.get('XDG_DATA_HOME', str(Path(account.pw_dir) / '.local/share'))) / args.name
        config_base = Path(os.environ.get('XDG_CONFIG_HOME', str(Path(account.pw_dir) / '.config')))
        config_dir = config_base / args.name
        unit_dir = config_base / 'systemd/user'
        default_data = prefix / 'data'
    installed = prefix / 'bin/molr-server'
    configuration = config_dir / 'server.json'
    marker = config_dir / 'installation.json'
    unit = unit_dir / (args.name + '.service')
    for path in (prefix, prefix/'LICENSE', prefix/'THIRD_PARTY_NOTICES.md', config_dir, unit_dir, installed, configuration, marker, unit):
        check_managed(path, uid)
    if marker.exists():
        previous = json.loads(marker.read_text())
        if account is None or previous['uid'] != account.pw_uid or previous['system'] != system:
            raise InstallError('Existing installation uses another identity. Use another --name; migrate data explicitly.')
    elif unit.exists():
        raise InstallError(f'Unmanaged service already exists: {unit}; use another --name.')
    # Updates preserve the installed configuration and data, rather than replacing them with a template.
    config_source = configuration if configuration.exists() else source_config
    initial_config = json.loads(config_source.read_text()) if config_source else {'http': default_http(args.port, args.public_host)}
    config = normalize_config(initial_config, default_data)
    allow_full_access = config['sandbox'].get('allow_full_access', False)
    for command in ('bwrap', 'nsenter'):
        if not shutil.which(command) and not allow_full_access and source_config is not None:
            raise InstallError(f'Missing host dependency: {command}. Install dependencies before running this script.')
    match = re.match(r'systemd (\d+)', output(['systemctl', '--version']))
    delegate = bool(match and int(match[1]) >= 254)
    if not delegate and not allow_full_access and source_config is not None:
        raise InstallError('systemd >= 254 is required for delegated sandbox resources.')
    if find_static_busybox() is None and not allow_full_access and source_config is not None:
        raise InstallError('Static BusyBox required; install busybox-static with your system package manager.')
    data = check_path(Path(config['data_dir']))
    if data.exists() and (not data.is_dir() or account is None or data.stat().st_uid != account.pw_uid):
        raise InstallError(f'Existing data directory must belong to the selected user; no recursive ownership changes: {data}')
    if source_config is not None and not args.no_start and not args.init_owner and not (data/'runtime.db').is_file():
        raise InstallError('New installation requires --init-owner NAME (password on stdin), or --no-start for manual initialization.')
    if not system:
        # Fail before changing files when no user manager is available.
        run(['systemctl', '--user', 'show-environment'], stdout=subprocess.DEVNULL)
    if account is None:
        # No HOME creation/chown or extra groups; state is in the service data directory.
        run(['useradd', '--system', '--user-group', '--no-create-home', '--home-dir', '/nonexistent', '--shell', '/usr/sbin/nologin', args.user])
        account = pwd.getpwnam(args.user)
    for directory in (prefix/'bin', config_dir, unit_dir):
        directory.mkdir(parents=True, exist_ok=True, mode=0o755)
    if not data.exists():
        if system:
            check_managed(data.parent, 0)
        missing = []
        parent = data.parent
        while not parent.exists():
            missing.append(parent)
            parent = parent.parent
        for parent in reversed(missing):
            parent.mkdir(mode=0o755)
            parent.chmod(0o755)
        data.mkdir(mode=0o700)
        if system:
            os.chown(data, account.pw_uid, account.pw_gid)
    # Validate access as the target user before initializing or restarting. Never run Molr as root.
    as_user = ['runuser', '-u', account.pw_name, '--'] if system else []
    config_bytes = (json.dumps(config, indent=2, ensure_ascii=False)+'\n').encode()
    file_uid, file_gid = (0, account.pw_gid) if system else (uid, os.getegid())
    if configuration.exists():
        atomic_write(config_dir/'server.previous.json', configuration.read_bytes(), 0o640 if system else 0o600, file_uid, file_gid)
    atomic_write(configuration, config_bytes, 0o640 if system else 0o600, file_uid, file_gid)
    # Keep installation directories traversable by the selected account; new files remain private.
    for directory in (prefix, prefix/'bin', config_dir):
        directory.chmod(0o755)
    run(as_user + ['/usr/bin/python3', '-c',
        'import os,sys; assert os.access(sys.argv[1],os.R_OK); assert os.access(sys.argv[2],os.R_OK|os.W_OK|os.X_OK)', str(configuration), str(data)])
    if installed.exists():
        atomic_write(installed.with_name('molr-server.previous'), installed.read_bytes(), 0o755, uid, os.getegid())
    atomic_write(installed, binary.read_bytes(), 0o755, uid, os.getegid())
    for name, contents in notices.items():
        atomic_write(prefix/name, contents, 0o644, uid, os.getegid())
    if source_config is None and (not (data/'runtime.db').is_file() or ((data/'initial-password').is_file() and not marker.exists())):
        run(as_user + [str(installed), 'bootstrap', '-config', str(configuration)])
    elif args.init_owner:
        run(as_user + [str(installed), 'init', '-config', str(configuration), '-username', args.init_owner, '-password-stdin'])
    atomic_write(unit, render_unit(installed, configuration, account if system else None, delegate=delegate).encode(), 0o644, uid, os.getegid())
    atomic_write(marker, (json.dumps({'uid': account.pw_uid, 'user': account.pw_name, 'system': system})+'\n').encode(), 0o600, uid, os.getegid())
    if not system:
        try:
            if output(['loginctl', 'show-user', account.pw_name, '--property=Linger', '--value']) != 'yes':
                print('To keep running after logout: sudo loginctl enable-linger ' + account.pw_name)
        except (OSError, subprocess.CalledProcessError):
            print('Check user lingering if this service should run without an active login.')
    control = ['systemctl'] + ([] if system else ['--user'])
    run(control + ['daemon-reload'])
    if args.no_start:
        print('Installed without enabling or starting the service.')
        print('Initialize if needed: '+shlex.join(as_user + [str(installed), 'init', '-config', str(configuration), '-username', 'owner', '-password-stdin']))
        print('Start after initialization: '+shlex.join(control + ['enable', '--now', str(unit)]))
    else:
        # Absolute path also supports an explicit XDG_CONFIG_HOME without changing the user manager environment.
        run(control + ['enable', str(unit)])
        run(control + ['restart', unit.name])
        run(control + ['is-active', '--quiet', unit.name])
        print('Service started: ' + config.get('http', {}).get('public_url', ''))
        if (data / 'initial-password').is_file():
            # Poll through authenticated API checks; never consume the delivery login link.
            for attempt in range(30):
                check = subprocess.run(as_user + [str(installed), 'doctor', '-config', str(configuration)], capture_output=True, text=True)
                if any(reason in check.stderr for reason in ('HTTP 401', 'HTTP 403', 'MFA required')):
                    print('Service is reachable; use your current password/MFA with doctor and access to finish verification.')
                    break
                if check.returncode == 0 or check.stdout.strip():
                    print(check.stdout.strip())
                    if check.returncode:
                        print(check.stderr.strip())
                    break
                time.sleep(1)
            else:
                raise InstallError('Service did not pass authenticated health checks: ' + check.stderr.strip())
            print('Access command: ' + shlex.join(as_user + [str(installed), 'access', '-config', str(configuration), '-password-stdin']))
            link = subprocess.run(as_user + [str(installed), 'access', '-config', str(configuration)], capture_output=True, text=True)
            if link.returncode == 0:
                print('Open this one-time login link within 15 minutes:')
                print(link.stdout.strip())
                if not args.no_browser and not system and not os.environ.get('SSH_CONNECTION') and (os.environ.get('DISPLAY') or os.environ.get('WAYLAND_DISPLAY')) and shutil.which('xdg-open'):
                    subprocess.Popen(['xdg-open', link.stdout.strip()], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            else:
                print('Automatic login unavailable; use your current password and MFA with the access command.')
        else:
            print('Existing credentials preserved. Log in normally and run authenticated sandbox checks.')
    print(f'Runtime and local sandbox account: {account.pw_name} (UID {account.pw_uid})')
    print(f'Configuration: {configuration}\nData: {data}\nService: {unit}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--user', help='system service account; requires root; create a service account if missing')
    parser.add_argument('--name', default='molr', help='service name, default molr; use molr-<name> for separate instances')
    parser.add_argument('--no-start', action='store_true', help='prepare files without enabling/starting the service')
    parser.add_argument('--init-owner', metavar='NAME', help='initialize a new owner using password from stdin')
    parser.add_argument('binary', help='statically linked molr-server binary')
    parser.add_argument('--port', type=int, help='new quick-start instance port (default: first free port from 8787)')
    parser.add_argument('--public-host', help='private IP shown in browser links')
    parser.add_argument('--install-deps', action='store_true', help='offer apt dependency installation')
    parser.add_argument('--no-browser', action='store_true', help='print the login link without opening a browser')
    parser.add_argument('--skip-dependencies', action='store_true', help='do not install optional sandbox dependencies')
    parser.add_argument('--yes', action='store_true', help='approve dependency installation without a prompt')
    parser.add_argument('config', nargs='?', help='initial server JSON template; installed config is preserved on updates')
    args = parser.parse_args()
    os.umask(0o077)
    os.environ['PATH'] = '/usr/sbin:/usr/bin:/sbin:/bin'
    try:
        install(args)
    except (InstallError, OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        print(f'Installation failed: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
