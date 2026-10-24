# Docker deployment

The backend and standalone sandbox node use the same ordinary image, `ghcr.io/molr-ai/molr`, for Linux AMD64 and ARM64. Only Docker is required on the host; the Compose method also needs Docker Compose. First startup automatically creates configuration, the owner account or node credentials. No configuration download, separate bootstrap command or host OpenSSL installation is needed.

The Debian 13 slim image contains a static Molr executable and embedded WebUI, running as ordinary user `65532:65532`. Examples use named volumes, created and retained by Docker without host-directory ownership setup. Choose either Docker or Compose.

## Image versions

| Tag | Meaning |
| --- | --- |
| `latest` | Latest stable release; unavailable before the first stable image |
| `0.1.0` | Example fixed stable version |
| `0.2.0-beta.1` / `0.2.0-rc.1` | Example fixed prerelease versions |
| `nightly` | Latest verified daily build, without fixed daily tags |

Examples default to `latest`; replace it with `nightly` before the first stable release. Production deployments may pin a version or digest. Images retain their full version and commit internally. Historical nightly retention and rollback availability are not guaranteed. Public images support anonymous pulls.

## Deploy the backend

First installation defaults to the personal preset, exposing directories, networks and compute within the service account’s access and allowing separately granted Full Access. Ordinary images do not provide controlled compute inside the container; connect a capable sandbox node for isolated execution.

### Docker commands

```bash
docker run -d \
  --name molr \
  --init --restart unless-stopped \
  -v molr-data:/data \
  -p 8787:8787 \
  ghcr.io/molr-ai/molr:latest
```

Open `http://127.0.0.1:8787`. First startup creates `owner` and writes a random initial password to `/data/initial-password` (0600), never to logs. Configuration is stored in `/data/server.json`. Restarting or replacing the container preserves accounts, configuration and data.

### Docker Compose

Save this as `compose.yaml`:

```yaml
services:
  molr:
    image: ghcr.io/molr-ai/molr:latest
    init: true
    restart: unless-stopped
    ports:
      - "8787:8787"
    volumes:
      - molr-data:/data

volumes:
  molr-data:
```

Run `docker compose up -d`. The repository's `compose.prebuilt.yaml` is the corresponding template and accepts `MOLR_IMAGE` to select the image. Compose prefixes volume names with the project name; keep the same project name when upgrading.

### Login and checks

Read the initial password privately and sign in as `owner`:

```bash
# Docker
docker exec molr cat /data/initial-password
# Compose
docker compose exec molr cat /data/initial-password
```

Alternatively, generate a one-time login link, valid for 15 minutes:

```bash
# Docker
docker exec molr /usr/local/bin/molr access -config /data/server.json
# Compose
docker compose exec molr /usr/local/bin/molr access -config /data/server.json
```

The container must reach its configured `public_url`; otherwise use password login. After changing the password, use `-password-stdin`; supply `-mfa-code` for MFA. Do not loop login diagnostics: successful logins also count toward rate limits.

Once ready, run `docker exec molr /usr/local/bin/molr doctor -config /data/server.json`, or the corresponding Compose command. With the personal preset, check `Enabled=true`, `Storage=true` and `Capabilities.FullAccess=true`. Ordinary images report `Capabilities.Shell=false`, meaning controlled compute is unavailable. Follow the web onboarding flow to connect a model after signing in.

## Deploy a sandbox node

### Docker commands

```bash
docker run -d \
  --name molr-sandbox \
  --init --restart unless-stopped \
  -v molr-sandbox-data:/data \
  -p 8443:8443 \
  -e MOLR_MODE=sandbox \
  ghcr.io/molr-ai/molr:latest
```

`MOLR_MODE=sandbox` selects standalone node mode without accounts or a web interface. First startup creates `/data/sandbox.json`, `/data/private/server.crt`, `/data/private/server.key`, `/data/private/sandbox-token` and writable `/data/projects`. Credentials survive restarts. An existing certificate with a missing private key prevents startup instead of silently replacing node identity.

### Docker Compose

Save this as `compose.yaml` in a separate directory:

```yaml
services:
  molr-sandbox:
    image: ghcr.io/molr-ai/molr:latest
    init: true
    restart: unless-stopped
    environment:
      MOLR_MODE: sandbox
    ports:
      - "8443:8443"
    volumes:
      - molr-sandbox-data:/data

volumes:
  molr-sandbox-data:
```

Run `docker compose up -d`. The repository template is `compose.sandbox.yaml`. Each backend needs its own node and data volume. Multiple instances also need distinct container names or Compose project names and ports.

### Connection and permissions

Export the connection code in a private terminal:

```bash
# Docker
docker exec molr-sandbox molr connection-code --config /data/sandbox.json
# Compose
docker compose exec molr-sandbox molr connection-code --config /data/sandbox.json
```

In the backend, follow [connect a sandbox](../resources/sandbox.md#connect-in-the-webui), enter `https://<node-host-IP>:8443`, paste the connection code, test the connection and save. Users enter the address separately; the code does not contain it. The backend automatically validates the code format version and the node's actual protocol version; release version numbers need not match exactly.

The connection code contains the public key fingerprint and service token. It appears only when explicitly exported, never in startup logs. Treat it as a credential and do not share it publicly. Restarts, upgrades and certificate renewal preserve it; replacing the private key or token requires exporting a new code and updating the backend credentials.

Generated self-signed certificates last one year and enable `http.auto_renew_certificate` by default. At startup and hourly while running, the service checks whether fewer than 30 days remain, renews with the original private key and atomically replaces the certificate. New TLS connections use the new certificate without interrupting existing connections. Startup also renews expired certificates. Custom certificates remain operator-managed by default; automatic renewal can be explicitly enabled for self-signed certificates only and does not issue CA certificates. The backend pins the public key and checks certificate validity.

Ordinary container nodes provide file access and explicitly authorized Full Access commands, without in-container process, file or network isolation or per-command resource limits. Docker separates the container from the host. Full Access commands can access files readable or writable by the service user, including private credentials and other authorized directories. `restrict_directories` limits file-tool directory registration, not Full Access commands.

Verify authenticated node information reports `Enabled=true`, `Storage=true` and `Capabilities.FullAccess=true`, then grant Full Access separately as needed. Full Access covers files, commands and networks accessible to the service account; ordinary calls still use separate storage, compute and network grants. For per-assistant command isolation, use a native sandbox or the source isolation target at the end of this page.

## Addresses and custom configuration

| Environment variable | Purpose |
| --- | --- |
| `MOLR_RESOURCE_PRESET` | First configuration only: `permissive` (default, personal) or `least-privilege`; existing settings are preserved |
| `MOLR_MODE` | `server` (default) or `sandbox` |
| `MOLR_PUBLIC_URL` | Explicit access address; updates the saved public URL and listener port at startup, and replaces the previous allowed origin for backends |

Backends default to local access. For LAN access, add `-e MOLR_PUBLIC_URL=http://<host-LAN-IP>:8787` to `docker run`, or configure it under the service's `environment` in Compose. Use a numeric LAN IP. When changing ports, keep the public URL, container listener and published mapping aligned: for example `MOLR_PUBLIC_URL=http://192.168.1.20:9898` with `9898:9898`.

When `MOLR_PUBLIC_URL` is unset, existing configuration is retained. An explicit value preserves other custom fields and additional allowed origins. HTTP quick start is limited to local/trusted LAN use and does not automatically trust arbitrary Host/Origin headers. Configure public HTTPS, reverse proxies and trust boundaries using [configuration and access](configuration.md).

Configuration lives in the volume. Stop the container before copying it out, editing it and writing it back, retaining mode 0600 and ownership `65532:65532`. Restart to load the changes. Invalid configuration is not replaced with defaults. For fully managed configuration, existing `serve -config`, `bootstrap` commands and read-only configuration mounts remain available.

Backend and standalone node first installs share the same resource preset. Personal enables command execution and Full Access, allows registering any container directory, and registers Internet, LAN and host-service networks. Network grants govern controlled commands, not Runtime’s own network access. Presets do not grant assistants access or add in-container isolation to ordinary images. “Local” means the container environment; mounts, the service account and external network policies still apply.

For least privilege, add `-e MOLR_RESOURCE_PRESET=least-privilege` to Docker, or run `MOLR_RESOURCE_PRESET=least-privilege docker compose -f compose.prebuilt.yaml up -d` with the prebuilt template. This only affects initial configuration on a new volume. For existing instances, edit `server.json` or `sandbox.json` in the volume and restart; reinstalling or changing this variable does not overwrite existing permission settings.

Install additional tools while building a derived image, then restore `USER 65532:65532`.

When using a host bind mount instead of a named volume, make the directory writable by UID/GID `65532:65532`. The container does not recursively change host ownership as root. Mount only explicitly authorized host directories, never the host root or Docker socket. Mount reference data read-only.

Optional deployment hardening includes `read_only: true`, `cap_drop: [ALL]` and `security_opt: [no-new-privileges:true]`, with a writable tmpfs for `/tmp`. Configuration, credentials and persistent data still go to `/data`. Docker CPU/memory limits apply to the whole container, not individual assistants.

## Updates, backups and removal

Runtime and WebUI update together. Prebuilt backends always check the stable `latest` image on GHCR, regardless of whether the current build is Nightly or a prerelease. A newer stable version offers an update; a higher current version never prompts a downgrade. An absent stable release is reported explicitly without falling back to another channel. Administrators who want to remain on Nightly can manually pull `nightly` and recreate the container. Version details show the target image and host instructions; the app does not replace its executable. Administrators update nodes alongside the backend.

After editing the target image version in Compose:

```bash
docker compose pull
docker compose stop
# Back up the original data volume while stopped.
docker compose up -d
```

For Docker commands, pull the target image, stop the original container, back up its named volume, remove the container and recreate it with the original arguments and volume. `restart` alone does not adopt a new image. Automatic startup preserves accounts, databases and node identity without reinitialization.

Back up the whole backend volume. Back up the whole node volume too, including configuration, private credentials and project files. Recheck login, version and enabled capabilities after updating. A fixed version or recorded digest can select an older image; use the matching backup when restoring data. Replacing an image does not restore data.

`docker compose down` retains volumes; `down -v` deletes project-managed volumes. Docker `rm` does not delete the named volumes used here. Delete volumes only when intentionally clearing the instance.

## Build from source

```bash
git clone https://github.com/molr-ai/molr.git
cd molr
cp deploy/docker/server.example.json deploy/docker/server.json
MOLR_COMMIT="$(git rev-parse HEAD)" docker compose build
docker compose run --rm molr bootstrap -config /etc/molr/server.json
docker compose up -d
```

Local `compose.yaml` retains explicit configuration and manual initialization; prebuilt templates use automatic `molr container` startup. The default Dockerfile target starts automatically, while explicitly supplied CLI commands keep their original behavior. Local builds default to `0.1.0-local` and do not automatically compare releases. Node, Go and Debian slim pin multi-platform digests; proxies must retain the same digest. Static Go builds omit debug information; the final image excludes Node/Go toolchains and retains licenses and third-party notices.

## Build the in-container isolation target from source

This target is retained for local source builds only. No official isolated image is published, and it is not part of the ordinary Docker/Compose installation entries on the website. Check out the repository as described in “Build from source” before continuing.

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
