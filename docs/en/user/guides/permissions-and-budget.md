# Permissions and Budgets

Administrators connect resources and assign available scopes via user groups; within this scope, you authorize actions, directories, models, and quotas for your assistants.

## Selecting Models

Set the default model for the current account in "Resources → Models → More (top right) → Default Model", or select "None". When hiring a new assistant, this model is pre-selected by priority; models specified individually for an assistant take precedence. Hiring only authorizes the selected model and does not override other models from the same provider.

For steps on how administrators connect models, see [Connecting and Managing Models](../../admin/resources/models.md).

## Browsing Tools

Browse tools by purpose such as chat, files, and planning in "Resources → Tools", or search by name, tool identifier, and feature description. Category counts update with search; clear filters if there are no matching results. Click a tool to open details, viewing the feature summary, execution impact, and required operations. The "Model Tool Description" in the details retains the full original text provided to the model, and you can switch to "Parameter Structure" to view input parameter definitions. The specific available scope is determined by assistant permissions, which can be viewed and configured in the assistant information.

## Full Access Permissions

The authorization form first requires selecting the resource type "Model / Tool / Sandbox". After selecting Sandbox, select the specific sandbox and permission type "Storage / Compute / Network / Full Access". Storage continues by selecting storage resources and paths, Network continues by selecting network scopes, and Compute requires filling in compute quotas. If deployment allows Full Access, you can select "Full Access". Regular user groups also require explicit administrator permission to grant this privilege. It allows assistants to explicitly choose non-isolated file and command invocations without resource limits; unselected invocations continue to follow original permissions. The same resource can retain both regular compute authorization and Full Access authorization simultaneously, with separate validity periods, modifications, and revocations.

When selecting Full Access, do not fill in quotas, and do not separately grant directory and network permissions. See [Full Access Deployment and Execution Semantics](../../admin/sandbox/linux.md#granting-full-access-on-demand) for details.

## Assistant Requesting Authorization

When permissions are missing, the assistant sends an application card in the chat, explaining the purpose, resources, actions, directory scope, quota, and validity period. The applicable scope is determined by resources already connected and assigned by the administrator; contact the administrator first if resources are missing.

Click "View and Authorize" to open a pre-filled form, verify and modify the scope, quota, and expiration time as needed, then click "Approve Authorization". Clicking "Reject" does not add permissions. Combined applications are submitted only if all are valid; a single card can combine up to eight compute, network, or storage applications.

The card retains the original application text, final approved content, and processing status, and remains viewable after refreshing or switching devices. After approval, the assistant continues based on the final effective authorization; after rejection, it receives a clear result. Tasks that have been manually stopped, paused, or ended will not automatically resume due to processing old applications.

The assistant permissions page retains the most recent up to 200 applications, with pending ones prioritized. Approval records indicate past approvals, but actual access still depends on whether the authorization is valid and whether the resource is online; if the sandbox is offline, restore the resource connection.

## Modification and Revocation

Authorizations can limit actions, data scope, and validity period. Disconnecting resources, revoking, or expiration will restrict subsequent execution, and related runs may be stopped; completed writes or already sent data cannot be withdrawn via revocation.

Shell read-write mounts require both read and write permissions and directly modify the original directory; cancellation does not roll back. Write-only permissions can be used by file tools that support this operation and do not automatically grant Shell read permissions.

Model and external service credentials are managed by administrators in resource configuration and are not entered in regular chats. After a provider is deleted, affected assistants need to re-select available models.

## Assistant Budgets

Set the amount, currency, and daily/weekly reset cycle when hiring an assistant or in "Assistant Info → Overview". All chats, background, and scheduled executions share the quota of the owning assistant; there is no global default budget or independent task budget.

Before model invocation, reserved costs are occupied, and settled upon obtaining actual costs. Local invocations automatically end cost reservations and concurrency occupancy upon completion; when costs are unknown, historical records are retained, not settled as zero cost, and manual handling is not required. Running invocations across cycles still occupy quota, and costs are attributed to the original cycle of the invocation after completion. Runtime restarts first settle existing receipts, then clean up invocation occupancy left by the previous process; unknown invocations will not automatically retry because of this. Modifying budgets, revising tasks, or cycle resets do not delete historical bills.

Budgets are used to prevent accidental large consumption during personal use, executed based on model prices configured in the service and returned usage, and do not equal the final bill or payment limit of the vendor account. Interrupted requests may have been charged on the vendor side but not counted in local consumption; they will be supplemented when actual costs are obtained later, and new invocations are blocked after exceeding the budget. When prices are unknown, configuration needs to be supplemented, not treated as free. Manual prices and currencies should be verified against actual services.

The currency in regular settings only affects default values for new inputs and display; editing existing amounts retains the original currency and precision. Amount displays are uniformly converted to the preferred currency, multi-currency costs show a total, and original currency amounts are not displayed side-by-side; fixed to two decimal places, rounded up if less than one cent, converted amounts include `≈`. When conversion is temporarily unavailable, display `—`. Conversion uses versioned reference exchange rates, not real-time trading quotes. Reset times are calculated based on the account timezone; pay attention to the next reset time displayed on the page after setting the timezone.

## Data Transmission Scope

When using remote models, data submitted to the model is sent to that service; locally deployed Runtime does not eliminate such transmissions. Allowing Shell to read data and connect to a target also means scripts can send readable data to the approved target.

Network target authorization limits connectable hosts and ports, and is not equivalent to authorization for a specific business action within a website. HTTPS tunnels do not promise to inspect paths, request bodies, or specific actions within encrypted content.

## Compute, Network, and Workspace Quotas

Compute and network quotas are set in assistant authorizations and can also be applied for by assistants via chat cards. Users can modify application values before approving; a single card can combine up to eight compute, network, or storage applications, and if any item fails validation, no partial authorization is created. Combined approvals first display a summary of each resource and quota, which can be modified after expanding; the authorization time is set uniformly at the top, and the entire group of permissions shares this validity period, and approved cards also display only one authorization time. Temporary quotas have a time to restore the base quota, which must not be later than the authorization expiration. Edit existing authorizations when increasing quotas is needed.

| Type | Authorization Attribute | Usage Metric |
| --- | --- | --- |
| Compute | CPU power, memory, process/thread count | Shared by executions of the same assistant in the same sandbox |
| Compute | Maximum single run duration, memory temp disk `/tmp` | Limits each execution separately; unlimited run duration requires explicit approval |
| Network | Bidirectional traffic, cycle, timezone | Accumulated across executions by assistant and actual network source; supports cumulative, daily, weekly, monthly |
| Network | Concurrent connections | All executions from the same network source share TCP connection and UDP session slots |
| Workspace | Capacity, single file size, file and directory count | `/work` for the same assistant in this sandbox, all write entry points share the file system quota |

The network page displays access scope, while traffic and connection quotas are set and viewed in the assistant's network authorization; the compute page displays the range of computable capabilities. The permissions page displays actually approved quotas and usage. 1 core of CPU power equals the original 100%. Authorization validity period and maximum single run duration are set separately.

Cycles are calculated based on the timezone in the authorization record; restarts or re-executions do not reset; modifying quotas retains used amounts. Existing cycles and timezones cannot be changed via editing to avoid duplicate quota claims. Temporary quotas retain the base value and restore upon expiration; temporary additions in the current period can raise the temporary traffic limit to the base quota plus the addition, and set the expiration to the end of the current cycle. When used amounts exceed the restored quota, further consumption stops, and historical bills are not deleted.

Commands automatically use the network permissions approved, valid, and with deployment targets for the current assistant in the current sandbox by default. Models do not need to pass network resource IDs; offline commands can still be executed without authorization. Exhausted traffic cannot be bypassed by restarting commands. Network quota increases can be used for running tasks; network authorization expiration or revocation restricts its connections, and does not stop compute due to the expiration of other unused network permissions. Compute authorization modifications stop affected tasks, and new executions use the approved quotas; ended commands do not automatically rerun.

Workspace capacity requires deployment enablement and pre-configured Linux file system project hard quotas. Unconfigured workspaces explicitly display that capacity quotas are not supported, and cannot claim expansion by only modifying interface numbers. Reaching capacity or temporary expansion expiration does not delete files, and reading and cleaning are still possible; the single file limit applies to Shell and file writing tools. External shared directories do not count towards a specific assistant's workspace quota. Upload attachment library limits and workspace capacity are managed separately.

`/tmp` is the memory temp disk (`temp_bytes`) in compute authorization, independent for each execution, with writes counted towards compute memory based on actual usage, and cleared after execution ends. 64 MiB is an adjustable example application value, not persistent storage capacity; files that need to be retained should be written to `/work`, whose capacity is managed by storage authorization.

## GPU Authorization

When deployment has opened GPU and user groups allow it, devices can be selected in assistant compute permissions; cards display model and VRAM, with short identifiers attached to devices of the same model. Regular commands use devices approved in the current compute permissions, and restore to base authorization after temporary configuration expires. Modifying compute authorization stops affected tasks, and new executions use the updated device range.

GPU is shared access, with no VRAM reservation or compute power guarantee. When unavailable, it will not automatically switch to CPU or Full Access; verify the reason and contact the deployer, or remove GPU authorization to continue with regular compute.
