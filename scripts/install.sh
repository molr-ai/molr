#!/bin/sh
# Bootstrap only: installation and maintenance belong to the verified package.
set -eu
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077
stage='Read options'
fail() { printf '\nStopped during: %s\nReason: %s\n' "$stage" "$*" >&2; exit 1; }
step() { stage=$1; printf '\n[Download] %s\n' "$stage"; }
source=${MOLR_RELEASE_URL:-https://molr.ai/releases}
channel=stable
version=
archive=
requested_arch=
remaining=$#
while [ "$remaining" -gt 0 ]; do
 case "$1" in
  --source|--channel|--version|--archive|--arch)
   [ "$remaining" -ge 2 ] || fail "Missing value for $1"
   case "$1" in --source) source=$2;; --channel) channel=$2;; --version) version=$2;; --archive) archive=$2;; --arch) requested_arch=$2;; esac
   shift 2; remaining=$((remaining - 2));;
  --help|-h)
   printf '%s\n' 'Usage: sh install.sh [--source URL] [--channel stable|preview|nightly] [--version VERSION] [--archive FILE] [--arch amd64|arm64] [setup options]' 'Setup: --mode server|sandbox, --resource-preset permissive|least-privilege, --interactive | --non-interactive, --dry-run, --name, --user, --program-root, --install-deps, --directory-permissions ask|fix|ignore, --no-browser, --init-owner NAME (password from stdin) | --auto-init-admin' 'An extracted package can be installed directly with ./bin/molr-setup. Installed instances update with molr-setup update.'
   exit 0;;
  *) set -- "$@" "$1"; shift; remaining=$((remaining - 1));;
 esac
done
case "$channel" in stable|preview|nightly) ;; *) fail 'Invalid channel';; esac
case "$version" in *[!0-9A-Za-z.-]*|.*|-*) fail 'Invalid version';; esac
# BEGIN GENERATED PLATFORMS
case "$(uname -s)/$(uname -m)" in
  Linux/x86_64|Linux/amd64) os=linux; arch=amd64 ;;
  Linux/aarch64|Linux/arm64) os=linux; arch=arm64 ;;
  Darwin/arm64|Darwin/aarch64) os=darwin; arch=arm64 ;;
  Darwin/x86_64|Darwin/amd64) os=darwin; arch=amd64 ;;
  *) fail "Unsupported OS or architecture" ;;
esac
# END GENERATED PLATFORMS
[ -z "$requested_arch" ] || [ "$requested_arch" = "$arch" ] || fail "Selected architecture $requested_arch does not match this device ($arch)"
for tool in tar gzip mktemp chmod mkdir rm wc; do command -v "$tool" >/dev/null 2>&1 || fail "Missing bootstrap tool: $tool"; done
if command -v sha256sum >/dev/null 2>&1; then hash_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then hash_tool=shasum
else fail 'SHA-256 verification requires sha256sum or shasum'; fi
sha256() { if [ "$hash_tool" = sha256sum ]; then sha256sum "$1"; else shasum -a 256 "$1"; fi; }
temporary=$(mktemp -d "${TMPDIR:-/tmp}/molr-download.XXXXXXXX")
trap 'rm -rf "$temporary"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Package contents are public; the eventual service account must traverse them.
chmod 755 "$temporary"
source=${source%/}
fetch() {
 case "$1" in https://*) ;; *) fail 'Bootstrap downloads require HTTPS; use a local archive for offline installation';; esac
 if command -v curl >/dev/null 2>&1; then
  if [ -t 2 ]; then curl --fail --location --proto '=https' --proto-redir '=https' --connect-timeout 30 --max-time 1800 --progress-bar --output "$2" "$1"
  else curl --fail --location --proto '=https' --proto-redir '=https' --connect-timeout 30 --max-time 1800 --silent --show-error --output "$2" "$1"; fi
 elif command -v wget >/dev/null 2>&1; then
  wget --https-only --timeout=30 --tries=2 --progress=bar:force -O "$2" "$1"
 else fail 'Download needs an existing curl or wget; alternatively download and extract the package manually'; fi
}
if [ -n "$archive" ]; then
 step 'Read local archive and checksum'
 [ -f "$archive" ] && [ -f "$archive.sha256" ] || fail 'Archive and adjacent .sha256 file are required'
 IFS=' ' read -r expected rest < "$archive.sha256" || fail 'Invalid checksum file'
 printf 'Package: %s\nPlatform: %s/%s\n' "$archive" "$os" "$arch"
else
 step "Resolve release: ${version:-$channel}"
 descriptor="$source/$channel/download-$os-$arch.txt"
 [ -z "$version" ] || descriptor="$source/versions/$version/download-$os-$arch.txt"
 fetch "$descriptor" "$temporary/descriptor" || fail 'Cannot resolve release; check source, channel, version and connectivity'
 [ "$(wc -c < "$temporary/descriptor")" -le 8192 ] || fail 'Oversized download descriptor'
 # Fixed line protocol generated from the release manifest; never evaluated as shell code.
 { IFS= read -r schema; IFS= read -r selected; IFS= read -r commit; IFS= read -r platform; IFS= read -r filename; IFS= read -r url; IFS= read -r expected; ! IFS= read -r extra; } < "$temporary/descriptor" || fail 'Invalid download descriptor'
 [ "$schema" = molr-download-v1 ] && [ "$platform" = "$os-$arch" ] || fail 'Wrong descriptor platform/schema'
 case "$selected" in ''|*[!0-9A-Za-z.-]*|.*|-*) fail 'Invalid release version';; esac
 [ -z "$version" ] || [ "$version" = "$selected" ] || fail 'Version mismatch'
 [ "$filename" = "molr_${selected}_${os}_${arch}.tar.gz" ] || fail 'Invalid package name'
 printf 'Version: %s\nCommit: %s\nPlatform: %s/%s\nPackage: %s\nURL: %s\nSHA-256: %s\n' "$selected" "$commit" "$os" "$arch" "$filename" "$url" "$expected"
 archive="$temporary/package.tar.gz"
 step 'Download package'
 fetch "$url" "$archive" || fail 'Download failed; check connection, proxy and CA certificates'
 set -- "$@" --source "$source" --channel "$channel"
fi
step 'Verify checksum and extract package'
case "$expected" in *[!a-f0-9]*|'') fail 'Invalid SHA-256';; esac
[ "${#expected}" -eq 64 ] || fail 'Invalid SHA-256 length'
actual=$(sha256 "$archive")
actual=${actual%% *}
[ "$actual" = "$expected" ] || fail 'Checksum mismatch'
[ "$(wc -c < "$archive")" -le 536870912 ] || fail 'Archive exceeds size limit'
# Extract each allowlisted member to a fixed output path. Archive paths and link targets
# are never used for filesystem writes, including in malicious duplicate/link entries.
tar -tzf "$archive" > "$temporary/members" || fail 'Invalid archive'
count=0
while IFS= read -r member; do
 case "$member" in molr/LICENSE|molr/THIRD_PARTY_NOTICES.md|molr/release.json|molr/bin/molr|molr/bin/molr-setup) count=$((count + 1));; *) fail "Unexpected archive member: $member";; esac
done < "$temporary/members"
[ "$count" -eq 5 ] || fail 'Incomplete or duplicate archive members'
mkdir -m 755 "$temporary/molr" "$temporary/molr/bin"
for member in LICENSE THIRD_PARTY_NOTICES.md release.json bin/molr bin/molr-setup; do
 entry=$(tar -tvzf "$archive" "molr/$member") || fail "Missing $member"
 case "$entry" in -*) ;; *) fail "Non-regular package member: $member";; esac
 tar -xOzf "$archive" "molr/$member" > "$temporary/molr/$member" || fail "Cannot extract $member"
 chmod 644 "$temporary/molr/$member"
done
chmod 755 "$temporary/molr/bin/molr" "$temporary/molr/bin/molr-setup"
if [ "$os" = darwin ]; then
 for binary in molr molr-setup; do
  /usr/bin/codesign --verify --strict "$temporary/molr/bin/$binary" || fail 'Invalid executable code signature'
 done
fi
step 'Run package installer'
"$temporary/molr/bin/molr-setup" "$@"
