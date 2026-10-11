#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
# This bootstrap downloads a versioned package; installation is owned by that package.
command -v python3 >/dev/null || { echo 'Install Python 3.11 or newer, then run this command again.' >&2; exit 1; }
python3 - "$@" <<'PY'
import argparse, hashlib, ipaddress, json, os, pathlib, platform, re, subprocess, sys, tarfile, tempfile
from urllib.parse import urlsplit
from urllib.request import urlopen

# BEGIN GENERATED PLATFORMS
PLATFORMS = [{"os":"linux","arch":"amd64","aliases":["x86_64","amd64"]},{"os":"linux","arch":"arm64","aliases":["aarch64","arm64"]}]
RELEASE_PATTERN = r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:beta|rc)\.(?:0|[1-9][0-9]*)|0\.nightly\.[1-9][0-9]{7}\.(?:0|[1-9][0-9]*)\.g[0-9a-f]{7,40}))?$"
# END GENERATED PLATFORMS

def fail(message):
    raise ValueError(message)

def public_url(value):
    url = urlsplit(value)
    if url.username or url.password or url.fragment or not url.hostname:
        fail('Invalid download URL')
    if url.scheme == 'https':
        return value
    try:
        address = ipaddress.ip_address(url.hostname)
        if url.scheme == 'http' and (address.is_private or address.is_loopback):
            return value
    except ValueError:
        pass
    fail('Downloads require HTTPS (HTTP is allowed only for explicit private IP addresses)')

def fetch(url, limit):
    with urlopen(public_url(url), timeout=60) as response:
        public_url(response.url)
        data = response.read(limit + 1)
    if len(data) > limit:
        fail('Download exceeds the size limit')
    return data

def main():
    parser = argparse.ArgumentParser(description='Install Molr from a release channel or a local archive. Remaining arguments are forwarded to the package installer.')
    parser.add_argument('--source', default=os.environ.get('MOLR_RELEASE_URL', 'https://molr.ai/releases'))
    parser.add_argument('--channel', choices=['stable', 'preview', 'nightly'])
    parser.add_argument('--version')
    parser.add_argument('--archive')
    args, forward = parser.parse_known_args()
    if sys.version_info < (3, 11):
        fail('Python 3.11 or newer is required')
    os_name, machine = platform.system().lower(), platform.machine().lower()
    supported = next((p for p in PLATFORMS if p['os'] == os_name and machine in p['aliases']), None)
    if supported is None:
        fail(f'Unsupported platform: {os_name}/{machine}')
    root = args.source.rstrip('/')
    suffix = root.rsplit('/', 1)[-1]
    channel = args.channel or (suffix if suffix in ('stable', 'preview', 'nightly') else 'stable')
    if suffix in ('stable', 'preview', 'nightly'):
        root = root.rsplit('/', 1)[0]
    public_url(root)
    if args.version and not re.fullmatch(r'[0-9][0-9A-Za-z.-]*', args.version):
        fail('Invalid version')
    with tempfile.TemporaryDirectory(prefix='molr-install-') as temporary:
        stage = pathlib.Path(temporary)
        expected_version = args.version
        if args.archive:
            archive = pathlib.Path(args.archive).resolve(strict=True)
            checksum = pathlib.Path(str(archive) + '.sha256')
            if not checksum.is_file():
                fail('Missing archive.sha256 checksum file')
            expected = checksum.read_text().split()[0]
            expected_arch = supported['arch']
        else:
            path = f'/versions/{args.version}.json' if args.version else f'/{channel}/manifest.json'
            manifest = json.loads(fetch(root + path, 2 * 1024 * 1024))
            expected_version = manifest['version']
            if not re.fullmatch(r'[0-9][0-9A-Za-z.-]*', expected_version) or (args.version and expected_version != args.version):
                fail('Manifest version mismatch')
            if not re.fullmatch(RELEASE_PATTERN, expected_version):
                fail('Unsupported release version')
            actual_channel = 'nightly' if '-0.nightly.' in expected_version else 'preview' if '-' in expected_version else 'stable'
            if manifest.get('channel') != actual_channel:
                fail('Manifest channel mismatch')
            allowed = [channel, 'stable'] if channel == 'preview' else [channel]
            if manifest.get('schema') != 1 or (not args.version and actual_channel not in allowed):
                fail('Manifest channel mismatch')
            target = next((p for p in manifest['platforms'] if p['os'] == os_name and machine in p['aliases']), None)
            if target is None:
                fail(f'Version {expected_version} has no package for {os_name}/{machine}')
            expected_arch = target['arch']
            filename = f'molr_{expected_version}_{os_name}_{expected_arch}.tar.gz'
            if target['file'] != filename or not re.fullmatch(r'[a-z0-9]+', expected_arch):
                fail('Invalid release filename')
            expected = target['sha256']
            archive = stage / filename
            archive.write_bytes(fetch(target['url'], 512 * 1024 * 1024))
        if not re.fullmatch(r'[a-f0-9]{64}', expected):
            fail('Invalid checksum')
        with archive.open('rb') as content:
            actual = hashlib.file_digest(content, 'sha256').hexdigest()
        if actual != expected:
            fail('Checksum mismatch; installation stopped')
        with tarfile.open(archive, 'r:gz') as package:
            seen, size = set(), 0
            for item in package.getmembers():
                p = pathlib.PurePosixPath(item.name)
                size += item.size
                if p.is_absolute() or '..' in p.parts or not p.parts or p.parts[0] != 'molr' or not (item.isfile() or item.isdir()) or item.name in seen or size > 1024 * 1024 * 1024:
                    fail('Unsafe archive member')
                seen.add(item.name)
                item.mode &= 0o755
            package.extractall(stage, **({'filter': 'data'} if hasattr(tarfile, 'data_filter') else {}))
        metadata = json.loads((stage / 'molr/release.json').read_text())
        if metadata['os'] != os_name or metadata['arch'] != expected_arch:
            fail('Wrong release architecture')
        if expected_version and metadata['version'] != expected_version:
            fail('Wrong release version')
        if not args.archive:
            if metadata['commit'] != manifest['commit']:
                fail('Wrong release commit')
            forward += ['--release-source', root + '/' + channel]
        subprocess.run(['bash', str(stage / 'molr/scripts/install-linux.sh'), '--install-deps', *forward, str(stage / 'molr/bin/molr-server')], check=True)

try:
    main()
except (ValueError, KeyError, OSError, subprocess.CalledProcessError, tarfile.TarError) as error:
    print(f'Installation stopped: {error}', file=sys.stderr)
    sys.exit(1)
PY
