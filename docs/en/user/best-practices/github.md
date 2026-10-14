# Use GitHub

With the official [GitHub CLI](https://github.com/cli/cli) (`gh`), your assistant can inspect repositories, issues, pull requests (PRs), and Actions runs. It can also create issues, comments, or PRs as requested.

## Installation and Authorization

First [prepare the execution environment](README.md#prepare-the-execution-environment), then send this request to your assistant:

> [!PROMPT]
> Follow the official instructions at https://github.com/cli/cli#installation to install GitHub CLI (gh) in your workspace. Check the environment and installation location, guide me through GitHub login, and send me the authorization link and device code. Start by checking the PR list for a repository I specify.

Ask the assistant to select an official release package for the sandbox's operating system and architecture, and keep the program inside `/work`. For cloning, committing, or pushing, also check that Git is available. Commands below use `gh`; use its absolute path if it is not on PATH.

Keep configuration in the workspace with `GH_CONFIG_DIR`, using the same directory for every invocation. Run the initial setup in a terminal:

```bash
gh --version
GH_CONFIG_DIR=/work/.config/gh gh auth login --hostname github.com --git-protocol https --web
GH_CONFIG_DIR=/work/.config/gh gh auth status --hostname github.com
```

Open the CLI's link in your own browser, enter the device code, and approve access. Without a system credential store, `gh` falls back to a plaintext token file; keep this configuration directory out of repositories and deliverables. For non-interactive use, you can supply a token through `GH_TOKEN` on every invocation; it overrides saved credentials. See the [authentication manual](https://cli.github.com/manual/gh_auth_login) and [environment variable reference](https://cli.github.com/manual/gh_help_environment).

## Example Requests

> [!PROMPT]
> Review the ten most recently updated open issues in the specified repository. Group them by problem type and include numbers, titles, and links.

<!-- -->

> [!PROMPT]
> Read this PR's description, diff, and check results. Summarize the changes, potential problems, and checks that have not passed, with source links.

<!-- -->

> [!PROMPT]
> Draft an issue for the specified repository from this bug report, including reproduction steps, expected behavior, and actual behavior. Let me review it before creating the issue.

Provide the repository's `OWNER/REPO` or full URL, and include a number or link for a specific issue or PR. For comments, branch pushes, or PR creation and merging, specify the target repository, branches, and scope.

## Verification and Troubleshooting

Replace `OWNER/REPO` below with the target repository and query a few open PRs:

```bash
GH_CONFIG_DIR=/work/.config/gh gh pr list --repo OWNER/REPO --state open --limit 5 --json number,title,url
```

An explicit `--repo` lets you query outside a local repository and avoids selecting the wrong project. A successful empty response may mean no PRs match; adjust the state and limit to widen the query. See the [PR list manual](https://cli.github.com/manual/gh_pr_list).

If login succeeds but the repository is inaccessible, check the active account, the token's repository access and permissions, and any authorization required by the organization. If the identity is unexpected, check whether `GH_TOKEN` overrides saved credentials. If later calls lose login state, verify that `GH_CONFIG_DIR` stays consistent and its configuration persists.

Check other commands with `gh <command> --help`; see the [official manual](https://cli.github.com/manual/) for the full command set.
