#!/usr/bin/env bash
set -euo pipefail
# Use trusted system tools even when invoked through sudo with a custom PATH.
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PYTHONDONTWRITEBYTECODE=1
exec /usr/bin/python3 "$(dirname "$0")/install-linux.py" "$@"
