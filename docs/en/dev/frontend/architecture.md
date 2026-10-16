# Frontend Architecture

WebUI manages account sessions, navigation, and page loading. The SDK, Host, and shared UI packages maintain the protocol, theme resolution, and base components respectively.

The desktop entry point is [`apps/desktop`](../../../../apps/desktop/README.md). Its trusted management interface handles connections and local services. A separate sandboxed Electron `WebContentsView` loads the selected Runtime's WebUI with an independent persistent session per connection. Remote pages have no preload or desktop-management IPC. Windows packages contain only the client; Linux/macOS bundle an independent Runtime managed through the existing installer. Closing desktop windows does not stop the service.

## Implementation Boundaries

- `packages/sdk` / `@molr/sdk`: Runtime client, DTOs, pagination, idempotent writes, uploads and downloads, errors, SSE, and UI/Host protocols; no dependency on React or `@molr/ui`.
- `packages/ui-host` / `@molr/ui-host`: manifest validation, version-pinned theme identity, theme-owned page mappings, permission policies for the prototype isolation adapter, and plugin theme types.
- `apps/webui/src/App.tsx`: login, navigation, theme resolution and page loading, and failure recovery.
- `apps/webui/src/ui-host/ThemeSelector.tsx`: the theme selector in theme settings.
- `apps/webui/src/interfaces/official`: the default theme and existing product components.
- `apps/webui/src/interfaces/test`: a test theme that declares only the chat page.

`@molr/ui` provides official shared components, CSS variables, and assets. When changing shared contracts, check their callers in WebUI, the website, and UI Design. The project has no stable release yet and does not retain an additional compatibility layer for older versions.

## Shared State and Recovery

Switching themes does not affect assistants, messages, or tasks in Runtime. Chat drafts use the `{ text, files }` format and are stored by account and assistant ID in the application shell's memory and the current tab's `sessionStorage`. The default and test themes share the same draft. Each page manages its other temporary state.

The current assistant in both the default and test themes is determined by the route ID. `/#/assistants` leaves the selection empty instead of falling back to the first assistant. List and selector actions update the URL through navigation, then open the corresponding conversation.

If a page fails to load, the shell retains navigation and offers actions to restore the default theme or open theme settings. Restoring the default theme first updates account settings, then reloads the application after success to clear cached module loading errors. If Runtime is temporarily unavailable or saving fails, the default theme is restored only within the current session, without reloading. Business pages handle normal API errors.

## Extension Foundations

The SDK's `connectUI()`, standalone page manifests, and isolated iframe adapter remain foundations for further development. External product pages use the separate, narrower contract in the [Plugin System](../extensions/plugins.md); the early general-purpose iframe HTTP proxy is not a plugin entry point.

Plugin themes may choose whether to use `@molr/ui`, provided they follow the SDK/Host contract. Remote pages require origin and permission approval; changing browser configuration alone cannot grant execution permission. Themes always define page mappings, while users select only the theme.

The existing isolation adapter uses an iframe without same-origin permission and a private MessageChannel. It validates the method, canonical path, and authorization scope for every request, and does not pass host Cookies, CSRF proofs, or Bearer tokens to the page. The permission table is in `packages/ui-host/src/permissions.ts`. Publishers must maintain immutable versioned assets; pinning the entry URL alone does not pin the contents of remote assets.
