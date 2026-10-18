# MCP tools

Administrators connect remote MCP services under Resources → Tools → MCP tools. Built-in and plugin tools have separate categories; hiding plugins does not hide MCP tools.

## Connect and grant access

1. Select Add MCP connection and enter a name and complete HTTPS Streamable HTTP endpoint, such as `https://mcp.example.com/mcp`.
2. Choose no authentication, Bearer Token or API Key. Supported API key headers are `X-API-Key`, `API-Key` and `X-Auth-Token`. Credentials are encrypted and never read back by the page.
3. Set lifetime calls, concurrency and timeout limits. Save, then select Test and refresh tools. This discovers tools without invoking a business tool.
4. Tools are grouped by connection. Open a tool to review its description and parameters, enable it in the detail footer, then grant execution to an assistant. The same tools are available in the assistant permission editor.
5. Members also need group access to both the connection and the tool. Each assistant still needs its own tool grant.

The Resources page is restricted to administrators. Members use permitted tools through assistant permissions and capabilities; connection endpoints, credentials and cross-member call history remain private.

Public endpoints require HTTPS. Private services additionally require deployment approval in `network.allowed_private_ips` and a TLS certificate trusted by Runtime. URLs cannot include credentials, query strings or fragments. Requests originate from Runtime, so the service must be reachable from its machine.

## Update and disable

Discovery saves a catalog snapshot. Opening pages and assembling model context do not contact the remote service. New tools start disabled. Changes to a tool's name, description or input schema produce a new identifier requiring enablement and new assistant grants; old grants no longer apply. Unchanged tools retain enablement and grants.

Endpoints and authentication types are immutable; create another connection to change services. Replacing a key disables existing tools until discovery and approval. Disabling or deleting a connection prevents new calls and cancels local requests; operations already accepted by the server may continue. Deletion removes the dedicated credential and retains call history.

## History and limits

Connection details show the latest 100 calls with tool, state, time and duration. Timeouts, disconnects and Runtime restarts can leave an unknown outcome; these calls are not automatically retried. Check the external system before starting another operation. Tool errors and calls that were never dispatched have separate states.

Lifetime and concurrency limits apply across all members. Groups can additionally set per-member connection and tool allowances. Admitted calls consume connection allowance; failures and unknown outcomes are not refunded. Local completion releases concurrency.

Text and structured JSON results are supported. Non-text content is explicitly omitted; resource links are not fetched. Stdio, local process startup, OAuth, Resources, Prompts and interactive requests are not supported. Parameters support nested objects and arrays, but not schema references or `x-mcp-header` mappings. Unsupported tools display a reason.

An instance supports up to 32 undeleted connections. Discovery is limited to 128 tools, 16 pages and a 512 KiB catalog per connection. Individual schemas are limited to 32 KiB, descriptions to 16 KiB, and arguments and results to 64 KiB each. A connection operation reads up to 4 MiB. Timeouts range from 1–60 seconds and concurrency from 1–16.

See [MCP integration](../../dev/extensions/mcp.md) for implementation and APIs.
