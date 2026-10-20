# Native macOS Command Permissions

Controlled macOS commands load a Seatbelt policy through `/usr/bin/sandbox-exec`, using the same native tools and assistant workspace as Full Access. Compute, file, and network permissions retain existing resource grants, group scopes, approvals, and expiry. Full Access remains independently authorized.

## Enable and authorize

The installer disables command execution by default. Set `sandbox.enabled: true`, restart, check compute readiness under Sandboxes, and grant compute access to the assistant. Controlled commands do not require `allow_full_access`.

```json
{
  "sandbox": {
    "enabled": true,
    "allow_full_access": false,
    "network_presets": ["network"],
    "tool_directories": ["/opt/homebrew", "/usr/local", "/Library/Developer/CommandLineTools"]
  }
}
```

`tool_directories` selects deployment-managed read-only tools; it does not install anything. Missing directories are skipped. Tool directories cannot contain Runtime private data, workspaces, or protected installation directories. Narrow the list to the tools required. The base policy provides the system shell, system libraries, and basic utilities.

| Permission | macOS behavior |
| --- | --- |
| Compute | Allows execution with a per-execution duration limit or unlimited duration |
| Files | Read/write access to the assistant workspace, read-only access to owner-shared files, and approved read or read/write external paths |
| Network access | One `preset:network` resource allowing TCP/UDP to any destination, DNS, and listening except on reserved browser control ports |
| Full Access | Requires deployment opt-in, group permission, a separate grant, and `full_access:true`; uses the service account's access |

Without a valid network grant, or with `network_mode:none`, controlled commands cannot use IP networking, bind, or listen. File access does not imply network access. Internet, LAN, or destination-specific scopes are never converted into unrestricted network grants. macOS provides no destination, traffic, or connection limits and uses no Molr network proxy. Arbitrary Unix sockets remain denied; only system communication required for DNS is opened.

Compute grants specify duration, with CPU, memory, process, and temporary disk fields set to zero. Network grants have no `constraints`. The UI presents node-specific options. CPU, memory, process, temporary disk, workspace capacity/file-count, network usage, and GPU controls are unsupported; submitting those settings or constraints is rejected. Existing assistant and user-group task concurrency limits remain enforced.

## File paths and tasks

Shell scripts use `$MOLR_WORKSPACE`, `$MOLR_GLOBAL`, `$TMPDIR`, and each resource's `ShellPath` from the permission catalog. File tools retain `/work`, `/global`, and `/resources/...`. The working-directory argument accepts `/work` and `/tmp` aliases; they are not shell mounts, and script text is never rewritten. External files use approved native paths. A task's temporary directory is removed when its managed process finishes.

Foreground, background, and TTY execution reuse existing task management. Controlled processes do not inherit Runtime credential variables, proxy variables, or extra file descriptors. TTY access is limited to the terminal device allocated for that task. Processes cannot read unrelated process arguments/environments or signal those processes, but macOS has no Linux process namespace.

File and network scopes are fixed at launch. Expiry, revocation, or relevant scope changes trigger reauthorization and termination of the managed process group. Newly granted access requires a new task. Descendants that create another session may survive; open files and connections do not have an object-level immediate revocation guarantee. Use Linux nodes for mandatory whole-process-tree cleanup or hard resource quotas.

File policies constrain direct path access. They do not hide all metadata or provide object isolation for pre-existing hard-link aliases. A network-enabled command can access host services; if a service exposes a file, Seatbelt does not reapply file permissions to the service's response.

## Port forwarding

Existing `forward_ports` creates authenticated HTTP/WebSocket relays to host `127.0.0.1:<port>`. Deployment opt-in and administrator checks still apply. Controlled commands also need compute and network grants; ordinary network-disabled calls cannot declare forwarded ports. Ports `39200–39263` are reserved for browser control; controlled commands cannot connect, listen or use general forwarding on these ports.

Declared ports select relay targets. They do not restrict actual listening interfaces or ports, or establish that the target belongs to the task. A network-enabled service can listen directly on the LAN, outside Molr relay authentication. Task completion or revocation closes Molr-managed relays and connections.

## Managed browser tasks

Local browsers support Apple Silicon and Intel macOS. Choose **Install and start** in the browser panel to prepare components; see [component management](../administration/components.md). No SIP changes, manual system configuration or screen-recording permission are required. For externally managed programs, the agent directory contains `agent-browser`; the Chromium directory contains the complete `Google Chrome for Testing.app`, not just its main executable.

Browsers run inside Molr's outer Seatbelt with Chromium's `--no-sandbox` flag. Chromium's internal renderer sandbox is disabled because macOS does not permit its Seatbelt to initialize within an effective outer Seatbelt. The isolation boundary is the whole browser task. Workspace, temporary files, authorized file scopes and network permission remain constrained by the outer policy; Linux process namespaces and hard resource quotas are unavailable.

Each task owns a pair of control ports and a private daemon socket. Ordinary controlled commands and other browser tasks cannot access those ports. WebUI commands and frames pass through Runtime authentication. Internal control remains available offline while page networking is denied. Full Access and other host processes are outside this control boundary.

Normal closure asks Chromium to flush and exit before reclaiming the controller. Authorization changes or a child crash close the browser task. If Runtime dies, pipe guardians terminate both managed process groups. Profiles persist per assistant; forced termination may lose data not yet written to disk.

## Verification and limitations

Startup probes verify workspace writes, denial of private-file access, and both network states. After success, authenticated `GET /api/sandbox` reports `Reason=available`, `Shell=true`, `FileIsolation=true`, `NetworkIsolation=true`, and `NetworkBackend=seatbelt-toggle`. `ProcessIsolation` and `ResourceLimits` remain false. Failed probes disable controlled execution without falling back to Full Access.

Run native regression tests from `runtime/` on a Mac:

```bash
CGO_ENABLED=0 go test ./internal/platform/sandbox ./internal/app ./internal/config -run 'TestSeatbelt|TestNativeSandbox' -v
```

Isolated audio/video decoding remains unsupported. Managed browsers run only on the local Runtime, not on remote nodes. Standalone macOS nodes use the same Seatbelt capabilities; see [remote sandbox deployment](remote.md#macos-nodes). Both Linux and macOS Runtimes can connect to either node type, with capabilities reported per node.

Browser regression tests use preinstalled pinned components and the current Runtime build:

```bash
MOLR_TEST_BROWSER_AGENT=/absolute/agent-directory \
MOLR_TEST_BROWSER_CHROMIUM=/absolute/chromium-directory \
MOLR_TEST_HELPER=/absolute/molr \
CGO_ENABLED=0 go test ./internal/platform/sandbox -run TestSeatbeltNativeBrowser -v
MOLR_SANDBOX_INTEGRATION=1 MOLR_TEST_AGENT_BROWSER=/absolute/agent-directory \
MOLR_TEST_CHROMIUM=/absolute/chromium-directory \
CGO_ENABLED=0 go test ./internal/app -run 'TestBrowserRealTask|TestBrowserProxyReal' -v
```
