# Use GitHub

With the official [GitHub CLI](https://github.com/cli/cli) (`gh`), your assistant can inspect repositories, issues, pull requests (PRs), and Actions runs. It can also create issues, comments, or PRs as requested.

## Installation and Authorization

First [prepare the execution environment](README.md#prepare-the-execution-environment), then send this request to your assistant:

> [!PROMPT]
> Follow the official instructions at https://github.com/cli/cli#installation to install GitHub CLI (gh) in your workspace. Check the environment and installation location, then guide me through creating a fine-grained personal access token for only the repositories I specify. Start with the permissions needed to read PRs and set an expiration. Provide the official creation page and configuration steps so I can save the token in a terminal, without sending it in chat. Load the token through GH_TOKEN on every invocation and first verify the specified repository's PR list. Explain any additional permissions needed so I can decide whether to grant them.

Ask the assistant to select an official release package for the sandbox's operating system and architecture, and keep the program inside `/work`. For cloning, committing, or pushing, also check that Git is available. Commands below use `gh`; use its absolute path if it is not on PATH.

### Recommended: Limit Repositories and Permissions

On GitHub's [fine-grained token creation page](https://github.com/settings/personal-access-tokens/new), choose the repository's **Resource owner**, set **Repository access** to **Only select repositories**, select only the repositories needed for the task, and set an expiration, such as 30 days. Under **Repository permissions**, choose permissions for the task and leave unused permissions disabled:

| Task | Starting permissions |
| --- | --- |
| Read PR lists, descriptions, and diffs | Pull requests: Read-only |
| Read issues | Issues: Read-only |
| Read or clone code | Contents: Read-only |
| Read Actions runs and logs | Actions: Read-only |
| Create or comment on issues | Issues: Read and write |
| Create or review PRs | Pull requests: Read and write; add Contents: Read and write if pushing a branch |

Combine permissions as needed rather than enabling every row. Keep the required read-only Metadata permission at its default. A command may call multiple endpoints; consult [GitHub's endpoint permission reference](https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens) for their requirements. The token can still read public repositories; repository selection primarily limits access to private resources and write access. Organizations may require administrator approval. Outside collaborators and operations across organizations have limitations; see [token configuration and limitations](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens).

Use the token through `GH_TOKEN` without running `gh auth login`, as recommended by the CLI for fine-grained PATs. Have the assistant prepare `/work/.config/gh` for credentials, then save the token yourself in a terminal to `/work/.config/gh/token`, containing only the token. Set directory permissions to `0700` and file permissions to `0600`. Keep them out of repositories and deliverables, and do not expose the token in chat, command history, or logs. This file stores the token in plaintext; assistants or processes that can read it can use the token.

Explicitly load `GH_TOKEN` from the file on every invocation and use the same configuration directory through `GH_CONFIG_DIR`. First check the program and authentication status:

```bash
gh --version
(
  GH_TOKEN="$(cat /work/.config/gh/token)" || exit 1
  export GH_TOKEN
  : "${GH_TOKEN:?Token file is empty}"
  GH_CONFIG_DIR=/work/.config/gh gh auth status --hostname github.com
)
```

The subshell checks stop execution if the file is missing or empty, avoiding fallback to other saved credentials. Regular non-interactive commands do not load `/work/.profile`, so do not rely on an `export` from a previous terminal. Use the explicit loading pattern below for subsequent calls. `GH_TOKEN` takes precedence over `GITHUB_TOKEN` and saved credentials. See the [authentication manual](https://cli.github.com/manual/gh_auth_login) and [environment variable reference](https://cli.github.com/manual/gh_help_environment).

### Alternative: Default Browser Authorization

If you accept broader account access, you can use the CLI's default browser and device-code login flow:

> [!PROMPT]
> Install GitHub CLI (gh) following the official instructions and use the default browser authorization flow. I understand that this requests broad permissions such as repo and is not restricted to the repository for this task. Send me the official authorization link and device code so I can approve access in my browser. Keep configuration in the workspace and first verify the specified repository's PR list.

This flow requests scopes including `repo`, `read:org`, and `gist` by default. The `repo` scope grants broad read and write access to public and private repositories, subject to the account's permissions and organization policies. A leaked token or an unintended command may affect repositories outside the task. Neither `--repo OWNER/REPO` nor `GH_CONFIG_DIR` restricts token permissions, and `gh auth refresh --remove-scopes` cannot remove the minimum `repo` scope. See [OAuth scopes](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps) and the [scope refresh manual](https://cli.github.com/manual/gh_auth_refresh).

Clear token environment variables in a subshell to use browser login and its saved credentials for this invocation:

```bash
(
  unset GH_TOKEN GITHUB_TOKEN
  export GH_CONFIG_DIR=/work/.config/gh
  gh auth login --hostname github.com --git-protocol https --web &&
    gh auth status --hostname github.com
)
```

Open the CLI's link in your own browser, enter the device code, and approve access. Without a system credential store, `gh` falls back to a plaintext token file; keep this configuration directory out of repositories and deliverables too. Switching to a scoped token later does not invalidate the old OAuth authorization. When no longer needed, revoke GitHub CLI under GitHub Settings → Applications → Authorized OAuth Apps; this can also affect other CLI environments using that authorization. Running `gh auth logout` alone does not revoke the server-side token. See the [logout manual](https://cli.github.com/manual/gh_auth_logout).

## Example Requests

> [!PROMPT]
> Using the repository-scoped token, review the ten most recently updated open issues in OWNER/REPO. Group them by problem type and include numbers, titles, and links. This is a read-only task; explain if the token needs Issues read permission.

<!-- -->

> [!PROMPT]
> Using the repository-scoped token, read this PR's description and diff. Summarize the changes and potential problems with source links. Explain the corresponding read permissions if you need source files or Actions logs, and mark any inaccessible check results as unverified.

<!-- -->

> [!PROMPT]
> Draft an issue for OWNER/REPO from this bug report, including reproduction steps, expected behavior, and actual behavior. Let me review it before creating the issue. Before creation, check that the token includes this repository and Issues write permission; guide me through adjusting that permission if needed.

Provide the repository's `OWNER/REPO` or full URL, and include a number or link for a specific issue or PR. For comments, branch pushes, or PR creation and merging, specify the target repository, branches, and scope.

## Verification and Troubleshooting

Replace `OWNER/REPO` below with a repository selected for the token and query a few open PRs with Pull requests read permission:

```bash
(
  GH_TOKEN="$(cat /work/.config/gh/token)" || exit 1
  export GH_TOKEN
  : "${GH_TOKEN:?Token file is empty}"
  GH_CONFIG_DIR=/work/.config/gh gh pr list --repo OWNER/REPO --state open --limit 5 --json number,title,url
)
```

If you chose default browser authorization, verify using its saved credentials instead:

```bash
(
  unset GH_TOKEN GITHUB_TOKEN
  GH_CONFIG_DIR=/work/.config/gh gh pr list --repo OWNER/REPO --state open --limit 5 --json number,title,url
)
```

An explicit `--repo` lets you query outside a local repository and avoids selecting the wrong project, but is not a permission boundary. A successful empty response may mean no PRs match; adjust the state and limit to widen the query. See the [PR list manual](https://cli.github.com/manual/gh_pr_list).

If authentication succeeds but the repository is inaccessible, check the Resource owner, selected repositories, token permissions and expiration, and organization approval status. For 403, 404, or unsupported endpoints, check the specific endpoint's requirements first; do not automatically switch to all-repository access or default browser authorization. A successful `gh auth status` does not prove access to the target operation; verify with an actual read-only query.

If the identity is unexpected, check whether `GH_TOKEN` or `GITHUB_TOKEN` overrides saved credentials, without printing their values. If later calls lose authentication, check that every invocation loads the token, that its file is readable, and that `GH_CONFIG_DIR` stays consistent and its configuration persists. When a token expires or is revoked, update authorization on GitHub and replace the local credential file.

Check other commands with `gh <command> --help`; see the [official manual](https://cli.github.com/manual/) for the full command set.
