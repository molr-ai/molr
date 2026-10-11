#!/usr/bin/env python3
"""Install one Runtime. Its systemd identity also owns every local sandbox."""
import argparse
import platform
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
import ssl
import urllib.request
import urllib.error
from urllib.parse import urlsplit


from install_layout import (InstallError, check_path, check_managed, layout,
                            require_separate, receipt, PROGRAM_FILES)


def platform_requirements():
    registry = json.loads(Path(__file__).with_name('platforms.json').read_text())
    for item in registry['platforms']:
        if item['os'] == sys.platform and platform.machine().lower() in item['aliases']:
            return item['requirements']
    raise InstallError(f'Unsupported platform: {sys.platform}/{platform.machine()}')


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def output(args):
    return subprocess.check_output(args, text=True).strip()


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


def render_unit(binary, config, account=None, delegate=True, cache=None):
    identity = '' if account is None else f'User={account.pw_name}\nGroup={account.pw_gid}\n'
    delegation = 'Delegate=yes\nDelegateSubgroup=supervisor\n' if delegate else ''
    environment = '' if cache is None else f'Environment={unit_quote("TMPDIR="+str(cache/"tmp"))}\nEnvironment={unit_quote("XDG_CACHE_HOME="+str(cache))}\n'
    target = 'default.target' if account is None else 'multi-user.target'
    return f'''[Unit]
Description=Molr Runtime

[Service]
Type=exec
{identity}{environment}ExecStart={unit_quote(binary)} serve -config {unit_quote(config)}
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
    packages = platform_requirements()['packages']
    print('Missing dependencies: ' + ', '.join(missing))
    if not args.install_deps or not shutil.which('apt-get'):
        print('Install '+ ' '.join(packages)+' to enable isolated commands. Continuing with available capabilities.')
        return
    command = ([] if os.geteuid() == 0 else ['sudo']) + ['apt-get']
    print('Installing: '+ ' '.join(packages))
    if not args.yes:
        with open('/dev/tty') as terminal:
            print('Continue with system dependency installation? [y/N] ', end='', flush=True)
            if terminal.readline().strip().lower() not in ('y', 'yes'):
                print('Continuing without installing dependencies; isolated commands may be unavailable.')
                return
    run(command + ['update'])
    run(command + ['install', '-y', *packages])


def configure_updates(config, source):
    if source is None:
        return
    parsed = urlsplit(source)
    if parsed.scheme not in ('https', 'http') or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise InstallError('Release source must be an HTTP(S) URL without credentials, query or fragment.')
    if parsed.scheme == 'http':
        try:
            ip = ipaddress.ip_address(parsed.hostname)
            if not (ip.is_loopback or ip in ipaddress.ip_network('10.0.0.0/8') or ip in ipaddress.ip_network('172.16.0.0/12') or ip in ipaddress.ip_network('192.168.0.0/16') or ip in ipaddress.ip_network('fc00::/7')):
                raise ValueError()
        except ValueError:
            raise InstallError('Plain HTTP release sources require a private numeric address.')
    updates = config.setdefault('updates', {})
    updates['source'] = source.rstrip('/')
    updates.setdefault('auto_check', True)


def wait_http(config, timeout=30):
    http = config['http']
    context = ssl.create_default_context(cafile=http.get('cert_file') or None)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context))
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with opener.open(http['public_url'].rstrip('/')+'/', timeout=min(2, max(.1, deadline-time.monotonic()))) as response:
                if response.status == 200:
                    return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(.25)
    raise InstallError('HTTP readiness deadline exceeded; previous program and configuration are retained for recovery.')


def verify_health(check, expected):
    if any(reason in check.stderr for reason in ('HTTP 401', 'HTTP 403', 'MFA required', 'HTTP 429')):
        return False
    if check.returncode:
        raise InstallError('Authenticated verification failed: '+check.stderr.strip())
    result = json.loads(check.stdout)
    build = result.get('Runtime', {}).get('build', {})
    if any(build.get(key) != expected.get(key) for key in ('version', 'commit')):
        raise InstallError('Running Runtime build differs from the installed program.')
    sandbox = result.get('Sandbox', {})
    caps = sandbox.get('Capabilities', {})
    if sandbox.get('Enabled') and not (caps.get('Reason') == 'available' and all(caps.get(key) for key in ('Shell','ProcessIsolation','FileIsolation','NetworkIsolation','ResourceLimits'))):
        raise InstallError('Runtime sandbox verification failed.')
    return True


def install(args):
    requirements = platform_requirements()
    if sys.version_info[:2] < tuple(map(int, requirements['python'].split('.'))):
        raise InstallError('Python '+requirements['python']+' or newer is required for safe offline uninstallation.')
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
    paths = layout(args.name, account.pw_dir if account else '/', system, program_root=getattr(args, 'program_root', None))
    prefix, config_dir, default_data = paths['prefix'], paths['config'], paths['data']
    cache = paths['cache']
    unit_dir = paths['unit'].parent
    installed = prefix / 'bin/molr-server'
    configuration = config_dir / 'server.json'
    marker = config_dir / 'installation.json'
    unit = unit_dir / (args.name + '.service')
    for path in (prefix, prefix/'LICENSE', prefix/'THIRD_PARTY_NOTICES.md', config_dir, unit_dir, installed, configuration, marker, unit):
        check_managed(path, uid)
    previous = None
    if marker.exists():
        previous = json.loads(marker.read_text())
        if account is None or previous.get('schema') != 1 or previous['uid'] != account.pw_uid or previous['system'] != system or previous['paths'] != {k:str(v) for k,v in paths.items()} or type(previous.get('managed_data')) is not bool:
            raise InstallError('Existing installation uses another identity. Use another --name; migrate data explicitly.')
    elif unit.exists():
        raise InstallError(f'Unmanaged service already exists: {unit}; use another --name.')
    # Updates preserve the installed configuration and data, rather than replacing them with a template.
    config_source = configuration if configuration.exists() else source_config
    initial_config = json.loads(config_source.read_text()) if config_source else {'http': default_http(args.port, args.public_host)}
    config = normalize_config(initial_config, default_data)
    configure_updates(config, getattr(args, 'release_source', None))
    allow_full_access = config['sandbox'].get('allow_full_access', False)
    for command in ('bwrap', 'nsenter'):
        if not shutil.which(command) and not allow_full_access and source_config is not None:
            raise InstallError(f'Missing host dependency: {command}. Install dependencies before running this script.')
    match = re.match(r'systemd (\d+)', output(['systemctl', '--version']))
    delegate = bool(match and int(match[1]) >= requirements['systemd'])
    if not delegate and not allow_full_access and source_config is not None:
        raise InstallError(f"systemd >= {requirements['systemd']} is required for delegated sandbox resources.")
    if find_static_busybox() is None and not allow_full_access and source_config is not None:
        raise InstallError('Static BusyBox required; install busybox-static with your system package manager.')
    data = check_path(Path(config['data_dir']))
    require_separate([prefix, config_dir, cache, data, paths['unit']])
    check_managed(cache.parent, uid)
    if cache.exists() and (not cache.is_dir() or account is None or cache.stat().st_uid != account.pw_uid or cache.stat().st_mode & 0o022):
        raise InstallError(f'Cache directory must belong to the selected user: {cache}')
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
    for relative in PROGRAM_FILES:
        check_managed(prefix/relative, uid)
    for directory in (prefix/'bin', prefix/'scripts', config_dir, unit_dir, cache.parent):
        missing = []
        parent = directory
        while not parent.exists():
            missing.append(parent)
            parent = parent.parent
        for parent in reversed(missing):
            parent.mkdir(mode=0o755)
            parent.chmod(0o755)
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
    cache.mkdir(parents=True, exist_ok=True, mode=0o700)
    check_path(cache/'tmp')
    if (cache/'tmp').exists():
        info = (cache/'tmp').stat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != account.pw_uid or info.st_mode & 0o022:
            raise InstallError('Cache tmp directory must be private and owned by the service account.')
    (cache/'tmp').mkdir(exist_ok=True, mode=0o700)
    if system:
        os.chown(cache, account.pw_uid, account.pw_gid, follow_symlinks=False)
        os.chown(cache/'tmp', account.pw_uid, account.pw_gid, follow_symlinks=False)
    control = ['systemctl'] + ([] if system else ['--user'])
    as_user = ['runuser', '-u', account.pw_name, '--'] if system else []
    expected_build = json.loads(output(as_user + [str(binary), 'version', '--json']))
    release_metadata = binary.parent.parent / 'release.json'
    if release_metadata.is_file():
        declared = json.loads(release_metadata.read_text())
        if any(declared.get(key) != expected_build.get(key) for key in ('version', 'commit', 'os', 'arch')):
            raise InstallError('Package metadata differs from its executable build identity.')
    if previous and not args.no_start:
        run(control + ['stop', unit.name])
    # Record ownership before initialization so failed installs remain removable.
    record = receipt(args.name, account, system, paths, data)
    if previous:
        record['complete'] = previous.get('complete', False)
        record['managed_data'] = record['managed_data'] or previous['managed_data']
    atomic_write(marker, (json.dumps(record)+'\n').encode(), 0o640 if system else 0o600, uid, account.pw_gid if system else os.getegid())
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
    for name in ('uninstall-linux.sh', 'uninstall-linux.py', 'install_layout.py'):
        atomic_write(prefix/'scripts'/name, Path(__file__).with_name(name).read_bytes(), 0o755 if name.endswith('.sh') else 0o644, uid, os.getegid())
    wrapper = '#!/usr/bin/env bash\nset -euo pipefail\nexec /bin/bash '+shlex.quote(str(prefix/'scripts/uninstall-linux.sh'))+' --record '+shlex.quote(str(marker))+' "$@"\n'
    atomic_write(prefix/'uninstall.sh', wrapper.encode(), 0o755, uid, os.getegid())
    if source_config is None and (not (data/'runtime.db').is_file() or not previous or not previous.get('complete')):
        run(as_user + [str(installed), 'bootstrap', '-config', str(configuration)])
    elif args.init_owner:
        run(as_user + [str(installed), 'init', '-config', str(configuration), '-username', args.init_owner, '-password-stdin'])
    atomic_write(unit, render_unit(installed, configuration, account if system else None, delegate=delegate, cache=cache).encode(), 0o644, uid, os.getegid())
    record['complete'] = True
    atomic_write(marker, (json.dumps(record)+'\n').encode(), 0o640 if system else 0o600, uid, account.pw_gid if system else os.getegid())
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
        wait_http(config)
        verified = False
        if (data / 'initial-password').is_file():
            # Authenticate once after HTTP readiness; never retry a login loop.
            check = subprocess.run(as_user + [str(installed), 'doctor', '-config', str(configuration)], capture_output=True, text=True)
            verified = verify_health(check, expected_build)
            if verified:
                print('Upgrade/install verified: '+expected_build['version'])
            else:
                print('Program installed; verification pending. Use your current password/MFA with doctor. Respect any authentication rate limit.')
            print('Access command: ' + shlex.join(as_user + [str(installed), 'access', '-config', str(configuration), '-password-stdin']))
            link = subprocess.run(as_user + [str(installed), 'access', '-config', str(configuration)], capture_output=True, text=True) if verified else subprocess.CompletedProcess([], 1, '', '')
            if link.returncode == 0:
                print('Open this one-time login link within 15 minutes:')
                print(link.stdout.strip())
                if not args.no_browser and not system and not os.environ.get('SSH_CONNECTION') and (os.environ.get('DISPLAY') or os.environ.get('WAYLAND_DISPLAY')) and shutil.which('xdg-open'):
                    subprocess.Popen(['xdg-open', link.stdout.strip()], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            else:
                print('Automatic login unavailable; use your current password and MFA with the access command.')
        else:
            print('Program installed; verification pending. Existing credentials preserved. Run doctor with your current password/MFA to verify the version and sandbox.')
            print('Verify: '+shlex.join(as_user + [str(installed), 'doctor', '-config', str(configuration), '-password-stdin']))
    print(f'Runtime and local sandbox account: {account.pw_name} (UID {account.pw_uid})')
    print(f'Program: {prefix}\nConfiguration: {configuration}\nData: {data}\nCache: {cache}\nService: {unit}')
    print('Uninstall (keeps configuration and data): bash '+shlex.quote(str(prefix/'uninstall.sh')))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--user', help='system service account; requires root; create a service account if missing')
    parser.add_argument('--name', default='molr', help='service name, default molr; use molr-<name> for separate instances')
    parser.add_argument('--release-source', help='persist the explicitly selected release source')
    parser.add_argument('--program-root', help='program base directory; each instance gets its own subdirectory')
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
