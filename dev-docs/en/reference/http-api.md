# Runtime HTTP Interfaces

The Go monolithic server uses explicitly registered `/api` routes. Local initialization/recovery, assistant message publishing, checkpoints, tool execution, budget reservation/settlement, and operation receipt writing are not exposed here.

## Public Protocol Boundaries

Clients currently connect to the Runtime via HTTP/JSON and SSE; route implementations are in [`runtime/internal/api`](../../../runtime/internal/api), and TypeScript DTOs, clients, and resumption logic are in [`@molr/sdk`](../../../packages/sdk/README.md). The repository does not have a separate OpenAPI/JSON Schema distribution package. HTTP paths use `/api`, and there is currently no separate client protocol version negotiation; the `Version` / `ExpectedVersion` fields of objects are used for concurrent modification checks and do not represent protocol versions. The current product client is the WebUI; desktop and mobile browsers can connect to the same Runtime, while standalone Desktop/Mobile/CLI clients are not published capabilities.

Public interfaces only expose explicitly registered business capabilities, which is not equivalent to all Go module interfaces. Tasks use current domain objects such as `Task`; early `Work` and virtual OS proposals are not part of the current public interface and cannot be used to construct requests. Client DTOs, internal persistence records, and Agent checkpoints are different boundaries; when modifying one format, client compatibility and data migration must be verified separately, and internal formats must not be treated as stable network contracts.

The authentication layer determines the principal from a valid session or access token; self-reported identity by clients and Agents does not constitute permission. Except for explicitly public login entry points, business APIs, SSE, search, attachments, and artifact downloads all require authentication and object permission checks; cursors and object IDs cannot substitute for authorization. Multi-account support is currently available: private business data is isolated by account, operations such as managing the resource directory require administrator privileges, and resource execution is further constrained by user group scope, member quotas, and assistant authorization; see [Multi-user and Member Quotas](../security/users-and-groups.md) for details. Rules for browsers, tokens, one-time login links, and sensitive operation verification are described below; do not interpret additional identity confirmation as a unified process unconditionally required for every administrative operation.

Command reception, operation execution results, and user-visible message publishing are different stages. Request idempotency does not guarantee that all external side effects occur exactly once; unknown results need to be verified. Internal scheduling events do not directly become chat messages; assistants publish via session tools and cannot impersonate users. See [Runtime Architecture](../architecture/overview.md) for internal queues, tools, and recovery mechanisms. Agent tools do not expose account, credential, or access token management; model-visible content must not contain real credentials, authentication headers, cookies, or internal credential references; see [Security Boundaries](../security/security-model.md) for details.

The Runtime remains a Go monolith and does not load Go dynamic business modules; the implemented [Plugin API 3](../extensions/plugins.md) supports isolated pages, themes, personal configurations, service connections, and declarative HTTP tools. The plugin protocol and [UI/Host Contract](../frontend/themes.md) are maintained separately; this does not imply that the HTTP API has the same version negotiation or arbitrary backend module loading capabilities.

## Request and Transport Conventions

- JSON requests reject unknown fields and trailing content, and limit size and processing time. Domain DTOs correspond to field names of Go public request types (requests support case-insensitive matching for Go JSON); authentication, credential, and sensitive setting operations use separate DTOs, with underscore fields such as `new_password`, `mfa_code`, `expires_at`, `include_messages`, `object_ids`. Secret values must be strings; private `Secret` objects are not accepted.
- `Mutation` in business write DTOs carries `IdempotencyKey`; modifying existing objects also requires `ExpectedVersion`. Interfaces such as session read status and authentication revocation are handled according to their respective contracts.
- Lists support `cursor`, `limit`, and module query parameters; time filtering uses RFC3339.
- `ModelID` of `POST /api/assistants` simultaneously expresses usage authorization for the selected model: creating an assistant, session, `read` / `execute` Grant for that model (`Scope.ModelID` specifies the model, `ResourceID` binds the owning Provider, and the call address follows its current configuration) and the Agent Loop are committed in the same transaction. All failures are rolled back, and duplicate requests do not duplicate authorization; retrying the original hiring request or renaming after revocation will not restore permissions. Other resources still require explicit authorization, and cloning and rehiring only restore user-selected permissions.
- Model-class `POST /api/grants` must provide a valid `Scope.ModelID`, and the model must belong to the Provider specified by `ResourceID`; non-model resources cannot carry model scopes. Overall authorization for a Provider without specifying a model is not accepted. The model catalog and inference entry points check read/execute permissions per model; other models under the same Provider do not automatically receive authorization. Cloning preserves model scopes, and revocation only affects the corresponding authorization.
- `Actions` returned by resources is the subset of authorization items supported by the current Provider. `read` / `execute` for models are "View Model / Call Model" respectively, and `read` / `write` for local storage are "Read File / Write File". `POST /api/grants` rejects actions not supported by the resource; assistant execution also re-checks current capabilities, and old authorizations cannot bypass capability limits. The frontend displays names by resource type and only provides options actually declared by the resource; switching resources clears inapplicable actions and scopes.
- The WebUI atomically creates/updates Providers via `POST /api/resources/provider`: passing `Mutation`, optional `ID`, `Name`, `Endpoint`, `UseAPIKey`, and passing `APIKey` separately when saving or replacing the Key; when sensitive operation verification is enabled, `verification` with purpose `credential.put` is required, and it can be empty when disabled. Leaving the Key empty during editing means retaining the configured Key; `UseAPIKey: false` cancels authentication for that connection. The new Key, resource binding, version, and audit are committed in the same transaction, and failures are rolled back together. `GET /api/resources/{id}/connection` is only for administrators to read editable metadata and does not return keys.
- `POST /api/resources/delete-provider` accepts `ID`, `Mutation`, and optional `verification`; only administrators can delete model-class Providers, and disconnected connections are also supported. When sensitive operation verification is enabled, proof with purpose `credential.delete` is required. Deleting connections, binding/model catalogs, credentials not referenced by other connections, revoking resource authorizations, and clearing invalid default models are all committed in the same transaction; failures are rolled back, and duplicate requests are idempotent. Assistant, chat, task, and audit history are retained; assistants explicitly using deleted models need to re-select a model and will not automatically switch to other providers.
- Model connections treat provider as the resource; private network access is controlled by the deployment static configuration `network.allowed_private_ips`, and resource APIs do not accept network allowlist parameters. `POST /api/resources/test-connection` accepts `Provider`, `Binding`, and optional `APIKey`, returning `ModelCount`; it is only available to administrators, where a temporary API Key and saved `CredentialID` are mutually exclusive, configurations are not saved, chat content is not uploaded, and deployment network policies are used. `Binding.Models` can be empty; optional manual models include name, context, tool capabilities, and price, where `Pricing: null` indicates price not configured; explicitly providing all three unit prices in `Pricing` as zero indicates confirmation of free usage. `POST /api/resources/refresh-models` accepts `ID` and `Mutation`, discovers models outside the transaction, and then saves results by version. `GET /api/models` returns independent model IDs, owning `ResourceID`, and `Pricing`; model authorization binds both the resource and `Scope.ModelID`, and inference is not allowed for unknown prices. `POST /api/resources/configure-model` accepts `ID`, `Model`, and `Mutation`, configuring context, tool capabilities, and optional price for a single model; refresh preserves manual configurations. Discovery failures retain existing lists, repeated refreshes do not change model IDs, and models exit the available list after the provider is disconnected.
- `GET /api/status` requires authentication and returns `name` (Molr Runtime), `status` (running), and `public_url` from the configuration, for connection settings to display the actual Runtime address.
- Message lists are in ascending order by default; `order=desc` starts from the latest message and continues using the returned `NextCursor` to load earlier messages. Cursors are bound to direction and cannot be mixed. Messages include the currently persisted `Feedback` (`like` / `dislike` / `none`).
- `http.requests_per_minute` controls the general request limit for each trusted source address, defaulting to 600; login/MFA completion requests independently use `auth.login_attempts_per_minute` (default 5). Both must be positive.
- SSE: `GET /api/events/stream`, the `id` of events is an opaque cursor bound to the owner and topic, which can be reconnected via `Last-Event-ID`. The service continuously verifies sessions, and revocation or expiration will close the connection. `GET /api/events` is used for limited pagination fetching. The `topic` parameter for both entry points allows a maximum of 16 items, requiring non-empty, non-duplicate, and each not exceeding 128 bytes; cursors are bound to the account and the normalized set of topics. Real-time delivery is awakened by transaction commit notifications, and disconnections are backfilled by cursor; network interruption boundaries may result in duplicate delivery, and clients should handle idempotency by event ID/cursor.
- Upload: `POST /api/files` uses raw body, `Content-Length`, `Content-Type`, `X-File-Name`, `Idempotency-Key`, and optional `X-Task-ID`. For non-ASCII filenames, `X-File-Name` can be percent-encoded and `X-File-Name-Encoding: percent` set; old unencoded headers remain compatible. Downloads are only via authenticated `/api/files/{id}/content`, using attachment, application/octet-stream, and sandbox CSP, without exposing disk paths.
- For local development, `tls_mode: "local"` can be explicitly configured: listen only on the numeric loopback interface, use HTTP loopback URLs, and do not accept proxy forwarding headers. The development session/pre-login cookies are `molr_dev_session` / `molr_dev_login`, retaining HttpOnly/SameSite=Strict while omitting Secure; the independent proof cookie is the readable `molr_dev_csrf`; the authentication and CSRF flows remain the same.
- HTTPS is terminated directly or via an explicitly trusted proxy. The Host must match PublicURL; in proxy mode, only configured network segments are trusted, proxy HTTPS headers are validated, and trusted proxy addresses are stripped from right to left. Cross-origin sources must be explicitly configured; wildcards are not used.

## Route Inventory

Verified on 2026-10-03 against the registrations in `runtime/internal/api/*_routes.go`, including sandbox action routes. Synchronize this table when adding or adjusting routes; the existence of a path does not imply that the current identity has access rights, and deployment capability switches remain in effect. For authentication and common fields, refer to this document; for specific behaviors, see [Plugins](../extensions/plugins.md#lifecycle-and-http-routing), [Dreaming](../runtime/dream-memory.md#interface-and-validation), [Workbench](../runtime/workbench.md#runtime-implementation), [Member Quotas](../security/users-and-groups.md), and [Resource Quotas](../security/resource-quotas.md).

| Method | Path |
| --- | --- |
| DELETE | `/api/auth/login-links/{id}` |
| DELETE | `/api/auth/sessions/{id}` |
| DELETE | `/api/auth/tokens/{id}` |
| DELETE | `/api/files/{id}` |
| GET | `/api/account` |
| GET | `/api/account/quotas` |
| GET | `/api/admin/catalog` |
| GET | `/api/admin/groups` |
| GET | `/api/admin/usage` |
| GET | `/api/admin/users` |
| GET | `/api/assistants` |
| GET | `/api/assistants/activity` |
| GET | `/api/assistants/groups` |
| GET | `/api/assistants/{id}` |
| GET | `/api/assistants/{id}/capabilities` |
| GET | `/api/assistants/{id}/shell-tasks` |
| GET | `/api/assistants/{id}/status` |
| GET | `/api/audit` |
| GET | `/api/auth/csrf` |
| GET | `/api/auth/login-links` |
| GET | `/api/auth/me` |
| GET | `/api/auth/security` |
| GET | `/api/auth/sessions` |
| GET | `/api/auth/tokens` |
| GET | `/api/conversations` |
| GET | `/api/conversations/{id}` |
| GET | `/api/conversations/{id}/messages` |
| GET | `/api/conversations/{id}/messages/{message}/metrics` |
| GET | `/api/credentials` |
| GET | `/api/credentials/{id}` |
| GET | `/api/debug/assistants/{id}/checkpoint` |
| GET | `/api/debug/runs` |
| GET | `/api/debug/runs/{id}` |
| GET | `/api/debug/runs/{id}/steps/{step}` |
| GET | `/api/debug/stream` |
| GET | `/api/debug/trajectory` |
| GET | `/api/dreams` |
| GET | `/api/dreams/config` |
| GET | `/api/dreams/conflicts` |
| GET | `/api/dreams/runs/{id}` |
| GET | `/api/dreams/windows` |
| GET | `/api/events` |
| GET | `/api/events/stream` |
| GET | `/api/files/{id}` |
| GET | `/api/files/{id}/content` |
| GET | `/api/grants` |
| GET | `/api/jobs` |
| GET | `/api/memories` |
| GET | `/api/memories/{id}` |
| GET | `/api/models` |
| GET | `/api/operations` |
| GET | `/api/operations/{id}` |
| GET | `/api/permission-requests` |
| GET | `/api/permission-requests/{id}` |
| GET | `/api/plans` |
| GET | `/api/plans/events` |
| GET | `/api/plugins` |
| GET | `/api/resource-usage` |
| GET | `/api/resources` |
| GET | `/api/resources/{id}` |
| GET | `/api/resources/{id}/connection` |
| GET | `/api/sandbox` |
| GET | `/api/sandboxes` |
| GET | `/api/sandboxes/{id}/resources` |
| GET | `/api/schedules` |
| GET | `/api/schedules/{id}` |
| GET | `/api/search` |
| GET | `/api/search/rebuild/{id}` |
| GET | `/api/settings` |
| GET | `/api/settings/currency` |
| GET | `/api/settings/exports/{id}` |
| GET | `/api/status` |
| GET | `/api/tasks` |
| GET | `/api/tasks/{id}` |
| GET | `/api/tasks/{id}/revisions` |
| GET | `/api/todos` |
| GET | `/api/todos/{id}` |
| GET | `/api/tools` |
| GET | `/api/usage` |
| POST | `/api/account/profile` |
| POST | `/api/admin/groups` |
| POST | `/api/admin/groups/delete` |
| POST | `/api/admin/users` |
| POST | `/api/admin/users/update` |
| POST | `/api/assistants` |
| POST | `/api/assistants/clone` |
| POST | `/api/assistants/dismiss` |
| POST | `/api/assistants/groups` |
| POST | `/api/assistants/groups/delete` |
| POST | `/api/assistants/groups/update` |
| POST | `/api/assistants/place` |
| POST | `/api/assistants/rehire` |
| POST | `/api/assistants/update` |
| POST | `/api/assistants/{id}/compress` |
| POST | `/api/assistants/{id}/expedite` |
| POST | `/api/assistants/{id}/shell-tasks` |
| POST | `/api/assistants/{id}/stop` |
| POST | `/api/auth/login` |
| POST | `/api/auth/sessions/revoke-others` |
| POST | `/api/auth/login-links` |
| POST | `/api/auth/login-links/preview` |
| POST | `/api/auth/login-links/redeem` |
| POST | `/api/auth/logout` |
| POST | `/api/auth/mfa/complete` |
| POST | `/api/auth/mfa/confirm` |
| POST | `/api/auth/mfa/disable` |
| POST | `/api/auth/mfa/enroll` |
| POST | `/api/auth/password` |
| POST | `/api/auth/reauthenticate` |
| POST | `/api/auth/security` |
| POST | `/api/auth/tokens` |
| POST | `/api/conversations/feedback` |
| POST | `/api/conversations/messages` |
| POST | `/api/conversations/read` |
| POST | `/api/credentials` |
| POST | `/api/credentials/delete` |
| POST | `/api/debug/assistants/{id}/context/reset` |
| POST | `/api/dreams/config` |
| POST | `/api/dreams/run` |
| POST | `/api/dreams/undo` |
| POST | `/api/files` |
| POST | `/api/grants` |
| POST | `/api/grants/revoke` |
| POST | `/api/grants/update` |
| POST | `/api/jobs/{id}/cancel` |
| POST | `/api/memories` |
| POST | `/api/memories/delete` |
| POST | `/api/memories/update` |
| POST | `/api/operations/reconcile` |
| POST | `/api/operations/{id}/cancel` |
| POST | `/api/permission-requests/resolve` |
| POST | `/api/plans/configure` |
| POST | `/api/plans/execute` |
| POST | `/api/plugins/configure` |
| POST | `/api/plugins/preference` |
| POST | `/api/plugins/install` |
| POST | `/api/plugins/remove` |
| POST | `/api/plugins/ui` |
| POST | `/api/plugins/settings/read` |
| POST | `/api/plugins/settings/save` |
| POST | `/api/plugins/connections/list` |
| POST | `/api/plugins/connections/save` |
| POST | `/api/plugins/connections/remove` |
| POST | `/api/plugins/connections/test` |
| POST | `/api/plugins/connections/history` |
| POST | `/api/plugins/connections/acknowledge` |
| POST | `/api/resources` |
| POST | `/api/resources/configure-model` |
| POST | `/api/resources/delete-provider` |
| POST | `/api/resources/disconnect` |
| POST | `/api/resources/provider` |
| POST | `/api/resources/refresh-models` |
| POST | `/api/resources/test-connection` |
| POST | `/api/resources/update` |
| POST | `/api/resources/{id}/check` |
| POST | `/api/sandboxes/connect` |
| POST | `/api/sandboxes/disconnect` |
| POST | `/api/sandboxes/rename` |
| POST | `/api/sandboxes/resource` |
| POST | `/api/sandboxes/test` |
| POST | `/api/schedules` |
| POST | `/api/schedules/delete` |
| POST | `/api/schedules/update` |
| POST | `/api/search` |
| POST | `/api/search/rebuild` |
| POST | `/api/settings` |
| POST | `/api/settings/cleanup` |
| POST | `/api/settings/export` |
| POST | `/api/tasks` |
| POST | `/api/tasks/cancel` |
| POST | `/api/tasks/pause` |
| POST | `/api/tasks/resume` |
| POST | `/api/tasks/revise` |
| POST | `/api/todos` |
| POST | `/api/todos/delete` |
| POST | `/api/todos/update` |

HTTP rate limiting reads and updates persistent counters within a transaction; passing requests consume the current window quota. When the quota is exhausted, it returns 429 and `Retry-After`, without rewriting unchanged counters or extending the window. Concurrent requests share the transaction counter, and the window is preserved across restarts; if storage fails, requests are denied.

## Authentication and Browser Flow

1. `GET /api/auth/csrf` obtains `csrf_token`, and the server simultaneously sets an independent pre-login Cookie.
2. `POST /api/auth/login` sends JSON `username`, `password`, carrying Cookies, `Origin`, and `X-CSRF-Token`. If MFA is enabled, it returns `challenge`, then calls `/api/auth/mfa/complete`.
3. Successful HTTPS login sets `__Host-molr_session`: Secure, HttpOnly, Path=/, SameSite=Strict. JSON returns session metadata and a new independent `csrf_token`, but does not return the browser session token. It also sets a readable `__Host-molr_csrf` (Secure, Path=/, SameSite=Strict), used for refreshing or restoring request headers in new tabs; it differs from the session credential, and validation remains bound to the session digest stored on the server side. It is cleared upon logout.
   LAN HTTP (`tls_mode: lan`) uses `molr_lan_session_p<端口>`, `molr_lan_csrf_p<端口>`, `molr_lan_login_p<端口>`, does not set Secure, but retains HttpOnly session, SameSite, origin, and CSRF validation; local mode on the local machine retains the development Cookie name.

4. Modification requests authenticated via Cookie check CSRF and origin. Native clients can use `Authorization: Bearer …`; credentials in the URL are prohibited. Providing both Cookie and Bearer simultaneously is rejected.
5. `GET /api/auth/security` returns `MFAEnabled`, `ReauthenticationEnabled`, `UnprotectedPurposes`, and `Version` to the current owner session. Sensitive operation verification is disabled by default; `credential.put`, `credential.delete`, and `export` do not require re-entering the password, but still require a valid owner session and CSRF. When enabled, both frontend and backend uniformly require purpose-bound verification proofs; if MFA is enabled, a verification code is additionally required. `POST /api/auth/security` receives `enabled`, `expected_version`, and `verification` with the purpose `security.update`. Password changes, MFA management, token creation, and data cleanup always require verification. When verification is required, call `/api/auth/reauthenticate` to obtain a single-use, short-lived `verification` bound to the session and purpose. Tokens return the plaintext `value` only once upon successful creation.

"Owner" in this document refers to the current account's ownership of its private objects, which is not equivalent to the initially initialized account or the administrator role; shared resource configuration additionally requires an administrator. Identity is injected via HTTP authentication; business requests cannot provide Principal, Owner identity, or execution identity. Each business module continues to verify current permissions. Errors return fixed `code`, `message`, `request_id`, and an optional `reason`. Published reasons are limited to the reviewed `network_private_address_denied`, `network_http_not_allowed`, and `network_destination_prohibited` codes. Clients localize these reasons and fall back to the code message for unknown reasons; underlying exceptions and fields remain private.

Verifier binding first passes identity confirmation with the purpose `mfa.enroll`, obtaining a short-lived enrollment and setup key bound to the current session. `POST /api/auth/mfa/confirm` only requires `enrollment_id` and `code`; after binding is complete, a one-time recovery code is returned, without needing to re-enter the password.

### Device Sessions

- `POST /api/auth/login` and `/api/auth/login-links/redeem` accept `device_id`, `client_type`, `device_name`. device_id is a 32–128 character URL-safe random string, saved by the client per instance and service address; if omitted, an independent temporary session is created. client_type supports `web`, `ios`, `android`, `desktop`, `cli`, and is used only for display. device_name is up to 128 bytes and does not accept control characters.
- Repeated authentication with the same account and device_id replaces the original session within a transaction, returning a new SessionID, Cookie, and CSRF; CreatedAt is preserved, while LastLoginAt, LastSeenAt, and ExpiresAt are updated. CLI sessions without a device_id are created independently and logged out upon termination.
- `POST /api/auth/mfa/complete` must carry the device_id from the start of the login; device name and type use the values bound to the challenge. If an older challenge is completed while the device already has a new session, it returns 409, and login must be restarted.
- `GET /api/auth/sessions` returns only currently valid sessions, including ClientType, DeviceName, LastLoginAt, LastSeenAt, ExpiresAt, and Current; it paginates in descending order of recent activity. The WebUI pins the current device to the top. `POST /api/auth/sessions/revoke-others` revokes all valid sessions for the current account except the requesting session, requiring a login session and CSRF, and records an audit log.

### Login Links

- `POST /api/auth/login-links`: A logged-in owner provides `name`, `target_path`, `expires_in` (60–86400 seconds), and an identity verification proof with the purpose `login_link.create`. The default UI duration is 1 hour, with additional options of 15 minutes and 24 hours. It returns `link` metadata and `value` visible only this time; the database stores only the Token digest.
- `GET /api/auth/login-links` returns paginated metadata, including `UsedAt`, `RevokedAt`, `ExpiresAt`, and `PendingMFAUntil` (has a value only when waiting for MFA); it does not return credentials. `DELETE /api/auth/login-links/{id}` revokes the link and also invalidates its pending MFA challenge, without affecting already issued sessions.
- The WebUI uses `#/login?token=…`, clears it from the address bar immediately after reading, does not write the Token to localStorage/sessionStorage, and does not put it in the HTTP URL. After the page obtains the pre-login CSRF, it POSTs to `/api/auth/login-links/preview`, with the request body containing only `token`. The preview returns only the real `expires_at`, does not consume the link, and does not issue a session; invalid, expired, used, or revoked links return 401. This interface uses the same Origin, pre-login CSRF, rate limiting, and no-store protections as the login interface. The confirmation page displays one-time usage instructions and expiration time; only after the user clicks confirm does it re-obtain the pre-login CSRF and POST to `/api/auth/login-links/redeem`, submitting `token`, `device_id`, `client_type`, and `device_name`. State is re-verified during redemption to prevent login via links that were revoked or expired after the preview.
- Redemption reuses the login's Origin, CSRF, rate limiting, and HttpOnly Cookie rules. The server places Token consumption and session/MFA challenge issuance in the same transaction; concurrent redemptions succeed only once. If MFA is enabled, it still calls the existing `/api/auth/mfa/complete`; incorrect verification codes do not issue a session.
- The target is saved by the server at creation time, limited to known internal routes; redirect overrides in the URL are not accepted. Link validity and session validity are independent. Password changes, local account recovery, and backup recovery all invalidate old links.
- Invalid, used, revoked, or expired links uniformly return `unauthenticated`, and the page prompts to obtain a new link or log in with a password. Request responses prohibit caching, and audit records contain only the link ID, not the original Token.

## Account and User Group Management

`GET /api/auth/me` retains the Principal field and adds `User` (Display Name, Avatar, Groups, Administrator, Disabled, Version). Users can only read their own identity.

| Route | Permissions and Purpose |
| --- | --- |
| `GET /api/account` | Current account profile |
| `POST /api/account/profile` | Update own DisplayName and Avatar, with Mutation.ExpectedVersion optimistic locking; Avatar omitted/null is preserved, empty string clears it, supports `preset:<id>` or PNG/JPEG data URLs not exceeding 128 KiB and dimensions not exceeding 256×256. Images are re-encoded after decoding to remove metadata; external URLs and SVG are rejected |
| `GET /api/account/quotas` | Own group configuration, cumulative `Summary`, current quota usage for each resource `Resources` and `AsOf` |
| `GET /api/admin/users` | Admin lists account metadata with pagination |
| `POST /api/admin/users` | Admin creates a user, Username / DisplayName / Password / GroupID / Mutation |
| `POST /api/admin/users/update` | Admin modifies a group, name, or disabled status, ID / DisplayName / GroupID / Disabled / Mutation |
| `GET /api/admin/groups` | Admin lists user groups |
| `POST /api/admin/groups` | Admin saves Group and Mutation; Resources only accept existing model/tool/sandbox references and quotas |
| `POST /api/admin/groups/delete` | Admin deletes an empty custom group, ID and Mutation.ExpectedVersion |
| `GET /api/admin/catalog` | Admin reads the referenceable catalog and sandbox quota support status |
| `GET /api/admin/usage` | Admin reads cumulative statistics, cannot read detailed business data of other users |

For identity and metering boundaries, see [Multi-user and Member Quotas](../security/users-and-groups.md).

### Usage Statistics Response

The `Summary` and `GET /api/admin/usage` of `GET /api/account/quotas` return `Users` where corresponding members use the same cumulative statistics: `ModelCalls`, `ToolCalls`, `ConnectionCalls`, `Spent`, `Reserved`, `AttachmentBytes`, `Active`, `Unknown`. Amounts are returned in the original currency; attachments only count Ready files. `Active` / `Unknown` are the occupied/unknown counts for model and tool operations; HTTP tools are included in tool operations and are not double-counted. Admin interfaces only output statistics and account identifiers, and do not return call parameters, receipt bodies, or private business content.

Context usage: The `ContextStorage` of `GET /api/account/quotas` only counts objects reachable by the current account's references, deduplicated within the account; `GET /api/admin/usage` returns `{Users, ContextStorage}`, where `ContextStorage` counts all stored objects (including those pending reclamation) after cross-account deduplication, independent of member filtering. The fields for both are `Bytes` (byte count of the object JSON body, including messages, shared definitions, and debug details, excluding indexes, reference metadata, and database free space) and `Messages` (number of unique message nodes). Statistics are completed within the same read transaction; shared objects are counted separately for each account referencing them, and account values cannot be summed to replace global values. The interface only returns aggregate numbers, not messages, node IDs, or references from other accounts.

Personal `Resources` includes `Kind`, `ID`, `Name`, `Calls`, `Active`, `Unknown`, `Carried`, `Spent`, `Reserved`, `PeriodStart`, `PeriodEnd`. Statistics and admission for models/tools/connections are consistent; calls still executing outside the current period continue to count towards occupancy and are listed separately under `Carried`. `Unknown` indicates that the local call has ended but the result or cost is unknown; it is not counted in `Active` / `Reserved`, and the call count is only counted in the original period; it is not a subset of `Active`. `PeriodEnd=null` represents the cumulative period; actual sandbox usage is still obtained via `/api/resource-usage`, and its configured limits are not treated as usage.

## Settings

### UI Theme

UI Theme: `Settings.UITheme` saves `id@version` (e.g., `molr.test@1.0.0`), separate from `Settings.Theme` which controls light/dark mode. Default is `molr.official@1.0.0`, saved per Owner and using settings version checks; old clients omitting or passing empty fields retain the existing theme. Runtime only validates identifier formats and does not load UI code; available themes are determined by the client's built-in catalog and plugin themes enabled and authorized by Runtime; see [Plugin Mechanism](../extensions/plugins.md#lifecycle-and-http-routing) for the plugin interface.

### Currency and Exchange Rates

Currency and Exchange Rates: `Settings.Currency` supports `USD` / `CNY`, used as the default currency for new amount inputs and the display currency. Budget `Budget.Limit.Currency` and manual model prices `Model.Pricing.Currency` independently save the original currency selected during input, and retain the original currency when editing. Changing display preferences does not rewrite budgets, prices, reservations, or settlements. When configuring manual model prices, the currency must be explicitly passed; the three token prices share this currency.

`GET /api/settings/currency` returns the current `Currency`, `Timezone`, and `Rates` (`version`, `as_of`, `source_url`, `units`). `units` is an integer ratio under the same base, converted using `amount * units[target] / units[source]`, retaining six decimal places. Server-side budget checks round up; only explicitly configured currencies are supported, missing exchange rates and overflows are rejected.

`Usage.Spent` / `Reserved` aggregate into the current display currency, while also returning amounts grouped by original currency via `OriginalSpent` / `OriginalReserved`, with `Exchange` marking the exchange rate snapshot for this aggregation. The WebUI converts original currency subtotals into the user's preferred currency for aggregation, displaying only the total without listing original currency amounts side-by-side; fixed to two decimal places, rounding up if less than one cent, converted amounts are marked with `≈`. If preferences or exchange rates are unavailable, `—` is displayed. This only affects display; amount inputs and backend accounting retain original precision. Budget comparisons use the same exchange rate snapshot; `Reserved` in reservation records and settlement `Settled` always retain the original currency; `BudgetEstimate` and `Exchange` save the conversion results and versions from budget validation. Subsequent rule updates do not modify these credentials.

### Plugin Display

Plugin Display: `Settings.PluginsVisible` is an account-level boolean preference, defaulting to `false`, read and written via the existing `GET /api/settings` and settings update interfaces. It only controls WebUI display and does not change plugin installation, start/stop, authorization, or tool execution.

## Models and Initial Configuration

### First Use and Local Provider Discovery

- `GET /api/setup`: Admin reads `{ Required, AssistantID }`. Only accounts initialized with local `bootstrap` have a pending status; regular `init` and existing accounts are not forced into onboarding.
- `POST /api/setup`: Logged-in admin submits `ModelID`, `Language` (zh/en), `Timezone`, `Budget` (AssistantBudget), and optional `Pricing` (ModelPricing). Unknown prices must be explicitly submitted; 0 indicates free. Setting preferences, configuring prices, creating the default Molr assistant, and completing the status are submitted in the same database transaction; duplicate requests return the original AssistantID, and the assistant does not re-enter onboarding after deletion. Reuses Hire's Born event; the first greeting goes through the normal model budget chain.
- `GET /api/resources/discover-local`: Admin triggers a limited local probe, returning an array of `Name`, `Preset`, `Endpoint`, `State`, `Models`, `ResourceID`. State is ready, empty, or authentication_required; a non-empty ResourceID indicates an existing connection with the same address. Results are not automatically saved or authorized; after selection, the provider/refresh-models interface is used.

Probe candidates are fixed to loopback 11434, 8080, 8000, 30000; limited concurrency, short timeouts, redirects are rejected, and the deployment's actual connection address limits are followed. Only model metadata is queried; no inference requests are sent. The tool capability configuration in the general model list remains an overridable compatibility default and cannot serve as proof of passed execution.

Static resources for the embedded WebUI do not require login, but business APIs continue to require authentication; unknown `/api/` paths do not fall back to the web page. Development builds without the `webui` build tag only provide a build hint page.

### Model Pricing

Model Pricing Supplement: `GET /api/models` returns effective `Pricing`, and indicates `Source` (`builtin` / `manual`), available automatic prices `Automatic`, `RuleID`, `Version`, `Period` (`peak` / `off_peak`), `SourceURL`, and `CalendarKnown` via `PricingInfo`. The `Model.Pricing=null` of `configure-model` indicates the use of matching built-in rules; if no rules exist, the price remains unknown. Saving a Provider can pass `PricingProvider` (`deepseek` / `openai` / `ollama` / `custom`), and connection details return this field; if empty, it is identified based on the official address, and `custom` disables automatic rules. Currently, only DeepSeek is built-in, and it only matches official addresses and model identifiers explicitly listed in the configuration.

## Assistants and Plans

### Assistant Avatar

The assistant object adds an optional `Avatar` string. It can be omitted when creating an assistant; when updating, omitting it or passing `null` retains the original value, and passing an empty string clears it. If not set, the UI uses the first character of the name, and cloning inherits the original avatar.

Supports preset IDs (`preset:` prefix followed by `robot`, `cat`, `fox`, `bear`, `rabbit`, `panda`, `owl`, `frog`, `penguin`, `whale`, `sprout`, `star`), or Base64 data URLs for PNG/JPEG. Avatar uploads containing a full data URL must not exceed 128 KiB, with width and height not exceeding 256 px; the Runtime decodes, validates, and re-encodes, removing metadata. External URLs, SVG, invalid images, and unknown presets are rejected. Thumbnails are saved and backed up with the assistant identity, without creating separate attachments.

The WebUI supports uploading PNG, JPEG, and WebP files within 10 MB / 40 million pixels. The browser centers, crops, and compresses the image to a 256 × 256 JPEG before submission; the original image is not uploaded. The 12 presets are original SVGs for this project, with source code located at `packages/ui/src/components/AssistantAvatar.tsx`, and no license dependencies on third-party avatar assets or libraries.

### Assistant Self-Profile Update Tool

The built-in `profile_update` accepts optional `name`, `status`, `avatar`, and required `expected_version`; at least a name, status, or avatar must be provided. The `type` of the avatar object is `preset` (`id` uses the aforementioned preset directory), `file` (`path`, optional `sandbox_id` / `resource_id`, consistent with `file_read` path rules), or `clear`. Omitted fields remain unchanged. `null`, unknown parameters, external URLs, and Base64 are rejected. The tool object is determined by the execution identity and does not accept `assistant_id`.

`assistant.Service.UpdateSelfProfile` validates the current assistant identity and checks the version within a transaction, writing only to `Name` / `Status` / `Avatar`, while saving an idempotent receipt and `assistant.profile` real-time events. Image reading supports local and remote sandboxes, using existing read authorization, boundary path, and symbolic link checks; PNG/JPEG/WebP are limited to 10 MiB / 40 million pixels. The Runtime centers, crops, scales, and re-encodes to 256 × 256 JPEG, then performs existing avatar validation. Processing images does not require the model to have visual input capabilities.

The success receipt is `{Changed, Profile: {Name, Status, Avatar: {Type, ID?, Digest?}, Version}}`; avatar summary types are `initial` / `preset` / `image`, without returning image payloads. Identical content does not increment the version or emit duplicate events. Version conflicts return `conflict`, `side_effects_possible=false`, and `error.current_profile`, without automatic retries. Tool success receipts are replayed via the existing operation chain.

The owner update interface remains owner-only. Names for hiring, cloning, and updating are uniformly trimmed of leading and trailing whitespace, limited to a maximum of 256 Unicode characters, rejecting empty names and control characters. Manual user changes to names or avatars also publish `assistant.profile`. The WebUI refreshes profiles upon receiving events; unedited forms sync new values, while forms with drafts retain the original version, reopening for verification after submission conflicts. `Status` is persisted and returned with the assistant, and can only be set by the assistant tool. Hire and owner update requests reject this field. Status is trimmed, allows up to 256 Unicode characters without control characters, and is cleared with an empty string; omission preserves it.

### Assistant Wait Reasons

The `WaitingReason` of `GET /api/assistants/{id}/status` returns stable reason codes: `model_not_configured`, `model_unavailable`, `model_not_authorized`, `model_pricing_missing`, `budget_exceeded`, `model_authentication_failed`, `model_rate_limited`, `model_service_unavailable`, `network_access_denied`, `context_limit`, `unsupported_attachment`, `step_limit`, etc. Unsubdivided errors retain the original error code, without returning exception text, vendor responses, or credentials. Existing HTTP status and scheduling error classifications are still determined by the code.

The WebUI displays corresponding descriptions and action entries based on the reason: model permissions, model selection, model pricing, provider connection, and current assistant budget settings. Existing generalized `failed_precondition` status can explicitly display missing prices, unavailable models, or unset assistant budgets based on the current model catalog; it only reads configuration, does not replay messages, and does not automatically call models.

### Assistant List Activity Summary

`GET /api/assistants/activity` returns a paginated summary of active assistants for the current account, using generic `limit` (default 50, maximum 200) and `cursor`, returning `Items`, `NextCursor`. Each item includes `AssistantID`, `ConversationID`, `Dreaming`, and nullable `Preview: { Kind, Text, CreatedAt }`. `CreatedAt` is the creation time of the message, used by the client to sort assistants by latest message in descending order within pinned and unpinned groups respectively; when there are no messages, `Preview` is `null`. Text merges whitespace and retains the first 80 Unicode characters, with excess indicated by an ellipsis; cards take the title or summary, and empty text displays placeholder copy by type on the client side. The summary uses a fixed-size character buffer, stopping traversal after confirming overflow, without constructing a normalized copy of the entire message; trailing whitespace is not counted as additional content. Dismissed assistants are not included in the results. Each page batch reads the latest message references and corresponding messages for authorized sessions, without reading history lists individually.

This interface follows the account ownership checks of the assistant and session services. Execution status is batch-projected based on explicitly authorized assistant keys on the page, reading only fields required for dreaming judgment, checking the runtime environment once per page; it does not load context, urgent, or interruption details. Messages only query the latest one per session, without traversing history. The full status of the current chat still uses `/api/assistants/{id}/status`.

### Assistant Capability Information

Assistant capability information: `GET /api/assistants/{id}/capabilities` allows access only to owners with corresponding assistant read permissions, does not depend on the development workbench toggle, and does not create an assistant execution identity. It returns `Tools` installed by the Runtime (definitions, parameters, required operations, current `Available` / `Reason`, matching storage `ResourceIDs`) and `Skills`. Conditions for storage tools are calculated based on resource type, status, valid authorization, read-only scope, and expiration time; specific resources and paths are re-authorized at execution time, and capability display cannot be treated as invocation permission. Currently supports discovery and on-demand context loading for assistant workspace `.agents/skills/*/SKILL.md`, `Skills.Supported=true`; `Items` returns Skills discoverable within the workspace. Skills are workflow guidelines, not additional executors, and do not grant permissions.

### Assistant Budget

Assistant budget: Hiring and updating assistants require passing `Budget: {"Limit":{"Currency":"USD","Micros":1000000},"Period":"daily"}`, with cycles supporting only `daily` / `weekly`. Quotas are non-negative, with zero explicitly allowing only free calls; there is no global default quota. Tasks do not accept `Budget`, and settings do not accept `DefaultTaskBudget`. Cloning inherits quotas and cycles, with each assistant accounting independently, and rehiring retains the original assistant ledger.

`GET /api/usage?assistant_id=<id>&current_period=true` returns `Limit`, `Spent`, `Reserved`, `PeriodStart`, `PeriodEnd`, and historical cost unknown operations for the current cycle. Current cycle queries must specify the assistant and cannot pass `from` / `until` simultaneously; historical costs can still be queried when the current cycle is not specified. Resets are calculated at midnight daily / midnight Monday based on the timezone in settings, including daylight saving time changes. Calls still executing across cycles continue to occupy reserved quotas; after local calls exit, unknowns no longer occupy reservations, but subsequent trusted receipt settlements are still allowed. Settlements are counted in the original cycle of the call; historical records are not cleared, and adjusting limits or cycles does not clear the ledger. When no valid assistant budget is set, `budget_exceeded` and `assistant_budget_missing` reasons are returned.

### Upcoming Scheduled Events

`GET /api/plans` loads todos and schedules within the account in the same read transaction, batch reading visible schedules and their directly dependent associated records and execution history. Batches are limited to the account and schedule references, not reading executions of deleted or unrelated schedules; invisible dependencies are only used for readiness judgment and are not added to the return list.

`GET /api/plans/events?from=<RFC3339>&limit=50&scope=all&kind=all&state=all&cursor=…` returns `{Items: [{Key, At, Plan}], NextCursor}`. `limit` is 1–200, default 50; `scope` is `all`, `me`, or assistant ID; `kind` is `all|item|event|automatic`; `state` is `all|open|done`. `from` is required and includes timezone offset.

Each item represents an occurrence, stably sorted by occurrence time and schedule reference. Event projection retains only the earliest `limit + 1` candidates, then generates the current page and subsequent cursors; schedule source reading still grows with the number of account schedules. Cursors are bound to the owner, start point, and filter conditions, and cannot be used across queries. Different schedules at the same time are not missed due to pagination; each occurrence of a recurring schedule is not collapsed into a series. Projection reuses generic recurrence rules, including sparse arrangements like cross-year, not limited by calendar month date ranges, and does not advance scheduling cursors, create tasks, or trigger execution. Completed filters only check execution records for the corresponding occurrence and cannot mark the entire series as completed. The original `GET /api/plans?from=…&to=…` continues to be used for calendars and schedule details.

### Manual Context Compression

`POST /api/assistants/{id}/compress` accepts `{ "Mutation": { "IdempotencyKey": "..." } }`, requiring owner identity and assistant write permissions. It returns `{ "EventID": "...", "State": "queued" }` indicating the request has been accepted, not that compression is complete; the same idempotency key returns the original receipt, and overlapping requests with different keys are merged. It fixedly creates `after_step` events, with no client delivery method parameters. Actual processing follows model authorization, budget, and compression validation; persistently disabled/paused assistants reject requests.

The status interface adds optional `Compression: { EventID, State, Reason? }`. It only displays "Compressing context" when `State=running`, displaying the original lifecycle status after completion; failure results provide reason codes. It refreshes via existing `assistant.state` SSE, without exposing summaries or private context.

## Authorization

### Authorization Request Approval

- `GET /api/permission-requests/{id}`: The owner reads a single real request for chat cards and approval forms, validating object ownership.
- `GET /api/permission-requests?assistant_id=<id>`: The owner reads up to the most recent 200 requests for that assistant, prioritizing pending approvals, returning an array.
- `POST /api/permission-requests/resolve`: `Mutation` (idempotency key and expected version), `RequestID`, `Decision: approve|reject`. Approval also requires `ResourceID`, `Actions`, `Scope`, and optional `ExpiresAt`; standard compute, network, and managed workspace authorizations additionally require `Constraints`. For combined approval fields, see [Resource Quotas](../security/resource-quotas.md#data-and-approvals). Rejections must not carry authorization fields. Owners can modify the pre-filled resources, actions, scopes, and authorization durations, but the final values are still validated by the Grants service for resource ownership, supported actions, scopes, and deployment limits.
- The status is `pending / approved / rejected`, recording `GrantID`, a snapshot of the actually approved `Granted`, creation and decision timestamps, and the version. The request itself does not equal authorization; both approval and rejection generate decision contexts and recovery events, and old stop boundaries and task terminal states are still respected.
- The SSE `permission.request` contains only assistant references; clients re-read the request and permission lists. Request creation also publishes an assistant chat card via the standard `message.created`, and the request and card are persisted together.

For tool parameters, waiting, and recovery semantics, see [Authorization Requests](file-tools.md#authorization-requests).

### Permission Editing

Permission editing: `POST /api/grants/update` accepts `GrantID`, `Actions`, `Scope`, `ExpiresAt`, and `Mutation` with `ExpectedVersion`. The authorization subject and resource are immutable; the model scope or storage path within that resource can be modified. The operation requires owner management permissions, validates the resource's supported actions and new scope, atomically updates the version, and writes an audit log; revoked authorizations cannot be restored.

### Full Access Permission Fields

The WebUI uses a display hierarchy of "Resource Type: Sandbox → Select Sandbox → Permission Type: Storage / Compute / Network / Full Access"; Full Access is not embedded in the compute quota form. The API still expresses independent authorizations using the node compute resource ID and `Scope.FullAccess`; the frontend resets unrelated paths, actions, and quotas when switching targets, and the server continues to validate the final request.

- Sandbox configuration: `sandbox.allow_full_access` (default false), no node mode field. `GET /api/sandbox` and the Info in the sandbox list provide `Capabilities.FullAccess`; the remaining five capabilities still only describe standard isolation paths. The standard member list hides Full Access capabilities that cannot be granted when delegating by group.
- Create/Update Authorization and Approval: `Scope.FullAccess: true` is used only for `Actions: ["execute"]` of compute resources; it cannot carry `Constraints`, `Temporary`, paths, models, or read-only scopes. Standard compute authorizations still require quotas; the two can exist independently.
- Model `permission_request` and Combined Sub-requests: `full_access: true`, must not include quotas; expiration time is still required. `permission_list` retains metadata for Full Access that is requestable and already authorized.
- Model Command/File Invocation: `full_access: true` selects the direct path, re-validating authorization each time; if omitted, the standard path is used. Shell tasks return `FullAccess`; the concurrency limit of ten tasks applies only to standard isolated tasks.
- User Group Sandbox Reference: `Resources[].AllowFullAccess`, default false, can only be set by `Kind: "sandbox"`. The admin directory returns `FullAccessAvailable`; revoking group delegation simultaneously invalidates existing authorizations.
- Remote protocol version 7, URL remains `/v2/sandbox/`, trusted requests carry `FullAccess`. Both ends check deployment capabilities and authorization leases separately, not relying on old caches for automatic privilege escalation.

### GPU in Compute Permissions

`SandboxInfo.GPUs` returns a `{ID, Name, Vendor, MemoryBytes, Available, Reason}` array; `AuthorizationCeilings.GPUDeviceIDs` is the device scope allowed by the deployment and user group. `Constraints.compute.gpu` is optional, with the structure `{"device_ids":["GPU-00000000-0000-0000-0000-000000000001"]}`; default or empty list means no GPU permissions. Base/temporary authorizations and `permission_request` use the same field. `shell_execute` does not add new GPU parameters, injecting them based on the effective authorization of `compute_id`; task details `GPUDeviceIDs` record the startup device. The remote sandbox protocol version is 7.

## Agent Shell Sandbox

`GET /api/sandbox` requires account identity and read permissions, returning `Enabled`, `Capabilities`; only administrators can see `Networks` and `Directories` that the deployment allows connecting to, while standard members receive an empty directory list; it does not return helper, rootfs, cgroup, or private data directories, nor does it expose HTTP interfaces for modifying deployment isolation configurations or directly creating commands; read/write and stop operations for existing Shell tasks are described below.

When enabled, `POST /api/resources` supports `Kind: "compute", Provider: "local-sandbox", Binding: {}` and `Kind: "network", Provider: "sandbox-network", Binding: {"SandboxTarget": "部署目标名"}`. They support `execute` authorization; targets cannot be added by the WebUI itself. Storage still uses `local-directory`, defaulting to direct mounting based on storage resources and assistant authorization; deployers can enable additional directory whitelists via `sandbox.restrict_directories`.

The built-in `permission_list` is an authorization and requestable permission query independent of the Shell switch, returning `Resources` (valid authorization IDs, actions, scopes, and validity periods in `Grants`, paths or model scopes within the resource, availability and reasons for each action, stable `ShellPath`), `Requestable` (security descriptions of requestable resources, not authorizations), and `BuiltInStorage` (assistant workspaces and temporary directories for this execution provided by the Runtime). Network resources provide `Network` in `Resources` and `Requestable`: current target addresses, ports, protocols, and exclusions, system limits, loopback access methods, and execution network limits; when offline or source is lost, `Known=false`, without guessing scopes by name. The resource path `/` is not the host root directory. File tools and Shell share storage authorizations and deployment directory limits; Shell still requires additional compute authorization; the query is not an authorization credential. `shell_execute` uses the existing Agent tool invocation chain and operation receipts, not directly publishing chat messages. The assistant info page displays tool status via the original capabilities interface. For deployment conditions and semantics, see [Sandbox Usage Guide](../../../docs/en/sandbox/linux.md).

At Runtime startup, the command environment is automatically prepared and probed, and local compute resources are automatically registered, but assistants are not automatically authorized. `Capabilities.DiskProtection` is currently `file_limit_and_free_space_guard`, indicating a single-file hard limit and disk low-watermark protection, not a hard quota for total directory capacity.

Appearance preferences: `Settings.Theme` maintains `system` / `light` / `dark`; `Settings.Palette` supports `forest` / `ocean` / `iris` / `sand` / `rose` / `monochrome`. Old records default to `forest` when read; old clients omitting or passing empty color fields retain existing selections; unknown values return `invalid_argument`. Updates still use settings version checks, without modifying other business preferences.

### Shell Task Management

- `GET /api/assistants/{id}/shell-tasks`: The current owner reads all running tasks and the most recent 50 history records for the assistant, returning `Items` and `Limit: 10`. The mode is `foreground/background/tty`, and the status is `running/completed/failed/stopped/interrupted`.
- `POST /api/assistants/{id}/shell-tasks`: `action` is `read/write/interrupt/stop/stop_all/resize/list`. Single-task operations use `task_id`; reads can pass `cursor`, `stream`, returning `Output`, `NextCursor`, `Path`; ending a task also returns `Finished` (output capture ended) and `OutputComplete` (whether capture is complete). `Finished=true` and after the current stream is read empty, reading can stop; `OutputComplete=false` caused by truncation must not be treated as the task still executing. Reading is a read-only operation; clients should not trigger business data invalidation based on the POST method. TTY input passes `data` (max 8 KiB), resize passes `rows/cols` (1–500). Owner sessions still require CSRF; cross-owner or cross-assistant access is denied.
- The first creation of a Shell task and status changes append `shell.tasks` SSE in the same transaction, containing only the owning assistant reference, not commands or output; clients refresh the corresponding task list based on this, retaining low-frequency calibration.
- The model uses the `shell_task` tool with the same semantics, which can only manage its own tasks. `shell_execute` adds `tty/background/timeout_seconds`, with foreground waiting up to 60 seconds. Optional `forward_ports` declares up to 4 HTTP/WS service ports. The `Task.Forwards` in the creation receipt and `Forwards` in the task list return `Protocol/Port/Address/URL/State`, with status `waiting/ready/closed`; addresses are uniformly located on the Runtime machine, and URLs require the current Molr session. The creation receipt includes the task and notification description; completion and running reminders use `shell_task_finished` / `shell_tasks_running` events, with structured information in `Details`, not faking chat messages.


### Browser tasks

`GET /api/assistants/{id}/browser` returns availability, the active task and cached `TabCount`. `ControllerClientID` is returned only to the controller’s login session; clients must also match their current page’s client ID. `POST` accepts start/status/close, browser actions, takeover and release. Start requires `compute_id`; control requests require `client_id` and current `generation`. User input additionally supplies increasing `sequence`; stale generation, another controller or replay returns conflict. The task has `Kind: browser` and public Browser control/generation/NeedsSnapshot fields; controller credentials are never serialized. A normal close flushes profile state before cancellation.

`GET /api/assistants/{id}/browser/stream?task_id=…` is authenticated SSE containing frame/status/tabs/url messages. It rechecks owner/assistant scope, watches session validity and ends on task cancellation. This endpoint is not subject to the regular 30-second JSON request deadline. POST uses the normal session and CSRF checks. There is no public agent-browser WS or command endpoint.

## Path Memory Interface

Memory has no dedicated ID, located by `Scope: assistant|global`, `AssistantID`, and relative `Path` under the current account. The AssistantID for global scope is empty. Path is a semantic file path under `MEMORY.md` or `.agents/memory/`, which must be provided for creation; same names do not overwrite. The file `Version` is the full byte SHA-256; updates and deletions use `Mutation.ExpectedVersion`, and old versions return 409.

`GET /api/memories` supports `scope`, `assistant_id`, `state`, `q`, and pagination parameters. Results are sorted by scope, assistant ID, and path; `NextCursor` binds the account and filter conditions, maintaining these conditions during pagination while allowing page size adjustment. The cursor records the logical file position; external edits to files take effect immediately, and files added before the read position require viewing from the first page again.

`GET /api/memories/file?scope=…&assistant_id=…&path=…` reads files, and `GET /api/memories/directory` browses directories. `POST /api/memories`, `/update`, and `/delete` manage files; deleting `Forget: true` explicitly enables forgetting. `/mkdir` creates directories, `/move` accepts ToPath, Directory, and the original version, and `/rmdir` only deletes empty directories. All write requests carry a Mutation. The old `/api/memories/{id}` does not exist. See [File Memory](../runtime/dream-memory.md) for detailed constraints.

Memory hits from global search return references via `Memory: {Scope, AssistantID, Path}`. Setting cleanup passes an array of memory files via `memory_files`; other business objects continue to use object_ids. Dream candidates use Action, Path, ExpectedVersion, and optional Replaces file references; returned records use MemoryFiles and MemoryVersions to record paths and committed versions.

## WeChat Chat Entry

The following interfaces require the current account to have read/write permissions for the target assistant; POST requests use existing authentication, CSRF, and Origin checks. Status responses do not return WeChat IDs, tokens, reply credentials, or cursors. After channel binding, standard `POST /api/conversations/messages` returns `failed_precondition`; read-only records and other management interfaces retain their original authorization.

| Method and Path | Request | Return |
| --- | --- | --- |
| `GET /api/assistants/{id}/weixin` | Assistant ID | `Channel` (`webui` / `weixin`), `State`, connection and last inbound time, `LastError`, last 30 `Deliveries` |
| `POST /api/weixin/login` | `AssistantID` | `ID`, `Status`, `QRCode` (content requiring local encoding into a QR code), `ExpiresAt` |
| `POST /api/weixin/poll` | `AssistantID`, login `ID`, optional `VerifyCode` | Login status; waiting requests last up to approximately 25 seconds, with the frontend continuing to poll |
| `POST /api/weixin/cancel` | `AssistantID`, login `ID` | `null`; only cancels the QR code scan, does not delete existing bindings |
| `POST /api/weixin/disconnect` | `AssistantID` | `null`; deletes the binding, cancels pending replies, and restores WebUI input |

Login status includes `wait`, `scaned`, `need_verifycode`, `verify_code_blocked`, `expired`, `conflict`, `confirmed`, and `already_connected`; connected responses do not require a QR code, and existing bindings and queues are preserved. Redirects are validated and handled by the backend. Binding status includes `connected`, `reconnecting`, `login_required`, and `paused`. Delivery status includes `pending`, `sending`, `accepted`, `waiting`, `failed`, `unknown`, and `canceled`. `accepted` does not indicate that the user has read the message. Text segments and each attachment occupy a separate delivery record; `Error=attachment_failed` indicates file read or upload failure before message sending, and remains `unknown` if the final sending interface cannot confirm. Changes in binding and delivery trigger `weixin.changed` events to cause the interface to re-read the status.

## System Reminder Status

- `GET /api/notifications`: Current account notifications, supporting `filter=unread|pending|active|all`, `assistant_id`, `kind`, `limit`, `cursor`. Both `pending`/`active` indicate not ignored and not resolved; `unread` additionally requires unread. Returns `Items`, `NextCursor`, current account global `UnreadCount`, `PendingCount`. The cursor binds the account and query conditions.
- `GET /api/notifications/{id}`: Reads the specified notification for the current account.
- `POST /api/notifications/update`: `{ "IDs": ["通知ID"], "Action": "read" | "dismiss" }`, up to 100 IDs at a time, committed as a single transaction. Duplicate confirmations have no additional effect; ignoring also records as read, without changing the business source status. The interface follows session, Origin, and CSRF validation, and cannot create notifications or mark them as resolved on the client side.

Records include `Kind`, `Code`, `Source`, `AssistantID`, `Label`, `Severity`, creation/update time, `ReadAt`, `DismissedAt`, `ResolvedAt`, `Version`. Source changes are notified for refresh via `notification.changed` SSE events; notifications missed during disconnection can be recovered via the list.

### Notification References in Session Events

Session SSE events add an optional `ConversationID`, including `message.created`, `message.feedback`, `message.accepted`, `message.read`, and `conversation.read`. It is a safe object reference and does not carry message body; old events may lack this field. Clients call the existing `GET /api/conversations/{id}/messages?message_id=…` based on the session and `Object.ID`; read permissions are still validated by the Runtime. Notifications only consume new assistant messages and cannot treat arbitrary events or query refreshes as new messages.

## Debug Workbench

Debug Workbench: `GET /api/status` adds a boolean field `workbench_available`; `Settings.WorkbenchVisible` only controls the display of the user interface entry and does not affect collection or debug interface permissions. Collection is controlled only by deployment configuration `debug.enabled`; if deployment is not enabled, saving the display value `true` is rejected. See [Development Workbench](../runtime/workbench.md) for dedicated debug interfaces and record boundaries; checkpoints and full requests do not enter standard status/chat SSE.

Retention Policy: `GET /api/debug/retention` requires the owner and `debug.read`, returning `Policy` (`Version`, `MaxRuns`) and the current account's `Runs`, unaffected by the personal display toggle. `GET /api/admin/debug/retention` returns the effective policy, `Configured` (`MaxRuns` is an integer or null), and `Defaults`; `POST` requests are `{Policy: {MaxRuns}, Mutation: {IdempotencyKey, ExpectedVersion}}`, both requiring administrator and `admin` scope. Count range is 1–10000; null or omitted restores the deployment default. Saving persists and notifies clients, cleaning up the earliest completed executions by count at subsequent writes and execution ends; running executions are retained until completion, and dreaming trajectories also participate in count calculation. Cleanup is not based on time or total capacity, and no longer generates `capacity_reached`, though individual payloads may still be marked `too_large`. Details use independent references from [Context Storage](../runtime/context-storage.md); historical deletion cannot break current execution state.

Context Reset: `POST /api/debug/assistants/{id}/context/reset`, request is `{ "Mutation": { "IdempotencyKey": "..." }, "ExpectedSequence": 0 }`. `ExpectedSequence` uses the sequence number of the latest checkpoint, or 0 if no checkpoint exists. Requires owner identity, assistant write permission, and workbench enabled status; tokens also require `debug.read` and `debug.write`. Reset cancels old executions, clears private context, and increments the sequence number, preserving chat, memory, permissions, and ledger, without automatically requesting the model. Version conflicts return 409; resubmitting the same idempotency key does not reset again.

## Version and Release Updates

`GET /api/status`'s `build` returns the Runtime's `version`, `commit`, `built_at`, `os`, `arch`, `embedded_webui`.

- `GET /api/updates`: Reads the instance shared snapshot after login, without accessing the release source. Includes `current`, `latest`, `comparison`, `check`, `auto_check`, `source_configured`, `supported`. `comparison` is `unknown/current/available/ahead/uncomparable`; `check` records checking, last attempt, last success, next allowed manual check time, and fixed error codes respectively.
- `POST /api/updates/check`: Requests `{ "automatic": false }` for manual check; `true` is used for automatic check on the About page. Follows CSRF; automatic check interval is at least five minutes, manual at least one minute, and merges in-progress requests. Release source network or manifest failures are saved in the snapshot and do not clear the success cache.
- `GET /api/updates/installation`: Only administrators read installation records and release sources required for the upgrade command; missing matching records return `null`. Does not perform installation.

The check service runs within the Runtime lifecycle; the memory cache is re-established after restart. Requests are limited to ten seconds, and manifests to a maximum of 64 KiB. Public HTTPS, explicit private network addresses, and DNS validation during dialing reuse platform network boundaries, prohibit redirects, and do not carry user credentials. See the deployment guide for version and release metadata; interfaces do not accept arbitrary URLs.

## Validation

API contract tests use a real HTTP server, Auth, CSRF, persistent rate limiting, Realtime, and SQLite; other modules also use actual implementations. Only the vendor catalog and composition layer ownership dependencies are controlled. Tests cover login cookies and secret DTOs, two-factor authentication consumption, permission capability isolation, trusted proxies, SSE revocation, service start/stop, and fail-closed behavior.
