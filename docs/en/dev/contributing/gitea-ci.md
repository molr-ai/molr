# Gitea CI

`.gitea/workflows/ci.yml` runs CI on every pull request, pushes to `main`, and manual dispatch. It covers all platforms by default, with optional scope selection below. Source checks run once and WebUI/desktop application assets build once. Five native jobs download those assets, verify their version, commit, lockfile and content hashes, then build Runtime, test the desktop, package it and evaluate the final installer. Linux AMD64 and macOS ARM64 run browser tests; four separate jobs validate Unix Runtime installation. `Gitea CI passed` succeeds only when every selected group succeeds. Shared application assets, Runtime archives and desktop installers are retained for seven days.

Desktop acceptance is part of this workflow, with no PR path filters or separate `desktop.yml` entry point. The native matrix reuses `nativeBuildMatrix()`, filtering the selected platforms and translating runner names into the local labels below. Publication remains in `.github/workflows/`; its release gate still checks GitHub CI records.

## Select CI scope

Configure defaults under repository **Settings → Actions → Variables**. Pull requests and pushes to `main` use these variables. Manual `Gitea CI` runs can override them with the inputs below; blank inputs inherit repository variables. Without configuration, CI validates all platforms.

| Repository variable | Manual input | Values | Default |
| --- | --- | --- | --- |
| `MOLR_CI_PLATFORMS` | `platforms` | `all`, or a comma-separated list of `linux-amd64`, `linux-arm64`, `macos-amd64`, `macos-arm64`, `windows-amd64` | `all` |
| `MOLR_CI_INSTALLERS` | `installers` | `enabled` / `disabled`, controlling separate Runtime installation acceptance | `enabled` |

For a single Linux AMD64 build runner, set:

```text
MOLR_CI_PLATFORMS=linux-amd64
MOLR_CI_INSTALLERS=disabled
```

This runs source checks, shared frontend compilation, Linux AMD64 Runtime and desktop builds/tests/package acceptance, and Linux browser tests. It does not schedule ARM64, macOS, Windows or separate installation runners. Desktop package acceptance still runs: `installers=disabled` only disables the separate Runtime installation jobs that need disposable environments.

Select `all` to restore all five platforms. For example, `linux-amd64,linux-arm64` selects both Linux architectures and needs both native runners. Browser tests follow selected Linux AMD64 and macOS ARM64 targets. When enabled, separate installation acceptance follows selected Unix targets. Windows-only selection has no separate installation job; desktop acceptance covers the Runtime lifecycle. Source checks, shared assets and the final gate always require `molr-linux-amd64`, even when native targets select only other platforms.

Unknown platforms, duplicates and invalid installation switches fail the source job. Selection never shrinks automatically based on online runners: selected runners that are not configured leave jobs waiting. Unselected platforms are excluded from matrices. When the entire browser or installation group is disabled, the gate accepts only its `skipped` status; failures, cancellations and unexpected skips of selected jobs fail the gate. Source and gate logs list the actual validated scope. Partial coverage does not establish full-platform validation or change the GitHub release gate.

## Local action mirrors

Create these pull mirrors in the Gitea `actions` organization, preserving upstream branches, tags and built `dist/` files. Workflows follow the current major references; validate mirror updates with a manual CI run.

| Local repository | Upstream | Workflow reference |
| --- | --- | --- |
| `actions/checkout` | <https://github.com/actions/checkout> | `v7` |
| `actions/setup-node` | <https://github.com/actions/setup-node> | `v7` |
| `actions/setup-go` | <https://github.com/actions/setup-go> | `v7` |
| `actions/gitea-upload-artifact` | <https://github.com/ChristopherHX/gitea-upload-artifact> | `v7` branch |
| `actions/gitea-download-artifact` | <https://github.com/ChristopherHX/gitea-download-artifact> | `v8` branch |

The first three use stable major tags. The artifact actions use the maintainer's Gitea-compatible branches, not the official GitHub artifact repositories. Downloads stay within the current run, without `github-token` or cross-run query inputs. No additional Docker, Python or cache actions are needed.

Set the following in the **Gitea server's** active `app.ini`, then restart Gitea:

```ini
[actions]
ENABLED = true
DEFAULT_ACTIONS_URL = self
```

Docker deployments normally use `/data/gitea/conf/app.ini` inside the container. Alternatively set `GITEA__actions__DEFAULT_ACTIONS_URL: self` on the Gitea service in Compose and recreate that container. This is not a runner setting or repository variable. It affects the entire instance: `actions/checkout@v7` resolves to the instance's `actions/checkout` repository. Other repositories must mirror any missing actions or use complete upstream URLs. See the [Gitea configuration reference](https://docs.gitea.com/1.23/administration/config-cheat-sheet/#actions-actions).

Enable Actions in repository settings. Both the server and runners must reach the actual instance `ROOT_URL`, including artifact download URLs. Public workflows contain no private deployment addresses or registration tokens.

## Native runners

Install a current stable Gitea Runner that supports `runs.using: node24` and provide Node 24 for actions. Project builds use Node `22.21.0` installed by setup-node and the Go version in `runtime/.go-version`; action runtimes and project toolchains are separate requirements. Verify checkout, setup-node, setup-go and subsequent artifact actions, not just runner connectivity.

Register each environment with its matching native architecture label in **host mode**. These are five build environments and four separate installation environments, not necessarily nine physical machines; independent virtual machines can provide them.

| System | Build / desktop label | Separate installation label |
| --- | --- | --- |
| Ubuntu 24.04 AMD64 | `molr-linux-amd64:host` | `molr-install-linux-amd64:host` |
| Ubuntu 24.04 ARM64 | `molr-linux-arm64:host` | `molr-install-linux-arm64:host` |
| macOS 15 Intel | `molr-macos-amd64:host` | `molr-install-macos-amd64:host` |
| macOS 15 Apple Silicon | `molr-macos-arm64:host` | `molr-install-macos-arm64:host` |
| Windows Server 2025 AMD64 | `molr-windows-amd64:host` | Native Runtime lifecycle covered by desktop workflow |

Create configuration with the installed runner's `config init`, then edit it (`config generate` shows all options). For example, an AMD64 Linux build runner uses:

```yaml
runner:
  capacity: 1
  timeout: 90m
  labels:
    - molr-linux-amd64:host
```

Give every runner its own registration file, workspace, writable tool cache and test account. Do not run two runner processes concurrently under the same test account: sandbox preparation uses a fixed systemd probe unit name and installation tests modify user services. Installation environments must not register build labels; destroy them or restore a clean snapshot after a job. macOS/Windows native jobs also install and launch the final desktop package using the user profile, so their build runners need disposable dedicated user environments restored to a clean state after every job. Gitea 1.23 does not guarantee workflow `timeout-minutes` enforcement, so configure the runner timeout; installation runners can use `40m`. See [runner configuration](https://docs.gitea.com/runner/configuration/) and [labels](https://docs.gitea.com/runner/labels/).

- Preinstall Git, Python 3 and the Node 24 runtime needed by actions. Windows must have a working `python3` command. The runner account needs writable tool installation directories.
- Linux uses a dedicated non-root account with noninteractive sudo, systemd user services, cgroup v2 delegation, AppArmor, Bash, tar, gzip, file and basic build tools. The existing prepare-sandbox action installs sandbox dependencies and probes startup. Desktop jobs install Xvfb and Electron libraries; website tests install Chromium and its dependencies. Ordinary Docker jobs do not replace these native environments.
- macOS needs Xcode Command Line Tools, `codesign`, `hdiutil`, `file`, tar, Bash and Python 3. Run the runner in the dedicated user's login session for user launchd and Electron graphical tests.
- Windows needs PowerShell 7 (`pwsh`), Git, tar and Python 3. Run Electron tests in a dedicated user's interactive desktop session, not a headless service session. The workflow explicitly selects `pwsh`.

Linux installation acceptance removes Bubblewrap, static BusyBox and AppArmor profiles before the installer restores dependencies. Assign `molr-install-*` labels only to dedicated disposable environments, never a development machine or another project's shared runner.

## Network and validation

Action mirrors do not include Node/Go distributions, npm/Go modules, Playwright browsers, Electron or packaging tools. Runners need access to those download sources; configure proxies, package mirrors or preinstalled tool caches in private runner configuration as needed. Setup actions disable automatic dependency caching, so initial setup needs no additional cache service. `GOCACHE` and `GOMODCACHE` explicitly use `.cache/go-build` and `.cache/go-mod` in the current workspace, sharing caches across tool setup, tests and packaging while keeping workspaces isolated.

The workflow uses ordinary jobs rather than reusable workflows. Native builds, browser tests and installation jobs check out the exact commit recorded by the source job; installation acceptance downloads artifacts from that same run. Shared tool setup lives in `.gitea/actions/setup-build/action.yml` and continues to use the mirrored v7 actions. Server features are independent of the new runner: upgrading it does not establish support for `permissions`, `concurrency` or GitHub PR merge previews on Gitea 1.23. See the [1.23 comparison](https://docs.gitea.com/1.23/usage/actions/comparison/) and [PR trigger semantics](https://docs.gitea.com/1.23/usage/actions/faq/).

When a browser job fails, it uploads `apps/website/test-results/` as `website-failure-<runner label>` for seven days; missing diagnostics skip the upload. Documentation navigation and desktop download tests save errors, page URLs, network/page failures, screenshots and `trace.zip`. Download the artifact and run `npx playwright show-trace <trace.zip>` to investigate. These tests use artificial data only. A failed assertion still fails the job and final gate; uploading diagnostics does not change the validation result.

Validate setup in this order:

1. Manually dispatch `Gitea CI`, check the logged scope, and require source checks, shared asset compilation and all selected jobs to pass. For complete validation, explicitly enter `platforms=all` and `installers=enabled`: five native jobs, two browser jobs and four separate installation jobs must pass. Installation jobs cover both server and sandbox modes. A failed, cancelled or unexpectedly skipped selected job must fail the final gate.
2. Verify that the selected final desktop installers pass extraction/installation, application startup and bundled Runtime lifecycle acceptance, and that corresponding `ci-desktop-*` artifacts and Unix `ci-runtime-*` archives can be downloaded. The workflow builds `0.1.0-local`: macOS uses ad-hoc signing and Windows has no publisher signing. Both complete packaging directly without Apple notarization; signing certificates and release credentials are not required.
3. Use an ordinary PR to verify automatic scope selection from repository variables without desktop path filters. For a single runner, verify that unselected platforms do not queue and separate installation is skipped as configured. Expand the variables and validate again when adding runners.

Syntax checks and local tests are not an actual Gitea acceptance run. During the first run, record server, runner and action mirror versions and verify artifact transfers. Keep logs and actual deployment configuration in private internal material.
