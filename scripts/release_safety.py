#!/usr/bin/env python3
"""Check publishable source/assets and build source archives without local runtime data.

Only high-confidence credential formats are detected. This is an accident-prevention
check, not a general secret scanner or a substitute for reviewing changes/history.
Errors show paths and categories, never matched credential values.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parent.parent
POLICY = json.loads((ROOT / 'scripts/release-policy.json').read_text())
PRIVATE_DIRS = {'internal-docs', '.git', '.cache', '.aws', '.ssh', '.gnupg', '.codex', '.agents', '.private',
                'node_modules', '.venv', '__pycache__', 'dist', 'build', 'coverage',
                'playwright-report', 'test-results'}
PRIVATE_NAMES = {'.npmrc', '.netrc', 'id_rsa', 'id_ed25519', 'credentials.json',
                 'initial-password', 'owner-password', 'server.json', 'sandbox.json', 'delivery-login.json'}
SECRET_RULES = {
    'private key': re.compile(rb'-----BEGIN (?:RSA |DSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----'),
    'saved login link': re.compile(rb'/login\?token=[A-Za-z0-9_%+./=-]{32,}'),
    'provider credential': re.compile(rb'\b(?:sk-(?:proj-|ant-api\d+-)?[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{25,}|github_pat_[A-Za-z0-9_]{30,}|AKIA[A-Z0-9]{16}|ASIA[A-Z0-9]{16}|AIza[A-Za-z0-9_-]{30,}|xox[baprs]-[A-Za-z0-9-]{15,})\b'),
}


class UnsafeRelease(ValueError):
    pass


def fail(path, reason):
    label = str(path).encode()
    for pattern in SECRET_RULES.values():
        label = pattern.sub(b'[REDACTED]', label)
    raise UnsafeRelease(f'{json.dumps(label.decode(), ensure_ascii=False)}: {reason}')


def private_path(name, generated=False):
    path = PurePosixPath(name)
    if path.is_absolute() or '..' in path.parts or '\\' in name or '\x00' in name:
        return 'unsafe path'
    parts = [p.lower() for p in path.parts]
    if any(p in PRIVATE_DIRS for p in parts):
        return 'private/generated directory'
    if not generated and (parts[:1] == ['data'] or parts[:2] == ['runtime', 'data']):
        return 'runtime data directory'
    leaf = parts[-1] if parts else ''
    if parts[:2] == ['runtime', 'configs'] and leaf.endswith('.json') and not leaf.endswith('.example.json'):
        return 'non-example Runtime deployment configuration'
    if leaf in PRIVATE_NAMES:
        return 'local configuration or credential file'
    if leaf.startswith('.env') and leaf not in ('.env.example',) and not (leaf.startswith('.env.') and leaf.endswith('.example')):
        return 'local environment file'
    if re.search(r'\.(?:env|pem|key|p12|pfx|jks|bak|backup|local\.json)$', leaf):
        return 'private configuration, key, or backup'
    if re.search(r'\.(?:db|sqlite3?)(?:$|-)|\.log(?:$|\.)', leaf):
        return 'database or log'
    if ('login-link' in leaf and leaf.endswith(('.json', '.txt'))) or 'login-url' in leaf:
        return 'saved login link'
    return None


def check_bytes(name, data, generated=False):
    reason = private_path(name, generated)
    if reason:
        fail(name, reason)
    if data.startswith(b'SQLite format 3\x00'):
        fail(name, 'SQLite database content')
    for label, pattern in SECRET_RULES.items():
        if pattern.search(data):
            fail(name, f'possible {label}; review and remove before publishing')


def git(*args, root=ROOT):
    return subprocess.check_output(['git', '-C', str(root), *args])


def source_entries(root=ROOT, revision=None):
    entries = []
    command = ('ls-tree', '-rz', '--full-tree', revision) if revision else ('ls-files', '--stage', '-z')
    for record in git(*command, root=root).split(b'\0'):
        if not record:
            continue
        meta, name = record.split(b'\t', 1)
        fields = meta.decode().split()
        mode, oid = (fields[0], fields[2]) if revision else (fields[0], fields[1])
        name = name.decode()
        if not revision and fields[2] != '0':
            fail(name, 'unmerged index entry')
        if mode not in ('100644', '100755'):
            fail(name, 'source exports only support regular files (no links/submodules)')
        reason = private_path(name)
        if reason:
            fail(name, reason)
        entries.append((name, oid))
    return entries


def check_objects(entries, root=ROOT):
    # Batch only approved paths. Neither diagnostics nor logs include matched values.
    result = subprocess.run(['git', '-C', str(root), 'cat-file', '--batch'],
                            input=''.join(oid+'\n' for _, oid in entries).encode(),
                            stdout=subprocess.PIPE, check=True).stdout
    offset = 0
    for name, oid in entries:
        end = result.index(b'\n', offset)
        header = result[offset:end].split()
        if len(header) != 3 or header[1] != b'blob' or header[0].decode() != oid:
            fail(name, 'invalid Git object')
        size = int(header[2])
        check_bytes(name, result[end+1:end+1+size])
        offset = end+size+2


def regular_file(path, boundary):
    # Inspect every component before opening a worktree file; never follow links.
    for item in [path, *path.parents]:
        if item == boundary:
            break
        if item.is_symlink():
            fail(path.relative_to(boundary), 'symbolic link is not publishable')
    if not path.is_file():
        fail(path.relative_to(boundary), 'expected regular file')


def check_source(root=ROOT):
    entries = source_entries(root)
    check_objects(entries, root)
    files = git('ls-files', '--cached', '--others', '--exclude-standard', '-z', root=root)
    count = 0
    for name in sorted(set(x.decode() for x in files.split(b'\0') if x)):
        path = root / name
        if not path.exists() and not path.is_symlink():
            continue  # A working-tree deletion; the index was still checked above.
        regular_file(path, root)
        reason = private_path(name)
        if reason:
            fail(name, reason)
        check_bytes(name, path.read_bytes())
        count += 1
    return count


def tree_files(directory):
    if directory.is_symlink():
        fail(directory, 'symbolic directory is not publishable')
    for current, directories, files in os.walk(directory, followlinks=False):
        for name in directories:
            if (Path(current)/name).is_symlink():
                fail(Path(current)/name, 'symbolic directory is not publishable')
        for name in files:
            path = Path(current)/name
            regular_file(path, directory)
            yield path


def check_public(directory, root=ROOT):
    allowed = set(POLICY['publicFiles'])
    count = 0
    for path in tree_files(directory):
        name = path.relative_to(root).as_posix()
        if name not in allowed:
            fail(name, 'not in reviewed publicFiles list in scripts/release-policy.json')
        check_bytes(name, path.read_bytes())
        count += 1
    return count


def release_metadata(data, name):
    try:
        value = json.loads(data)
    except (ValueError, UnicodeError):
        fail(name, 'invalid release metadata')
    if not isinstance(value, dict) or set(value) != {'version', 'commit', 'builtAt', 'os', 'arch'}:
        fail(name, 'unexpected release metadata fields')
    if (not isinstance(value['version'], str) or not re.fullmatch(r'[0-9][0-9A-Za-z.-]*', value['version'])
            or not isinstance(value['commit'], str) or not re.fullmatch(r'[a-f0-9]{40}', value['commit'])
            or value['os'] != 'linux' or value['arch'] not in ('amd64', 'arm64')
            or not isinstance(value['builtAt'], str) or not re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z', value['builtAt'])):
        fail(name, 'invalid release metadata values')
    return value


def check_release(archive):
    if archive.is_symlink() or not archive.is_file():
        fail(archive.name, 'expected regular archive')
    expected = {'molr/'+name for name in POLICY['releaseFiles']}
    allowed_dirs = {'molr', 'molr/bin', 'molr/scripts'}
    seen = set()
    metadata = None
    # Never extract an archive during verification.
    with tarfile.open(archive, 'r:gz') as source:
        for member in source:
            name = member.name.rstrip('/')
            if name in seen:
                fail(name, 'duplicate archive member')
            seen.add(name)
            if member.isdir() and name in allowed_dirs:
                continue
            if not member.isfile() or name not in expected:
                fail(name, 'unexpected release member (only the explicit releaseFiles list is allowed)')
            if member.size > 256 * 1024 * 1024:
                fail(name, 'oversized release member')
            data = source.extractfile(member).read()
            check_bytes(name, data, generated=True)
            if name == 'molr/release.json':
                metadata = release_metadata(data, name)
        if not expected.issubset(seen):
            fail(archive.name, 'missing required release files')
    return metadata


def check_release_directory(directory, strict=False):
    if directory.is_symlink():
        fail(directory, 'symbolic release directory is not publishable')
    allowed_name = re.compile(r'(?:molr_[0-9][0-9A-Za-z.-]*_linux_(?:amd64|arm64)\.tar\.gz(?:\.sha256)?|latest-linux-(?:amd64|arm64)\.json)')
    selected = []
    for path in sorted(directory.iterdir()):
        if not allowed_name.fullmatch(path.name):
            if strict:
                fail(path.name, 'unexpected file in published download directory')
            continue
        regular_file(path, directory)
        selected.append(path)
    for path in selected:
        if path.name.endswith('.tar.gz'):
            info = check_release(path)
            if path.name != f"molr_{info['version']}_linux_{info['arch']}.tar.gz":
                fail(path.name, 'metadata does not match archive filename')
        elif path.name.endswith('.sha256'):
            target = directory/path.name.removesuffix('.sha256')
            if target not in selected:
                fail(path.name, 'checksum refers to a missing archive')
            expected = hashlib.sha256(target.read_bytes()).hexdigest()+'  '+target.name+'\n'
            if path.read_text() != expected:
                fail(path.name, 'checksum file is not the exact expected digest and filename')
        else:
            try:
                value = json.loads(path.read_bytes())
            except (ValueError, UnicodeError):
                fail(path.name, 'invalid download manifest')
            if not isinstance(value, dict) or set(value) != {'version', 'commit', 'published_at', 'notes_url', 'file', 'sha256'}:
                fail(path.name, 'unexpected download manifest fields')
            from datetime import datetime
            from urllib.parse import urlparse
            try:
                if not isinstance(value['published_at'], str) or datetime.fromisoformat(value['published_at'].replace('Z', '+00:00')).tzinfo is None:
                    fail(path.name, 'invalid publish timestamp')
                notes = urlparse(value['notes_url'])
                if notes.scheme != 'https' or not notes.netloc or notes.username or notes.password:
                    fail(path.name, 'invalid release notes URL')
            except (ValueError, TypeError, AttributeError):
                fail(path.name, 'invalid release metadata')
            if not isinstance(value['commit'], str) or not re.fullmatch(r'[a-f0-9]{40}|[a-f0-9]{64}', value['commit']):
                fail(path.name, 'invalid release commit')
            arch = path.name.removeprefix('latest-linux-').removesuffix('.json')
            version = value['version']
            if not isinstance(version, str) or not re.fullmatch(r'[0-9][0-9A-Za-z.-]*', version):
                fail(path.name, 'invalid manifest version')
            filename = f'molr_{version}_linux_{arch}.tar.gz'
            if value['file'] != filename or directory/filename not in selected:
                fail(path.name, 'manifest does not name an approved archive')
            if check_release(directory/filename)['commit'] != value['commit']:
                fail(path.name, 'release commit mismatch')
            if value['sha256'] != hashlib.sha256((directory/filename).read_bytes()).hexdigest():
                fail(path.name, 'manifest checksum mismatch')
    return len(selected)


def check_frontend(directory):
    count = 0
    for path in tree_files(directory):
        name = path.relative_to(directory).as_posix()
        if name.startswith('releases/'):
            continue  # Opt-in download assets have their own exact archive policy.
        check_bytes(name, path.read_bytes(), generated=True)
        count += 1
    if (directory/'releases').exists():
        check_release_directory(directory/'releases', strict=True)
    return count


def build_source(revision, output, root=ROOT):
    # Resolve before touching the output: this command exports exactly one commit.
    commit = git('rev-parse', '--verify', '--end-of-options', revision+'^{commit}', root=root).decode().strip()
    if not re.fullmatch(r'[a-f0-9]{40}', commit):
        fail('revision', 'expected a commit')
    entries = source_entries(root, commit)
    check_objects(entries, root)
    output.mkdir(parents=True, exist_ok=True)
    target = output/f'molr_source_{commit[:12]}.tar.gz'
    fd, temp = tempfile.mkstemp(dir=output, suffix='.tar.gz')
    os.close(fd)
    try:
        git('archive', '--format=tar.gz', '--prefix=molr-source/', '--output='+temp, commit, root=root)
        expected = {name: oid for name, oid in entries}
        seen = set()
        with tarfile.open(temp, 'r:gz') as archive:
            for member in archive:
                if member.isdir():
                    continue
                if not member.isfile() or not member.name.startswith('molr-source/'):
                    fail(member.name, 'unexpected source archive member')
                name = member.name.removeprefix('molr-source/')
                if name not in expected or name in seen:
                    fail(name, 'unexpected source archive member')
                data = archive.extractfile(member).read()
                check_bytes(name, data)
                oid = hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
                if oid != expected[name]:
                    fail(name, 'archive differs from reviewed Git blob (including export substitutions)')
                seen.add(name)
        if seen != set(expected):
            fail(target.name, 'archive omitted tracked files; review export-ignore attributes')
        os.replace(temp, target)
    finally:
        Path(temp).unlink(missing_ok=True)
    target.with_name(target.name+'.sha256').write_text(hashlib.sha256(target.read_bytes()).hexdigest()+'  '+target.name+'\n')
    return target, len(entries)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    sub.add_parser('check')
    for name in ['public', 'frontend', 'release', 'release-directory']:
        sub.add_parser(name).add_argument('path', type=Path)
    source = sub.add_parser('source')
    source.add_argument('--ref', default='HEAD')
    source.add_argument('--output', type=Path, default=ROOT/'dist/sources')
    args = parser.parse_args()
    try:
        if args.command == 'check':
            count = check_source()
            print(f'Checked index and {count} working-tree source files; ignored local data was not opened.')
        elif args.command == 'public':
            print(f'Checked {check_public(args.path.absolute())} approved public assets.')
        elif args.command == 'frontend':
            print(f'Checked {check_frontend(args.path.absolute())} frontend files.')
        elif args.command == 'release':
            check_release(args.path.absolute())
            print('Release archive matches the explicit file list; content checks passed.')
        elif args.command == 'release-directory':
            print(f'Checked {check_release_directory(args.path.absolute())} selected download assets.')
        else:
            path, count = build_source(args.ref, args.output.absolute())
            print(f'Source archive: {path} ({count} files from the selected commit only)')
    except (UnsafeRelease, OSError, subprocess.CalledProcessError, tarfile.TarError, UnicodeError) as error:
        print(f'Release safety check failed: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
