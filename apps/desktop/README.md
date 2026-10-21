# Molr Desktop

Electron hosts the existing Runtime WebUI in a separate, sandboxed `WebContentsView`. The bundled React interface manages connections and the desktop-owned local service. The active Runtime main frame receives only the narrow `window.molrApp` bridge; it cannot access general connection or service-management IPC; the update bridge permits only a native-confirmed bundled upgrade of the active managed local service.

All desktop packages include the native Runtime and installer (`molr.exe` and `molr-setup.exe` on Windows). WSL is an ordinary remote connection, including localhost addresses. Each connection has an independent persistent storage partition. Changing its origin rotates the partition; removing it clears its session and storage. Only one Runtime view is active at a time.

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

For local management, run `npm run build:release` on Linux/macOS or `node apps/desktop/scripts/build-windows-runtime.mjs` on Windows and set `MOLR_DESKTOP_RUNTIME` to `.cache/release/<os>-<arch>/molr` (absolute path). Packaged applications always use their fixed `resources/runtime` directory.

## Window and App mode

Before connecting, while loading, and after disconnect or failure, the bundled interface owns the window controls and connection manager. After the Runtime WebUI completes its App-mode handshake, its `WebContentsView` fills the frameless window. The bundled toolbar is hidden. WebUI owns the draggable title area, current connection name, minimize/maximize/restore/close buttons, and Switch connection action, including on login and error screens. Ordinary browser visits retain web mode.

Both titlebars stay 48 CSS px high in windowed and fullscreen modes. On macOS, the window supplies native traffic lights. Main measures their height using the trusted manager's Window Controls Overlay, then sets equal top/left margins to center them in the titlebar at the active page zoom. Neither renderer supplies native dimensions or button positions; preload only notifies main of viewport resizing. Windowed interfaces use the native safe inset before the logo; fullscreen restores normal 16px left padding. The logo and right-side actions remain vertically centered. Windows/Linux retain their custom controls on the right. Validate traffic-light visibility and clicks in the manager, login, workspace, recovery, dialogs, zoom, and fullscreen transitions on a Mac; Linux layout checks cannot verify native macOS controls.

`@molr/ui-host/app` defines the shared type-only contract. The sandboxed Runtime preload exposes `getInfo`, window controls, `disconnect`, validated appearance synchronization, a disposable window-state subscription, native notification controls, desktop update status/actions, and the OAuth bridge described below. Main-process handlers validate the active WebContents, main frame, connection origin, and argument count. Plugin and custom-theme iframes do not receive the bridge. `molrDesktop`, general service management, arbitrary IPC, filesystem and command access stay unavailable to Runtime pages. Only the active managed local connection can request the fixed bundled-upgrade action, which requires native confirmation.

The manager defaults to the Forest palette and system display mode. After authentication, WebUI synchronizes the account's saved palette and display-mode preference through `setAppearance`. Main validates the fixed palette/mode enums and persists them in the desktop profile; returning to management or restarting restores the last signed-in service's appearance. System mode remains responsive to OS changes. Anonymous login pages and failed preference saves do not replace it. The manager has no separate appearance settings or text beside the logo, and loads platform and appearance before its first render.

Opening the desktop or reopening its window automatically reconnects the saved selection using its existing persistent session. Explicitly returning to connection management clears that selection; merely saving a connection does not select it. Failed restoration keeps retry and connection management available, without starting a stopped local service.

The local-service management page refreshes status on entry and every five seconds while open. Polls never overlap, pause during maintenance, and stop when leaving the page or connecting to a service. Repeated polling errors do not create repeated Toast notifications. The status/log section opens the log location; it has no manual refresh button.

Disconnect returns to the connection list without clearing authentication or stopping Runtime tasks; unfinished downloads from the closing view are cancelled. Failed navigation, renderer crashes and handshake timeout return to the bundled recovery screen. Ctrl/Cmd+Shift+M returns to connection management and Ctrl/Cmd+R reconnects. Windows/Linux have no application menu bar; Ctrl+plus/minus/0 controls zoom and F11 toggles full screen. macOS retains its system application menu. Native Windows Snap Layout hover and platform-specific window-manager behavior require native acceptance.

## Language

macOS packages declare English and Chinese localizations so Electron and its WebUI receive the selected system language. Native menu labels use that language, with English as the fallback; the application name is Molr. First-time WebUI setup saves the detected language and device timezone to the account. Later changes use the existing account settings (Chinese or English and an explicit timezone).

To verify a packaged macOS app with a fresh profile and a new managed Runtime, run `MOLR_LANGUAGE_APP=/absolute/path/to/Molr.app/Contents/MacOS/Molr node apps/desktop/tests/language.mjs`. The test uses a synthetic model, submits the actual setup screen, checks saved preferences and removes its own service afterward. `MOLR_LANGUAGE_SYSTEM=en-US` or `fr-FR` tests a process-only system-language override; global macOS preferences are unchanged.

## Notifications

The existing WebUI desktop-notification switch initializes the native OS channel through `molrApp.notifications`. There is no extra per-Runtime permission dialog. Browser notification permission is not used in App mode. On macOS, initialization can trigger system authorization; it runs on user activation, not page load. Channel support is not reported as OS permission. The operating system remembers its authorization decision, while WebUI keeps preferences in the connection partition per account.

Native delivery accepts bounded text and internal routes only. Notifications are scoped to each login lifetime and cleared on logout, navigation, disconnect, or disabling notifications. Old clicks cannot route into a later scope. Main retains native handles and waits for show/failed events with a deadline; failure falls back to in-app alerts. Focus and system settings can still suppress display. Use a signed macOS app to verify OS permission, display, sound, and click behavior.

The independent **App icon unread count** switch in notification settings defaults to off. It counts unread messages in non-archived conversations for the active connection/account, including while minimized. Windows renders a numeric overlay (`99+` above 99); macOS/Linux use native badge counts. macOS requires notification/badge authorization; Linux requires a LauncherEntry-compatible desktop and the installed `molr.desktop` file. Turning the switch off, logout, navigation and disconnect clear the badge. The bridge accepts counts only and rejects stale login scopes.

## Local OAuth callback and Runtime handoff

`window.molrApp.oauth.authorize(providerID)` opens a short-lived `127.0.0.1` callback listener, prepares a session-bound attempt with the active Runtime, confirms the authorization website and destination in a native dialog, and opens the system browser. It sends the authorization code directly to the original Runtime using its existing session and CSRF proof. Tokens are exchanged, validated and stored by a registered Runtime provider; neither codes nor tokens are returned to WebUI. `oauth.cancel()` cancels the pending local flow. A view change, disconnect, window close or five-minute timeout closes the listener.

The result contains `status` (`connected`, `cancelled`, `failed`, or `unknown`), an optional `attemptId`, and a `connectionId` on success. A lost completion response triggers a status query, never a repeated token exchange. An `unknown` result requires checking the Runtime attempt before starting another authorization. Once exchange has started, cancellation does not promise to undo credentials already saved.

Local and remote Runtimes use the same flow. Explicitly enabled private-network HTTP connections are supported alongside HTTPS; HTTP has the same lack of transport confidentiality as ordinary Runtime traffic. External provider authorization uses HTTPS. The bridge supports OAuth authorization-code responses with S256 PKCE and a GET callback at `/auth/callback`; it does not expose arbitrary listening ports, forwarding targets or token storage. Provider registration, exchange, scope/identity validation, encrypted storage and refresh are service-specific adapters. Runtime registers the administrator-only ChatGPT adapter. Optional authorization options carry a saved connection ID/version, display name and sensitive-action verification. Google and X adapters are not registered in production. See the [HTTP contract](../../docs/dev/reference/http-api.md#桌面-oauth-授权) and [frontend architecture](../../docs/dev/frontend/architecture.md#桌面-oauth-回调).

Run the full simulated flow in a real sandboxed Electron process after building:

```bash
node apps/desktop/tests/oauth-electron.mjs
# Linux without a display, retaining Chromium sandboxing:
aa-exec -p chrome -- node apps/desktop/tests/oauth-electron.mjs --headless
# Keep an isolated artificial Runtime and interactive acceptance control page:
aa-exec -p chrome -- node apps/desktop/tests/oauth-electron.mjs --headless --serve
```

The fixture replaces only native confirmation and system-browser opening at the test boundary. It exercises the real preload, main process, session cookies, CSRF, loopback listener and HTTP handoff; its artificial Runtime exchanges a one-time code with its artificial token endpoint. `--serve` binds the control page on port 5496 (`MOLR_OAUTH_DEMO_PORT` overrides it), with no real accounts or credentials. It is a test harness, not a production authentication endpoint. Native macOS and Windows acceptance remains platform-specific.

## Connection manager

The manager combines local and remote connections in cards with an Add connection card. It shares the login page’s `--background-entry` gradient. Connection, login and login-link confirmation cards have no shadow. An empty list opens the add flow. When no local service is installed, choose this computer or a remote address; once installed, adding opens the remote form directly. Names are optional and default to the hostname. Only a successful uninstall removes the managed local card; stop and errors retain it. Maintenance lives in its card menu. The local card shows an update notice only when the bundled service has higher SemVer precedence. A newer installed service, differing build metadata or unrecognized version strings show a version mismatch instead. Version notices use the same non-interactive Badge component as the running status. Use the card menu to open management with both versions; newer installed services show guidance to update the desktop app without offering a download shortcut or a downgrade. Equal versions show no extra notice.

Local installation asks for an administrator username and confirmed password. Credentials pass via trusted IPC and child stdin (`--init-owner NAME`), never arguments, saved state or logs; manual initialization does not create `initial-password`. Installation returns to the list. Main holds the one-time login link in memory for up to 14 minutes and consumes it when the local card is opened. The active installation view alone can consume `molrApp.consumeInitialLogin()` to skip the WebUI confirmation; ordinary links still require confirmation. Expired links or application restarts use the chosen account credentials. Retained-data reinstallation preserves the existing account and uses normal sign-in.

Browser interaction preview: start `npm run dev:desktop` and open `/tests/fixtures/manager-preview.html`. This uses labeled artificial state and never installs or connects to a service. `node apps/desktop/tests/manager-ui.mjs` checks this preview; `MOLR_MANAGER_PREVIEW` overrides its URL.

## Local service ownership

The desktop creates a unique `molr-desktop-<id>` instance and stores its identity in its application data directory. `installation.json` remains authoritative and marks `manager: "desktop"`. Existing CLI installations are not adopted. The Go installer owns locking, permission checks, versioned programs, rollback, service registration and initial authenticated health checks. Manual installations retain no plaintext password; subsequent starts report `verification_pending` until independently authenticated. CLI unattended installation can explicitly opt into `--auto-init-admin` and its existing initial-password file workflow.

The local interface is `molr-setup desktop <status|plan|install|upgrade|start|stop|restart|uninstall> --json --non-interactive --name molr-desktop-<id> --program-root <directory>`. JSON lines contain maintenance receipts and a `type: "desktop"` result. The first-install `loginURL` is ephemeral: consume it only in the matching Runtime view; never log or save it. Status contains no credentials. New instances listen on loopback. The creation form selects `--resource-preset permissive|least-privilege` (initially permissive). Omitting it in unattended setup uses least privilege. Presets expand into editable sandbox settings; updates preserve them. On Linux/macOS, standalone Sandbox installation is available through CLI `molr-setup --mode sandbox`.

Closing or quitting the desktop leaves the Runtime running. Explicit stop/restart/upgrade/uninstall requires native confirmation and shows currently visible running tasks when authenticated. Other accounts and channels may also be executing. Uninstall retains configuration and data. CLI/web automatic updates refuse desktop-managed instances; upgrade them explicitly from the desktop's bundled package.

Linux requires systemd 254+, cgroup v2 delegation, Bubblewrap, static BusyBox, util-linux and CA certificates. The DEB declares dependencies. Host policy/permission errors are reported without disabling isolation or changing parent directory permissions. macOS requires native launchd, file-permission and signing verification. Windows uses a least-privilege, interactive-user Task Scheduler task with a logon trigger; no administrator service or WSL is installed. It keeps running after desktop exit while the Windows user remains signed in. Runtime diagnostics append to `program/runtime.log`. Private Runtime data inherits a protected owner/SYSTEM ACL. The first installation returns an authenticated login link; reinstalling retained data preserves credentials and requires normal sign-in. Failed upgrades restore the previous task and installation record, and restart the previous binary if it was running.

## Verification

```bash
npm run test:desktop
npm run test:desktop:electron
# Real WebUI/Runtime App-mode acceptance (isolated initialized server):
MOLR_DESKTOP_WEBUI_URL=http://127.0.0.1:5193 MOLR_DESKTOP_TEST_LINK=/path/to/test-link.json node apps/desktop/tests/app-mode.mjs
# Linux without a display: xvfb-run -a npm run test:desktop:electron
npm run build:release
node apps/desktop/tests/local-runtime.mjs
node apps/desktop/tests/installation.mjs
```

Electron tests use a local HTTP fixture for partition, privilege-boundary, SSE and recovery checks. The separate native lifecycle test installs a unique service, checks authenticated capabilities, verifies stop/start and session restoration, then uninstalls it while checking data retention. It cleans up only its own instance. Screenshots go to `.cache/acceptance/desktop`.

Electron tests explicitly enable Chromium sandboxing. On Ubuntu systems restricting unprivileged user namespaces, an unpacked development Electron needs an appropriate loaded AppArmor profile; CI uses the existing `chrome` profile via `aa-exec -p chrome -- xvfb-run -a npm run test:desktop:electron`. Do not add `--no-sandbox` to bypass failure. Installed DEBs carry electron-builder's application-specific AppArmor profile and installation hooks.

`.github/workflows/ci.yml` calls `native-build.yml` for native tests and evaluation packages for Linux amd64/arm64, macOS amd64/arm64 and Windows amd64. A workflow definition does not demonstrate that those platforms have passed.

## Packaging and release

```bash
# Linux/macOS first build the native Runtime with the same MOLR_VERSION.
npm run build:release
npm run package:desktop
# Inspect an unpacked app:
npm run package:desktop -- --dir
```

On Windows first run `node apps/desktop/scripts/build-windows-runtime.mjs`; it builds embedded-WebUI PE executables with `CGO_ENABLED=0` and verifies their architecture and Go build identity. Outputs under `dist/desktop/<os>-<arch>` include DEB, DMG or NSIS installer, SHA-256 and `desktop-manifest.json`. Explicit file lists include Molr notices, Electron's LICENSE and the matching distribution's `LICENSES.chromium.html`. All variants include the native Go programs and local-service adapter.

Versions ending in `-local` are evaluation builds. Other versions require a clean checkout. macOS signing uses `CSC_LINK`/`CSC_KEY_PASSWORD` and notarization uses `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`; Windows uses its signing identity via `CSC_LINK`/`CSC_KEY_PASSWORD`. Store credentials in CI secrets. Native acceptance and required signing precede publication. Developer ID-signed macOS DMG packaging uses the saved-submission notarization flow; `--defer-notarization` saves the intermediate without waiting. Local signed packaging defaults to required notarization; `MOLR_NOTARIZATION_POLICY=optional` allows publication metadata after a 30-minute wait if Apple still reports `In Progress`. Explicit rejection and query/verification errors fail. Packaging may upload the DMG to Apple for notarization; it never publishes GitHub Release assets.

The main process checks `https://molr.ai/releases/desktop/catalog.json` against the desktop’s own version and native platform at startup, every 24 hours, and on resume when due. Manual checks have a one-minute cooldown; concurrent checks coalesce, failures retain the last successful result, and development builds are not compared. Only stable releases are offered. The Runtime About page exposes the result through the restricted update bridge, independent of Runtime administrator status. No packages are downloaded or installed automatically.

Install the new desktop package, then explicitly complete its managed local service update. About always shows the desktop version at the top. Version mismatch and update availability have independent tags; clicking the version or either tag opens component versions and update actions in one dialog. Pending local upgrades appear in that dialog and the local service manager. The native confirmation lists task interruption risks; cancelling preserves the running service, failure permits retry, and success reconnects the active local view. Restarting the app never upgrades the service. Only a strictly newer bundled version is offered, preventing accidental downgrades. Remote connections cannot invoke local upgrades and do not receive local installation state. Remote Runtimes are maintained by their operators.

After native acceptance and packaging, collect per-platform manifests under distinct filenames:

```bash
node scripts/desktop-catalog.mjs --manifests <reviewed-manifests> --base-url https://downloads.example.com/desktop/<version>
MOLR_DESKTOP_CATALOG=.cache/desktop-catalog/catalog.json VITE_SITE_MODE=live npm run dev:website
```

`--existing <catalog.json>` preserves other channels. The website publishes only the validated `/releases/desktop/catalog.json`; installers stay on the release host. Without a catalog it shows no desktop installers. Local builds cannot enter the release catalog. Public builds must declare independent `signed` and `notarized` boolean fields; notarization can only be true for signed macOS artifacts. Developer ID-signed macOS packages that time out in optional mode are labeled as signed but not notarized; macOS Nightly uses ad-hoc signing and reports both fields as false. The download page retains the actual signing and notarization status of historical releases.

## GitHub Release integration

`release-build.yml` calls `release-desktop.yml` for the same commit and version as Runtime. All five native targets must pass desktop and Runtime lifecycle tests before publication. The collector verifies the complete platform set, build identity and SHA-256 sidecars, then the existing publisher uploads installers, checksums and a combined `desktop-manifest.json` into the same GitHub Release draft as Runtime and source. A failure keeps the release unpublished; retries never overwrite different bytes. The unified `ci.yml` creates unsigned evaluation artifacts for PRs, main pushes and manual checks. `CI passed v2` requires all native targets, source checks, Linux AMD64/macOS ARM64 browser tests and Unix installer acceptance to pass.

The `release` environment supplies `MAC_CSC_LINK`, `MAC_CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`, `WIN_CSC_LINK` and `WIN_CSC_KEY_PASSWORD`. Certificate links use electron-builder's certificate input format. macOS Nightly uses ad-hoc signing and writes checksums and the manifest directly, without Apple credentials, a notarization artifact or a notarization job. Beta/RC and stable macOS builds sign the app, bundled Go binaries and DMG, then submit the DMG to Apple. Beta/RC uses `optional` notarization; stable Release uses `required`. The workflow derives this policy from the release channel and no longer reads `MAC_NOTARIZATION_POLICY`.

Windows Nightly, Beta/RC and stable builds sign when credentials are configured and otherwise publish unsigned installers; partial credentials or signing failures still fail the build. Windows has no Apple notarization step. Linux packaging requires neither signing nor notarization. Missing or partial credentials fail macOS builds that require publisher signing. Local `-local` evaluation builds require no publisher credentials, use ad-hoc signing on macOS, and do not enter public catalogs. See [release maintenance](../../docs/en/dev/releasing/releases.md#desktop-installers) for recovery, timeout behavior and immutable published artifacts.

`release:catalog` and `build:website:release` read the latest stable, preview and nightly GitHub Releases under the shared bounded SemVer selection policy. Desktop manifests must match Runtime's commit/version and declare all five native targets with explicit signing status and downloadable assets. Releases before desktop support produce no desktop entry. The combined catalog includes `desktop/catalog.json`, so production builds need no manually supplied `MOLR_DESKTOP_CATALOG`. Post-release website verification waits for both Runtime and desktop catalogs to match GitHub.

Release builds reuse complete CI results for the exact source SHA. `build-assets.yml` builds shared WebUI and desktop JS/TS once; `native-build.yml` builds each Runtime payload once. `release-desktop.yml` downloads those assets and the checksummed Runtime payload instead of recompiling them. Set `MOLR_BUILD_ASSETS=1` only when restoring the complete assets plus `.cache/build-assets/manifest.json`; packaging verifies source, version, lockfile and output digests. Local commands without it retain their existing build behavior and `-local` signing policy.

`node scripts/test-desktop-package.mjs [installer-directory]` verifies the final manifest and checksum, extracts or installs the package, launches its actual executable, and tests the bundled Runtime in an isolated instance. macOS and Windows package acceptance runs only on disposable GitHub runners; Linux uses isolated XDG directories. It does not replace platform-specific installer-hook testing. Signing/package failures can retry their jobs without rebuilding Runtime; notarization and publication retain their separate retry boundaries. Only stable releases automatically refresh the website.

## Native Windows execution

Controlled commands run in PowerShell inside a fresh AppContainer with per-execution file ACLs. Network access is an all-or-nothing capability toggle; Windows loopback and host firewall restrictions still apply. Job Objects terminate the process tree on cancellation, permission revocation and Runtime shutdown. This backend reports file and network isolation; it does not advertise Linux process namespaces or hard quotas. Separately authorized Full Access executes PowerShell with the signed-in user's authority. Neither mode provides ConPTY/TTY in this version; controlled port forwarding and unsupported quotas are rejected. Windows script length is limited to 12,000 UTF-8 bytes.

File tools use Go `os.Root` confinement and reject device paths, alternate streams, escaping reparse paths and hard-linked files. Moves use handle-relative no-replace rename; deletion targets the verified object handle. Replacement uses the same optimistic version check as Unix; arbitrary host writers do not participate in a filesystem CAS. Controlled tasks are serialized per Windows user while temporary ACLs exist (concurrent starts report `sandbox_file_scope_busy`); scope trees above 100,000 entries are rejected. Existing descendants are checked for hard links/reparse points and pinned during ACL inheritance changes, then released before user code. AppContainer scope handles pin ancestors until SID ACE removal; private journals recover stale grants after a crash. A moved or inaccessible recovery scope leaves controlled execution unavailable until repaired.

Native acceptance requires Windows; cross-compilation does not establish enforcement. After building the Windows payload, run:

```powershell
$env:CGO_ENABLED = '0'
$env:MOLR_TEST_BINARY = (Resolve-Path '.cache/release/win32-amd64/molr/bin/molr.exe').Path
go -C runtime test ./internal/platform/sandbox ./internal/textfiles ./internal/installationwindows ./internal/config ./internal/persistence/sqlitestore -run TestWindows -count=1
node apps/desktop/tests/local-runtime.mjs
node apps/desktop/tests/installation.mjs
```

The startup probe verifies workspace writes, denied private-file access, read-only scope enforcement, actual AppContainer token capabilities and network permission denial. Its TEST-NET connection attempt checks policy, not Internet reachability. A policy probe failure keeps controlled execution unavailable and never switches to Full Access.
