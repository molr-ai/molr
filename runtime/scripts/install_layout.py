"""Linux installation paths shared by the installer and offline uninstaller."""
import os
from pathlib import Path
import re
import shlex
import stat

class InstallError(Exception):
    pass



def check_path(path):
    """Never write through symlinks, including dangling ones and ancestors."""
    path = Path(path)
    if not path.is_absolute() or path == Path('/') or '..' in path.parts or any(c in str(path) for c in '\n\r\x00'):
        raise InstallError(f'Dedicated absolute path required: {path}')
    for part in [*reversed(path.parents), path]:
        if part.is_symlink():
            raise InstallError(f'Refusing symlinked path: {part}')
    return path


class DirectoryPermissions:
    """One explicit choice per writable directory, shared by install and uninstall."""
    def __init__(self, action='ask', dry_run=False):
        self.action = action
        self.dry_run = dry_run
        self.accepted = set()

    def check(self, path, info):
        identity = (str(path), info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode)
        if identity in self.accepted:
            return
        print(f'Directory is group/other writable: {path} (mode {stat.S_IMODE(info.st_mode):04o}).', flush=True)
        print('Recommended: chmod go-w -- ' + shlex.quote(str(path)) +
              ' (this directory only). Keeping it allows other members of its group or other users to modify directory entries.', flush=True)
        action = self.action
        if self.dry_run:
            print('Dry run: directory permissions will not be changed.')
        else:
            if action == 'ask':
                try:
                    with open('/dev/tty') as terminal:
                        while True:
                            print('Fix permissions [f, recommended], ignore and continue [i], or quit [q]? ', end='', flush=True)
                            answer = terminal.readline()
                            if not answer:
                                raise InstallError('No directory permission choice received.')
                            answer = answer.strip().lower()
                            if answer in ('f', 'fix', 'i', 'ignore', 'q', 'quit'):
                                break
                except OSError as error:
                    raise InstallError('Directory permissions need a choice; rerun interactively or use --directory-permissions fix|ignore.') from error
                action = {'f': 'fix', 'fix': 'fix', 'i': 'ignore', 'ignore': 'ignore', 'q': 'quit', 'quit': 'quit'}[answer]
            if action == 'quit':
                raise InstallError('Stopped at the directory permission prompt; this directory was not changed.')
            if action == 'fix':
                # Pin the directory itself; never chmod recursively or follow a replacement symlink.
                fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
                try:
                    current = os.fstat(fd)
                    if (current.st_dev, current.st_ino, current.st_uid, current.st_gid, current.st_mode) != identity[1:]:
                        raise InstallError(f'Directory changed during permission confirmation: {path}')
                    os.fchmod(fd, stat.S_IMODE(current.st_mode) & ~0o022)
                finally:
                    os.close(fd)
                print(f'Directory permissions updated: {path}')
            elif action == 'ignore':
                print(f'Keeping directory permissions: {path}')
            else:
                raise InstallError('Invalid directory permission choice.')
        self.accepted.add(identity)


def check_managed(path, uid, directory_permissions=None):
    check_path(path)
    for part in [path, *path.parents]:
        if part.exists():
            info = part.stat()
            if info.st_uid == 0 and stat.S_ISDIR(info.st_mode) and info.st_mode & stat.S_ISVTX:
                continue  # e.g. /tmp; later components must still be installer-owned
            if info.st_uid not in (0, uid):
                raise InstallError(f'Installation path has owner UID {info.st_uid}; expected UID {uid} or root: {part}')
            if info.st_mode & 0o022:
                if stat.S_ISDIR(info.st_mode) and directory_permissions is not None:
                    directory_permissions.check(part, info)
                else:
                    raise InstallError(f'Installation path must not be group/other writable: {part} (mode {stat.S_IMODE(info.st_mode):04o})')


PROGRAM_FILES = (
    'bin/molr-server', 'bin/molr-server.previous', 'LICENSE', 'THIRD_PARTY_NOTICES.md',
    'uninstall.sh', 'scripts/uninstall-linux.sh', 'scripts/uninstall-linux.py', 'scripts/install_layout.py',
)
CONFIG_FILES = ('server.json', 'server.previous.json', 'installation.json')


def layout(name, home, system, environ=None, program_root=None):
    if not re.fullmatch(r'molr(?:-[a-z0-9][a-z0-9-]{0,47})?', name):
        raise InstallError('Service name must be molr or molr-<lowercase name>.')
    instance = 'default' if name == 'molr' else name[5:]
    # molr-default would otherwise alias the default instance.
    if name == 'molr-default':
        raise InstallError('Use --name molr for the default instance.')
    env = os.environ if environ is None else environ
    if system:
        roots = dict(prefix=Path('/opt/molr'), config=Path('/etc/molr'),
                     data=Path('/var/lib/molr'), cache=Path('/var/cache/molr'))
        unit_dir = Path('/etc/systemd/system')
    else:
        roots = dict(prefix=Path(home)/'.local/lib/molr',
                     config=Path(env.get('XDG_CONFIG_HOME') or str(Path(home)/'.config'))/'molr',
                     data=Path(env.get('XDG_DATA_HOME') or str(Path(home)/'.local/share'))/'molr',
                     cache=Path(env.get('XDG_CACHE_HOME') or str(Path(home)/'.cache'))/'molr')
        unit_dir = roots['config'].parent/'systemd/user'
    if program_root is not None:
        roots['prefix'] = check_path(Path(program_root))
    paths = {key: check_path(value/instance) for key, value in roots.items()}
    paths['unit'] = check_path(unit_dir/(name+'.service'))
    require_separate(list(paths.values()))
    return paths


def require_separate(paths):
    for i, a in enumerate(paths):
        for b in paths[i+1:]:
            if a == b or a in b.parents or b in a.parents:
                raise InstallError(f'Installation paths must not overlap: {a}, {b}')


def receipt(name, account, system, paths, data):
    return dict(schema=1, name=name, uid=account.pw_uid, user=account.pw_name,
                system=system, program_root=str(paths['prefix'].parent),
                paths={k:str(v) for k,v in paths.items()}, data_dir=str(data),
                managed_data=(data == paths['data']), complete=False)


def validate_receipt(record, home, uid, directory_permissions=None):
    if (record.get('schema') != 1 or type(record.get('system')) is not bool
            or type(record.get('uid')) is not int or record['uid'] <= 0
            or type(record.get('managed_data')) is not bool):
        raise InstallError('Invalid installation record.')
    if record['system']:
        if uid != 0:
            raise InstallError('System uninstallation requires root.')
        env = {}
    else:
        if uid == 0 or record.get('uid') != uid:
            raise InstallError('Run the uninstaller as the original installation user.')
        env = {f'XDG_{key.upper()}_HOME': str(Path(record['paths'][key]).parent.parent)
               for key in ('config', 'data', 'cache')}
    paths = layout(record['name'], home, record['system'], env, record['program_root'])
    if record['paths'] != {k:str(v) for k,v in paths.items()}:
        raise InstallError('Installation paths do not match the instance.')
    for key in ('prefix', 'config', 'unit'):
        check_managed(paths[key], uid, directory_permissions)
    check_path(Path(record['data_dir']))
    return paths
