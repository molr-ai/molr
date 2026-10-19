# Frontend Architecture

WebUI manages account sessions, navigation, and page loading. The SDK, Host, and shared UI packages maintain the protocol, theme resolution, and base components respectively.

The desktop entry point is [`apps/desktop`](../../../../apps/desktop/README.md). Its trusted management interface handles connections and local services. A separate sandboxed Electron `WebContentsView` loads the selected Runtime's WebUI with an independent persistent session per connection. The remote main frame receives only a restricted `window.molrApp` API through a dedicated preload, without desktop-management IPC; plugin subframes receive no bridge. Linux, macOS and Windows bundle an independent native Runtime managed through the installer. Closing desktop windows does not stop the service.

## Implementation Boundaries

- `packages/sdk` / `@molr/sdk`: Runtime client, DTOs, pagination, idempotent writes, uploads and downloads, errors, SSE, and UI/Host protocols; no dependency on React or `@molr/ui`.
- `packages/ui-host` / `@molr/ui-host`: manifest validation, version-pinned theme identity, theme-owned page mappings, permission policies for the prototype isolation adapter, and plugin theme types.
- `apps/webui/src/App.tsx`: login, navigation, theme resolution and page loading, and failure recovery.
- `apps/webui/src/ui-host/ThemeSelector.tsx`: the theme selector in theme settings.
- `apps/webui/src/interfaces/official`: the default theme and existing product components.
- `apps/webui/src/interfaces/test`: a test theme that declares only the chat page.

`@molr/ui` provides official shared components, CSS variables, and assets. When changing shared contracts, check their callers in WebUI, the website, and UI Design. The project has no stable release yet and does not retain an additional compatibility layer for older versions.

The official plans, resources, settings and workbench pages, and chat side panels load on demand. Toolbar buttons keep lightweight subscriptions separate from panel code. Notification detail requests skip undisplayed notices when alerts are disabled or the notice belongs to the initial snapshot; concurrent changes to the same notice are serialized and coalesced by version, retaining a follow-up read for newer state. Displayed notices still reconcile read, dismissal and recovery.

## Shared State and Recovery

Switching themes does not affect assistants, messages, or tasks in Runtime. Chat drafts use the `{ text, files }` format and are stored by account and assistant ID in the application shell's memory and the current tab's `sessionStorage`. The default and test themes share the same draft. Each page manages its other temporary state.

The current assistant in both the default and test themes is determined by the route ID. `/#/assistants` leaves the selection empty instead of falling back to the first assistant. List and selector actions update the URL through navigation, then open the corresponding conversation.

If a page fails to load, the shell retains navigation and offers actions to restore the default theme or open theme settings. Restoring the default theme first updates account settings, then reloads the application after success to clear cached module loading errors. If Runtime is temporarily unavailable or saving fails, the default theme is restored only within the current session, without reloading. Business pages handle normal API errors.

WebUI renders the weekly calendar with `PlanWeekCalendar`. The plans page defaults to week mode, while embedded chat plans retain their list. Week and month modes query the same plan endpoint for their visible date range, using Runtime recurrence projections and event identities. The timeline uses the browser’s local timezone, splits events at local midnight, and allocates overlapping columns using the minimum clickable height. Events without an end time mark only their occurrence time. Date navigation uses calendar arithmetic across daylight-saving changes. Shared plan type tokens supply colors, with icons and state styles as additional cues.

## Extension Foundations

The SDK's `connectUI()`, standalone page manifests, and isolated iframe adapter remain foundations for further development. External product pages use the separate, narrower contract in the [Plugin System](../extensions/plugins.md); the early general-purpose iframe HTTP proxy is not a plugin entry point.

Plugin themes may choose whether to use `@molr/ui`, provided they follow the SDK/Host contract. Remote pages require origin and permission approval; changing browser configuration alone cannot grant execution permission. Themes always define page mappings, while users select only the theme.

The existing isolation adapter uses an iframe without same-origin permission and a private MessageChannel. It validates the method, canonical path, and authorization scope for every request, and does not pass host Cookies, CSRF proofs, or Bearer tokens to the page. The permission table is in `packages/ui-host/src/permissions.ts`. Publishers must maintain immutable versioned assets; pinning the entry URL alone does not pin the contents of remote assets.

## Desktop App mode

Electron keeps its bundled connection manager before connecting and after disconnecting. A separate Chromium-sandboxed WebContentsView uses a dedicated preload to expose `window.molrApp` to the Runtime main frame; `@molr/ui-host/app` defines the type contract. WebUI’s `AppFrame` performs the handshake outside authentication gates and page themes, providing the drag region, window controls and Disconnect action. Shared providers continue to manage locale, palette and light/dark mode. Ordinary browsers have no bridge and retain the web layout.

App mode places the full wordmark in the titlebar, including the workspace connection status and retry action, and starts the left navigation at the top. The login background extends behind the transparent titlebar. The titlebar is application content: dialogs and drawers use a window-wide modal scope, with their backdrops covering the titlebar and making its controls inert along with the rest of the background content.

The bundled connection manager uses the shared full wordmark's day/night animation and respects the system reduced-motion preference. Desktop maintenance and App titlebar failures reuse shared Toast instead of inserting error blocks into the page layout; detailed installer output remains available in the maintenance log.

The desktop main process validates the current WebContents, main frame, configured connection origin and arguments. Runtime pages can read the current connection, control the current window, disconnect and initiate the OAuth flow below. Connection editing and local-service management remain behind the bundled UI’s separate API. Plugin and custom-theme iframes receive no desktop bridge. The Runtime view fills the frameless window; failed navigation, crashes or handshake timeout return to bundled recovery. Disconnect preserves partition storage and authentication and leaves Runtime running.

## Desktop OAuth Callback

The main page calls `window.molrApp.oauth.authorize(providerID)` for a registered service, or `oauth.cancel()` to cancel waiting. `authorize` returns `{ status, attemptId?, connectionId? }`, where `status` is `connected`, `cancelled`, `failed` or `unknown`. Neither authorization codes nor tokens are returned. Ordinary browsers and iframes have no such bridge.

The client listens on a temporary `127.0.0.1` port before preparing authorization with the active connection partition's session and CSRF proof. Runtime owns state, nonce and PKCE. A native dialog identifies the authorization website and Runtime destination before opening the system browser. The listener accepts a single GET callback with the expected Host, path and state, then relays only allowlisted fields to fixed Runtime endpoints. The main process rechecks the active view and login identity on each request. Connection changes, main-frame navigation, window closure, cancellation and timeout close the listener. Request bodies, callback URLs and credentials are neither logged nor forwarded to plugins.

Local and remote Runtimes share this flow, including explicitly enabled private-network HTTP. The client relays an authorization code; the Runtime adapter exchanges and retains tokens. This does not add transport encryption to HTTP. Callback failures or unknown results never automatically repeat token exchange. No service adapters are integrated yet; an isolated simulation verifies the mechanism. See the [HTTP API](../reference/http-api.md#desktop-oauth-authorization) for contracts and adapter responsibilities, and [Desktop README](../../../../apps/desktop/README.md#local-oauth-callback-and-runtime-handoff) for running the simulation.
