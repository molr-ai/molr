# Installation and Service Management

## Local Distribution Packages and Quick Installation

Distribution packages are distributed via GitHub Releases. The official website displays the actually available platforms, versions, installation commands, and download links for each channel. Stable releases use `stable`, beta/rc use `preview`, and daily builds use `nightly`; channels without a released version do not provide an install button. Online and offline installations use the installation script from the selected Release; for creating your own installation package, see [Source Code Build](source-build.md).

Install from the installation package:

```bash
bash install.sh --archive /path/molr_0.1.0_linux_amd64.tar.gz
# 独立实例示例
bash install.sh --archive /path/package.tar.gz --name molr-demo --port 8788 --public-host "<LAN_IP>"
```

The `<LAN_IP>` in the above command must be replaced with the actual LAN IP of the installation or download server; it cannot be executed as-is.

Prepare Python 3.11+ before online installation. Copy the command from the installation section of the official website, or run it through a pipe without saving a script file:

```bash
curl -fsSL https://molr.ai/install.sh | bash -s -- --channel stable
curl -fsSL https://molr.ai/install.sh | bash -s -- --channel preview
curl -fsSL https://molr.ai/install.sh | bash -s -- --channel nightly
# 明确安装固定版本，同时保存后续跟踪的渠道
curl -fsSL https://molr.ai/install.sh | bash -s -- --channel preview --version 0.2.0-rc.1
```

The script uses `https://molr.ai/releases` by default; `--source URL` can specify other release sources. The default channel is `stable`; it will not automatically switch to `preview` or `nightly` if no stable version is available. Fixed versions are read from the historical manifest; installation stops if packages for the target system/architecture are missing. If verification fails, the installer within the package is not executed. HTTP download sources only accept explicit private network or loopback numeric addresses; SHA-256 verification does not constitute an independent publisher signature.

For offline installation, download the version package, the corresponding `.sha256` file, and the `install.sh` for that Release to the target machine, and run `bash install.sh --archive <包路径>`. Each Release retains a snapshot of the installation script; the actual installation logic is released with the package. The official website lists the corresponding version's systemd, Python, cgroup, and system dependency requirements. Adding or stopping platform support does not change historical distribution packages.

Installers that omit configuration files automatically generate LAN HTTP configuration, initialize the owner and setup, install the service, and check the authenticated API. `--install-deps` provides apt dependency installation, `--yes` explicitly agrees to dependency installation, and `--skip-dependencies` skips it. Missing isolation dependencies do not automatically open Full Access; sandbox capabilities are reported based on real probes. Installation and uninstallation require Python 3.11+; systemd and basic installation tools must still be available. The installer checks that BusyBox must be an executable static program that is not writable by the group or other users; if only dynamic BusyBox is available, it will still prompt to install `busybox-static`. If apt or offline installation is not supported, please pre-install static BusyBox and other dependencies via the target system's package manager. The installer does not copy BusyBox to the Molr installation directory.

`LICENSE` and `THIRD_PARTY_NOTICES.md` are saved in the root directory of the installation prefix, for example, the user installation directory `~/.local/lib/molr/default/` for the default instance or the system installation directory `/opt/molr/default/`. The embedded WebUI, standalone WebUI, and official website all provide `/LICENSE` and `/THIRD_PARTY_NOTICES.md`; these two files should be retained when distributing the build directory. The license text is centrally maintained in the repository root directory and does not use third-party submodules.

If linger is not enabled for the user service, logging out may stop the service; to allow the system to run when not logged in, the administrator must execute `loginctl enable-linger USER`. System services for specified accounts do not depend on linger.

After installation, use the following commands to check or regenerate access entries:

```bash
/path/molr-server doctor -config /path/server.json
/path/molr-server access -config /path/server.json
```

A random initial password in the data directory is used by default. Add `-password-stdin` if the password has been changed, and add `-mfa-code` if MFA is enabled; these commands work through the normal authentication API, do not open the running database, and do not bypass MFA. The link is valid for 15 minutes; the actually delivered link is not used for health checks.

## Linux Services and Command Environment

Linux Shell requires Bubblewrap, static BusyBox, available user namespace, and cgroup v2 delegation. For installation paths and current limitations, see [Linux Command Environment](sandbox.md). Use independent systemd user services; services with sandbox enabled must at least adopt:

```ini
[Service]
Type=exec
ExecStart=/path/molr-server serve -config /private/server.json
Delegate=yes
DelegateSubgroup=supervisor
KillMode=control-group
Restart=on-failure
```

Use real absolute paths; multiple instances are named, configured, and assigned ports and data directories separately. Bubblewrap/helper must be executable regular files; paths and parent directories cannot be accessed via symbolic links, and files cannot be group-writable or writable by other users. Do not relax isolation conditions to ensure successful startup.

### Selecting the Runtime Account During Installation

[Linux Installation Script](../../../runtime/scripts/install-linux.sh) installs the `molr.service` user service for the current regular user by default. When `--user USER` is specified, root is required, a system service is installed, and `User=` / `Group=` are used to make the entire Runtime, file operations, and local sandbox use the target identity; Molr programs do not start as root, nor do they switch users during runtime. No additional sandbox services, ports, or connection credentials are needed locally.

For the first installation, the owner can be initialized directly, with the password read from standard input:

```bash
# 当前账户，无需 sudo。依赖须预先安装。
bash runtime/scripts/install-linux.sh --init-owner owner \
  .cache/build/molr-server /path/server.json < /private/owner-password

# 指定账户，须由管理员执行；账户不存在时自动创建。
sudo bash runtime/scripts/install-linux.sh --user molr-sandbox --init-owner owner \
  .cache/build/molr-server /path/server.json < /private/owner-password
```

`--user` can specify an existing regular user, retaining their HOME, login shell, and supplementary groups; new users are created as system accounts without interactive login, without creating HOME, and without adding extra groups or sudo permissions. Root execution must explicitly specify `--user`, rejecting root and other UID 0 accounts. If pre-checks for the account and data directory fail, installation stops, and existing directories are not recursively chowned.

| Content | Current User Installation | Specified User Installation |
| --- | --- | --- |
| Program, License, Uninstaller | `~/.local/lib/molr/<实例>/` | `/opt/molr/<实例>/`, managed by root |
| Configuration and Installation Records | `${XDG_CONFIG_HOME:-$HOME/.config}/molr/<实例>/` | `/etc/molr/<实例>/` |
| Default Persistent Data | `${XDG_DATA_HOME:-$HOME/.local/share}/molr/<实例>/` | `/var/lib/molr/<实例>/` |
| Cache and Service Temporary Files | `${XDG_CACHE_HOME:-$HOME/.cache}/molr/<实例>/` | `/var/cache/molr/<实例>/` |
| Service Files | `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/<服务名>.service` | `/etc/systemd/system/<服务名>.service` |
| Management Method | `systemctl --user … <服务名>.service` | `sudo systemctl … <服务名>.service` |

The default service name is `molr`, and the instance directory name is `default`; `--name molr-demo` uses instance directory `demo` and service `molr-demo.service`. `molr-default` is a reserved name; please use `molr`. The user program directory defaults to being based on the home directory of the installation account and does not change with `XDG_DATA_HOME`. `--program-root /path/programs` can be used to change the program root directory, with the program installed in its `<实例>/` subdirectory; the same parameters are used during updates. The program entry point is `bin/molr-server` in the program directory; the configuration file is `server.json` in the configuration directory, with user installation permissions of `0600`, and system installation permissions of `0640` managed by root and readable by the target group.

Databases, keys, attachments, assistant projects, skills, memories, and tools installed in the workspace are all considered persistent data. The service uses `tmp/` in the independent cache directory as `TMPDIR`, and `XDG_CACHE_HOME` points to the cache directory of this instance; rebuilding the cache does not affect persistent data. Program, configuration, data, and cache directories cannot overlap.

The `data_dir` specified in the configuration template remains unchanged; existing data must be owned and accessible by the target account, otherwise an error is reported. When copying configuration for the first time, `sandbox.host_tools: true` is set by default; explicit `false` or custom rootfs are retained. Paths for certificates, external workspaces, etc., in the configuration must also allow access by the target account; the installer does not modify the permissions of these files.

Upgrades re-execute the same command, omitting `--init-owner`: configuration, data, credentials, and users at the installation location are retained, the program is atomically replaced while retaining `molr-server.previous`, and configuration is backed up as `server.previous.json`. Instances with the same name cannot silently switch accounts; data must be explicitly migrated, or a new instance name must be used. Stop the original instance and back up before migration; do not directly change ownership or copy the running database.

`--name molr-<名称>` can be used to install independent instances; configuration, ports, and data directories must also be independent. `--no-start` only prepares files, does not enable or start services, and outputs commands to initialize and start with the correct identity. Uninitialized data must use `--init-owner` or `--no-start`; a service that cannot listen will not be reported as successfully installed.

The installer checks for systemd ≥254, Python 3, Bubblewrap, static BusyBox, CA certificates, file, util-linux; system installation additionally requires useradd/runuser. Missing dependencies result in explicit errors; sudo or package managers are not called during regular user installation. Both units contain `Type=exec`, `Delegate=yes`, `DelegateSubgroup=supervisor`, `KillMode=control-group`, `NoNewPrivileges=yes`. User services require an existing user manager; if startup is required when not logged in, the administrator can separately configure `loginctl enable-linger USER`; system services do not require linger.

After installation, the authenticated sandbox check in the next section must still be completed. The installer's `is-active` only confirms service status and does not replace real isolation capability acceptance.

Existing Docker images are streamlined Runtime images and do not yet include a complete automatic command environment; it cannot be assumed that Shell can be executed simply by starting HTTP in the container.

## Post-Startup Checks

1. Log in via the actual WebUI address to verify pages, messages, and backend connections.
2. Request `/api/status` and `/api/sandbox` through the same-origin proxy in a logged-in browser; do not only check ports or process logs.
3. Unless the sandbox is explicitly disabled, require `Enabled=true`, `Capabilities.Reason=available`, as well as `Shell`, `ProcessIsolation`, `FileIsolation`, `NetworkIsolation`, `ResourceLimits` to all be `true`.
4. Use test assistants and non-sensitive materials to verify permissions, model calls, and actual results; unauthorized capabilities cannot be used.

Re-verify after every startup, rebuild, or restart. For failure reasons and handling entries, see [Sandbox Check](sandbox.md#checks-and-verification); do not mask anomalies by disabling the sandbox or falling back to host execution.

## Uninstallation

The installation package saves the offline uninstall entry as `uninstall.sh` in the program directory, requiring no source code or network. For example, for the default user instance:

```bash
bash ~/.local/lib/molr/default/uninstall.sh --dry-run
bash ~/.local/lib/molr/default/uninstall.sh
```

By default, uninstallation stops the corresponding service and tasks, removes service registration, programs, and instance cache, while retaining configuration, installation records, and persistent data. Re-running the installation command for the same instance can reuse this data; the uninstallation output lists the retained paths.

To completely clear the managed configuration and data for this instance:

```bash
bash ~/.local/lib/molr/default/uninstall.sh --purge
# 无交互执行时，显式确认数据删除
bash ~/.local/lib/molr/default/uninstall.sh --purge --yes
# 系统安装的实例
sudo bash /opt/molr/demo/uninstall.sh --purge
```

`--purge` first displays the deletion plan, then requires entering the service name for confirmation; `--dry-run --purge` only previews. Custom `data_dir`, external `sandbox.work_root`, authorized external directories, system accounts, system dependencies, and linger settings are not deleted. Only default data directories registered by the installer as managed are included in the cleanup; if the directory has other file systems mounted, they must be unmounted first. If service ownership cannot be confirmed, stopping fails, or path validation fails, deletion is aborted and the reason is reported.

Source code checkouts provide `bash scripts/uninstall.sh --name molr-demo`; for system instances, add `--system` and execute as root. `bash scripts/uninstall-linux.sh --name molr-demo` from the extracted distribution package can also be used. These two entries can continue to clear retained data after default uninstallation; when using custom XDG directories, reuse the environment variables from installation, or specify the retained installation record via `--record /path/installation.json`. Passwords and keys are not saved in the installation record.
