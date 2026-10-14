# Release Maintenance

Release packages are stored in GitHub Releases. The official website and release manifests are deployed together as static resources via Cloudflare Workers. The current artifact is a Linux Runtime with an embedded WebUI; platform and environment requirements are uniformly defined in [`release/platforms.json`](../../../../release/platforms.json).

## Initial Enablement

Enable Actions in the GitHub repository and create the `release` environment for releases. The official website retains the GitHub repository authorization for Cloudflare Workers Builds and automatic deployment of the `main` production branch. It is not necessary to configure `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN`, or `production` environments in GitHub settings.

The Cloudflare Worker's Settings → Builds use the following configuration:

| Setting | Value |
| --- | --- |
| Root directory | Repository root |
| Production branch | `main` |
| Build command | `npm run build:website:release` |
| Deploy command | `npx wrangler@4.45.0 deploy` |
| Build variables | `NODE_VERSION=22.21.0`, `VITE_SITE_MODE=live` |

The platform installs npm dependencies and then executes the build command. It reads recent public GitHub Release metadata, selects the latest version of each kind, verifies download links, generates independent temporary channel directories, and then builds the official website. There is no need to pre-install `gh` or manually provide a Cloudflare deployment token; deployment authentication is managed by Workers Builds. The repository defaults to `molr-ai/molr`; forks can set the Cloudflare build variable `MOLR_RELEASE_REPOSITORY`. Public release information is read anonymously by default. If GitHub API rate limits are encountered, a read-only `GH_TOKEN` can be provided in Cloudflare build secrets.

The default release source is `https://molr.ai/releases`. When using other domains, synchronously set `MOLR_RELEASE_URL` in both Cloudflare build variables and GitHub repository variables, pointing to the `/releases` path of the same Worker. Bind the `molr` Worker to the official website domain according to the existing [`wrangler.jsonc`](../../../../wrangler.jsonc).

Publishing a GitHub Release does not modify `main`. To ensure the official website refreshes after release/nightly completion, create a hook bound to `main` in Cloudflare Worker → Settings → Builds → Deploy Hooks, and save the URL as the GitHub repository secret `CLOUDFLARE_DEPLOY_HOOK`. This URL itself has permission to trigger builds, is not written to the repository or logs, and requires no additional Authorization header. For configuration entry points and principles, see [Cloudflare Deploy Hooks](https://developers.cloudflare.com/workers/ci-cd/builds/deploy-hooks/) and [Workers Builds Configuration](https://developers.cloudflare.com/workers/ci-cd/builds/configuration/).

`.github/workflows/website.yml` is only responsible for triggering the hook after release and waiting for the official website manifest and installation scripts to actually update, with a maximum wait time of 20 minutes; accepting a build request does not mean deployment is complete. Regular pushes to `main` are handled by Cloudflare's native integration, and Actions do not duplicate deployment. The website workflow can be run manually and separately to retry synchronization without republishing binaries. If the hook is not configured, builds and GitHub Releases can still succeed, but the official website synchronization step will explicitly fail; native automatic deployment for `main` is unaffected.

GitHub Releases must be publicly downloadable. It is recommended to enable immutable releases and set creation permissions and update/delete protection for official version tags. The workflow only grants `contents: write` during release tasks; PR checks do not touch deployment secrets. Whether environment approval is enabled is decided by repository maintainers.

CI for PRs and main runs checks, builds, and native installation acceptance on `ubuntu-24.04` and `ubuntu-24.04-arm`; website browser tests run on AMD64. After the first actual release and Cloudflare deployment, confirm remote connectivity via Cloudflare build history and the workflow's public download and site verification results. Local testing does not imply that remote accounts have been configured or that online releases have been executed.

CI and releases use the same `prepare-sandbox` action to prepare runners. When Ubuntu enables AppArmor restrictions on unprivileged user namespaces, the action installs and loads `bwrap-userns-restrict` from the `apparmor-profiles` package, then runs a Bubblewrap probe in a temporary service with `NoNewPrivileges=yes`. On failure, it prints the corresponding kernel denial logs. This configuration applies only to temporary runners; the system user namespace restrictions remain enabled.

## Versions and Channels

| Type | Tag Example | Channel |
| --- | --- | --- |
| Official | `v0.1.0` | stable; official versions of the same version can enter preview |
| Beta | `v0.2.0-beta.1` | preview |
| RC | `v0.2.0-rc.1` | preview |
| Nightly | `v0.2.0-0.nightly.20261011.30405.gabcdef123456` | nightly |

The version in the root `package.json` represents the base version of the current development cycle on `main`; update it and the corresponding root version in the lockfile when entering the next cycle. Release versions are taken from tags and do not require modifying internal workspace package versions. The date, UTC hour/minute/second numbers without leading zeros, and commit for nightly are generated by scripts to satisfy SemVer; the order for the same base version is nightly, beta, rc, official. The main branch cannot use versions such as `-dev` as base versions.

Tags for official, beta, and rc must point to commits within the `main` history. Pushing a tag triggers the Release workflow; the manual entry accepts existing tags, suitable for retries. Nightly runs daily at UTC 19:17 (Beijing time next day 03:17) and can also be run manually. GitHub scheduling may be delayed; if the exact version is already published or the highest nightly in the recent window has the same commit, duplicate builds are skipped, but official website deployment can still be fixed.

## Release Steps and Failure Recovery

Both entry points share `release-build.yml`: fixed commit → basic checks and tests → platform matrix static build → native installation, reinstallation, authentication, and sandbox acceptance → aggregate artifacts → create draft → upload and verify attachments → publish → verify public attachments → deploy official website.

Builds require a clean checkout, the repository-specified Go version and `CGO_ENABLED=0`, checking actual static linking, architecture, VCS commit, and `vcs.modified=false`. Build time is taken from the commit time; tar timestamps, user, and group information are fixed to facilitate retries for the same commit. Both architectures must succeed; runners lacking sandbox capabilities will fail, without lowering acceptance requirements. Local builds such as `0.1.0-local` allow uncommitted modifications and do not enter the official release manifest.

Release attachments include packages for two architectures and SHA-256, source packages and SHA-256 that have passed source release checks, and `manifest.json`. The website serves the shared `install.sh` downloader, which verifies the package and invokes its installer. Releases do not attach the script or record its URL and digest in the manifest. Manual installation verifies and extracts the archive, then runs `./bin/molr-setup`. Versions and attachments remain immutable. If an attachment with the same name exists, only content with the same digest is accepted, without overwriting; versions with the same commit that have already been published skip the build directly. Draft creation uses the release ID returned by the GitHub API; uploads and publication use that ID without requiring the new draft to appear in the list immediately. Stable and preview releases require an existing remote tag; nightly may create its tag at publication. If draft upload fails, the failed task of the original workflow can be rerun, reusing build artifacts; a full rerun will verify existing attachments, and if differences exist, the cause must be checked or a new version used. Converting RC to official uses a new official version tag and build.

`release-catalog.mjs` inspects only the first 100 GitHub Release metadata entries by default. It ignores drafts, entries without a manifest asset, and tags outside the version protocol. Within that window it selects the highest SemVer for each kind: stable, beta/RC, and nightly. It then downloads and validates at most three `manifest.json` files. Package bodies are never downloaded; download availability is checked with HEAD requests. Unselected historical manifests are not validated. An invalid selected release fails with its tag in the error, without silently falling back. Stable versions can still enter preview; nightly stays isolated.

Set the positive integer `MOLR_RELEASE_SCAN_LIMIT` to change the window N. Each page contains at most 100 entries, with at most `ceil(N / 100)` list requests; entries beyond N on the last page are excluded. Keep the Cloudflare build variable and the GitHub repository variable with this name equal; the website workflow defaults to 100. Missing release kinds do not trigger a search of older pages, so a channel that has not been updated recently can be empty (preview still accepts stable releases). Publication retries look up the exact tag independently of the website window.

The manifest root, platform entries, and requirements must contain the required fields and pass value validation. Additional fields are accepted and preserved in the version snapshot.

The official website is built by Cloudflare from `main`. Each build generates a directory for the selected releases and deploys static resources, including `/install.sh`, `/releases/catalog.json`, manifests for each channel, and `latest-<平台>.json` used by Runtime, and `/releases/versions/<版本>.json` for those selected versions. The website no longer retains every historical manifest. Download unselected versions and their checksums from GitHub Releases for offline installation. These entry points use `Cache-Control: no-cache`. Installation packages are still downloaded from GitHub and not uploaded to the Worker. Deployment failures retain the previous entire site and channel directories; check Cloudflare build logs, fix build configuration, hooks, or network issues, and then rerun the website workflow separately.

The hero shows the stable command in a terminal window with a title bar and inline copy button. Platform tabs above it show only the supported Linux target; the more-platforms link leads to the bottom installation section. Displayed and copied commands always use `https://molr.ai/install.sh`, in both development and deployed environments. Stable omits the default channel option; preview/nightly specify it explicitly, selecting the channel’s latest release. Commands omit source and version options; custom sources and version pinning are covered in the installation guide.

## Platform Maintenance

The platform manifest includes system, architecture, architecture aliases, available channels, native runners, archive formats, installers, minimum environment requirements, and acceptance types. The current implementation accepts Linux `tar.gz` + POSIX sh / Go installers. Adding a new operating system requires first implementing the installer and acceptance process; you cannot simply add a line of configuration to announce support.

After adjusting the manifest, run:

```bash
npm run release:platforms
npm run check:platforms
```

Generated files are read by Runtime and the in-package installer; the offline platform identification table in the bootstrap script is also generated. CI verifies the generated results; the build matrix, platforms actually downloadable on the official website, and dependency requirements use the same definition. You can initially open only nightly for new platforms, and open preview/stable after native installation and sandbox acceptance are stable.

The manifest for each version saves a snapshot of the platforms and environment requirements at that time. The official website displays based on the actual snapshot of the selected channel version, and does not generate download links for historical versions based on the current support list. When stopping new version builds for a platform, old releases and installation packages are retained, and users can still install offline according to old version instructions.

## Local Verification

```bash
npm ci
npm run check:platforms
npm run test:release-flow
npm run test:release
python3 scripts/test-install-download.py
cd runtime
CGO_ENABLED=0 go test ./internal/installation ./internal/updates
cd ..
npm run check:release
npm run check:licenses
npm run build:release
MOLR_TEST_ARCHIVE=dist/releases/molr_0.1.0-local_linux_amd64.tar.gz npm run test:release-install
```

Native installation tests require the current user's systemd manager, cgroup v2 delegation, static BusyBox, and Bubblewrap; tests use independent instances, configurations, and data, and clean up after completion. Source code export and complete release builds still comply with [Public File Checks](public-content.md).

To prepare a local directory, place verified release manifests in a dedicated directory and then execute `npm run release:promote -- --manifests <目录>`. The command only generates `.cache/release-catalog/` and does not upload files. Use `MOLR_CATALOG_DIR=.cache/release-catalog VITE_SITE_MODE=live npm run dev:website` to accept the official website; when the catalog is not configured, it shows no versions for each channel and does not fake download entry points. Browser regression uses manual manifest interception of the network; real package acceptance is executed separately. Detailed acceptance dates, environments, and results are saved in local internal records.

The installer checks destination paths first and lets the user fix or ignore group/other-writable directories; ownership, symlink, and managed-file checks remain separate. After dependencies are prepared, it probes Bubblewrap as the service account with `NoNewPrivileges`, prepares the distribution AppArmor policy when authorized, and probes again before writing the instance or restarting its service. Policy branches use artificial files and mocked system commands in tests; native installation acceptance verifies actual sandbox capabilities.

## Installer maintenance and acceptance

Archives contain two static Go programs: `runtime/cmd/molr` runs Runtime, and `runtime/cmd/molr-setup` manages installation, updates and removal. `runtime/internal/installation` owns the common flow. `host_linux.go` centralizes dependencies, systemd, AppArmor and host capability checks; `files_linux.go` handles paths, permissions, atomic writes and locking. Common orchestration does not identify support by Ubuntu names or versions. Automatic package preparation currently implements APT; native acceptance currently covers Ubuntu 24.04 AMD64/ARM64.

Package `release.json` describes the build. Program-directory `installation.json` is the authoritative instance record for state, paths, account, service, build and update subscription. Default behavior uses executable location, independently of the working directory. Invalid records stop execution rather than falling back to package mode. `.setup.lock` uses a kernel advisory lock: presence does not indicate an active operation, and its inode is retained when releasing the lock.

Planning reads and checks the environment; interactive and unattended modes share planning and execution. Confirmation and permission choices precede writes; `--dry-run` performs no mutations. Input uses `/dev/tty`, allowing interaction after piped startup. Unattended mode uses `sudo -n`; APT disables interaction and retains existing configuration. Temporary files and atomic renames replace programs, retaining previous binaries and configuration. Interruption can leave `installing` state, recoverable using a complete package. Failed verification is not reported as healthy installation.

`install.sh` is a POSIX downloader using existing curl or wget. Catalog generation adds `download-<platform>.txt`: seven fixed lines containing protocol identifier, version, commit, platform ID, filename, URL and SHA-256. Channel and historical descriptors derive from the same manifest, avoiding shell JSON parsing or evaluation of remote text. The downloader extracts five allowlisted files to fixed paths. The Go updater reads JSON manifests and uses standard-library HTTP, SHA-256 and tar/gzip to verify packages, then invokes the new package's `molr-setup`. Runtime and WebUI use the installation record's subscription; WebUI generates commands for the installed maintenance program.

To extend distributions, add package-manager or service support in the host adapter and add OS/version/architecture combinations to the independent acceptance matrix. Unknown distributions with manually satisfied capabilities are not rejected. Matrix entries need environments that run actual services, cgroups and sandbox probes; container file tests do not replace native acceptance.

`.github/workflows/installer-acceptance.yml` uses the built archive on fresh runners, removes Bubblewrap, static BusyBox and AppArmor profiles, and lets setup restore dependencies. This and prepared-host acceptance both gate publication. The current matrix covers Ubuntu 24.04 AMD64/ARM64; other distributions are not claimed as verified.

Regression tests cover permission decisions, mutation-free previews, repeated execution, record validation, concurrency locks, path/archive attacks, failed downloads, data retention and purge. Native package tests cover PTY installation, unattended repair, configuration retention, authenticated sandbox capabilities and removal. Python is only a development PTY driver and publication-check tool; it is neither packaged nor required by installation on the target machine.

```bash
cd runtime
CGO_ENABLED=0 go test ./internal/installation ./internal/updates
cd ..
python3 scripts/test-install-download.py
MOLR_TEST_ARCHIVE=dist/releases/molr_0.1.0-local_linux_amd64.tar.gz npm run test:release-install
```

Reproduce dependency preparation on a disposable Ubuntu host with `MOLR_TEST_PREPARE=1`; do not remove dependencies from a development machine. Add regression cases for installation failures and ship fixes in new packages. Dates, environments, logs and acceptance credentials remain in local internal records.
