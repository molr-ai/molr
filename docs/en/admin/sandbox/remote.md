# Standalone Sandbox Service

Runtime includes its local sandbox_manager. In the main Runtime, `sandbox.enabled: false` disables local command execution while workspaces, file tools, and remote connections remain available. In standalone `molr-sandbox`, the same switch still controls the whole remote node. Remote nodes support Linux and macOS. File operations need no compute grant but retain workspace ownership and directory authorization checks.

Remote computers run `molr-sandbox`, providing only controlled execution and authorized directories. They do not initialize accounts, Agents, sessions, assistant workspaces, or model services. The assistant and its unique `/work` always reside locally within the Runtime. Nodes reuse their platform file tools and execution backend: Bubblewrap on Linux and Seatbelt on macOS; the common protocol does not include Bubblewrap or cgroup paths.

This page is intended for deployers. After the service starts, administrators [connect to the sandbox](../resources/sandbox.md#connect-in-the-webui), and members use resources allowed by their user groups and authorize their own assistants.

Remote nodes also support on-demand [Full Access permissions](linux.md#granting-full-access-on-demand): configure `sandbox.allow_full_access: true` on the remote side, then grant the assistant Full Access compute permissions for that node. Local configuration does not enable this capability on the remote side. Standard isolation and explicit Full Access calls can coexist; the latter directly uses the remote service's system identity and environment.

The current Sandbox capability protocol is version 8, with the transport path `/v2/sandbox/`; the Runtime and remote node versions must match. Linux network configuration explicitly declares TCP/UDP, and rules take effect on the execution machine. See [Network Configuration](linux.md#network-and-advanced-deployment).

## Build and Startup

Use the Go version specified by `runtime/.go-version` in the deployment checkout directory:

```bash
mkdir -p .cache/build
cd runtime
CGO_ENABLED=0 go build -o ../.cache/build/molr-sandbox ./cmd/molr-sandbox
chmod 0755 ../.cache/build/molr-sandbox
file ../.cache/build/molr-sandbox
go version -m ../.cache/build/molr-sandbox
```

Verify a static Linux ELF or macOS Mach-O, `CGO_ENABLED=0`, and `0755` permissions. On macOS, run `codesign --force --sign - <binary>` and verify with `codesign --verify --strict <binary>`. On Linux, like the Runtime, this requires Bubblewrap, static BusyBox, user namespaces, and cgroup v2 delegation. For complete requirements, see [Linux Sandbox Deployment](linux.md).

Prepare a separate private data directory, for example `/srv/molr-sandbox/private`. The TLS private key and service token must be placed within this directory, must not contain symbolic links, and the token file permissions must be `0600`. Use a randomly generated token of at least 32 characters; do not write the token into startup parameters or commit it to Git. You can use a trusted CA or self-signed TLS certificates; explicitly verify the certificate SHA-256 fingerprint upon connection, and do not skip verification.

The service uses the Runtime's strict configuration format. For example:

```json
{
  "data_dir": "/srv/molr-sandbox/private",
  "http": {
    "listen": "0.0.0.0:8443",
    "public_url": "https://sandbox.example.com:8443",
    "tls_mode": "direct",
    "cert_file": "/srv/molr-sandbox/private/server.crt",
    "key_file": "/srv/molr-sandbox/private/server.key"
  },
  "sandbox": {
    "enabled": true,
    "restrict_directories": true,
    "directories": [
      {"path": "/srv/projects", "read_only": false},
      {"path": "/srv/reference", "read_only": true}
    ],
    "networks": [
      {"name": "Git service", "protocols": ["tcp"], "addresses": ["git.example.com"], "ports": ["443"]}
    ]
  }
}
```

The remote service always exposes only the directories in `directories`; `restrict_directories: false` does not expand the scope of the remote protocol. Actual paths remain only on the server side, and the Runtime references opaque directory IDs. The private data directory, workspaces, and allowed external directories must not overlap.

Use a separate systemd user service:

```ini
[Service]
Type=exec
ExecStart=/path/molr-sandbox -config /private/sandbox.json -token-file /srv/molr-sandbox/private/service.token
Delegate=yes
DelegateSubgroup=supervisor
KillMode=control-group
Restart=on-failure
```

Service names use the `molr-` prefix, and each instance uses a different unit, port, and data directory. Startup logs output the certificate fingerprint but do not output the service token. Currently, the credentials for each standalone service trust one Runtime; do not give the same service credentials to Runtimes that do not trust each other. Run isolated instances when different trust domains are required.

## macOS nodes

macOS uses the same `molr-sandbox -config … -token-file …` command, TLS and token authentication. Standalone nodes currently require a source build and are not managed by `molr-setup`. Build on the target Mac and validate ARM64 and AMD64 natively.

Use Mac absolute paths for `data_dir`, certificates, private keys, tokens and directories. Replace the example's `sandbox` section with:

```json
{
  "enabled": true,
  "allow_full_access": false,
  "directories": [
    {"path": "/Users/example/Projects", "read_only": false}
  ],
  "network_presets": ["network"],
  "tool_directories": ["/opt/homebrew", "/usr/local", "/Library/Developer/CommandLineTools"]
}
```

Do not configure Linux `networks`, Bubblewrap, cgroups or hard quotas. File, network, process cleanup and forwarding boundaries follow [native macOS command permissions](macos.md). Compute grants control duration only; network access is a separate toggle without quotas. Network expiry, revocation or scope changes stop managed tasks; connection loss remains subject to the 15-second lease. Full Access requires its deployment switch and separate grant.

Remote nodes create no assistant workspace or shared directory and expose neither `$MOLR_WORKSPACE` nor `$MOLR_GLOBAL`. For controlled commands, `HOME` and `TMPDIR` are private per-execution temporary directories. File tools continue using directory resource IDs and relative paths. For Shell, use `ShellEnvironment` from the permission list: request its exact `ResourceID` and `Path` in `files`, then quote the corresponding variable expansion in scripts. For a variable named `MOLR_RESOURCE_<digest>`, use `"$MOLR_RESOURCE_<digest>/file.txt"`, replacing the placeholder with the complete returned name. The node injects actual paths; directory discovery does not publish them.

`directory` accepts `/tmp` or the `/resources/<ResourceID>/<Path>` working-directory alias. Aliases are only resolved for the working directory, not mounted for scripts. Script text is never rewritten. Use the variable for the authorized subtree; unrequested directories are not injected.

Manage the node with a dedicated launchd configuration. Replace every path and create the log directory first:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>ai.molr.remote-sandbox</string>
  <key>ProgramArguments</key><array>
    <string>/Users/example/molr/bin/molr-sandbox</string>
    <string>-config</string><string>/Users/example/molr/private/sandbox.json</string>
    <string>-token-file</string><string>/Users/example/molr/private/service.token</string>
  </array>
  <key>WorkingDirectory</key><string>/Users/example/molr</string>
  <key>LimitLoadToSessionType</key><array><string>Aqua</string><string>Background</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>StandardOutPath</key><string>/Users/example/molr/private/stdout.log</string>
  <key>StandardErrorPath</key><string>/Users/example/molr/private/stderr.log</string>
</dict></plist>
```

Save as `~/Library/LaunchAgents/ai.molr.remote-sandbox.plist` and load into the current login session with `launchctl bootstrap gui/$(id -u) <absolute-plist-path>`. For an SSH-only session use the `user/$(id -u)` domain. This deployment depends on the user session. Logout, sleep and service restarts do not guarantee continued execution. Query the original operation after recovery; never automatically replay commands.

After startup inspect this node through the Runtime's `/api/sandboxes`: `Storage=true`, `Reason=available`, `Shell`, `FileIsolation` and `NetworkIsolation` true, `NetworkBackend=seatbelt-toggle`, and `ProcessIsolation` and `ResourceLimits` false. On standalone nodes, `enabled:false` disables both execution and storage.

## Protocol, Recovery, and Limitations

The Runtime and service use the `/v2/sandbox/*` HTTPS protocol, with certificate fingerprint pinning for trust targets and service tokens for caller verification. JSON endpoints are `info`, `submit`, `get`, `renew`, `cancel`, `revoke`, and `read`. TCP forwarding additionally uses an authenticated Upgrade endpoint `forward`; browser Origin requests are rejected, and these interfaces are not mounted to the Runtime's user API.

- File editing, Shell, and attachment imports record a stable operation ID, identity, and parameter summary before execution; duplicate requests return existing state, and parameter conflicts are rejected.
- The Runtime saves the target sandbox and original execution identity; upon network anomalies, it first queries the result and does not automatically generate new operations for retry. The two ends have independent databases and do not share the Runtime SQLite.
- Running operations hold a 15-second authorization lease. The Runtime re-validates authorization and renews the lease approximately every 3 seconds. Revoking authorization increases the authorization version and sends a termination notification; if connection is lost, the lease expires and terminates the operation. Instant revocation is not guaranteed under network partitions.
- If saving execution results fails, the service retains the result and retries saving with backoff, without re-executing the operation; it continues to occupy an execution slot until saving succeeds. Closing stops retries and reports unsaved errors. If the result exceeds the persistent record limit, a clear `unknown` status is saved, and it is not automatically replayed.
- Linux cancellation stops the process tree; macOS cancellation targets the managed process group and cannot guarantee cleanup of detached descendants but does not roll back written files. Unfinished operations after a service restart are marked `unknown`, without guessing success or automatically replaying.
- `/read` only allows file downloads, output segmentation, and media snapshot reads; large temporary content is not written to the operation ledger. Attachment imports maintain the existing 20 MiB limit; media uses existing read limits.
- Shell supports foreground, background, and TTY, declaring `ShellSessions` capabilities; output archiving and control follow `shell_read` / `shell_control`. Port forwarding requires `PortForwarding=true`, providing TCP addresses uniformly through the Runtime; the remote side only needs the original TLS service port. See [Shell Port Forwarding](../../user/guides/files-and-commands.md#shell-port-forwarding) for details.
- File operations have version checks and atomic replacements, but writes by arbitrary Shell/external processes are not part of the same transaction; they cannot be treated as distributed file CAS.

Connection tokens are encrypted and stored in the Runtime's private database; pages and models can only read connection metadata. Connection management, resource addition, and connection changes are audited. When backing up the remote, you must also save its own database, authorized directories, and private configuration; the Runtime's backup does not include remote authorized directories and operation records.

After startup, upgrade, or restart, you must authenticate and check the Runtime `/api/sandbox` and the capabilities of remote instances in `/api/sandboxes`. Linux compute readiness requires `Reason=available`, and Shell, process, file, network isolation, and resource limits must all be true; HTTP connectivity cannot replace isolation verification.

For platform differences and the unimplemented Windows backend, see [Sandbox Design](../../dev/security/sandbox.md).

## File and Execution Lifecycle

Standard remote Shell starts from the `/tmp` of the current command by default; persistent files can only be written to explicitly mounted authorized directories. Remote file tools must specify `sandbox_id` and directory `resource_id`; Full Access calls continue to check authorization and use remote absolute paths, with no `/work` alias. CLI local installation uses the assistant's `/work`; remote execution still requires explicit selection of remote resources and directories.

`file_transfer` transfers a single file between the local side and one explicit remote node, supporting binary, with a maximum of 20 MiB per transfer. Source and target are authorized independently, and the parent directory must exist; `overwrite` must be specified, and overwriting also requires the target `expected_version`. Direct transfer between remotes, directory synchronization, or automatic overwriting is not supported. Download to local first, then use for chat attachments or assistant avatars.

Remote Shell stdout/stderr is archived to local `/work/.local/state/molr/tool-shell-*.txt` via segmented reading. The execution node is identified by `SandboxID`, and the output node is `OutputSandboxID=local`; do not use the execution node to read local logs. After disconnection, read logs according to the original task without re-executing commands. If log recycling is incomplete, `OutputComplete=false`.

Remote internal output buffers are saved per operation and are not opened to the assistant as a storage directory. Each Shell output is at most 64 MiB (also limited by the deployment file limit), and at most 128 operation directories are retained; non-running directories are cleaned up before the next execution if they are older than seven days; when the entry limit is reached, the oldest non-running directories are evicted first; new commands are rejected if all are running. Receipts are still saved independently; log expiration does not mean the command was not executed. Media snapshots remain a bounded private cache.

Remote disconnection only affects the corresponding call. Related tasks can wait for the environment to recover. After the Runtime detects recovery, it submits continuation events for still-valid waiting tasks; canceled, paused, dismissed, or user-stopped work does not automatically recover. Recovery checks saved operation results and does not replay unknown side effects. If the local file environment is unavailable, the scheduler stops claiming new events, and messages and tasks are retained; a separate Shell capability failure does not equal local file environment offline.

After deployment, administrators test connections, add resources, and assign member scopes according to [Connect to Remote Sandbox](../resources/sandbox.md#connect-in-the-webui).
