# MCP integration

`runtime/internal/mcpclient` uses the official Go MCP SDK as a trusted HTTPS Streamable HTTP tool adapter. It joins plugin HTTP tools through the `agent/tools` extension entry points and shares operation admission, tool quotas, receipts, errors and cancellation. See [MCP tools](../../admin/resources/mcp.md) for usage.

## Data and authorization

`mcp.connections` stores shared configuration, tool snapshots and versions. `mcp.mutations` stores configuration idempotency digests. `mcp.calls` stores receipts under the actual caller. Connections use `connection` resources; individual tools use `tool` resources. Tool metadata includes `Source` (builtin / plugin / mcp), with `ConnectionID` and `RemoteName` for MCP entries.

Connection IDs start with `mcpconn_`. Model names use `mcp_` plus a digest of the connection ID, remote name, description and schema. Unchanged definitions retain identity; changes disconnect old resources. New and changed tools start disabled. JSON Schema validation rejects references, parameter header mappings and oversized definitions without silently changing contracts.

Management APIs require an administrator. Catalogs are filtered by group connection and tool access. Execution additionally requires enabled tools, an available connection, an assistant execution grant for that tool resource and current quotas. The existing vault stores dedicated credentials with Provider `mcp:<connection-id>`; the broker checks the connection, credential and origin. Member views omit endpoint, authentication settings and credential IDs.

## Networking and lifecycle

Discovery and execution each create an independent SDK client/session to avoid sharing account state. The injected `platform.Guard` client checks DNS, private destinations and dialing. Requests are confined to the exact registered endpoint, redirects are disabled and environment proxies are not inherited. Only approved authentication headers receive credentials. SDK logs do not expose remote bodies. Discovery rejects catalogs containing the active credential; results recursively redact it.

Discovery is a bounded read with time, page and byte limits, separate from business call quotas. Only one refresh per connection runs at once, and snapshot writes check the version. Connection admission and operation admission share one transaction. `quota.CallTotals` aggregates plugin and MCP buckets using corresponding SQLite accounting indexes.

SDK reconnect retries and automatic interactive request handling are disabled. Complete business error responses use `tool_error`; successful responses use `succeeded`; dispatched calls without usable responses use `unknown`. Successful operation replay returns the existing receipt. Failed or unknown operations do not redispatch. Startup recovery marks prepared/running records unknown, retaining call counts and releasing concurrency.

Configuration changes, disablement and deletion cancel in-flight connection requests. Account, group and tool grant changes revalidate running authority. Results are checked again before release. Local cancellation cannot guarantee remote cancellation or rollback.

## HTTP API

| Route | Purpose |
| --- | --- |
| GET /api/mcp/connections | Visible connections and tool snapshots; administrators also receive configuration and usage |
| POST /api/mcp/connections/save | Administrator save: ID?, Name, Endpoint, Auth, Header, APIKey?, Verification?, Enabled, EnabledTools, CallLimit, Concurrent, TimeoutSeconds, Mutation |
| POST /api/mcp/connections/refresh | Administrator test and refresh: ID, Mutation |
| POST /api/mcp/connections/remove | Administrator deletion: ID, Mutation, Verification? |
| GET /api/mcp/connections/{id}/history | Administrator access to the latest 100 safe summaries, without loading arguments or result bodies |

Mutation includes IdempotencyKey; updates, refresh and deletion also require ExpectedVersion. Credential changes and deletion use existing sensitive-operation verification. Assistant grants use `/api/grants`; group scopes use existing administration APIs. Ordinary resource update/disconnect APIs cannot bypass the MCP connection lifecycle.

## Validation

`go test ./internal/mcpclient` uses a real TLS MCP fixture to cover discovery, schemas, execution, replay, definition changes, quotas, cancellation, timeouts and recovery. `MOLR_E2E_MCP=1 MOLR_E2E_SANDBOX_SERVICE=1 npm run test:webui -- mcp-tools.spec.ts tools-catalog.spec.ts` covers management UI, grants, member isolation and Agent invocation. E2E uses a deterministic model fixture; real-model acceptance is separate.
