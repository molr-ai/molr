# Linux Sandbox Deployment

This page is for deployers, explaining the dependencies, resource boundaries, and verification methods required for Linux command isolation. For daily operations, see [Files and Commands](../../user/guides/files-and-commands.md). For administrator integration, see [Sandbox Resources](../resources/sandbox.md).

## Installation and Startup

`molr-setup` installs a user service by default; root can use `--user USER` for a system service. Runtime and sandbox use the same regular account. `--install-deps` authorizes system dependency preparation. See [installation](../deployment/installation.md#selecting-the-runtime-account-during-installation).

The current automatic installation path targets Linux systems with systemd 254+. Starting via standard commands requires an accessible systemd user manager, while installing as a service includes built-in execution resource delegation. On systems or restricted containers that do not support these conditions, features like chat still run, but command execution displays that the current system is unavailable, without degrading to non-isolated execution. Ordinary Docker images do not provide in-container isolation; commands require explicitly enabled and authorized Full Access. See [Docker deployment](../deployment/docker.md).

Builds use the Go version specified in [runtime/.go-version](../../../../runtime/.go-version). Static binaries are generated in the `runtime/` directory so that the same server can run internal helpers within minimal environments:

```bash
CGO_ENABLED=0 GOTOOLCHAIN="go$(cat .go-version)" go build -o /path/molr ./cmd/molr
chmod 0755 /path/molr
file /path/molr
go version -m /path/molr
```

Check that the final binary is statically linked, build information is `CGO_ENABLED=0`, and permissions are `0755`. Start via a systemd user or system service with delegation, setting `Type=exec`, `Delegate=yes`, `DelegateSubgroup=supervisor`, and `KillMode=control-group`. For complete startup and verification steps, see [Installation and Service Management](../deployment/installation.md). The installation script defaults to `molr.service`. For multi-instance acceptance testing, use `--name molr-<名称>` and assign independent ports, configurations, and data directories.

Normal configurations can completely omit the `sandbox` object. `configs/sandbox.example.json` only shows optional execution limits and no longer requires filling in paths.

## What Runtime Handles Automatically

- The base environment is constructed from the distribution package's `libexec/busybox` or an installed static BusyBox, including common Shell commands and CA certificates. It is cached by content version and published atomically. It copies small runtime assets, does not copy user directories, and does not scan user files before each call.
- Workspaces are automatically saved in the data directory managed by Runtime. Each owner/assistant uses an independent directory, displayed as `/work` within the sandbox. External resource directories are mounted directly to stable `ShellPath` according to authorization. Writes take effect immediately; failures or cancellations do not roll back.
- When the installed service already provides system resource delegation, Runtime automatically discovers and uses it. For standard command startup, it automatically creates temporary Molr systemd user units, without requiring users to find or fill in cgroup paths. A dedicated supervisor process survives via connection leases. The entire group stops when Runtime exits or the connection is lost.
- At startup, it executes isolation probes without external directory authorization, as well as transparent network probes using temporary local endpoints (not relying on the public internet). Readiness is displayed only if probes succeed. File existence or configuration enablement does not equal environment availability.
- Local compute resources are automatically registered once, preserving existing names, disconnection states, and authorizations. Assistants still need to obtain compute `execute`. File and network permissions are verified separately.

The minimal environment provides only BusyBox and does not promise full HTTPS download capabilities. Host tool mode reuses system programs like curl and Python according to the permissions of the Runtime service account, as described below. `molr-fetch` is no longer provided.

## GPU Compute Permissions

GPU is an optional capability of compute resources and is disabled by default. Currently, it supports Linux NVIDIA full-card CUDA compute. It requires the service account to have access to the specified GPU, the host to have matching NVIDIA drivers and `/usr/bin/python3` installed, and the use of host tool mode. Molr does not install drivers, does not run as root, and does not connect to the NVIDIA management socket.

Deployers first use `nvidia-smi --query-gpu=uuid,name --format=csv` to view hardware UUIDs, then explicitly configure the devices allowed to be exposed:

```json
{
  "sandbox": {
    "host_tools": true,
    "gpu": {
      "enabled": true,
      "devices": ["GPU-00000000-0000-0000-0000-000000000001"]
    }
  }
}
```

Example UUIDs must be replaced with actual hardware UUIDs. Adding or replacing devices does not automatically expand existing authorizations. After restarting, check GPU status in "Resources → Sandbox → Compute". Startup probes verify CUDA contexts, memory read/write, and in-execution IPC within the isolated environment. GPU unavailability does not affect normal compute without GPU authorization.

Administrators assign device scopes according to [Sandbox Resource Management](../resources/sandbox.md), and users then [approve assistant GPU permissions](../../user/guides/permissions-and-budget.md#gpu-authorization). Replacing devices does not expand existing authorizations. Changes to compute authorization stop affected tasks.

GPU access is shared. It does not reserve memory, guarantee compute power, or increase the quota for the number of GPU tasks. Normal executions continue to count towards existing concurrency limits. The total memory displayed in the interface is hardware information. Full Access accesses the system with original direct execution semantics and is not constrained by normal GPU authorization scopes.

GPU execution still retains file, process, and network isolation. The abstract Unix SEQPACKET communication required by CUDA is limited to the current execution's network namespace. Pathname Unix sockets are allowed only inside the execution’s private `/tmp`; host paths remain denied. If GPU probes or device preparation fail, it does not automatically fall back to CPU or Full Access. Check device reason codes, fix issues and restart to re-probe, or remove GPU from compute permissions to continue normal compute.

Common causes include `gpu_host_tools_required` (host tool mode not enabled), `gpu_driver_unavailable` (driver or nvidia-smi unavailable), `gpu_device_missing` (configured UUID does not exist), `gpu_probe_failed` (CUDA/IPC isolation probe failed), `gpu_device_changed` (hardware mapping changed), and `gpu_scope_denied` (exceeds deployment or user group scope).

## Granting Full Access On Demand

The same node can handle both normal isolated calls and Full Access calls simultaneously. Full Access is a permission type parallel to storage, compute, and network under the sandbox, and is not open by default. It is not a node running mode and is not automatically enabled upon isolation failure.

Deployers enable it in `molr` or `molr serve --mode sandbox` configurations:

```json
{ "sandbox": { "enabled": true, "allow_full_access": true } }
```

After restarting, the sandbox page displays "Full Access Authorizable" and provides four tabs: "Storage/Compute/Network/Full Access". Regular members also require administrators to allow granting Full Access in the "Permissions and Quotas" of the corresponding sandbox for the user group. In the assistant authorization form, select "Resource Type: Sandbox" → "Select Sandbox" → "Permission Type: Full Access" to authorize. This authorization has no compute, network, storage, or concurrency quotas. The form does not display quota settings, but authorization expiration time can still be set and revoked at any time. Assistants can also initiate Full Access permission requests for user approval.

Normal compute authorizations and Full Access authorizations can be retained separately. Assistants use Full Access only when explicitly passing `full_access: true` in file or command tools. Other calls continue to use original isolation and permissions. Full Access authorization does not replace normal compute authorization. Revoking Full Access does not stop simultaneously running normal isolated tasks.

Full Access directly executes `/bin/sh` with the system identity of the service process, inheriting its environment variables, and directly accessing files, networks, and processes in the environment. Molr does not provide isolation or resource limits, nor does it require additional directory or network authorizations. It is designed for scenarios where the service itself runs in a container or virtual machine, with access scope determined by the external environment and system account. Scripts use real paths or `$MOLR_WORKSPACE`. File tools still accept `/work` aliases and can also use absolute paths. Normal isolated calls still use the `/work`, temporary directories, egress, and quota rules described below.

Full Access does not require Bubblewrap, rootfs, or cgroup delegation. If these conditions are missing, only normal isolation capabilities are unavailable. `Capabilities.FullAccess` independently reports direct execution capability and does not indicate that other isolation capabilities are ready. The installer allows missing isolation dependencies for instances configured with `allow_full_access: true`. If both call types need to be available simultaneously, all isolation conditions must still be met.

## Default Limits and Capacity Semantics

| Limit | Default Value | Implementation |
| --- | --- | --- |
| Foreground Wait | Max 60 seconds | Automatically moves to background if not finished, retaining original task ID |
| Task Runtime | No default deadline | Optional `timeout_seconds` (1–604800 seconds). Authorization expiration, revocation, and resource limits still stop execution |
| Concurrent Executions per Assistant | 10 | Total of foreground, background, and TTY. Counted uniformly across local and remote sandboxes |
| Memory / Swap | 512 MiB / 0 | cgroup hard limit |
| CPU | 100%, i.e., time quota for one CPU | cgroup quota |
| Total Processes and Threads | 64 | cgroup PID limit |
| Temporary Space | 64 MiB | tmpfs size limit per execution |
| Single File Size | 1 GiB | RLIMIT_FSIZE hard limit that cannot be increased by scripts |
| Disk Free Space | At least 256 MiB | Checked before startup. Low water mark during runtime triggers stop |
| Output | Total 64 KiB | Bounded stdout/stderr |

Normal working directories and externally authorized directories can use existing filesystems. It no longer checks if the entire filesystem is smaller than a certain capacity, nor does it require being on a different filesystem from Runtime.

**Single file hard limits and disk low water mark protection are not hard quotas for total directory capacity.** Multi-file writes or concurrent external writes may cross the low water mark within monitoring intervals. Runtime does not promise to strictly reserve these remaining bytes. Deployments requiring hard quotas for total directory capacity should use filesystem quotas separately, rather than relying on the protections here.

Runtime does not count the total file changes per execution, nor does it traverse large directories. Artifacts are read via the file interface by explicit reference. The workbench displays commands, output, and exit classifications, but does not provide a complete file change list.

## Permissions and Boundaries

Mount sources are resolved by resource ID and authorized relative paths. It does not accept host paths or isolation switches submitted by the model. Read-only mounts require `read`. Read-write mounts require both `read` and `write`. Write-only permissions continue to use controlled file tools. Runtime private directories and their parents, kernel pseudo-filesystems, and unsupported nested mounts are still refused exposure. Allowing normal same-filesystem directories does not mean allowing direct reading of Runtime data.

Direct directory authorization covers actual file objects within the directory, following normal hard link semantics. If a user previously hard-linked a file to an authorized directory, its content is also within that directory's authorization. Shell cannot use unmounted host paths to create new links pointing to private data. Existing per-file tools retain stricter link checks. Do not conflate the two access models.

Original Bubblewrap, seccomp, capability removal, minimal device set, empty environment, and FD isolation remain in effect. Shell prohibits host Unix sockets, VSOCK, Netlink, mount namespace creation, joining namespaces, and related bypass system calls. GPU provides controlled device access according to compute authorization. Other hardware passthrough and host software installation are not provided. Specified Shell ports can be accessed via [HTTP/WS Forwarding](../../user/guides/files-and-commands.md#shell-port-forwarding).

Authorization updates/revocations, resource disconnections, and startups are coordinated serially. It waits for related executions to terminate before returning success. Currently, it conservatively stops all active Shells for that owner. Authorization expiration sets a deadline. Other execution conditions and disk space are re-checked periodically. Cancellation covers child processes that create new sessions. Unknown results are not automatically rerun.

## Network and Advanced Deployment

Shell has no external network by default. After obtaining network resource authorization, Runtime automatically selects the valid authorization for the current assistant in the same sandbox. `network_mode: "none"` can disable outbound networking for this instance. When networking is allowed, Runtime creates an independent user/net namespace and TUN network card for this execution, automatically preparing internal IP, routing, and DNS. Programs use normal TCP/UDP sockets without needing environment variables like `HTTP_PROXY`. Network cards, routing, firewalls, DNS, and IP forwarding in the host's default network namespace are not modified. Normal operation does not require host root. Installing system dependencies for the first time or when the system lacks TUN/user namespace support requires resolution by the installation process or administrators. Running Runtime does not call sudo or relax global security policies.

Network namespaces are managed by the host kernel. Host administrators can still inspect them. "Isolation" does not mean invisible to the host. Runtime's network helper processes use the host's existing network to create normal outbound connections. The trusted initialization process has management permissions only within the execution-specific user/network namespace. The final script still has no capabilities and retains seccomp. Nested user/PID/network namespaces may only further isolate execution; mount namespaces and joining other namespaces remain prohibited.

### Built-in Optional Presets

New configurations provide the following candidates by default. Users add them in the WebUI under "Resources → Sandbox → Network" and then authorize assistants to use them. The appearance of candidates or adding resources does not mean authorization has been granted.

| Preset ID | Interface Name | Scope |
| --- | --- | --- |
| `internet` | Internet Access | `0.0.0.0/0`, `::/0`, excluding the following private, loopback, and internal IPv4 mapped segments. TCP/UDP `1–65535` ports |
| `lan` | LAN Access | `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`. TCP/UDP `1–65535` ports, excluding loopback |
| `host` | Host Access | `127.0.0.0/8`, `::1/128`. TCP/UDP `1–65535` ports, including Molr's own services |

The complete exclusion list for internet access is `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`, `127.0.0.0/8`, `::1/128`, `198.18.0.0/15`. The shared address `100.64.0.0/10` does not belong to the RFC1918/ULA LAN classification here and is categorized under internet access. LAN access can connect to services hosted on the host machine's LAN address; host access corresponds only to loopback addresses.

The sandbox network gateway operates at Layer 4 (transport layer) and supports only TCP/UDP protocols. Protocols outside this scope (such as ICMP) are not forwarded. All three presets open all valid TCP/UDP ports. All presets still prohibit unspecified, multicast, link-local, zone-scoped addresses, and metadata addresses `100.100.100.200`, `168.63.129.16`, `fd00:ec2::254`. `198.18.0.0/15` and `fd00:6d6f:6c72:ffff::/64` are used for internal DNS virtual mapping and are not directly accessible real target subnets. Traffic and concurrent connections are enforced according to assistant authorization and are limited by deployment capability boundaries.

`localhost` always belongs to the sandbox itself. Accessing loopback services on the machine hosting the sandbox uses `host.molr.internal` (127.0.0.1) or `host6.molr.internal` (::1), and still requires authorization for the corresponding address, protocol, and port. Host services for remote sandboxes refer to the remote machine.

Deployers can restrict candidate presets and can also provide explicit custom rules. Ordinary users do not need to edit internal subnets, routes, network cards, or DNS:

```json
{
  "sandbox": {
    "network_presets": ["internet", "lan", "host"],
    "network_bytes": 67108864,
    "network_connections": 16,
    "networks": [
      {
        "name": "内部接口",
        "protocols": ["tcp"],
        "addresses": ["api.example.com", "192.168.1.0/24"],
        "ports": ["443", "8000-8999"],
        "exclude_addresses": ["192.168.1.10"],
        "exclude_ports": ["8080-8089"]
      }
    ]
  }
}
```

`network_presets: []` removes all built-in candidates; setting `networks: []` simultaneously completely disables network resources. When preset configuration is omitted, the three presets mentioned above are used. The `preset:` prefix is used by built-in resources; do not create custom rules with the same name. After modifying the deployment scope, restart. The WebUI can connect to and authorize candidate resources but cannot expand the deployment boundary.

Each custom rule must explicitly declare `protocols` and accepts only non-duplicate `tcp` / `udp`. Loading is rejected if the protocol is missing or the configuration is incorrect; it is not automatically replaced with a broad preset.

Addresses support exact domain names, IPs, and standard CIDRs; ports support single ports and closed intervals, all within 1–65535. Allow sets support 1–32 items each, and exclude sets support 0–32 items each. The protocol, address, and port of a single connection target must match the same rule; multiple authorized rules take the union. When domain name resolution returns multiple addresses, each address must match at least one rule (which can be covered by different rules respectively); if any address does not match, the entire connection is rejected. Exclusions take precedence over the allowances of the belonging rule. Note: **Hostname entries in the exclusion set only take effect on the domain name resolution path**—when connecting directly via IP, hostname exclusions do not match, and only IP/CIDR entries are checked; to block all entry points for a domain, its address range should also be excluded.

### DNS, Limits, and Revocation

DNS services are provided only within the execution, supporting UDP/TCP A/AAAA queries, and calling the system resolver to handle real domain names and CNAMEs. The sandbox receives execution-specific virtual IPs; each time a connection is established, the domain name is re-resolved, all real addresses are checked, and then the checked IPs are dialed directly, avoiding shared IPs or DNS rebinding bypassing authorization. When a domain name is excluded or unauthorized, unnecessary external queries are not performed. IP/CIDR rules require resolution to determine the target scope. The record TTL is 30 seconds, and virtual addresses are valid only within the current execution and should not be saved to the workspace for reuse in subsequent executions.

Authorization is granted for target hosts/addresses, ports, and transport protocols; it does not check URL paths, HTTP methods, or request content within TLS. Programs granted public network TCP/UDP permissions can run other approved protocols; there is no separate commitment to browser rendering capabilities, and raw IPs or multicast are not provided. Shell TCP port forwarding is independent of outbound network authorization; see [Forwarding Listen Addresses](#forwarding-listen-addresses).

Outbound connections/UDP sessions and bidirectional forwarding payloads are executed according to the independent quotas approved by the assistant (including TLS and DNS data, not line-rate IP packet billing); unauthorized access without quotas is no longer retained. DNS uses independent 8 concurrent processing slots and a maximum of 1024 domain/address family mappings to avoid occupying program connection quotas. UDP idle states are reclaimed after 60 seconds. Network helper processes are separately protected by cgroup limits of 256 MiB memory, 64 processes/threads, and one CPU; failure terminates the corresponding execution and does not fall back to direct host execution.

Each new connection and DNS request re-checks authorization, and the Runtime continuously checks the validity period of the authorization. Revocation, disconnection, cancellation, or expiration ends the execution and reclaims helper processes, TUN, and cgroup; existing TCP/UDP connections are also closed. Tool results clearly report target rejection, DNS/connection failure, traffic exhaustion, concurrency limits, or gateway failure; failed commands may still have produced side effects.

Advanced deployments can explicitly set `sandbox.restrict_directories: true`, in which case `directories` defines allowed roots including their subdirectories; when the default is false, existing storage resources and assistant authorization determine the directory scope. Entries explicitly set to read-only in `directories` still restrict writes. Network target scope, path coverage, and host protection boundaries are still determined by deployment configuration. Assistant compute, network, and workspace quotas are approved by the user within this boundary; the WebUI cannot expand the deployment boundary.

## Read-Only Reuse of Host Tools

No Debian tool image is required, and `/usr` or personal software environments are not copied. Whether host tools are shared is configured separately from the service running account:

```json
{
  "sandbox": {
    "enabled": true,
    "host_tools": true
  }
}
```

The sandbox inherits the Runtime's host identity. The installer defaults to the current user; after selecting an account via `sudo … --user USER`, the entire Runtime's file operations, background tasks, and sandbox use that user. Existing accounts retain their group permissions; newly created accounts have no additional groups. See [Selecting the Running Account During Installation](../deployment/installation.md#selecting-the-runtime-account-during-installation) for installation parameters and the automatic account creation process. Local execution needs no additional sandbox node.

When `host_tools: false` is set or omitted, directly started Runtimes retain a minimal BusyBox environment; the installer defaults to enabling host tools during the first installation when there is no custom rootfs. The service account is determined by deployment and cannot be switched via sandbox configuration.

Host tool mode rejects UID 0 or mismatched real/effective identities, maintaining read-only system mounts, file scope, network authorization, seccomp, no_new_privs, and resource limits. Private files within the mounted scope that the current account has permission to read are still readable; 0600 cannot isolate the Runtime and sandbox with the same UID; databases, keys, etc., must continue to be excluded from sandbox mounts. Personal HOME, full `/etc`, management sockets, and Runtime private data are not automatically shared. Startup is rejected if the private data directory or total workspace overlaps with shared system directories.

The shared scope includes `/usr`, the host's actual `/bin`, `/sbin`, `/lib*`, and necessary alternatives, fonts, ImageMagick, OpenSSL, and dynamic loader configurations; all are read-only, retaining the host's actual DAC/ACL permissions. Publicly readable data in directories will also be visible; "read-only" should not be understood as confidentiality. Host HOME, `/opt`, `/run`, full `/etc`, and Runtime private directories are not shared; software in locations such as Conda, user PATH, and Snap is not guaranteed to be available. Mount sources undergo fixed descriptor and nested mount checks, and sources cannot be injected by modifying the user PATH.

`/work` and authorized external resources are separate file scopes. Resource authorization does not bypass the operating system permissions of the selected execution account; when access to private directories is required, the deployer must arrange directory permissions separately, without automatic chmod, chown, or scanning the entire directory tree. Program files remain in place; host software upgrades affect new executions and long-term sessions, so it is not a fixed version snapshot. Foreground/background/TTY use the same Shell selection strategy: within the completed isolated environment, it sequentially verifies whether `/bin/sh`, `/bin/dash`, and `/bin/bash` can run and follow PATH; it prioritizes the first system Shell that passes verification, and finally uses the managed static BusyBox. Bash runs as `sh`. TTY is initialized by an internal helper with a valid foreground process group, not relying on selecting a specific Shell to bypass terminal issues. seccomp, no_new_privs, capabilities, network authorization, and resource limits remain effective.

The default tool lookup order is `/usr/bin:/bin:/usr/sbin:/sbin:/molr/bin`. The last `/molr/bin` is a read-only BusyBox applet link directory, containing tools actually provided by the currently packaged BusyBox; commands missing from the system can be supplemented by it. Normal Shell built-in commands are still handled according to the Shell's own semantics. When system commands fail to run, the original exit code is preserved, and it will not retry with BusyBox. When users actively modify PATH or define aliases in `.profile`, they are executed according to their settings.

If the host `/bin/sh` is a BusyBox that preempts commands, the Runtime will first try compatible dash/Bash. If neither is available, BusyBox is still used according to deployment compatibility conventions, and a prompt is displayed on stderr (terminal output in TTY) at startup that system tool priority cannot be guaranteed; at this time, built-in applets may take precedence over PATH, and absolute paths such as `/usr/bin/wget` can be used to explicitly select host tools. When explicitly starting other Shells, BusyBox, or executing scripts interpreted by them, command lookup is determined by the corresponding Shell, and there is no commitment to change their compilation behavior.

Tools not provided by either the host or BusyBox remain unavailable; even if a tool exists, dependencies and isolation compatibility must be checked. For example, the existence of Python does not mean venv/ensurepip is installed, online pip requires network authorization, and the installation location should be in the workspace; virtual environments can be used, or independent directories can be used via `pip3 install --target /work/packages ...` combined with `PYTHONPATH=/work/packages`; the existence of Tesseract does not mean chi_sim is installed. fd/rg can explicitly limit the number of threads to avoid exceeding execution limits on multi-core hosts. dig uses controlled DNS, and the returned virtual addresses are not equal to the original public DNS records; traceroute does not guarantee reflecting real external routing.

When using an independent remote Sandbox service, item-by-item functional checks can be performed through verified service connections; the check program itself runs on the host, but all tools to be tested are executed in the real sandbox via the authenticated API:

```bash
python3 runtime/scripts/check-host-tools.py host-sandbox-connection.json tool-results.json
```

Results record exit status, standard output, and errors item by item, distinguishing between not installed and functional failure; missing software is not automatically installed. Functions such as PDF/OCR scanning can only pass if the corresponding tools and language data exist. The JSON report uses an independent test workspace and does not read real user files.

## Authorization Quotas and Deployment Boundaries

`sandbox.authorization_ceilings` limits the computable and network upper bounds that can be approved; it is separate from the sandbox basic protection configuration; compute and network authorization must explicitly specify independent quotas. Omitting `CPUPercent`, `MemoryBytes`, or `Processes`, or setting them to `0`, adds no deployment ceiling for that resource; the compute page displays machine capacity. An explicit positive value is bounded by machine capacity. Setting one field does not inject default ceilings for the other two. Current nested fields adopt the names of capability DTOs, for example:

```json
{
  "sandbox": {
    "authorization_ceilings": {
      "CPUPercent": 800,
      "MemoryBytes": 17179869184,
      "Processes": 512,
      "TempBytes": 1073741824,
      "MaxFileBytes": 1099511627776,
      "NetworkBytes": 1099511627776,
      "NetworkConnections": 128
    },
    "workspace_project_quotas": false
  }
}
```

These are applicable boundaries, not the quotas already authorized for each assistant, nor do they reserve physical capacity. The gateway currently processes a maximum of 128 outbound connections/UDP sessions per execution, and the authorized shared connection count cannot exceed this infrastructure protection upper limit. DNS maintains independent processing slots, and requests and responses are counted against the effective network authorization selected during resolution; traffic quotas are obtained before querying. Port forwarding remains independent of outbound network authorization and is not counted in the outbound network quotas here.

The network ledger is saved in the SQLite on the actual execution side: local uses `runtime.db`, and independent Sandbox uses `sandbox.db`. Database snapshots include historical consumption and unsettled reservations; remote execution side databases and workspaces must be backed up independently. Unsettled traffic from abnormal interruptions still occupies quotas and is not automatically returned across cycles or restarts.

Workspace hard quotas are an optional deployment capability: the file system must have project quotas enabled, each assistant working directory must be configured with an independent, non-zero, inheritable project ID, and all existing files must belong to that project; the Runtime running identity must be authorized to call `quotactl_fd(Q_SETQUOTA)`. `workspace_project_quotas: true` is set only after these prerequisite configurations are completed. The Runtime does not automatically remount the file system, change the project ownership of existing files, or request host privilege escalation. Ordinary user services usually do not have this management permission; simply turning on the configuration switch does not mean hard quotas are available.

Capacity is measured by the space allocated by the file system, and quotas are rounded down to 1 KiB; file counts include directories and other inodes. Expiration downgrading only lowers the upper limit for subsequent growth and does not delete existing data. If the file system is not supported, the directory is not assigned a project, or the management call fails, `workspace_quota_unavailable` is returned, and it will not fall back to scanning monitoring and treat it as a hard quota.

## Checks and Verification

`GET /api/sandbox` is only for owners to query status and resource scope; it does not provide command execution or configuration writing. The interface only displays understandable states such as ready, component missing, system unsupported, or insufficient space; detailed reason codes are left for diagnostics.

After startup and each restart, check `/api/sandbox` via authenticated requests; when equipped with a WebUI, verification must also be done through its same-origin proxy. When the sandbox is not explicitly disabled, `Enabled=true`, `Capabilities.Reason=available`, and `Shell`, `ProcessIsolation`, `FileIsolation`, `NetworkIsolation`, `ResourceLimits` must all be `true`.

| Reason | Priority Check |
| --- | --- |
| `sandbox_package_incomplete` | Whether Runtime/helper is a static build, whether static BusyBox is available |
| `sandbox_helper_unavailable` | Whether Bubblewrap/helper is an executable regular file, whether permissions and paths meet requirements |
| `sandbox_network_probe_failed` / `sandbox_network_setup_failed` | TUN, nsenter, user namespace, and AppArmor support; retain original restrictions, do not disable network isolation |
| cgroup or isolation startup failure | Current service delegation, unit configuration, and logs; whether the actual isolation probe passes |

Do not bypass issues by disabling isolation, increasing write permissions, or changing to host execution. Users only need to grant explicit resource permissions; system dependencies and deployment strategies are handled by maintainers.

See [Sandbox Development Documentation](../../dev/security/sandbox.md) for real isolation tests and implementation boundaries. Include `sandbox/work` managed by the Runtime when backing up; database snapshots do not include these files.

See [Independent Sandbox Service](remote.md) for deployment, connection, and recovery semantics of independent remote sandboxes. `sandbox.enabled: false` disables the built-in local sandbox and does not affect connected remote sandboxes.

## Forwarding Listen Addresses

For normal standalone LAN deployments, no port needs to be specified. By default, `0.0.0.0` and an OS-assigned free port are used only when explicitly requested by the task; the private IPv4 address of the active network interface is preferred as the access address. For multi-NIC, container, or public network deployments, a reachable address must be explicitly specified:

```json
{
  "port_forward": {
    "enabled": true,
    "listen_host": "0.0.0.0",
    "public_host": "<LAN_IP>"
  }
}
```

Replace `<LAN_IP>` with the actual reachable LAN IP of the deployment host; it must not be written into the configuration as-is.

`listen_host` is the IP to which the Runtime binds, without a port; `public_host` is the hostname or IP accessible to users, without protocol, path, or port. For local-only use, both can be set to `127.0.0.1`. `enabled: false` prohibits creating tasks with forwarding ports, but does not affect regular Shell. This setting is managed by Runtime deployment, not modified by the model, and does not stop providing forwarding for remote tasks even if the built-in sandbox is disabled.

`public_host` only controls the return address; it does not configure routing, firewalls, or NAT. Deployers must ensure that users can connect to the TCP ports automatically assigned by the Runtime; proxying only the Molr HTTP page will not automatically proxy these ports. If the address cannot be automatically determined or port assignment fails, the task startup is rejected.


Forwarded HTTP/WS requests require the current Molr login session and task ownership. Use the same hostname/IP as the WebUI login, including for local deployments. Task links identify the login entry by port; this selector is not a credential. Logging out or revoking the selected session closes established streams. Runtime removes Molr cookies, authorization and CSRF headers before forwarding, and prevents upstream responses from setting reserved Molr cookies. Public raw TCP mode is disabled; the internal namespace/remote transport remains TCP.

Local/LAN HTTP uses HTTP forwarding. HTTPS uses `port_forward.cert_file` and `port_forward.key_file`, or inherits the Runtime certificate in direct TLS mode. Trusted-proxy deployments must configure a forwarding certificate: a proxy for the main WebUI does not cover dynamic forwarding ports. The certificate must cover `public_host`. Credentials are not transported through an HTTP downgrade.

## Local IPC and browser programs

All Shell executions use the same IPC policy. Unix STREAM/SEQPACKET sockets and anonymous pairs are supported; datagrams remain denied. Abstract addresses are isolated by the execution’s network namespace. Pathname sockets must use an absolute path under its private `/tmp`, without symlinks or mount crossings. Sockets in `/work` or granted resource directories remain inaccessible. Temporary sockets do not survive execution termination; run successive browser commands in the same foreground script or managed background Shell.

With host tools and Chromium’s system libraries available, agent-browser can run inside the assistant sandbox. Put the CLI in the workspace or a granted program directory, and grant read-only access to Chromium’s program directory. Configure the following within that Shell (replace the executable path with the granted path):

```sh
export AGENT_BROWSER_SOCKET_DIR=/tmp/agent-browser
export AGENT_BROWSER_EXECUTABLE_PATH=/resources/chromium/chrome
export AGENT_BROWSER_ARGS=--disable-dev-shm-usage
export TOKIO_WORKER_THREADS=2 RAYON_NUM_THREADS=2
mkdir -p "$AGENT_BROWSER_SOCKET_DIR"
```

Prefer Chromium's native sandbox. Molr permits nested user/PID/network namespaces while continuing to deny mount namespaces, mount APIs, setns, host IPC and privileged inspection syscalls. Nested namespace capabilities do not grant authority over the parent sandbox or host. Networking still uses the authorized gateway and all descendants stay in the execution cgroup. Both networked and offline Shells retain the service account's numeric UID/GID; only namespace mappings change, not the underlying host identity or file permissions. The IPC broker respects a caller's inner chroot and cannot connect back out through the outer `/tmp`.

Verify with `agent-browser open chrome://sandbox` and `agent-browser get text body`: Namespace, PID/network namespaces, Seccomp-BPF and TSYNC must be enabled. A successful page load alone is insufficient: agent-browser may add `--no-sandbox` automatically when it detects root. Host tool mode requires a non-root service account.

When using the default `/usr/bin/bwrap`, Runtime first tries `/usr/bin/aa-exec -p chrome` with the host's already loaded `chrome` AppArmor profile. Startup probes exercise real offline execution and controlled networking. Missing tools, an unloaded profile or failed probes select Bubblewrap's original profile and repeat the checks. If neither path works, Shell capabilities remain unavailable. Selection happens only during startup: failed business commands are never retried with another profile, and `--no-sandbox` is never added automatically. Runtime does not install, load or modify AppArmor policies or change sysctl settings. An explicit `sandbox.bubblewrap` pointing to another trusted launcher retains the administrator's choice.

The distribution's `chrome` profile is usually an application userns exemption; it does not itself provide Chromium's inner sandbox. Compared with Bubblewrap's restricted child profile, it removes the additional blanket capability denial, allowing Chromium to establish isolation in its own child namespaces and exposing more kernel functionality. Molr's file mounts, controlled networking, seccomp, zero host capabilities, `no_new_privs` and cgroups remain enforced. Check actual probes and `chrome://sandbox`; the presence of `/etc/apparmor.d/chrome` alone is insufficient.

When the host cannot support nested sandboxing, `AGENT_BROWSER_ARGS=--no-sandbox,--disable-dev-shm-usage` remains an explicit compatibility fallback. It disables Chromium's inner protection; browser processes then share the Shell's authorized scope under Molr's outer isolation. It is not a safe configuration for direct host execution or Full Access. A tested starting compute allocation is 2 GiB memory, 512 processes/threads, 200% CPU and 512 MiB temporary space. The managed browser task provides live display and takeover; program installation remains a deployment responsibility.

## Managed browser tasks

Use [component installation](../administration/components.md#command-line-maintenance), or manually install agent-browser 0.39.0 and a compatible Chromium distribution on the Runtime host, with Chromium's shared libraries available in host-tool mode. The executables must be regular, executable files, without symlinks or group/world write permission. Configure readonly program directories outside Runtime's private data directory:

```json
{"browser":{"agent_directory":"/opt/molr-browser/agent","chromium_directory":"/opt/molr-browser/chromium"}}
```

The directories must contain `agent-browser` and `chrome`, respectively, including Chromium's companion assets. Defaults are `libexec/browser/agent` and `libexec/browser/chromium` beside the Runtime binary. Programs are mounted readonly; no additional assistant directory grant is required for these trusted assets. Without explicit external paths, Runtime first reads the component receipt managed by molr-setup. Installation directories cannot be exposed through ordinary directory resources; only the managed browser launcher mounts the required programs.

The browser uses the existing local host-tools sandbox and compute/network grants. One active browser task per assistant may contain multiple tabs. Profile data persists at `/work/.local/state/molr/browser/profile`, isolated between assistants. Internal command and stream ports are accessible only through Runtime's managed execution capability, never public forwards. The managed launcher does not expose executable paths, flags, CDP or daemon sockets to API clients. It does not add `--no-sandbox`.

Use the chat's browser panel to start, observe and take control. Close normally to flush profile data; forced task cleanup can lose unflushed browser state. Revocation, time limits and resource limits still terminate the whole execution. Remote browser tasks are not supported. Full Access browsers use native host paths and task-private control connections, retaining Chromium sandboxing without requiring Bubblewrap or cgroup readiness. Startup may reuse the already-loaded, usable `chrome` AppArmor profile without installing or changing system policy. A non-root service account and a working Chromium sandbox are required; startup failures never add `--no-sandbox` automatically. See [Browser](../../user/best-practices/browser.md).
