#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
# Local-first installer. Downloads and installation use the same versioned package.
source_dir="${MOLR_RELEASE_URL:-}"
archive=""
version=""
forward=()
while (($#)); do
  case "$1" in
    --source) source_dir="${2:?--source requires a URL}"; shift 2 ;;
    --archive) archive="${2:?--archive requires a file}"; shift 2 ;;
    --version) version="${2:?--version requires a version}"; shift 2 ;;
    --help) printf '%s\n' 'Usage: bash install.sh --source URL [--version VERSION] [installer options]' '       bash install.sh --archive FILE [installer options]' 'Installer options: --name molr-NAME --program-root PATH --port PORT --public-host IP --user USER --no-start --no-browser --skip-dependencies --yes'; exit 0 ;;
    *) forward+=("$1"); shift ;;
  esac
done
[[ "$(uname -s)" == Linux ]] || { printf '%s\n' 'Linux is required.' >&2; exit 1; }
case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) printf '%s\n' 'Unsupported architecture.' >&2; exit 1 ;; esac
if ! command -v python3 >/dev/null; then
  command -v apt-get >/dev/null || { printf '%s\n' 'Install Python 3, then run this command again.' >&2; exit 1; }
  approved=no
  for arg in "${forward[@]}"; do [[ "$arg" != --yes ]] || approved=yes; done
  if [[ "$approved" != yes ]]; then
    printf '%s' 'Install Python 3 with apt to continue? [y/N] '
    read -r approved </dev/tty
  fi
  [[ "$approved" == yes || "$approved" == y || "$approved" == Y ]] || exit 1
  privilege=(); if (( EUID != 0 )); then privilege=(sudo); fi
  "${privilege[@]}" apt-get update
  "${privilege[@]}" apt-get install -y python3
fi
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
if [[ -n "$archive" ]]; then
  archive="$(realpath "$archive")"
  [[ -f "$archive.sha256" ]] || { printf '%s\n' 'Missing archive.sha256 checksum file.' >&2; exit 1; }
  expected="$(awk 'NR==1 {print $1}' "$archive.sha256")"
else
  [[ -n "$source_dir" ]] || { printf '%s\n' 'Specify --source with your local release server URL, or --archive with a local package.' >&2; exit 1; }
  case "$source_dir" in https://*|http://*) ;; *) printf '%s\n' 'Source must be HTTP(S).' >&2; exit 1 ;; esac
  source_dir="${source_dir%/}"
  if [[ -n "$version" ]]; then
    [[ "$version" =~ ^[0-9][0-9A-Za-z.-]*$ ]] || { printf '%s\n' 'Invalid version.' >&2; exit 1; }
    filename="molr_${version}_linux_${arch}.tar.gz"
    curl --fail --silent --show-error --location --max-time 60 "$source_dir/$filename.sha256" -o "$stage/checksum"
    expected="$(awk 'NR==1 {print $1}' "$stage/checksum")"
  else
    curl --fail --silent --show-error --location --max-time 60 "$source_dir/latest-linux-$arch.json" -o "$stage/manifest.json"
    read -r filename expected < <(python3 - "$stage/manifest.json" "$arch" <<'PY'
import json,re,sys
m=json.load(open(sys.argv[1]))
if not re.fullmatch(r'molr_[0-9][0-9A-Za-z.-]*_linux_'+sys.argv[2]+r'\.tar\.gz',m['file']): raise SystemExit('Invalid release filename')
print(m['file'],m['sha256'])
PY
    )
  fi
  archive="$stage/$filename"
  curl --fail --silent --show-error --location --max-time 600 "$source_dir/$filename" -o "$archive"
fi
[[ "$expected" =~ ^[a-f0-9]{64}$ ]] || { printf '%s\n' 'Invalid checksum.' >&2; exit 1; }
actual="$(sha256sum "$archive")"; actual="${actual%% *}"
[[ "$actual" == "$expected" ]] || { printf '%s\n' 'Checksum mismatch; installation stopped.' >&2; exit 1; }
python3 - "$archive" "$stage" "$arch" <<'PY'
import json,pathlib,sys,tarfile
with tarfile.open(sys.argv[1],'r:gz') as archive:
    for item in archive.getmembers():
        p=pathlib.PurePosixPath(item.name)
        if p.is_absolute() or '..' in p.parts or not p.parts or p.parts[0]!='molr' or not (item.isfile() or item.isdir()): raise SystemExit('Unsafe archive member')
        item.mode &= 0o755
    archive.extractall(sys.argv[2], **({'filter':'data'} if hasattr(tarfile,'data_filter') else {}))
metadata=json.load(open(pathlib.Path(sys.argv[2])/'molr/release.json'))
if metadata['os']!='linux' or metadata['arch']!=sys.argv[3]: raise SystemExit('Wrong release architecture')
PY
bash "$stage/molr/scripts/install-linux.sh" --install-deps "${forward[@]}" "$stage/molr/bin/molr-server"
