# Installation and service management

## Environment requirements

The following are initial sizing estimates for running the Molr service (Runtime and WebUI) while connecting to cloud models or models hosted on another device. They have not yet been validated by systematic performance benchmarks. The minimum configuration targets service startup and occasional single-user chat; the recommended configuration leaves room for daily use and a few concurrent tasks.

The CPU must use a 64-bit x86 (amd64) or ARM (arm64) architecture.

| Hardware | Minimum | Recommended |
| --- | --- | --- |
| CPU | 1 core | 4 or more cores |
| Memory | 512 MiB | 8 GiB or more |
| Available disk space | 5 GiB | 20 GiB or more, preferably on an SSD |
| GPU | No dedicated GPU required | Choose based on local model deployment requirements |

Running a local model on the same device requires additional storage for model weights, inference memory, and potentially GPU memory, depending on model size, quantization, and context length. Sandbox tasks require additional CPU and memory, especially browser automation, code builds, and concurrent sandboxes. Attachments, workspaces, and backups consume disk space over time, so scale capacity to your workload. For sandbox resource configuration, see [Linux Command Environment](../sandbox/linux.md).

For native macOS Runtime builds, Full Access, and service management, see [source deployment](source-build.md#native-macos-execution). The systemd steps below apply to Linux; macOS uses the user launchd installation described below.

## macOS command-line installation

macOS 13 and later has Apple Silicon (ARM64) and Intel (AMD64) packages containing `molr` with its embedded WebUI and `molr-setup`. End users do not need Go, Node.js, Python, or Homebrew. Run the website command in Terminal; it selects the native architecture:

```bash
curl -fsSL https://molr.ai/install.sh | sh
```

Run as the current user without `sudo` or `--user`. Setup registers a launchd user service, initializes the account, waits for HTTP and authenticated capability checks, and prints a one-time login link. It opens the default browser in a desktop session and only prints the link over SSH. The service uses `~/Library/LaunchAgents/<instance-name>.plist` and starts at desktop login; SSH-only environments use the current user's background launchd domain. This is not a system daemon that starts before login.

The default program directory is `~/Library/Application Support/Molr/program/default`. Configuration and data use `config/default` and `data/default` under the same Molr directory. Logs and caches use `~/Library/Caches/Molr/default`. Use `--name molr-<name>` for separate instances. Setup prints the actual paths and service commands; quote paths containing spaces.

```bash
"$HOME/Library/Application Support/Molr/program/default/bin/molr-setup" status
"$HOME/Library/Application Support/Molr/program/default/bin/molr-setup" update
"$HOME/Library/Application Support/Molr/program/default/bin/molr-setup" uninstall
```

Normal uninstall retains configuration and data; explicitly remove them with `uninstall --purge --yes`. Updates preserve accounts, configuration, and workspaces. In-app updates are not yet supported on macOS; use the CLI. Setup verifies the archive and Mach-O code signatures. Packages currently use ad-hoc signatures without Developer ID signing or Apple notarization; the supported distribution path is Terminal installation.

The installer defaults to `sandbox.enabled: false`; chat, files, and memory remain available. Set `sandbox.enabled: true` and restart to enable Seatbelt-controlled commands, then grant compute, file, and network access as needed. Full Access requires a separate `sandbox.allow_full_access` opt-in and grant. See [native macOS command permissions](../sandbox/macos.md).

For offline installation, download the archive and adjacent `.sha256`, verify with `shasum -a 256 -c <archive>.sha256`, extract, and run `./bin/molr-setup`. Do not substitute Finder double-clicking a bare executable for Terminal installation.


## Local packages and quick installation

Linux packages are available for AMD64 and ARM64 and include the `molr` application and the `molr-setup` maintenance program. Download the matching archive and SHA-256 file, verify and extract them, then run from the package directory:

```bash
sha256sum -c molr_0.1.0_linux_amd64.tar.gz.sha256
tar -xzf molr_0.1.0_linux_amd64.tar.gz
cd molr
./bin/molr-setup
```

The default operation installs or upgrades to this package's version without a channel or installation subcommand. An existing instance at the same version opens maintenance; use `--repair` to reapply files. See [source builds](source-build.md) to create packages.

For an initial online installation, use the website downloader:

```bash
curl -fsSL https://molr.ai/install.sh | sh
curl -fsSL https://molr.ai/install.sh | sh -s -- --channel preview
curl -fsSL https://molr.ai/install.sh | sh -s -- --channel nightly
# Pin a version and keep its update subscription
curl -fsSL https://molr.ai/install.sh | sh -s -- --channel preview --version 0.2.0-rc.1
```

The website installer lets you choose a release channel, operating system, architecture and curl/wget to generate a command. `--arch amd64` or `--arch arm64` checks the selected architecture and stops before download or installation if it does not match the device. Omit it for automatic detection. Package download mode shows the matching file URL, checksum file and a copyable installation command that passes the selected channel to `molr-setup` for future updates.

Stable tracks final releases, preview tracks beta/RC and subsequent final releases, and nightly tracks daily builds. Empty channels do not fall back to other channels. The downloader displays version, architecture, filename, source, checksum and progress, verifies the package, then runs its installer. It uses existing curl or wget, tar, gzip and a SHA-256 tool without installing download tools. The maintenance program itself needs no Python, curl or iproute2.

The default source is `https://molr.ai/releases`; use `--source URL` for another HTTPS source. The downloader also verifies and installs local archives:

```bash
sh install.sh --archive /path/package.tar.gz
```

An adjacent `package.tar.gz.sha256` is required. Running an extracted package needs no network when system dependencies are prepared. SHA-256 verifies downloaded content; it is not an independent publisher signature.

The website source contains manifests only for the stable, prerelease and nightly versions selected from recent releases. `--version` must name a version in the current catalog. For other historical versions, download the archive and SHA-256 file from GitHub Releases and use the offline installation steps above.

## Interactive and unattended installation

A controlling terminal enables interaction by default. The installer displays paths, account, dependencies and permissions before confirmation. Modes can be selected explicitly:

```bash
./bin/molr-setup --interactive
./bin/molr-setup --non-interactive --install-deps \
  --directory-permissions fix --no-browser
./bin/molr-setup --dry-run
```

Unattended mode neither prompts nor opens a browser. `--install-deps` authorizes system preparation; arrange administrator privileges beforehand. Use `--skip-dependencies` for manually prepared hosts. `--dry-run` shows the plan without changing accounts, permissions, configuration or services; previewing through the downloader still downloads a temporary package.

For group/other-writable directories, choose fix, ignore or quit. Fix removes only those directory write bits, without recursion. Unattended mode requires `--directory-permissions fix|ignore` when a choice is needed. `--yes` skips general confirmation but does not choose a permission policy. Ownership mismatches, symlinked paths and writable managed files cause errors.

Without `--config`, setup generates LAN HTTP configuration, initializes the owner, starts the service and verifies authenticated APIs and sandbox capabilities. Output follows inspection, preparation, installation, verification and results. `verified` means authenticated verification passed. `verification_pending` means installation succeeded but current credentials are needed, with exit code 3. `not_started` corresponds to `--no-start`. Failures report the current stage and possible changes.

## Linux services and command environment

The current service adapter uses systemd. The Linux sandbox requires systemd 254+, cgroup v2 delegation, Bubblewrap, static BusyBox, CA certificates and util-linux nsenter/setpriv. These are capability requirements; installation does not reject distribution names or versions. Automatic dependency preparation currently supports APT. Other environments can be prepared manually using the [sandbox requirements](../sandbox/linux.md). User services need a normal SSH/desktop login session and user manager.

When AppArmor restricts unprivileged user namespaces, setup probes Bubblewrap as the service account under `NoNewPrivileges`, first with the already loaded `chrome` profile and then through the original restricted path. No new profile is needed if the existing configuration works. If both paths fail, with authorization it can prepare and load the distribution's `bwrap-userns-restrict` policy and repeat the probe. Existing policies are preserved; conflicts require administrator intervention. System isolation restrictions are not disabled, and missing dependencies do not trigger Full Access.

### Selecting the runtime account during installation

The default is the current regular user's `molr.service`. Root installation requires a non-root `--user USER` and creates a system service. Missing accounts are created as system accounts without login access. Existing HOME, shell and supplementary groups are preserved; existing data ownership is not changed recursively.

```bash
./bin/molr-setup --name molr-demo --port 8788 --public-host "<LAN_IP>"
sudo ./bin/molr-setup --user molr-sandbox --install-deps
# Custom configuration; initialize using a password from stdin
./bin/molr-setup --config /path/server.json --init-owner owner \
  < /private/owner-password
```

Replace `<LAN_IP>` with the actual private IPv4 address. Initial installation with custom configuration requires `--init-owner`, or `--no-start` for later initialization. Runtime and its sandbox always use the same regular account.

| Content | User installation | System installation |
| --- | --- | --- |
| Programs, installation record and licenses | `~/.local/lib/molr/<instance>/` | `/opt/molr/<instance>/` |
| Configuration | `${XDG_CONFIG_HOME:-$HOME/.config}/molr/<instance>/` | `/etc/molr/<instance>/` |
| Default persistent data | `${XDG_DATA_HOME:-$HOME/.local/share}/molr/<instance>/` | `/var/lib/molr/<instance>/` |
| Cache and temporary files | `${XDG_CACHE_HOME:-$HOME/.cache}/molr/<instance>/` | `/var/cache/molr/<instance>/` |
| Service file | `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/<service>.service` | `/etc/systemd/system/<service>.service` |
| Management command | `systemctl --user` | `sudo systemctl` |

Service `molr` uses instance `default`; `molr-demo` uses `demo`. `molr-default` is reserved. `--program-root /path/programs` changes the program base; files go into its instance child. Both commands live under `bin/`; configuration is `server.json`, with mode `0600` for user installs or root-owned, service-group-readable `0640` for system installs.

Program, configuration, data and cache directories must not overlap. Custom `data_dir` and external workspaces retain their configured paths and must be accessible to the service account. Databases, keys, attachments and assistant workspaces are persistent data; caches can be rebuilt. Licenses are installed with the programs and available in WebUI at `/LICENSE` and `/THIRD_PARTY_NOTICES.md`.

An administrator can run `loginctl enable-linger USER` to keep a user service running after logout. System services need no linger. See [Docker deployment](docker.md) to run the complete application with WebUI, persistent data, and optional command execution.

## Updates and repeated execution

Running the installed maintenance program shows status and opens a maintenance menu; without a terminal it only shows status. It locates the instance using its executable location, independently of the working directory.

```bash
~/.local/lib/molr/default/bin/molr-setup
~/.local/lib/molr/default/bin/molr-setup status
~/.local/lib/molr/default/bin/molr-setup update
~/.local/lib/molr/default/bin/molr-setup update --version 0.2.0-rc.1
~/.local/lib/molr/default/bin/molr-setup update --channel preview
```

Updates use the source and channel saved in the installation record, show the selected package, then download and verify it. `--source`/`--channel` select the subscription for this update and subsequent successful updates. System instances use `sudo /opt/molr/default/bin/molr-setup update`. The Go updater also accepts explicitly specified private or loopback numeric HTTP mirrors.

Rerunning the website command or installing another extracted package also upgrades an existing instance, preserving configuration, data, credentials and accounts. Atomic program replacement retains `molr.previous`, `molr-setup.previous` and `server.previous.json`. The same version is not reapplied by default; use `--repair`. An intentional downgrade needs `--allow-downgrade` and a prior data backup.

Incomplete installation or damaged records produce explicit errors. Do not delete records to bypass detection; use a complete package to repair the original instance. `--record /path/installation.json` selects a specific installation.

Component installation, updates and repairs are covered in [Component management](../administration/components.md).

## Post-startup checks

After installation, update or restart, verify the actual page and authenticated sandbox capabilities:

```bash
~/.local/lib/molr/default/bin/molr doctor -config /path/server.json
~/.local/lib/molr/default/bin/molr access -config /path/server.json
```

The default credential is the data directory's initial password. After changing it, add `-password-stdin`; with MFA, add `-mfa-code`. These commands use normal authentication APIs. `access` creates a one-time login link valid for 15 minutes.

Unless the sandbox was disabled, `/api/sandbox` must report `Enabled=true`, `Capabilities.Reason=available` and all of `Shell`, `ProcessIsolation`, `FileIsolation`, `NetworkIsolation`, `ResourceLimits` as `true`. A listening port or active service does not replace this check. See [sandbox checks](../sandbox/linux.md#checks-and-verification).

## Uninstallation

Use the installed maintenance program for offline uninstallation:

```bash
~/.local/lib/molr/default/bin/molr-setup uninstall --dry-run
~/.local/lib/molr/default/bin/molr-setup uninstall
~/.local/lib/molr/default/bin/molr-setup uninstall --purge --yes
sudo /opt/molr/demo/bin/molr-setup uninstall --purge
```

The default stops the instance and removes programs, service files and cache while retaining configuration, persistent data and the installation record. Reinstalling the same instance reuses them. `--purge` removes managed configuration and default data; interactive mode requires typing the instance name, and unattended mode requires `--yes`.

Custom data, external workspaces, unknown files, system accounts, dependencies and linger settings are retained. Ambiguous service ownership, stop failures or mount points inside removal directories stop deletion. Default uninstallation removes the maintenance binary; to purge retained data later, use a complete package and the retained record:

```bash
./bin/molr-setup uninstall --record /path/installation.json --purge --yes
```
