# Frontend Architecture

WebUI manages account sessions, navigation, and page loading. The SDK, Host, and shared UI packages maintain the protocol, theme resolution, and base components respectively.

The desktop entry point is [`apps/desktop`](../../../../apps/desktop/README.md). Its trusted management interface handles connections and local services. A separate sandboxed Electron `WebContentsView` loads the selected Runtime's WebUI with an independent persistent session per connection. The remote main frame receives only a restricted `window.molrApp` API through a dedicated preload, without desktop-management IPC; plugin subframes receive no bridge. Linux, macOS and Windows bundle an independent native Runtime managed through the installer. Closing desktop windows does not stop the service.

## Implementation Boundaries

- `packages/sdk` / `@molr/sdk`: Runtime client, DTOs, pagination, idempotent writes, uploads and downloads, errors, SSE, and UI/Host protocols; no dependency on React or `@molr/ui`.
- `packages/ui-host` / `@molr/ui-host`: version-pinned theme identity, theme-owned page mappings, plugin theme types.
- `apps/webui/src/App.tsx`: login, navigation, theme resolution and page loading, and failure recovery.
- `apps/webui/src/ui-host/ThemeSelector.tsx`: the theme selector in theme settings.
- `apps/webui/src/interfaces/official`: the default theme and existing product components.

`@molr/ui` provides official shared components, CSS variables, and assets. When changing shared contracts, check their callers in WebUI, the website, and UI Design. The project has no stable release yet and does not retain an additional compatibility layer for older versions.

The official plans, resources, settings and workbench pages, and chat side panels load on demand. Toolbar buttons keep lightweight subscriptions separate from panel code. Notification detail requests skip undisplayed notices when alerts are disabled or the notice belongs to the initial snapshot; concurrent changes to the same notice are serialized and coalesced by version, retaining a follow-up read for newer state. Displayed notices still reconcile read, dismissal and recovery.

## Shared State and Recovery

Switching themes does not affect assistants, messages, or tasks in Runtime. Chat drafts use the `{ text, files }` format and are stored by account and assistant ID in the application shell's memory and the current tab's `sessionStorage`. Each page manages its other temporary state.

The current assistant in the default theme is determined by the route ID. `/#/assistants` leaves the selection empty instead of falling back to the first assistant. List actions update the URL through navigation, then open the corresponding conversation.

If a page fails to load, the shell retains navigation and offers actions to restore the default theme or open theme settings. Restoring the default theme first updates account settings, then reloads the application after success to clear cached module loading errors. If Runtime is temporarily unavailable or saving fails, the default theme is restored only within the current session, without reloading. Business pages handle normal API errors.

WebUI renders the weekly calendar with `PlanWeekCalendar`. The plans page defaults to week mode, while embedded chat plans retain their list. Week and month modes query the same plan endpoint for their visible date range, using Runtime recurrence projections and event identities. The timeline uses the browser’s local timezone, splits events at local midnight, and allocates overlapping columns using the minimum clickable height. Events without an end time mark only their occurrence time. Date navigation uses calendar arithmetic across daylight-saving changes. Shared plan type tokens supply colors, with icons and state styles as additional cues.

## Extension Foundations

External pages and themes use `connectPlugin()` and the capability-scoped contract in the [Plugin System](../extensions/plugins.md). The host supports built-in pages and installed plugin contributions.

Plugin themes may choose whether to use `@molr/ui`, provided they follow the SDK/Host contract. Remote pages require origin and permission approval; changing browser configuration alone cannot grant execution permission. Themes always define page mappings, while users select only the theme.

Plugin frames use an isolated iframe and a private MessageChannel. `PluginFrame` validates each named operation and its contribution capabilities; host cookies, CSRF proofs and bearer tokens stay outside the frame. Runtime validates installed content and permissions.

## Desktop App mode

Electron keeps its bundled connection manager before connecting and after disconnecting. A separate Chromium-sandboxed WebContentsView uses a dedicated preload to expose `window.molrApp` to the Runtime main frame; `@molr/ui-host/app` defines the type contract. WebUI’s `AppFrame` performs the handshake outside authentication gates and page themes, providing the drag region, window controls and Switch connection action. Shared providers continue to manage locale, palette and light/dark mode. Ordinary browsers have no bridge and retain the web layout.

App mode places the full wordmark in the titlebar, including the workspace connection status and retry action, and starts the left navigation at the top. The login background extends behind the transparent titlebar. Both the connection manager and WebUI keep a fixed 48 CSS px titlebar across platforms and fullscreen states, with the logo and right-side actions vertically centered. On macOS, Electron's `titleBarStyle: hidden` and `titleBarOverlay: true` retain native traffic lights. At startup, the main process measures the native button height from the trusted bundled page's safe area, then computes equal top and left margins for the active renderer's zoom to center the buttons in the 48px titlebar. Native coordinate rounding permits an error below one logical point. Preload reports viewport resize events caused by page zoom without passing dimensions; main reads the actual zoom factor. Windowed mode uses `env(titlebar-area-x)` to reserve space before the logo. Fullscreen removes this button-specific inset and uses the normal 16px left padding. Native button dimensions are not hard-coded. Windows/Linux retain right-side window buttons.

Web content in the titlebar belongs to the application's modal scope: dialog and drawer backdrops cover the whole window and make web titlebar controls inert along with the rest of the background content. Native macOS window buttons remain managed by the system and available for closing, minimizing, and fullscreen.

The bundled connection manager uses the shared full wordmark's day/night animation and respects the system reduced-motion preference. Desktop maintenance and App titlebar failures reuse shared Toast instead of inserting error blocks into the page layout; detailed installer output remains available in the maintenance log.

The desktop main process validates the current WebContents, main frame, configured connection origin and arguments. Runtime pages can read the current connection, control the current window, disconnect and initiate the OAuth flow below. Connection editing and local-service management remain behind the bundled UI’s separate API. Plugin and custom-theme iframes receive no desktop bridge. The Runtime view fills the frameless window; failed navigation, crashes or handshake timeout return to bundled recovery. Disconnect preserves partition storage and authentication and leaves Runtime running.

After creating a window, main reads `lastConnection` from the connection store and reconnects with its persistent partition, including process restarts and reopened macOS windows. Connecting saves the selection; explicitly returning to management clears it, while closing the window or a load failure preserves it. Adding a connection does not select it; removing the selected connection clears it. Restoration uses the normal connection and recovery flow and does not start a stopped local service.

After authentication, `AppAppearanceSync` subscribes to saved `/api/settings` within the account data scope and sends palette/display preferences through `setAppearance`. Main accepts only fixed enum values from the active service main frame and atomically stores them in the desktop profile’s `appearance.json`. The manager reads appearance and platform before mounting. Returning to the list and restarting retain the last signed-in service’s preferences; system mode remains `system`, not a resolved light/dark value. Login pages and failed settings saves do not replace desktop preferences, and desktop synchronization never writes service settings. The manager logo has no adjacent label.

During installation, administrator credentials pass through trusted manager IPC and child stdin, never connection storage, command-line arguments or logs. Installation returns to the list and holds its one-time link briefly in main-process memory. Opening the matching local view exposes a single-use `consumeInitialLogin` permission bound to that view and installation navigation. Other connections and ordinary URL parameters cannot enable automatic redemption. Reloading or link expiry returns to normal sign-in.

## Desktop notifications

App icon counts use `setBadge(scope, count)` within the notification scope. `end(scope)` clears the badge and ends that scope; old cleanup cannot affect later logins. Main accepts only non-negative safe integers, never renderer-supplied images or paths. Windows generates a fixed red/white numeric overlay in main; macOS/Linux use `app.setBadgeCount`. Linux packaging matches `desktopName` to the installed desktop file. The independent badge switch defaults to off. While enabled, a serial background-capable poll reads unread counts from non-archived conversations, refreshes on message/conversation/assistant events, and reconciles every 30 seconds with error backoff. Ordinary UI queries remain foreground-only.

In App mode, WebUI selects a native `NotificationAdapter` and calls Electron Notification through `molrApp.notifications`; browsers retain Web Notifications. The existing switch initializes the system channel. There is no extra Runtime permission dialog or dependency on browser notification permission. On macOS initialization can prompt for OS authorization, so `Notification.isSupported()` is not called on page load. Its result indicates support, not OS permission.

Each login lifetime gets a fresh notification scope. Main validates the current page, main frame, connection origin, scope, text lengths, and internal routes. Logout, navigation, disconnect, and disabling the switch clear notifications. Native objects are retained for read cleanup and stale-click isolation. Sending waits for native show/failed events with a deadline; failure falls back to in-app alerts. Focus can still suppress display. Status and test feedback belong to their settings row. Native macOS permission, display, and click acceptance requires a signed app.

## Desktop OAuth Callback

The main page calls `window.molrApp.oauth.authorize(providerID)` for a registered service, or `oauth.cancel()` to cancel waiting. `authorize` returns `{ status, attemptId?, connectionId? }`, where `status` is `connected`, `cancelled`, `failed` or `unknown`. Neither authorization codes nor tokens are returned. Ordinary browsers and iframes have no such bridge.

The client listens on a temporary `127.0.0.1` port before preparing authorization with the active connection partition's session and CSRF proof. Runtime owns state, nonce and PKCE. A native dialog identifies the authorization website and Runtime destination before opening the system browser. The listener accepts a single GET callback with the expected Host, path and state, then relays only allowlisted fields to fixed Runtime endpoints. The main process rechecks the active view and login identity on each request. Connection changes, main-frame navigation, window closure, cancellation and timeout close the listener. Request bodies, callback URLs and credentials are neither logged nor forwarded to plugins.

Local and remote Runtimes share this flow, including explicitly enabled private-network HTTP. The client relays an authorization code; the Runtime adapter exchanges and retains tokens. This does not add transport encryption to HTTP. Callback failures or unknown results never automatically repeat token exchange. The administrator-only ChatGPT adapter is registered. Optional authorization arguments carry a connection ID/version, name and sensitive-action proof. See the [HTTP API](../reference/http-api.md#desktop-oauth-authorization) for contracts and adapter responsibilities, and [Desktop README](../../../../apps/desktop/README.md#local-oauth-callback-and-runtime-handoff) for running the simulation.
