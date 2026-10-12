# Use Google Services

With [Google Workspace CLI](https://github.com/googleworkspace/cli) (`gws`), your assistant can find Google Drive files, read Gmail, organize Calendar events, and work with Docs and Sheets. The assistant invokes the CLI through Shell; no Molr-specific plugin is required. The project is hosted under the `googleworkspace` organization but is not an officially supported Google product.

## Installation and Environment

Complete [execution environment preparation](README.md#prepare-the-execution-environment), then send this request to your assistant:

> Follow the installation instructions at https://github.com/googleworkspace/cli to install gws in your workspace. Check the operating system and architecture, choose the appropriate release archive, and verify its checksum. First check the version, the Drive query schema, and a dry-run. Then guide me through Google authorization on my own computer, configure the credentials in the workspace, and finally retrieve the names and links of five files that are not in the trash.

Choose the program for your operating system and architecture from the [project releases](https://github.com/googleworkspace/cli/releases), and extract it into a workspace directory such as `/work/.local/bin/`. Prefer an `unknown-linux-musl` archive for a minimal Linux sandbox. Using a release binary directly does not require Node.js, npm, or Rust. The examples use `gws` to mean the installed program; use its absolute path if it is not on PATH.

```bash
gws --version
TOKIO_WORKER_THREADS=2 GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/work/.config/gws gws schema drive.files.list
TOKIO_WORKER_THREADS=2 GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/work/.config/gws gws drive files list \
  --params '{"pageSize":5,"fields":"files(id,name,webViewLink),nextPageToken","q":"trashed = false"}' \
  --dry-run
```

The sandbox examples set `TOKIO_WORKER_THREADS=2` to limit async worker threads. On machines with many CPU cores, the default thread count can exhaust the sandbox process quota and leave a command waiting.

`schema` describes command parameters and the response structure. `--dry-run` previews the request without executing the target API operation; it does not prove that an account is authorized. Fetching API definitions for the first time requires network access, even when requesting help for a service. Installation needs access to GitHub and release download endpoints; operation needs Google Discovery, OAuth, and the relevant service APIs. Grant the assistant the network access it actually needs.

## Authorize on Your Own Computer

`gws auth login` uses a local browser callback. When the browser runs on your computer and the CLI runs in a remote Molr sandbox, localhost in the authorization URL refers to different environments. This guide follows the project's [headless authorization workflow](https://github.com/googleworkspace/cli#authentication): sign in on a computer with a browser, then provide exported credentials to the sandbox.

1. Install `gws` on your own computer too. Follow the project's manual OAuth setup instructions to create or select a Google Cloud project, enable the APIs you need, configure the OAuth consent screen, and create a **Desktop app** OAuth client. Add your account as a test user if the app is in testing mode.
2. Download the client JSON and save it as `client_secret.json` at the `client_config` path reported by `gws auth status`. If `gcloud` is already installed and authenticated, you can use `gws auth setup` to assist with setup. Ordinary CLI operations do not require `gcloud`.
3. Start with read-only Drive access, confirm the account and permissions in your browser, then export the credentials. Run these commands in a terminal on your own computer:

```bash
gws auth login --readonly -s drive
umask 077
gws auth export --unmasked > credentials.json
```

Through an authorized file directory, copy `credentials.json` to the assistant's `/work/.config/gws/credentials.json`. Set directory permissions to `0700` and file permissions to `0600`. The credential file contains renewable authorization: keep its contents out of chat, Skills, and deliverable directories. See [Files & Commands](../guides/files-and-commands.md) for file transfer options.

Specify the same configuration directory and credential file on every invocation, rather than relying on environment variables set temporarily in a previous terminal:

```bash
TOKIO_WORKER_THREADS=2 \
GOOGLE_WORKSPACE_CLI_CONFIG_DIR=/work/.config/gws \
GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE=/work/.config/gws/credentials.json \
gws drive files list \
  --params '{"pageSize":5,"fields":"files(id,name,webViewLink),nextPageToken","q":"trashed = false"}'
```

When you need Gmail, Calendar, or Sheets, sign in again on your own computer and select the relevant services, for example with `gws auth login --readonly -s drive,gmail,calendar,sheets`. Export and replace the credentials again. Select the corresponding write permissions when creating or modifying content. Service names limit the scope selection; the OAuth scopes confirmed on the consent screen determine the actual authorization. The Google account must also have access to the target material.

## Example Requests

> Find material related to this week's meetings in the specified Drive folder. Organize names, summaries, and original links by topic.

> Organize Gmail messages from the past seven days that need my reply. List senders, subjects, and suggested next steps. Only draft replies.

> Review tomorrow's Calendar events and list times, participants, and preparation tasks in my specified timezone.

> Read the specified worksheet and cell range in this Google Sheets document, summarize the data, and identify missing entries. Include the source link.

Provide the target account, file or folder links, worksheet range, and time range. For sending email, creating events, or editing documents, specify the target and execution scope; ask for a draft first if you want to review it. Start with commands such as `gws gmail --help` or `gws calendar --help` for common operations, and use `gws schema <service.resource.method>` for complex parameters. See the [project documentation](https://github.com/googleworkspace/cli#helper-commands) for more examples.

## Verification and Troubleshooting

After initial authorization, run the five-file query above and check that the results belong to the intended account. An empty list may simply mean that no files match. Credentials appearing in `auth status` do not replace an actual API query. Verify each additional service with a small read-only request.

| Symptom | What to check |
| --- | --- |
| The program exists but cannot execute | Check the architecture, executable permissions, and archive type; in a minimal Linux environment, check whether you selected a GNU build that needs dynamic libraries |
| The version works but a service command keeps waiting | Set `TOKIO_WORKER_THREADS=2` on every invocation and check the sandbox process quota, which also counts threads |
| Schema or service help cannot load | Check network access to Google Discovery; missing account authorization is usually not the cause of a schema lookup failure |
| The CLI keeps waiting after browser authorization | Ensure the browser and login command run on the same computer; use the export workflow above for a remote sandbox |
| Missing credentials or lost login state | Check the configuration directory and credential file path on every invocation, and whether the files persist; `GOOGLE_WORKSPACE_CLI_TOKEN` takes precedence over the credential file |
| The API reports insufficient permissions or a disabled API | Check API enablement in the Cloud project, OAuth scopes, and sharing permissions on the target file; export credentials again after granting additional access |
| Refresh fails or returns `invalid_grant` | Check revoked authorization, OAuth app status, and refresh token validity; sign in again and replace the exported file |

After upgrading, check `gws --version`, the command's `--help`, and `schema`, then run a small read-only query. To disconnect, remove workspace credentials and revoke the application's access in your Google account authorization settings.
