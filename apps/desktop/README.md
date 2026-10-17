# Molr Desktop

Electron hosts the existing Runtime WebUI in a separate, sandboxed `WebContentsView`. The bundled React interface manages connections and the desktop-owned local service. Remote content has no preload or desktop IPC.

Linux/macOS packages include `molr` and `molr-setup`; Windows contains only the client. WSL is an ordinary remote connection, including localhost addresses. Each connection has an independent persistent storage partition. Changing its origin rotates the partition; removing it clears its session and storage. Only one Runtime view is active at a time.

## Development

From a task worktree at the repository root:

```bash
npm ci
node node_modules/electron/install.js
npm run build:desktop
npm run dev:desktop
```

In a second terminal:

```bash
MOLR_DESKTOP_DEV_URL=http://127.0.0.1:5196/ npm start --workspace @molr/desktop
```

Without that variable, Electron loads the built UI through `molr-desktop://app/`. Development URLs are restricted to loopback. `MOLR_DESKTOP_DATA` selects an isolated development profile. Both overrides are ignored in packaged applications. The browser preview cannot manage services or open Electron views.

For local management, run `npm run build:release` on Linux/macOS and set `MOLR_DESKTOP_RUNTIME` to `.cache/release/<os>-<arch>/molr` (absolute path). Packaged applications always use their fixed `resources/runtime` directory.

## Local service ownership

The desktop creates a unique `molr-desktop-<id>` instance and stores its identity in its application data directory. `installation.json` remains authoritative and marks `manager: "desktop"`. Existing CLI installations are not adopted. The Go installer owns locking, permission checks, atomic replacement, rollback, service registration and authenticated health checks.

The local interface is `molr-setup desktop <status|plan|install|upgrade|start|stop|restart|uninstall> --json --non-interactive --name molr-desktop-<id> --program-root <directory>`. JSON lines contain maintenance receipts and a `type: "desktop"` result. The first-install `loginURL` is ephemeral: consume it only in the matching Runtime view; never log or save it. Status contains no credentials. New instances listen on loopback. The creation form selects `--resource-preset permissive|least-privilege` (initially permissive). Omitting it in unattended setup uses least privilege. Presets expand into editable sandbox settings; updates preserve them. Standalone Sandbox installation is available through CLI `molr-setup --mode sandbox`.

Closing or quitting the desktop leaves the Runtime running. Explicit stop/restart/upgrade/uninstall requires native confirmation and shows currently visible running tasks when authenticated. Other accounts and channels may also be executing. Uninstall retains configuration and data. CLI/web automatic updates refuse desktop-managed instances; upgrade them explicitly from the desktop's bundled package.

Linux requires systemd 254+, cgroup v2 delegation, Bubblewrap, static BusyBox, util-linux and CA certificates. The DEB declares dependencies. Host policy/permission errors are reported without disabling isolation or changing parent directory permissions. macOS requires native launchd, file-permission and signing verification. Windows never installs or starts WSL.

## Verification

```bash
npm run test:desktop
npm run test:desktop:electron
# Linux without a display: xvfb-run -a npm run test:desktop:electron
npm run build:release
node apps/desktop/tests/local-runtime.mjs
```

Electron tests use a local HTTP fixture for partition, privilege-boundary, SSE and recovery checks. The separate native lifecycle test installs a unique service, checks authenticated capabilities, verifies stop/start and session restoration, then uninstalls it while checking data retention. It cleans up only its own instance. Screenshots go to `.cache/acceptance/desktop`.

Electron tests explicitly enable Chromium sandboxing. On Ubuntu systems restricting unprivileged user namespaces, an unpacked development Electron needs an appropriate loaded AppArmor profile; CI uses the existing `chrome` profile via `aa-exec -p chrome -- xvfb-run -a npm run test:desktop:electron`. Do not add `--no-sandbox` to bypass failure. Installed DEBs carry electron-builder's application-specific AppArmor profile and installation hooks.

`.github/workflows/desktop.yml` defines native tests and evaluation packages for Linux amd64/arm64, macOS amd64/arm64 and Windows amd64. A workflow definition does not demonstrate that those platforms have passed.

## Packaging and release

```bash
# Linux/macOS first build the native Runtime with the same MOLR_VERSION.
npm run build:release
npm run package:desktop
# Inspect an unpacked app:
npm run package:desktop -- --dir
```

Windows skips the Runtime build. Outputs under `dist/desktop/<os>-<arch>` include DEB, DMG or NSIS installer, SHA-256 and `desktop-manifest.json`. Explicit file lists include Molr notices, Electron's LICENSE and the matching distribution's `LICENSES.chromium.html`. Windows omits Go programs and the local-service adapter.

Versions ending in `-local` are evaluation builds. Other versions require a clean checkout. macOS signing uses `CSC_LINK`/`CSC_KEY_PASSWORD` and notarization uses `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`; Windows uses its signing identity via `CSC_LINK`/`CSC_KEY_PASSWORD`. Store credentials in CI secrets. Native acceptance and any configured signing/notarization precede publication. Packaging never uploads or publishes files.

Desktop updates are manual: install the new desktop package, then explicitly update its managed Runtime. Remote Runtimes are maintained by their operators. Background desktop update download/install is not implemented.

After native acceptance and packaging, collect per-platform manifests under distinct filenames:

```bash
node scripts/desktop-catalog.mjs --manifests <reviewed-manifests> --base-url https://downloads.example.com/desktop/<version>
MOLR_DESKTOP_CATALOG=.cache/desktop-catalog/catalog.json VITE_SITE_MODE=live npm run dev:website
```

`--existing <catalog.json>` preserves other channels. The website publishes only the validated `/releases/desktop/catalog.json`; installers stay on the release host. Without a catalog it shows no desktop installers. Local builds cannot enter the release catalog. Public builds must declare their actual publisher-signing status; unsigned Windows and ad-hoc signed, unnotarized macOS test builds are temporarily allowed and labeled on the download page.

## GitHub Release integration

`release-build.yml` calls `release-desktop.yml` for the same commit and version as Runtime. All five native targets must pass desktop and Runtime lifecycle tests before publication. The collector verifies the complete platform set, build identity and SHA-256 sidecars, then the existing publisher uploads installers, checksums and a combined `desktop-manifest.json` into the same GitHub Release draft as Runtime and source. A failure keeps the release unpublished; retries never overwrite different bytes. The independent `desktop.yml` only creates unsigned evaluation artifacts for PRs and manual checks.

The `release` environment supplies `MAC_CSC_LINK`, `MAC_CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`, `WIN_CSC_LINK` and `WIN_CSC_KEY_PASSWORD`. Certificate links use electron-builder's certificate input format. With complete credentials, the Mac app and its two bundled Go binaries are signed and the app is notarized. While certificates are pending, absent credentials produce unsigned Windows packages or ad-hoc signed macOS packages without notarization (`signed: false`). Partial credentials or configured signing failures fail the build. Restore missing-credential rejection in `scripts/signing.mjs` and require signatures in release collection/publication once certificates are ready. Local evaluation builds use ad-hoc signing on macOS and do not enter public catalogs.

`release:catalog` and `build:website:release` read the latest stable, preview and nightly GitHub Releases under the shared bounded SemVer selection policy. Desktop manifests must match Runtime's commit/version and declare all five native targets with explicit signing status and downloadable assets. Releases before desktop support produce no desktop entry. The combined catalog includes `desktop/catalog.json`, so production builds need no manually supplied `MOLR_DESKTOP_CATALOG`. Post-release website verification waits for both Runtime and desktop catalogs to match GitHub.
