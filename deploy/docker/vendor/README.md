# Container seccomp source

`moby-seccomp.json` is the unmodified Docker default seccomp policy from
[moby/profiles, commit 4b8214fc4971e06172938a1f1554ccff6647ee80](https://github.com/moby/profiles/blob/4b8214fc4971e06172938a1f1554ccff6647ee80/seccomp/default.json).
Copyright The Moby Authors; Apache-2.0, with the full license in `LICENSE.moby`.

`../seccomp-isolated.json` retains this policy and adds three allow rules conditional on `CAP_SYS_ADMIN`. Bubblewrap needs
`pivot_root` inside its child mount/user namespace. The trusted socket broker
needs `pidfd_getfd` with zero flags and `process_vm_readv` with one local and
remote iovec and zero flags. Guest seccomp continues to deny all three syscalls.
The container's Molr systemd service
drops all capabilities before accepting application requests.

When updating the upstream policy, review its changes, retain the license,
regenerate the derived file, and run the real container isolation tests.
