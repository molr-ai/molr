# Docker deployment

The repository-root Dockerfile builds the complete Molr application: it builds WebUI and embeds the assets into a static Go binary. One container serves the web interface and API on one port. Node, Go, and Debian base images use fixed versions and multi-platform SHA-256 digests; Go matches `runtime/.go-version`.

Images are currently built locally from source; there is no published official Molr image to pull. Install Git, Docker Engine or Docker Desktop using Linux containers, and Docker Compose. Go and Node.js are not needed on the host. Linux containers support AMD64 and ARM64; builds select the builder's architecture by default.

## Build and configure

```bash
git clone https://github.com/molr-ai/molr.git
cd molr
cp deploy/docker/server.example.json deploy/docker/server.json
MOLR_COMMIT="$(git rev-parse HEAD)" docker compose build
```

To build a fixed source version, check out its tag or commit first. Pinned base digests do not follow upstream tag updates; verify and update both versions and digests when upgrading. The Docker build arguments `NODE_IMAGE`, `GO_IMAGE`, and `RUNTIME_IMAGE` allow a maintainer-trusted registry mirror while retaining the same digests.

Edit `deploy/docker/server.json`. The example registers only `http://127.0.0.1:8787`. For LAN access, change `http.public_url` and the entry in `http.allowed_origins` to `http://<LAN_IP>:8787`, using the Docker host's numeric LAN IP. To allow both local and LAN access, explicitly list both origins.

Keep `http.listen` at `0.0.0.0:8787`. LAN mode requires the public URL, container listener, and Compose mapping to use the same port; change all three together. Configuration is mounted read-only; restart the container after editing. See [configuration and access](configuration.md) for HTTPS and reverse proxies.

## Initialize and start

Initialize the account before the first start:

```bash
docker compose run --rm molr bootstrap -config /etc/molr/server.json
docker compose up -d
docker compose logs --tail=50 molr
```

Bootstrap creates `owner` and writes a random initial password to `/data/initial-password` in the data volume with mode 0600, without printing it. Repeated bootstrap preserves existing accounts; run it only while the service is stopped, not against a live database.

After the listener has started, generate a one-time login link:

```bash
docker compose exec molr /usr/local/bin/molr access -config /etc/molr/server.json
```

Links expire after 15 minutes by default. The container must reach its configured `public_url`. If Docker Desktop or a host firewall blocks connections back to the host's published port, open the configured address in a browser and sign in as `owner` using the initial password:

```bash
docker compose exec molr cat /data/initial-password
```

This prints a password; use a private terminal. After changing the password, supply the current one through `access -password-stdin`; MFA also requires `-mfa-code`. Do not loop login diagnostics: successful logins count toward rate limits. See [instance setup](../getting-started.md) for onboarding, models, and members.

## Choose command execution

The default example explicitly sets `sandbox.enabled: false`. The web interface, chat, workspaces, file tools, and memory remain available. Choose command execution as follows.

### Separate Linux sandbox

Deploy `molr-sandbox` on another Linux device or the host using the [standalone sandbox guide](../sandbox/remote.md), connect the resource in WebUI, and grant assistants compute, directory, and network permissions. Keep the main container's default configuration. The remote node must pass its own isolation checks.

For LAN models or sandboxes, explicitly add the target numeric IP to the main container's `network.allowed_private_ips`. Container `127.0.0.1` refers to the container itself, not host services. Remote nodes do not automatically receive the main container's `/work` files; register the remote directories that assistants need through resource permissions.

### Full Access inside the container

The image includes `/bin/sh`. Explicitly enable it in configuration:

```json
{
  "sandbox": {
    "enabled": true,
    "allow_full_access": true
  }
}
```

Merge these fields into the existing configuration, run `docker compose restart molr`, then grant the selected assistant Full Access compute permission on the local sandbox in WebUI. The deployment switch alone does not authorize every assistant.

Commands run as the ordinary container user `65532:65532`. Compose retains a read-only root filesystem, drops Linux capabilities, and prohibits privilege escalation; writable locations are primarily the data volume and temporary directory. Full Access does not provide command-level file, process, or network isolation between assistants. Commands can access data available to the service user, including private Runtime data and other workspaces. Docker separates the container from the host; it does not isolate assistants within the container. Do not mount the Docker socket, host root directory, or data you do not intend to authorize.

For additional tools, build a derived image, install dependencies as root during the build, and restore `USER 65532:65532`. Do not install system software in the running container.

### Isolated execution inside the container

The `isolated` image target preinstalls systemd, Bubblewrap, static BusyBox, util-linux, and CA certificates. Container systemd runs Molr as an ordinary user with a dedicated cgroup through `Delegate=yes` and `DelegateSubgroup=supervisor`. Process, file, network isolation, and resource limits use the existing sandbox implementation, with `allow_full_access` remaining false.

This template targets rootful Docker on native Linux with cgroup v2, AppArmor 4.0+, and `/dev/net/tun`. It requires private PID/cgroup namespaces and mounts no host cgroup, host root, or Docker socket. Rootless Docker, userns-remap, Docker Desktop, and hosts without AppArmor are outside this template's acceptance scope; use the default container with a separate Linux sandbox instead.

Load the dedicated policy on the host before selecting the isolated configuration and image target. The host needs `apparmor_parser`. This policy applies only to containers selecting it explicitly; it does not replace `docker-default` or change global sysctls.

```bash
sudo apparmor_parser -r -W deploy/docker/molr-docker-isolated.apparmor
cp deploy/docker/server.isolated.example.json deploy/docker/server.json
```

Edit access addresses as described above. This function selects both Compose files; use the same combination for initialization, startup, and management:

```bash
dc() { docker compose -f compose.yaml -f compose.isolated.yaml "$@"; }
MOLR_COMMIT="$(git rev-parse HEAD)" dc build
dc run --rm molr bootstrap -config /etc/molr/server.json
dc up -d
dc logs --tail=50 molr
```

Wait for HTTP readiness, then check capabilities and issue a login link as the service user:

```bash
dc exec --user 65532:65532 molr /usr/local/bin/molr doctor -config /etc/molr/server.json
dc exec --user 65532:65532 molr /usr/local/bin/molr access -config /etc/molr/server.json
```

Require `Enabled=true`, `FullAccess=false`, `Reason=available`, and all of `Shell`, `ProcessIsolation`, `FileIsolation`, `NetworkIsolation`, and `ResourceLimits` to be true. If the page responds but capability checks fail, command execution is not ready; do not enable Full Access to hide the failure. Assistants still need compute, directory, and network grants.

Container PID 1 receives `SYS_ADMIN` and the other explicitly listed capabilities to prepare its private cgroup and launch the service. The Molr service clears its permitted, effective, bounding, and ambient capability sets and enables `NoNewPrivileges`. The dedicated AppArmor policy permits nested namespaces and mounts, with Unix communication and process inspection restricted to peers under the same policy. The outer seccomp policy retains a fixed Docker default and adds only `pivot_root` plus constrained `pidfd_getfd` and `process_vm_readv` calls for Bubblewrap and the trusted socket broker. Inner guest seccomp still denies these calls.

`systempaths=unconfined` removes Docker covering mounts that block nested procfs; corresponding sensitive `/proc` and `/sys` paths are protected by explicit AppArmor deny rules. This option must be used together with the supplied policy, never copied alone into an ordinary container. `/dev/net/tun` is available only to the trusted network helper; guest commands see a separate `/dev`. The configuration uses neither `privileged`, `seccomp=unconfined`, nor `apparmor=unconfined`.

Reload the policy after host reboots. For persistent deployment, an administrator can install it under `/etc/apparmor.d/` and use the host AppArmor service. After stopping every container using it, unload it with `sudo apparmor_parser -R deploy/docker/molr-docker-isolated.apparmor`. Separate instances use Compose project names for independent data volumes and cgroups.

Real kernel regression tests use a separate test image and temporary volumes, without model services or existing instance data:

```bash
docker compose -p molr-isolation-tests -f compose.yaml -f compose.isolated.yaml -f compose.isolation-test.yaml build
docker compose -p molr-isolation-tests -f compose.yaml -f compose.isolated.yaml -f compose.isolation-test.yaml up --abort-on-container-exit --exit-code-from molr --no-build
docker compose -p molr-isolation-tests -f compose.yaml -f compose.isolated.yaml -f compose.isolation-test.yaml down --volumes
```

Tests cover filesystem/PID isolation, read-only mounts, cgroup limits, allowed and denied networking, cancellation, and revocation. Tests requiring public networking, GPUs, or additional browser components use their own environment switches and explicitly skip when not enabled.


## Verify and maintain

After the service is ready, run one authenticated check:

```bash
docker compose exec molr /usr/local/bin/molr doctor -config /etc/molr/server.json
```

The default mode must report `Sandbox.Enabled=false` and `Storage=true`. Explicit Full Access requires `Capabilities.FullAccess=true`; this does not indicate isolation. Fully isolated deployments additionally require `Reason=available` and all of `Shell`, `ProcessIsolation`, `FileIsolation`, `NetworkIsolation`, and `ResourceLimits` to be true. Failed authentication or rate limiting means verification is incomplete, not that the container needs restarting.

The `molr-data` named volume retains databases, keys, accounts, workspaces, and the initial password; Compose adds its project prefix. Rebuilding or replacing a container preserves this volume. Before updating, stop the instance and back up its data volume and configuration, then check out the target source version, rebuild, and start. Preserve source versions and image tags for rollback and retain database backups separately. Update containers by rebuilding images; do not replace the read-only program through WebUI.

```bash
docker compose stop
# Back up the data volume and deploy/docker/server.json while stopped.
MOLR_COMMIT="$(git rev-parse HEAD)" docker compose build
docker compose up -d
```

Remove containers with `docker compose down`, retaining the data volume. `docker compose down -v` deletes instance data and is only for intentional removal. The image retains Molr's MIT license and third-party notices; Debian package license materials remain under `/usr/share/doc/`.
