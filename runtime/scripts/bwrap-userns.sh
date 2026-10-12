#!/bin/sh
# Deployment-only launcher for hosts with AppArmor user-namespace restrictions.
# Install/load configs/apparmor/molr-bwrap first; never fall back on failure.
set -eu
exec /usr/bin/aa-exec -p molr-bwrap -- /usr/bin/bwrap "$@"
