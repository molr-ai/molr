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

For maintainer version naming, platform management, initial remote configuration, and failure retries, see [Release Maintenance](../../../dev-docs/en/releasing/releases.md). Source code deployers can manually configure `updates.source` for the required channel. Switching channels requires explicitly modifying the release source; lower versions are not executed as automatic downgrades.

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

For deployment and connection of independent Linux execution machines, see [Remote Sandbox Service](remote-sandbox.md). Local sandboxes are still embedded in the Runtime and do not require additional services.

## Model Response Diagnostic Logs

Raw model HTTP/SSE responses are not saved by default and do not enter the database. For troubleshooting, set the following in the service configuration:

```json
{
  "diagnostics": { "model_responses": true }
}
```

Takes effect after restart. Responses are written to `<data_dir>/logs/model-responses/responses.jsonl`, retaining at most the current file and two rotated files, each not exceeding 64 MiB, with file permissions 0600. Logs include response bodies and call IDs, and may contain conversation content; request authentication headers are not recorded. Log write failures do not affect model execution. Turning off the switch stops new records; existing files must be deleted according to the deployer's retention requirements.

This switch is independent of `debug.enabled`; the workbench can still display summaries, standard message content, HTTP status, duration, and usage without enabling raw response logs.
