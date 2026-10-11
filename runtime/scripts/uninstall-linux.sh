#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PYTHONDONTWRITEBYTECODE=1
exec /usr/bin/python3 "$(dirname "$0")/uninstall-linux.py" "$@"
