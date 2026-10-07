#!/usr/bin/env bash
set -euo pipefail
# Run inside a dedicated delegated service, never modify the caller's cgroup.
cd "$(dirname "$0")/.."
if [[ ${MOLR_SANDBOX_TEST_UNIT:-} != 1 ]]; then
  exec systemd-run --user --wait --pipe --collect --unit="molr-sandbox-test-$$" \
    -p Type=exec -p Delegate=yes -p DelegateSubgroup=supervisor -p KillMode=control-group \
    --working-directory="$PWD" \
    /usr/bin/env MOLR_SANDBOX_TEST_UNIT=1 PATH="$PATH" \
    MOLR_SANDBOX_PERMISSION_FIXTURES="${MOLR_SANDBOX_PERMISSION_FIXTURES:-}" \
    MOLR_SANDBOX_HOST_TOOLS="${MOLR_SANDBOX_HOST_TOOLS:-}" MOLR_SANDBOX_PUBLIC_SMOKE="${MOLR_SANDBOX_PUBLIC_SMOKE:-}" \
    CGO_ENABLED=0 GOTOOLCHAIN=local GOCACHE="${GOCACHE:-/tmp/molr-go-build-cache}" \
    GOMODCACHE="${GOMODCACHE:-/tmp/molr-go-mod-cache}" GOPATH="${GOPATH:-/tmp/molr-go-path}" \
    bash scripts/test-sandbox-linux.sh "$@"
fi
cg=$(cut -d: -f3 /proc/self/cgroup)
parent="/sys/fs/cgroup${cg%/supervisor}"
test "$parent" != /sys/fs/cgroup
test -f "$parent/cgroup.subtree_control"
printf '+cpu +memory +pids' > "$parent/cgroup.subtree_control"
export MOLR_SANDBOX_INTEGRATION=1 MOLR_SANDBOX_CGROUP="$parent"
exec go test -p 1 ./internal/platform/sandbox ./internal/app "$@"
