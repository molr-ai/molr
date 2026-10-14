# Plugin Mechanism (Plugin API 3)

The Runtime remains a Go monolith. Plugins extend the product via immutable manifests, isolated HTML, and declarative HTTP operations; they do not load Go dynamic libraries or execute installation scripts. For user entry points, see [Plugin Usage](../../user/guides/plugins-and-skills.md). Search plugins or acceptance examples are not pre-installed.

## Packages and Protocols

The development package is a standard directory, distributed as a ZIP. The root directory is `plugin.json`, and the remaining files are accompanying text files. `manifestVersion: 1`, `pluginApi: 3`; old protocols are not executed and are not automatically migrated. The page communication contract is independent and still uses `apiVersion: 2`.

Import limits are 8 MiB, 256 accompanying files, 1 MiB per file, and 24 contributions. ZIPs reject links, duplicate paths, absolute paths, out-of-bounds paths, and content exceeding expansion limits. Files within the package are UTF-8 text. IDs use reverse domain notation, with `molr.*` reserved; versions consist of three numeric segments without leading zeros. Package storage is still normalized to `manifest` and `files`, with SHA-256 fixing the content; the same ID/version cannot be overwritten; the JSON API can directly submit the same canonical structure.

```json
{
  "manifestVersion": 1, "pluginApi": 3,
  "id": "example.notes", "name": "Notes", "description": "Notes page", "version": "1.0.0",
  "contributes": {"themes":[],"tools":[],"pages":[{"id":"notes","title":"Notes","entry":"notes.html","capabilities":[]}]}
}
```

The packaging command is `node scripts/pack-plugin.mjs <目录> <输出.zip>`, and the output must be placed outside the source directory.

| Extension | Contract |
| --- | --- |
| themes | `id`, `name`, `pages`, `capabilities`; can replace assistants / plans / resources / workbench, but cannot replace settings or login; pages are HTML within the package |
| pages | `id`, `title`, `entry`, `capabilities`; accessed via `/#/extensions/<plugin-id>/<page-id>` |
| tools | `id`, `description`, `execution`, `inputSchema`, `capabilities`; only HTTP tools are supported |
| settings | Mapping from top-level field names to Setting; maximum 16 items |
| settingsPage | Optional personal settings Page; once declared, it serves directly as the configuration entry point, and the host form is not displayed simultaneously |
| adminSettings / adminSettingsPage | Optional shared admin settings fields (maximum 16 items) and custom admin pages; only administrators can read and write |
| providers | Top-level HTTP provider list, maximum 8 items; interpreted by the fixed host executor |

Tool inputs still use a closed object Schema, with a maximum of 16 string / number / integer / boolean properties, supporting required and string enums, `additionalProperties: false`. Parameters are limited to 16 KiB, strings to 8 KiB, and integers must be JavaScript safe integers.

## Settings, Connections, and Identity

Installation, access status, and configuration are stored separately:

- `plugins.installations` / `plugins.packages`: Shared installation directory; administrators manage versions, permissions, enable/disable, and assistant bindings.
- `plugins.connections`: Shared service connections, mapped to `connection` type resources; managed by administrators, while regular users obtain usage rights through user group resource scopes. A provider is a protocol definition, while a connection is an account instance.
- `plugins.settings`: Personal configurations use the real `OwnerID + PluginID`; administrator configurations use the installation directory OwnerID and `admin:PluginID`, shared and readable/writable only by administrators. The connection ID derives the provider.
- `plugins.preferences`: Enable/disable preferences and CAS version for each real OwnerID; enabled by default, effective only when globally enabled and authorized by the user group, independent of the settings Schema.
- User group `plugin` resource references: ID is `plugins.Key(pluginID)`, controlling access to plugin pages, themes, configurations, and tools; tools and connections have their own respective scopes and quotas. Plugin references have no quota.

The installation directory retains a maximum of 32 undeleted plugins, and each plugin retains a maximum of 32 undeleted service connections; disabled items still count towards the limit. Removal frees up slots, while history, published versions, and call records are retained; list queries exclude deleted records in the database. Reinstallation also checks remaining slots; updates and idempotent retries for existing items do not repeatedly occupy slots.

Setting fields: `type`, `title`, optional `description`, supporting string / boolean / number / integer / connection. Regular fields must declare `default`; strings support `enum`, numbers support `minimum` / `maximum`. Connection fields declare `providers` and optional `requiredOperations`, with no default connection allowed. Users can explicitly leave fields empty; calls return a requirement for configuration, and another connection cannot be automatically selected.

Reading settings returns `PluginID, Digest, Version, Values, NeedsReview`. Saving is a complete value object; omitting regular fields applies default values; omitting connection fields clears the selection. The server validates fields, values, connection permissions, and versions. Configuration is bound to the package digest; new packages retain representable old values but set `NeedsReview`, taking effect after the user reviews and saves. Concurrent saves use `ExpectedVersion`; changes in personal preferences do not cancel the entire plugin.

The unified entry point is `/#/extensions`, displaying installation, removal, group authorization, global/personal enable/disable, and optional configuration based on role. Personal configuration path is `/#/extensions/<id>/@settings`, administrator configuration path is `/#/extensions/<id>/@admin`; reserved character `@` avoids conflicts with page contribution IDs. The Schema and page for configuration can both be omitted; if not declared, no entry point is displayed, and the personal settings API returns NotFound. Custom pages take precedence; otherwise, the host generates a form. Administrator configuration additionally displays service connections corresponding to provider declarations. Connection descriptions obtained by plugin pages do not contain API Keys or CredentialIDs. Credential input and two-factor authentication are handled by the host. The first version does not support regular members managing Keys themselves.

## CLI and Skills Use Standard Workspaces

CLI, dependencies, scripts, and Skills are all work files managed by the assistant itself, not plugin contributions. Users provide sources and tasks in chat, and the assistant completes installation and usage via existing file tools, `shell_execute`, and `permission_request`. Programs and dependencies are installed within `/work`; Skills are placed in `/work/.agents/skills/<name>/SKILL.md`, reusing existing directory discovery and `file_read` on-demand loading. See [Context Assembly](../runtime/agent-context.md) for details.

Compute, network, storage quotas, and permissions follow standard tool rules; login status and upgrade methods are determined by the CLI itself. Plugin services do not create workspace copies, installation records, state files, credential fields, or virtual file paths, and do not participate in CLI installation, updates, or deletions. Disabling or uninstalling plugins does not change standard workspace files or resource authorizations.

## HTTP Tools

```json
{
  "id": "lookup", "description": "Look up a value",
  "execution": { "type": "http", "connectionSetting": "backend", "operation": "lookup" },
  "capabilities": ["http.request"],
  "inputSchema": { "type": "object", "properties": { "query": { "type": "string" } }, "required": ["query"], "additionalProperties": false }
}
```

HTTP tools do not bind to ComputeID. When `execution.settingsScope` is omitted, the configuration field `backend` points to the current user's connection; when set to `"admin"`, it uses the administrator-configured connection and regular fields, allowing the plugin to declare no personal settings at all. HTTP requests fix the settings snapshot for the selected scope; all setting fields referenced by the operation must be declared within that scope. Every candidate provider declared by the tool must implement the same-named operation, with input contracts consistent with the tool, and result field type structures consistent. Specific paths and field mappings may differ. Switching connections within the same package version does not change the tool name; the tool name is still derived from the plugin ID, package digest, and contribution ID.

HTTP tools are constrained by global enablement, personal enablement, user group plugin authorization, tool authorization, member group tool scope/quota, connection group scope/quota, and total connection quota. Resource status and authorization are re-checked at usage time. Switching the current user's connection does not change configurations for other accounts.

## HTTP Provider Contract

Provider fields are `id, name, origin, auth, operations, test?`.

- `origin` is a fixed HTTPS origin within the package, containing no user info, path, query, or fragment; no runtime custom Base URL is provided.
- `auth.type` is `none`, `bearer`, or `header`; header mode additionally declares `auth.header`. Sensitive headers such as Host, Cookie, proxy authentication, and transport control are rejected. Keys can only be injected into authentication headers by the broker.
- `operations` has a maximum of 16 items, each containing `method, path, inputSchema, query?, body?, result, usage?, errorField?`. Method is GET or POST; path is fixed; GET has no body; each operation involves only one HTTP request.
- Each query/body field is `{"value": <JSON常量>}`, `{"argument":"参数名"}`, or `{"setting":"非连接设置名"}`, one of the three. Referenced parameters must be declared in the operation Schema. Constants can contain nested JSON. The host is responsible for encoding, performing no text interpolation or script evaluation. The final value of query must be a scalar.
- `test` can declare `{operation, arguments, settingsScope?}`, and settingsScope can also be set to `"admin"`; fixed parameters must pass through the Schema of that operation. Tests are initiated only when the user actively clicks, also using connection quotas; test idempotency keys reuse successful results and do not automatically resend unknown results.

Projection fields are `type, path?, default?, fields?, items?, maxItems?`. Path is a JSON Pointer; objects use fields to specify output fields; arrays use items and maxItems between 1–50; scalars support string / number / boolean. Maximum nesting depth is 4; runtime limits projection nodes and text volume. Missing fields require an explicit default, otherwise failure; type mismatches fail, without silently creating empty data.

Request body is limited to 64 KiB, full URL to 16 KiB, response reading to 1 MiB, and projection results to 16 KiB; single strings are limited to 8 KiB. Results are `{data, provider, connection_id, usage, duration_ms}`. Usage retains vendor units; unpriced does not mean free. When `errorField` is non-empty, it reads the business error pointer; non-empty/non-zero error values are converted to fixed errors, without exposing the service provider's raw error body.

The first version does not provide OAuth, dynamic URL fetching, request chains, loops, automatic pagination, multi-service automatic fallback, or vendor pricing engines. Plugins can use business URLs as parameters for fixed services, but the Runtime does not thereby gain permission to access arbitrary URLs.

## Credentials, Network, and Receipts

Connections use the existing credential vault to encrypt and store dedicated credentials; credential Provider identity is bound to `plugin-http:<connection-id>`. At usage, the real caller, the plugin and digest to which the connection belongs, connection status, credential ID, and origin are validated. Plugin packages, HTML, tool parameters, and model contexts do not contain plaintext Keys; server-side results mask the current Key. Credentials can only be sent to interfaces approved by administrators; the remote service itself remains within the trust boundary of that connection.

HTTP requests reuse `platform.Guard`: resolve, check, and directly connect to checked addresses during actual dialing; automatic redirects are prohibited, and environment proxies are not inherited. Public networks require HTTPS; private networks still require deployers to allow them in static network configurations, and plugins or pages cannot relax this themselves. Production connections do not require Shell network authorization and do not grant Shell new networking permissions.

The Agent executor saves `plugins.http-calls` within the existing operation admission transaction: plugin digest, logical tool, connection ID/version, settings version, non-sensitive configuration, parameters, and time. Connection cumulative call and concurrency checks, member connection quota checks, and receipt creation are completed within the same transaction. Each connection must have a cumulative call limit, 1–16 concurrency, and 1–60 second timeout. Tests count towards the same quota; increasing the limit does not clear the count.

Calls in execution use the fixed configuration. Duplicate delivery of successful operations returns the original receipt; failures or unknown results do not re-resolve the provider and send concurrent requests. If no explicit response is obtained after sending the request, it is marked as unknown; Runtime restarts change leftover prepared/running records to unknown. Unknown results retain the original cycle call count, and local request exit releases concurrency occupancy. Startup recovery executes after acquiring the Runtime exclusive lock and before starting worker threads. Administrators can mark unknown results as read in call records, but this is no longer a prerequisite for releasing concurrency, does not refund, does not retry, and does not claim external request success or failure.

Connection updates, disabling, or deletion cancel in-flight requests for that connection; plugin disabling, uninstallation, or version configuration cancel in-flight requests for that plugin; user disabling/group changes and user group updates re-validate in-flight permissions. Cancellation does not guarantee that external vendors stop processing or charging.

## Page Bridging

HTML must be self-contained and can package `@molr/sdk/plugin-host` inline. Iframes use opaque origin, `sandbox="allow-scripts"`, and a CSP prohibiting network access; the parent page `frame-src blob:` restricts navigation. HTML Blob URLs are created in the effect after component submission and released by the same effect upon document invalidation, error, retry, or unmount; after changing plugin references, old documents are no longer displayed or connected. Color scheme and language changes can reuse the current document URL while re-establishing the iframe's host context. Pages cannot access host Cookies, CSRF, Bearer, DOM, or generic HTTP proxies.

The handshake checks the frame window, opaque origin, API 2, and single MessageChannel. Maximum four concurrent requests. The host injects page identity in a fixed manner, and the backend re-validates the digest, contribution declaration, and permissions. The settings method for regular page/theme/settings only operates on the current user's configuration; the same-named method for admin-settings only operates on shared administrator configuration, and all calls first verify administrator identity. Plugins cannot switch scopes themselves.

| SDK method | Page capability |
| --- | --- |
| `plans.list()` | `plans.read` |
| `settings.get()` | `settings.read` |
| `settings.update({expectedVersion, values})` | `settings.write` |
| `connections.list()` | `connections.read` |
| `connections.test({connectionId, idempotencyKey})` | `connections.test` |

The host context provides only locale, theme, reducedMotion, and semantic style tokens. Temporary state is lost after frame reconstruction; persistent configuration must use settings. Page methods cannot select OwnerID, package digest, or other plugin identities.

## Lifecycle and HTTP Routing

Adding packages, granting global capabilities, global enable/disable, and uninstalling are managed by administrators; personal preferences are managed by the owning user. Group or individual revocation denies new page access and HTTP calls; in-flight HTTP calls re-validate authorization. Importing a new version only stages it; version switching retains applicable capabilities and bindings. Changes to the HTTP connection digest require review; personal/administrator configurations continue to bind to the package digest. Re-adding an uninstalled plugin does not restore global capability permissions or assistant bindings.

Deleting a connection deactivates resources and deletes dedicated credentials; historical receipts are retained. Regular members cannot call management interfaces. Credential usage, configuration changes, and manual confirmation of unknown results are audited; audits do not include request parameters, configuration values, or plaintext credentials.

| Route | Request and Purpose |
| --- | --- |
| GET /api/plugins | Visible installations; members see only items that are globally enabled and group-authorized; individually disabled items remain visible; does not return administrator tool bindings or administrator setting declarations; version catalog is visible only to administrators. Also returns CatalogID, ToolIDs, UserEnabled, UserVersion |
| POST /api/plugins/install | application/zip binary or JSON Package; saves the package only |
| POST /api/plugins/import-url | URL; public HTTPS ZIP, private networks, proxies, and redirects are prohibited |
| POST /api/plugins/preference | ID, Digest, Enabled, ExpectedVersion; changes only the caller's personal enable/disable state |
| POST /api/plugins/configure | ID, ExpectedVersion, Enabled, Grants, Bindings, Release? |
| POST /api/plugins/remove | ID, ExpectedVersion |
| POST /api/plugins/settings/read | ID, Digest, Scope? (admin only or omitted) |
| POST /api/plugins/settings/save | ID, Digest, Scope?, ExpectedVersion, Values |
| POST /api/plugins/connections/list | PluginID, Digest |
| POST /api/plugins/connections/save | PluginID, Digest, ID?, Name, ProviderID, Enabled, APIKey?, Verification?, Mutation, CallLimit, Concurrent, TimeoutSeconds |
| POST /api/plugins/connections/remove | PluginID, ID, Mutation, Verification? |
| POST /api/plugins/connections/test | PluginID, Digest, ID, Mutation |
| POST /api/plugins/connections/history | PluginID, ID; administrators read the last 100 secure call summaries; queries extract only display fields and provider usage, without loading parameters, settings, or full result bodies |
| POST /api/plugins/connections/acknowledge | ConnectionID, OwnerID, ID; administrators confirm unknown results |
| POST /api/plugins/ui | Fixed page reference, Method, and corresponding method parameters, constructed by the bridge |

The Mutation for creating connections and test requests includes IdempotencyKey; editing/deleting also includes ExpectedVersion. APIKey is write-only; connection view does not return CredentialID. Bindings manage only HTTP tools, including AssistantID and Tools. CLI and Skill do not use the plugin API. Connection user group quotas reuse Calls, Concurrent, Period, Timezone; monetary budgets or sandbox constraints are not configured.

## Implementation and Verification Entry Points

- `runtime/internal/plugins`: ZIP/HTTPS import, configuration, HTTP connections, and receipts.
- `agent/tools/executor.go`: HTTP admission with existing operation transactions and receipt reuse.
- `app/build.go` / `plugin_connections.go`: Credentials and network boundaries, group-level connection quotas.
- `packages/sdk/src/plugins.ts`: Package API 3 and HTTP types; `plugin-host.ts`: Independent page communication API 2.
- `apps/webui/src/plugins`: Managing connections, personal configuration, settings page iframe, call records.
- `preference_test.go`: Personal enable/disable, plugin group authorization, administrator setting isolation, shared connection execution without personal settings.
- `http_test.go`: Provider switch snapshots, authentication, personal isolation, CAS, quotas, cancellation, and manifest rejection.
- `apps/webui/tests/plugin-http.spec.ts`: Real TLS provider, pages, personal settings, member authorization, and Agent chain; fixtures are generated in tests, no search product plugin is published.

Browser-specific: `MOLR_E2E_SANDBOX_SERVICE=1 MOLR_E2E_PLUGIN_HTTP=1 npm run test:webui -- plugin-market.spec.ts plugin-http.spec.ts plugins.spec.ts plugin-workspace.spec.ts`. Model fixtures are deterministic protocol-driven, verifying the execution chain, and do not represent real model compliance rates or search quality.

## Writing Plugins

Develop in a standard directory, with `plugin.json` in the root, and package via `node scripts/pack-plugin.mjs <插件目录> <输出.zip>`. The current package protocol is Plugin API 3, and the page communication protocol independently uses API 2.

Files within the package are UTF-8 text; the package is limited to 8 MiB and 256 accompanying files, with each file limited to 1 MiB; links or out-of-bounds paths are not accepted. For package structure, page communication, and HTTP contracts, see [Plugin Architecture](plugins.md).
