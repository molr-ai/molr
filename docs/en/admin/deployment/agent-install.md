# Let an Agent install for me

Choose “Deploy backend” or “Deploy sandbox” on the website, then use “Let an Agent install for me” to copy the instructions to an Agent that can read URLs and use the target device’s terminal. Website instructions request the latest stable release by default. The Agent detects the OS, architecture, and download tool. You can copy instructions on a phone, but the Agent still needs terminal access to the target device.

This guide is also available as Markdown without JavaScript: [中文](https://molr.ai/agent-install.md), [English](https://molr.ai/en/agent-install.md). The following workflow is for the installing Agent.

## Confirm the target and existing instances

Identify the target device from the user’s instructions. Ask if it is unclear; do not treat your own temporary container or workspace as the installation device. For remote installation, use a connection the user has authorized. If you cannot connect, explain what is missing.

Inspect the OS, architecture, current account, service manager, available space, and existing Molr services and installation records. The release catalog defines supported native targets. macOS uses the current non-root user’s launchd service, without sudo. Linux defaults to the current non-root user’s systemd service. Root installation requires an explicit non-root `--user USER`.

If an instance exists, run its `molr-setup status --non-interactive` and check the version, paths, and service. A working instance can be handed over directly; updates, repairs, or additional instances must match the user’s intent. Do not delete installation records or data to bypass problems. Use the actual program paths reported by the instance for maintenance commands.

Choose `server` (default) for complete Molr or `sandbox` to add a resource node to an existing Molr server. Both use the same Runtime package. The installation record retains the mode for maintenance; updates and repairs cannot change it. Resource presets are independent of the mode.

## Check releases

When the user specifies a version, verify that version and the target platform in the release catalog and pass `--version VERSION` to both preview and installation. Do not substitute another version. For explicit channel selection or instructions without a version, read `https://molr.ai/releases/catalog.json` and check the user’s selected `stable`, `preview`, or `nightly` channel for a matching OS and architecture and its requirements. Default to stable only when the user has not selected a channel. If the channel is empty or lacks a compatible platform, explain and let the user choose; do not switch channels automatically.

If fetching fails, report that release information is unavailable and retry on the target device when appropriate. Do not invent versions or download URLs, or substitute unofficial packages. For Docker, follow [Docker deployment](https://molr.ai/en/docs/admin/deployment/docker/). Explain available alternatives for other unsupported environments.

## Preview the installation plan

Download the official script into a private temporary directory on the target device and keep it for execution. For example, with stable selected:

```sh
curl -fsSL --proto '=https' --proto-redir '=https' https://molr.ai/install.sh -o install.sh
sh install.sh --channel stable --mode server --non-interactive --dry-run
```

For a sandbox node, replace `--mode server` with `--mode sandbox` in both example commands; use `--name molr-node` for a separate instance. To expose commands and directories within the user’s authorization, explicitly select `--resource-preset permissive`. Unattended setup without a preset uses least privilege.

Use wget instead of curl if available. Replace stable with the selected channel; omit `--arch` to let the installer detect the device architecture. To pin the preview and execution to the same version, pass a catalog version with `--version VERSION` in both calls. The bootstrap downloads and verifies the package before running its `molr-setup`; do not bypass verification.

`--dry-run` downloads a temporary package and shows the plan without changing installation accounts, directory permissions, configuration, or services. It does not verify runtime readiness. Inspect the reported paths, account, service, dependencies, and permission issues. If customizing the instance, port, or access address, use the same options for preview and execution: `--name molr-NAME`, `--port PORT`, `--public-host PRIVATE_IPV4`.

Briefly explain the actual changes. Continue routine installation within existing authorization. Request specific authorization only for system changes outside that scope, such as installing system dependencies or changing shared directory permissions.

## Run the installer

Once the environment and permissions meet the requirements, execute the same script with the selected channel and inspected options:

```sh
sh install.sh --channel stable --mode server --non-interactive --no-browser
```

Add options according to the inspection:

- `--install-deps`: authorizes system dependency and policy preparation. Automatic preparation currently supports APT. Required administrator privileges must be available without waiting for password input in a silent flow.
- `--directory-permissions fix`: after authorization, removes group/other write permissions from directories in the plan without recursively changing their contents. Do not default to ignore.
- `--skip-dependencies`: use only after manually preparing and checking dependencies, never to conceal missing requirements.

`--yes` does not replace explicit dependency and permission choices. If installation fails, record the stage, changes already made, and next step. Repair the existing instance while retaining data. Do not disable the sandbox, weaken system isolation, or enable Full Access to make installation appear successful.

See [Installation and service management](https://molr.ai/en/docs/admin/deployment/installation/) for installation and service details.

## Verify and hand over

Use the actual installer result:

| Result | Meaning and next step |
| --- | --- |
| `verified` | Running version and authenticated capability checks passed; hand over the access entry |
| `verification_pending`, exit code 3 | Installed, but authenticated verification is incomplete; use current credentials as instructed, without reinstalling |
| `not_started` | `--no-start` was used; the service has not started and is not ready for use |
| Other failure | Report the failed stage and inspect the corresponding service logs before proceeding |

Repeating the same version may only display status; exit code 0 alone does not prove a fresh verification. For `server` mode, when additional checks are needed, run `molr doctor -config CONFIG` using the actual binary and configuration paths. After a password change, use `-password-stdin`; supply an MFA code as required. Use private input for credentials, never chat, command-line password arguments, or public logs. Wait for HTTP readiness before authentication, reuse valid sessions, and stop repeated logins on HTTP 429 until the rate limit allows them.

Verify the actual resource configuration; mode does not imply permissions. With Linux command execution enabled, authenticated `/api/sandbox` checks must show `Enabled=true`, `Capabilities.Reason=available`, and Shell, ProcessIsolation, FileIsolation, NetworkIsolation, ResourceLimits all true. macOS controlled execution requires Shell, FileIsolation, NetworkIsolation true, NetworkBackend seatbelt-toggle, and ProcessIsolation and ResourceLimits false. Least privilege disables commands: server mode retains built-in workspace storage, while sandbox mode disables the whole resource node.

In `sandbox` mode the installer verifies HTTPS capabilities using the node token. Do not run `molr doctor`, initialize users or models, or generate login links. Hand over the node HTTPS address, certificate SHA-256 fingerprint, and token file location, then connect it from the complete server’s Resources → Sandboxes page. Transfer tokens privately. The following login handover steps apply only to `server` mode.

Report the installed version and channel, target device, access address, service status, verification result, and unfinished steps. After a successful first install, privately hand over the installer’s 15-minute one-time login link with its validity period. Do not open or redeem it first. If no link was generated or it expired, use `molr access -config CONFIG` with current credentials via private input when needed. If credentials are unavailable, explain that the login entry is missing; never invent a link.

Check how the user will reach the device: a remote machine’s local or private address may require an existing SSH tunnel or network connection. Do not open public ports without authorization. Finally, guide the user to connect a model and create an assistant in the setup wizard. State when no model is configured; a successful installation alone does not make model conversations available.
