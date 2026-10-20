# Set up a Molr instance

> [!WARNING]
> **Project status:** Molr is under active development and has no stable release yet. You may encounter instability, and future versions may introduce breaking changes.

<!-- -->

> [!WARNING]
> **Network access:** Exposing the Molr service directly to the public internet is not currently recommended. Use it in a secure network environment, such as a trusted local network.

This walkthrough is for the person installing and preparing a service. Existing members can start with the [user quick start](../user/getting-started.md). Installation and system changes require access to the server; resource and member management require a Molr administrator account.

## Install and initialize

This guide deploys Runtime in `server` mode (the default), providing WebUI, accounts, assistants, and local sandbox capabilities. To add another device’s resources to an existing complete server, run the same Runtime package in `sandbox` mode using the [sandbox node guide](sandbox/remote.md); nodes initialize no users or assistants. Desktop local services and remote connections are covered in the [installation guide](deployment/installation.md#desktop-app).

For Docker, follow [Docker deployment](deployment/docker.md) to build Runtime and WebUI together, configure access, and initialize the account. The steps below describe native installation.

Copy the command under “Deploy backend” on the website. It uses curl, detects the OS and architecture automatically, and installs the latest stable release. It shows “Coming soon” when no stable package is available. Native packages support Linux and macOS on AMD64/ARM64 and include Runtime and WebUI. Linux Bubblewrap and static BusyBox dependencies come from the target system’s package manager; macOS uses system Seatbelt.

Alternatively, run the installer with the default stable channel, without saving a script file:

```bash
curl -fsSL https://molr.ai/install.sh | sh -s -- --mode server
```

To select another channel, append `--channel preview` or `--channel nightly`. See [installation and service management](deployment/installation.md) for channels and versions. The website has no channel selector; its command always installs the latest stable release. Users do not need Go, Node.js, or a separate web server.

First installation uses the current non-root user, selects an available port starting at 8787, and creates independent configuration. Linux installs a systemd user service; root execution requires a non-root account via `--user molr`. APT systems can prepare missing dependencies when authorized; other systems follow the reported requirements. The systemd user manager must be available; the installer checks and attempts to enable linger for operation after logout. If authorization is required or enabling it cannot be verified, follow the administrator command instructions and confirm `Linger=yes`. macOS 13 or later uses the current non-root user’s launchd service, without sudo.

Resource presets are independent of the mode. Interactive setup offers permissive or least privilege. Unattended setup without a preset uses least privilege, disabling commands while the complete server retains built-in workspace storage. Select `--resource-preset permissive` to expose local directories, networks, and compute; assistants still need grants. See [resource presets](deployment/installation.md#runtime-modes-and-resource-permission-presets) for the scope.

Upon completion, the installation outputs a LAN access address and a one-time login link valid for 15 minutes. Click the link and confirm login to enter the initial setup:

1. Select an auto-discovered local model service, or configure a cloud/custom Provider.
2. After connecting, confirm the selected model and the default daily budget of USD 1, which can be modified. Unknown prices must be supplemented; entering 0 explicitly confirms that it is free.
3. Click "Get Started" to automatically create the **Molr** assistant and enter the chat. The initial greeting calls the selected model and counts towards the budget.

Language and timezone follow browser settings. Interactive CLI installation asks for an administrator username and password by default, without saving a plaintext password. Unattended installation can explicitly use `--auto-init-admin` to create `owner` with a random password saved in the data directory’s `initial-password` file (0600). Alternatively, supply `--init-owner NAME` and the password on stdin. To generate another login link, use the installed program:

```bash
/path/molr access -config /path/server.json -username YOUR_ADMIN -password-stdin
```

Replace `YOUR_ADMIN` with your administrator username and provide the current password on stdin. Omit `-password-stdin` only for automatic initialization with a still-valid initial-password file. With MFA enabled, also provide `-mfa-code`. Re-running the installation preserves the original configuration, accounts, and data, without resetting passwords or recreating the assistant.

LAN can use HTTP without certificates. HTTP does not encrypt passwords, sessions, or messages, and is only suitable for trusted networks. For HTTPS configuration, see [Deployment and Maintenance](README.md). Browser capabilities such as system notifications may require HTTPS, but in-page reminders remain usable.

## Connect resources

Verify model capabilities and prices in [Model connections](resources/models.md). Prepare [sandbox resources](resources/sandbox.md) when assistants need files or commands. A connection does not grant assistants access automatically.

## Give members access

For multiple users, create a group, assign models, tools and quotas, then create member accounts using [Users and groups](administration/users-and-groups.md). New groups have no resources by default. A personal installation can use the initialized owner account.

## Start using and maintaining the instance

Follow the [user quick start](../user/getting-started.md) to hire an assistant and verify a small task. Use [configuration and access](deployment/configuration.md) for HTTPS and network settings, and [maintenance](deployment/maintenance.md) for updates and backups.
