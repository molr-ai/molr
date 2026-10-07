#!/usr/bin/env bash
set -euo pipefail
# Requires a trusted, statically linked BusyBox (e.g. busybox-static).
destination=${1:?Usage: build-sandbox-rootfs.sh NEW_DIRECTORY [STATIC_BUSYBOX]}
busybox_binary=${2:-/usr/bin/busybox}
test ! -e "$destination"
file "$busybox_binary" | rg -q 'statically linked'
umask 022
mkdir -p "$destination"/{bin,usr/bin,etc,work,tmp,proc,dev,resources,molr}
: > "$destination/etc/resolv.conf"
install -m 0755 "$busybox_binary" "$destination/bin/busybox"
for applet in sh ls cat cp mv rm mkdir rmdir touch echo printf pwd test '[' head tail wc sort uniq cut tr sed awk grep find xargs chmod ln readlink realpath sleep date env timeout tar gzip gunzip unzip wget nc dd du df sha256sum; do
  ln -s busybox "$destination/bin/$applet"
done
if test -f /etc/ssl/certs/ca-certificates.crt; then
  mkdir -p "$destination/etc/ssl/certs"
  install -m 0644 /etc/ssl/certs/ca-certificates.crt "$destination/etc/ssl/certs/ca-certificates.crt"
fi
