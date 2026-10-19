# Updates and Backup Recovery

## Version Checks and Releases

User Settings → About displays the versions, commits, and consistency status of the WebUI and Runtime, while also showing the recommended version from the release source. After login, the server-side version status is read every five minutes; a supplementary check is performed when the page becomes visible again or the connection is restored. Disabling automatic release checks still retains the front-end and back-end consistency checks. If the version or commit is inconsistent, a prompt is displayed, requiring the user to save their input and refresh manually, or requiring the administrator to verify the versions in a separated deployment.

Runtime configuration supports:

```json
{"updates": {"source": "https://downloads.example.com/releases/stable", "auto_check": true}}
```

For package installations, the program directory’s `installation.json` supplies the authoritative source and channel; the configuration above is for source deployments without setup. No network access occurs if no release source is configured. `auto_check: false` disables automatic release checks in the background and on the About page, but manual checks are still possible. Development/local builds do not perform automatic checks by default. Public network sources require HTTPS; HTTP sources must be explicitly configured private network or loopback numeric addresses.

After the background starts, a check is performed with a delay of 30–60 seconds, followed by checks every 24 hours with a small random delay; failures are retried after one hour. When entering the About page or when the page becomes visible again, release information is refreshed if more than five minutes have passed since the last check. The minimum interval for manual checks is one minute; concurrent requests are merged across the entire instance. If a check fails, the last successful result and timestamp are retained; the check itself does not download or install programs.

Builds generate version packages and checksum files; the release pipeline publishes a GitHub Release after all target architectures pass, then deploys the official website and channel manifests. The saved release source for installation includes the channel, for example `https://molr.ai/releases/preview`. The Runtime reads the recommended version from this channel; `stable` rejects pre-release versions, `preview` can recommend official versions, and `nightly` only tracks nightly builds. Checks only prompt for updates and do not automatically download or install.

For maintainer version naming, platform management, initial remote configuration, and failure retries, see [Release Maintenance](../../dev/releasing/releases.md). Source code deployers can manually configure `updates.source` for the required channel. Switching channels requires explicitly modifying the release source; lower versions are not executed as automatic downgrades.

After discovering an update, administrators can view the upgrade command for the fixed target version. The running account, instance name, and program path are taken from the installation record; for source code deployments or missing records, follow this guide. Example:

```bash
~/.local/lib/molr/default/bin/molr-setup update --version 0.2.0
```

Before upgrading, preserve data according to the backup process on this page. The installer verifies the version and architecture of the downloaded package, retains existing accounts, data, and configurations, saves `molr.previous`, `molr-setup.previous` and `server.previous.json`, then replaces the program and restarts. Service restarts interrupt running tasks. After startup, wait for HTTP readiness, then perform an authentication check, verifying the actual Runtime version and sandbox capabilities; if authentication requires the current password/MFA or is rate-limited, output "Verification pending" and the `doctor` command, without looping login. On failure, backups and error information are retained, and no automatic rollback occurs; the previous program and configuration do not include a complete data backup.

## Backup Recovery and Upgrades

The data directory permissions are `0700`, and the master key `keys/master.key` is `0600`. If an existing database lacks a master key, the service refuses to start, and a new key cannot replace the old key.

Stop the corresponding instance before executing maintenance commands:

```bash
/path/molr backup -config /private/server.json > /private/backup.json
/path/molr restore -config /private/server.json -backup-manifest /private/backup.json
```

`backup.json` is the backup description; the command only generates a database snapshot and does not package the complete instance. Complete disaster recovery should retain a complete copy of the configuration and data directory after stopping the service; if restoring from a database snapshot, the snapshot file and its description must also be retained, and the following data must be restored simultaneously:

| Content | Backup Location and Requirements |
| --- | --- |
| Attachments and delivery files | `files/` in the data directory |
| Encrypted master key | `keys/` in the data directory, protected as secret material |
| Assistant workspace | Default `sandbox/work/`; if `sandbox.work_root` is configured, back up the actual external path |
| Deployment configuration and external files | Configuration files, certificates, external price/exchange rate files, etc., retaining actual paths and permissions |

Network quota usage and unsettled reservations are included in the actual execution-side database: local machine is `runtime.db`, independent Sandbox is `sandbox.db`.

Snapshots and the above files must come from the same stop-service backup window. Data in external authorization directories and remote Sandboxes is not included in the Runtime database snapshot and must be backed up independently on the corresponding machines; when restoring workspace project quotas, the file system project ID and quota configuration must also be verified. Copying only the main database file currently in use by WAL is not a complete backup.

Recovery requires the service to be stopped. Upon success, old sessions, access tokens, and challenges in the recovery copy are revoked. After recovery, first confirm that accounts, resources, and credentials are available, then verify incomplete operations to avoid repeating external actions with unknown results.

If the password is forgotten, an operator with host management privileges executes `recover -password-stdin` after stopping the service; to reset two-factor authentication, additionally add `-reset-mfa`. Local recovery revokes all sessions and tokens.

Before upgrading, retain the old binary, configuration, and complete backup. Use a clean build of the target commit, verify static linking, permissions, and checksums, then atomically replace the binary using a temporary file, and restart the corresponding instance. Code rollback does not automatically undo data migrations or external side effects. After the upgrade is complete, re-perform page, authentication, model, and sandbox capability checks.

For deployment and connection of independent Linux execution machines, see [Remote Sandbox Service](../sandbox/remote.md). Local sandboxes are still embedded in the Runtime and do not require additional services.

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

Takes effect after restart. Responses are written to `<data_dir>/logs/model-responses/responses.jsonl`, retaining at most the current file and two rotated files, each not exceeding 64 MiB, with file permissions 0600. Logs include response bodies and call IDs, and may contain conversation content; request authentication headers are not recorded. Log write failures do not affect model execution. Turning off the switch stops new records; existing files must be deleted according to the deployer's retention requirements.

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
└── backups/         # 执行备份命令生成的数据库快照
```

Key points to understand:

- Chat content and task records are stored in raw form in your own database and are not sent to Molr official servers.
- Sensitive credentials such as model access keys are stored encrypted; encryption keys are stored separately in the `keys/` directory and **do not appear in backup files**. Please keep backups and the `keys/` directory separate—if keys are lost, saved model keys cannot be recovered.
- When conversing with cloud models, conversation content is sent to the model provider you select; this is independent of whether you self-host.

For specific backup and recovery operations, see the [Deployment Guide](../README.md).
