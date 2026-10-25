# Updates and Backup Recovery

## Version Checks and Releases

User Settings → About displays the versions, commits, and consistency status of the WebUI and Runtime, while also showing the recommended version from the release source. After login, the server-side version status is read every five minutes; a supplementary check is performed when the page becomes visible again or the connection is restored. Disabling automatic release checks still retains the front-end and back-end consistency checks. If the version or commit is inconsistent, a prompt is displayed, requiring the user to save their input and refresh manually, or requiring the administrator to verify the versions in a separated deployment.

Runtime configuration supports:

```json
{"updates": {"source": "https://downloads.example.com/releases/stable", "auto_check": true}}
```

For package installations, About uses the source saved in the program directory’s `installation.json` and checks its `stable` channel; the configuration above is for source deployments without setup. No network access occurs if no release source is configured. `auto_check: false` disables automatic release checks in the background and on the About page, but manual checks are still possible. Development/local builds do not perform automatic checks by default. Public network sources require HTTPS; HTTP sources must be explicitly configured private network or loopback numeric addresses.

After the background starts, a check is performed with a delay of 30–60 seconds, followed by checks every 24 hours with a small random delay; failures are retried after one hour. When entering the About page or when the page becomes visible again, release information is refreshed if more than five minutes have passed since the last check. The minimum interval for manual checks is one minute; concurrent requests are merged across the entire instance. If a check fails, the last successful result and timestamp are retained; the check itself does not download or install programs.

Builds generate version packages and checksum files; the release pipeline publishes a GitHub Release after all target architectures pass, then deploys the official website and channel manifests. About in Desktop, Docker, and Runtime recommends only stable releases, including when the current build is Nightly, Beta, or RC. Runtime replaces a trailing `/preview` or `/nightly` in the source URL with `/stable`. Custom sources without a channel suffix keep their path, but must return a stable version. A newer stable release offers an upgrade; an equal version is up to date; a higher current version is identified as newer than stable without recommending a downgrade. No suitable stable release is reported explicitly, without falling back to prereleases or Nightly. Checks only prompt for updates and do not automatically download or install.

For maintainer version naming, platform management, initial remote configuration, and failure retries, see [Release Maintenance](../../dev/releasing/releases.md). Source deployments can configure a stable release source through `updates.source`. Manual installation or updates can still explicitly select other channels with `molr-setup update --channel preview|nightly`; About recommendations do not change the saved CLI subscription. Lower versions are not installed as automatic downgrades.

After discovering an update, administrators can view the upgrade command for the fixed target version. The running account, instance name, and program path are taken from the installation record; for source code deployments or missing records, follow this guide. Example:

```bash
molr-setup update --version 0.2.0
```

Before upgrading, preserve data according to the backup process on this page. The installer verifies the version and architecture of the downloaded package, retains existing accounts, data, and configurations, saves `molr.previous`, `molr-setup.previous` and `server.previous.json`, then replaces the program and restarts. Service restarts interrupt running tasks. After startup, wait for HTTP readiness, then perform an authentication check, verifying the actual Runtime version and sandbox capabilities; if authentication requires the current password/MFA or is rate-limited, output "Verification pending" and the `doctor` command, without looping login. On failure, backups and error information are retained, and no automatic rollback occurs; the previous program and configuration do not include a complete data backup.

## Backup Recovery and Upgrades

Use `molr-setup backup / restore` to migrate installer-managed instances. The database-only CLI commands have been removed from `molr`. Linux/macOS identify instances by their installation record; Windows uses the current user's desktop Runtime root and instance name. These commands do not automatically stop Docker or manually launched processes.

### Create a backup

```bash
molr-setup backup --output /private/molr-backup.tar.gz
# Select another instance with --record /path/installation.json or --name molr-example.
# Keep the source stopped when switching machines:
molr-setup backup --output /private/molr-migration.tar.gz --keep-stopped
```

The command acquires the maintenance lock, stops the service gracefully, confirms shutdown, copies files into private staging, creates `backup.json`, and packs the archive. By default it restores the original service state after copying, before compression. Previously stopped instances remain stopped. Failures attempt to restore the original service state and never publish a partial archive. The destination must not exist and must be outside the source directories. `TMPDIR` can select staging storage with sufficient free space; staging must also be outside the sources.

| Content | Runtime | Standalone Sandbox |
| --- | --- | --- |
| Programs | `molr`, `molr-setup`, release metadata, and licenses | Same |
| Database and identity | Complete database and encryption master key | Complete `sandbox.db`, service token, and keys, including operation receipts and network quota ledgers |
| Files | Data directory, including attachments, memories, and default workspaces; custom `sandbox.work_root` is collected separately | No user files, command-output caches, or external directory contents |
| Configuration | Deployment configuration directory and referenced TLS certificate/key and pricing/exchange-rate files | Same |
| External resources | Directory references, grants, and remote Sandbox connection information only | Directory configuration only |

Old backups, process locks, and regenerable `sandbox/runtime` execution-environment caches are excluded. Backup does not contact remote Sandboxes or include host dependencies, model services, or separately installed browser components. Prepare those dependencies on the destination. Archives contain master keys and credentials and must be treated as private data. Losing `keys/master.key` prevents recovery of encrypted Runtime credentials.

### Restore on another machine

If Molr is not installed on the destination, extract the backup programs into an empty directory and run the extracted tool on the same platform; no PATH entry exists yet. For a different OS or CPU architecture, use a complete destination-platform release package with the same version and commit as the backup:

```bash
tar -xzf /private/molr-backup.tar.gz program
./program/bin/molr-setup restore --archive /private/molr-backup.tar.gz --name molr-restored --dry-run
./program/bin/molr-setup restore --archive /private/molr-backup.tar.gz --name molr-restored --yes
```

For cross-platform recovery, replace the source executable above with the destination package's `bin/molr-setup`. The archive is still fully verified, but foreign binaries are not installed. Databases, attachments, and workspaces are restored; platform-specific sandbox settings use native defaults and are reported. Service accounts and registrations are generated for the destination. External references and remote connections are retained; missing or unparseable directories are unavailable. Filename collisions or unsupported symlinks prevent application instead of silently dropping files. Platform-specific executables inside workspaces may not run.

Restore supports:

- A new instance: select `--name`; `--program-root` sets the program base, with a separate instance subdirectory. Configuration and data follow Linux XDG/system or macOS user layouts.
- Replacement: explicitly select `--record` or the installed tool and pass `--overwrite --yes`. Modes must match; programs and data are restored together to the backup version. After stopping, a complete `before-restore-*.tar.gz` is saved in the target cache before replacement. Application failures before service startup restore original files. Database rollback is not automatic after startup because external actions may already have occurred.
- Preview and delayed startup: `--dry-run` extracts and validates the archive and shows the plan without modifying the target. `--no-start` restores and registers without starting, allowing address, certificate, and host configuration changes. Local/LAN HTTP addresses can use `--port` and `--public-host`; for direct TLS/proxy deployment, use `--no-start` and edit the restored configuration.

On Windows, run the destination-platform `molr-setup.exe`:

```powershell
.\bin\molr-setup.exe backup --name molr-desktop-0123456789abcdef --program-root C:\Molr\existing --output C:\Backups\molr.tar.gz
.\bin\molr-setup.exe restore --name molr-restored --program-root C:\Molr\restored --archive C:\Backups\molr.tar.gz --yes --no-start
```

Windows setup currently manages desktop Runtime instances, not standalone Sandbox service registration. Desktop recovery uses loopback HTTP and reports that authenticated sandbox verification is still required.

Source UID/GID and service registration files are not copied. Restore revokes Runtime sessions, API tokens, and login challenges while preserving accounts and MFA configuration. Remote Sandbox connections try their saved addresses, tokens, and certificate trust; network reachability is not guaranteed. Missing external directories are not replaced with empty folders and do not remove internal data.

Data restoration and service verification are reported separately. Check accounts, assistants, conversations, attachments, workspaces, model connections, and sandbox capabilities before switching; keep the source stopped to avoid duplicate channel and task processing. Running processes cannot be restored. Reconcile incomplete operations from their saved state without blindly replaying unknown external outcomes. Recheck filesystem project IDs and workspace quotas on the destination.

For password recovery, a host administrator stops the service and runs `molr recover -password-stdin`; add `-reset-mfa` to reset MFA. Local recovery revokes all sessions and tokens. Keep a complete backup before upgrading; recovery and upgrading are separate steps.


## File logs and rotation

Runtime-managed file logs use the shared `internal/logging` module, including service logs selected by `--log-file` (HTTP requests, startup, and runtime diagnostics) and optional model response logs. Callers define record formats; the shared module owns file permissions, concurrent writes, rotation, and archive cleanup. Standard output/error bypass file rotation; collection or redirection by journald, launchd, or a shell follows deployment policy.

The service configuration applies one policy to each file log set. These are the defaults:

```json
{
  "logging": {
    "max_file_bytes": 268435456,
    "backup_files": 1
  }
}
```

`max_file_bytes` is the positive integer size limit in bytes per file, defaulting to **256 MiB**. `backup_files` is the number of archived files, excluding the current file, from 0 to 100. The default keeps the current file and one archive, totaling at most approximately **512 MiB** per log set. Rotation occurs before a write would exceed the limit. Archives use `.1`, `.2`, and so on, with `.1` newest; the oldest files beyond the limit are deleted. With 0 backups, rotation retains only the new current file. A record larger than the file limit is rejected rather than split.

Restart after changing the configuration. Service logs apply the policy when opened; enabled model logs apply it on the first write. Reducing size or retention removes archives exceeding the new policy. Newly created log directories use `0700`, and files use `0600`. Retention is based on size and count, with no age-based expiration. After disabling model logging, remove existing files according to deployment policy.

## Diagnosing HTTP request latency

Runtime emits one structured `http_request` log for every `/api` request by default. `molr serve` writes it to stderr, or to the file selected by `--log-file /private/runtime.log`; systemd deployments can use the service journal. Fields are `request_id`, `method`, `route` (the registered route template), `status`, `header_ms`, `duration_ms`, `bytes`, `stream`, `aborted`, `canceled`, and `write_error`. Actual object paths, query parameters, request/response bodies, cookies, and authentication headers are omitted. Unmatched routes use `unmatched`.

Select a slow request in the browser Network panel and use its `X-Request-ID` response header to find the matching log. `Server-Timing: runtime;dur=...` reports milliseconds from entering the Runtime HTTP handler to committing response headers, matching `header_ms`. Browsers supporting Server Timing show it in the Timing panel. It includes authentication, rate limiting, business logic, and request body reads before headers are committed. It excludes connections/TLS and proxy queues before handler entry, and transfer after headers are committed.

- Large browser time to first byte and large `header_ms`: investigate server processing and dependencies; uploads may also be waiting for the request body.
- Browser time to first byte much larger than `header_ms`: inspect browser queues, connections, proxies, and the network. The difference is not an exact measurement of network latency.
- `duration_ms` much larger than `header_ms`: work continues after headers are committed, potentially response generation or blocked writes. Compare the download phase, `bytes`, and `write_error`.

`duration_ms` ends when the handler returns, including blocked response writes; it does not mean the client has received all bytes. SSE logs are emitted when connections end. For `stream=true`, total duration is connection lifetime, not slow request processing; `Server-Timing` is available as soon as the connection opens. Client cancellation sets `canceled=true`. An abnormal handler exit sets `aborted=true`; `status=0` means no response headers had been committed.

## Model Response Diagnostic Logs

Raw model HTTP/SSE responses are not saved by default and do not enter the database. For troubleshooting, set the following in the service configuration:

```json
{
  "diagnostics": { "model_responses": true }
}
```

Takes effect after restart. Responses are written to `<data_dir>/logs/model-responses/responses.jsonl`, with file permissions and retention following the `logging` configuration above. Logs include response bodies and call IDs, and may contain conversation content; request authentication headers are not recorded. Log write failures do not affect model execution. Turning off the switch stops new records; existing files must be deleted according to the deployer's retention requirements.

This switch is independent of `debug.enabled`; the workbench can still display summaries, standard message content, HTTP status, duration, and usage without enabling raw response logs.

## Data directories and encryption keys

The Runtime saves data in a private data directory by default; custom workspaces, externally authorized directories, and remote sandboxes require separate management. A schematic of the default directory is shown below:

```text
数据目录/
├── runtime.db       # 主数据库：账号、聊天、任务、执行记录、网络账本
├── keys/            # 加密密钥：用来加密封存的模型访问密钥
├── files/           # 聊天附件与助理产出的文件
├── media/           # 图片、音频等处理缓存（可自动重建）
├── sandbox/         # 默认助理工作目录
└── backups/         # 数据库快照的临时维护目录（不进入实例备份）
```

Key points to understand:

- Chat content and task records are stored in raw form in your own database and are not sent to Molr official servers.
- Sensitive credentials such as model access keys are stored encrypted; encryption keys are stored in `keys/` and included in complete instance archives. Protect the archive as secret material; losing the keys makes saved model credentials unrecoverable.
- When conversing with cloud models, conversation content is sent to the model provider you select; this is independent of whether you self-host.

For specific backup and recovery operations, see the [Deployment Guide](../README.md).
