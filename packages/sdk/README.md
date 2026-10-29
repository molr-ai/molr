# @molr/sdk

Framework-independent Runtime client and UI host contract. No React or @molr/ui dependency.

- `@molr/sdk`: APIError, createClient, default browser api, DTOs, resumable subscribeRuntime.
- `@molr/sdk/ui`: theme manifest, route and shared draft types.

Sandbox, GPU, account administration, quota and usage-analytics DTOs are exported from `@molr/sdk`; their source modules are `sandbox.ts`, `administration.ts` and `usage.ts`. Application modules own display helpers and local state, not copies of these protocol types.

The direct client authenticates with same-origin cookies/CSRF. Runtime responses identify the active proof cookie through `X-Molr-CSRF-Cookie`; the development proxy rewrites its port together with `Set-Cookie`. Clients read only that cookie, including after reloads and cross-tab session rotation, and never fall back to stale cookies from another transport mode. A first authenticated write discovers the cookie through `/api/auth/me` if needed; login continues to use its explicit pre-login proof. Sandboxed plugin pages use `connectPlugin()` and named capability-scoped operations; credentials never enter the frame.

Business writes use `api.mutate(path, body, { idempotencyKey, expectedVersion })`. The caller owns the key for one logical operation: retain it across uncertain failures and retries, replace it for new operations or changed inputs, and release it on completion or abandonment. The client does not cache request bodies or infer retries from equal payloads. WebUI forms use `useMutation` with explicit input dependencies; immediate settings changes restore/reconcile state after failure and treat a later selection as a new operation.

A fresh SSE subscription starts at the current committed position. The SDK retains the cursor from the server’s `ready` handshake and invokes `onRestore()` to refresh snapshots after subscription setup; reconnects resume from the last received cursor.

`subscribeRuntime({ session: client, onUpdate, onRestore, onConnection })` binds verification, request-success wakeups and expiry handling to a `createClient()` instance. The optional `session` implements `RuntimeSession`; omitting it uses the default browser client. Instances still share the browser's same-origin cookies and Runtime endpoint.

Connection status becomes healthy only after the `ready` handshake, not the transport's `open` event. Native EventSource handles transient retries within a 20-second attempt; a terminal error or timeout closes that attempt and schedules another with approximately 5, 10, 20, then at most 30 seconds of backoff and jitter. Offline pauses retries. Online, focus, visible-page and successful non-auth API signals can advance the next attempt, with a five-second minimum between automatically awakened attempts; they never replace an active stream. `onRequestSuccess()` excludes failed, cancelled and obsolete-session responses. Only `ready` clears the notice, resets backoff and refreshes snapshots. Disposal and confirmed session expiry remove listeners and stop retries.

SSE failures verify the current browser session through `verifySession()`. A current, uncancelled 401 emits the session-expired notification; ordinary `/api/auth/me` restoration remains caller-managed. Reconnect, recovery and disposal cancel obsolete checks, and responses from a previous cookie identity cannot expire the new session. Run `npm run test --workspace @molr/sdk` for the transport regressions.

Read and download requests preserve `AbortError` when cancelled, including while the response body is being read. Malformed successful JSON remains an `invalid_response` error. Run `npm run test --workspace @molr/sdk` for client transport regressions.

See [UI packages](../../docs/dev/frontend/themes.md) for the host architecture and [Plugin API](../../docs/dev/extensions/plugins.md) for supported product extensions.

- `@molr/sdk/plugins`: Plugin API 3 包、provider、连接、个人设置、工具执行和页面引用类型。
- `@molr/sdk/plugin-host`: 外部插件单 HTML 页面使用 `connectPlugin()` 建立受限宿主连接；需打包内联，开放 plans.list、settings.get/update、connections.list/test，按页面 capability 校验；不提供通用 HTTP 代理或明文凭证。

插件列表提供 `UserEnabled`、`UserVersion` 与 `CatalogID` / `ToolIDs`。宿主在 `/extensions` 统一管理；`pluginAllowed` 同时检查全局与个人启用。`adminSettings` / `adminSettingsPage` 仅管理员使用，设置页通过固定引用限定配置范围。HTTP 的 `settingsScope: "admin"` 可使用共享设置而无需个人 Schema。

`PluginTheme.pages` 使用 `Partial<Record<PluginThemePage, string>>`，主题可覆盖 `assistants`、`plans`、`resources`、`workbench` 的子集。`settings` 由宿主管理；Runtime 继续校验页面集合非空及 HTML 入口文件有效。运行 `npm run typecheck --workspace @molr/sdk` 验证类型契约。
