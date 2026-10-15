#!/bin/sh
set -eu

if [ "${1:-}" != serve ]; then
    exec /usr/bin/setpriv --reuid=65532 --regid=65532 --clear-groups \
        --bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs \
        /usr/local/bin/molr "$@"
fi
if [ "$#" != 3 ] || [ "$2" != -config ] || [ "$3" != /etc/molr/server.json ]; then
    echo 'Isolated mode expects: serve -config /etc/molr/server.json' >&2
    exit 1
fi

# Never remount a host-wide hierarchy. Docker must give this container its own
# cgroup namespace, whose root contains only this container's processes.
if [ "$(cat /proc/1/cgroup)" != '0::/' ] || [ "$(stat -f -c %T /sys/fs/cgroup)" != cgroup2fs ]; then
    echo 'Isolated mode requires cgroup v2 and a private container cgroup namespace.' >&2
    exit 1
fi
mount -o remount,rw /sys/fs/cgroup
exec /usr/lib/systemd/systemd --system --unit=molr-container.target --log-target=console \
    --machine-id="$(tr -d '-' < /proc/sys/kernel/random/uuid)"
