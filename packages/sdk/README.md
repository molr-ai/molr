# @molr/sdk

Framework-independent Runtime client and UI host contract. No React or @molr/ui dependency.

- `@molr/sdk`: APIError, createClient, default browser api, DTOs, resumable subscribeRuntime.
- `@molr/sdk/ui`: manifest, route, permission, context and draft types.
- `@molr/sdk/host`: connectUI() for sandboxed UI documents.

The direct client authenticates with same-origin cookies/CSRF. Runtime responses identify the active proof cookie through `X-Molr-CSRF-Cookie`; the development proxy rewrites its port together with `Set-Cookie`. Clients read only that cookie, including after reloads and cross-tab session rotation, and never fall back to stale cookies from another transport mode. A first authenticated write discovers the cookie through `/api/auth/me` if needed; login continues to use its explicit pre-login proof. Sandboxed interfaces must use connectUI(), which accesses only explicitly granted APIs via the host; credentials never enter the frame.

A fresh SSE subscription starts at the current committed position. The SDK retains the cursor from the server’s `ready` handshake and invokes `onRestore()` to refresh snapshots after subscription setup; reconnects resume from the last received cursor.

SSE failures verify the current browser session through `verifySession()`. A current, uncancelled 401 emits the session-expired notification; ordinary `/api/auth/me` restoration remains caller-managed. Reconnect, recovery and disposal cancel obsolete checks, and responses from a previous cookie identity cannot expire the new session. Run `npm run test --workspace @molr/sdk` for the transport regressions.

Read and download requests preserve `AbortError` when cancelled, including while the response body is being read. Malformed successful JSON remains an `invalid_response` error. Run `npm run test --workspace @molr/sdk` for client transport regressions.

See [UI packages](../../docs/dev/frontend/themes.md) for the host architecture and [Plugin API](../../docs/dev/extensions/plugins.md) for supported product extensions.

- `@molr/sdk/plugins`: Plugin API 3 包、provider、连接、个人设置、工具执行和页面引用类型。
- `@molr/sdk/plugin-host`: 外部插件单 HTML 页面使用 `connectPlugin()` 建立受限宿主连接；需打包内联，开放 plans.list、settings.get/update、connections.list/test，按页面 capability 校验；不提供通用 HTTP 代理或明文凭证。

插件列表提供 `UserEnabled`、`UserVersion` 与 `CatalogID` / `ToolIDs`。宿主在 `/extensions` 统一管理；`pluginAllowed` 同时检查全局与个人启用。`adminSettings` / `adminSettingsPage` 仅管理员使用，设置页通过固定引用限定配置范围。HTTP 的 `settingsScope: "admin"` 可使用共享设置而无需个人 Schema。
