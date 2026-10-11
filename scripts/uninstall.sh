#!/usr/bin/env bash
set -euo pipefail
exec /bin/bash "$(dirname "$0")/../runtime/scripts/uninstall-linux.sh" "$@"
