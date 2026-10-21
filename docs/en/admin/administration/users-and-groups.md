# Users and User Groups

Molr supports independent accounts for families and small teams on the same service. Administrators create accounts; public registration is not available. There are no shared spaces, group chats, or data exchange features between users.

## Two Settings Entry Points

The avatar at the bottom of the desktop sidebar, and the avatar and username at the top of the mobile assistant page, open **User Settings**, which manages display name, language, theme, timezone, memory, personal usage, and account security. The "Admin Settings" entry within User Settings opens **Admin Settings**, which is visible only to administrators and includes users, user groups, aggregate statistics, and plugin management. Resource configuration is uniformly accessed from the **Resources** page in the sidebar.

User messages in chat use the same profile as the navigation avatar: the display name takes priority, falling back to the username. Avatars support presets and uploaded images, with the first character of the name used when no avatar is set. Changes saved in User Settings also update existing messages in chat.

Regular users can create and modify their own assistants, and set assistant budgets and resource authorizations. Regular users cannot configure models, tools, or sandboxes, nor can they install, configure, or enable plugins. Plugin capabilities enabled and assigned by administrators can be used within the authorized scope.

## Creating User Groups and Assigning Resources

1. Administrators first configure models and sandboxes on the Resources page; plugins are managed in Admin Settings.
2. In Admin Settings → User Groups, create a new group, check models, tools, or sandboxes from the existing catalog, and then set the corresponding quotas. This only saves resource references; it does not copy connections, keys, or sandbox configurations.
3. In Admin Settings → Users, add users, specifying username, initial password, and user group. After logging in, users can change their password and set up two-factor authentication themselves.

Newly created regular user groups have no resources by default. Supporting basic chat requires at least one model and the `conversation_read` and `conversation_publish` tools; command and file capabilities also require relevant tools, sandbox references, and the assistant's own resource authorizations.

Deleting references causes existing assistants to lose the corresponding access permissions. Already created chats, assistants, and attachments are retained. Deactivating or changing a user group revokes the account's existing sessions, tokens, and one-time login links. Members must be moved out before deleting a group; the last active administrator cannot be deactivated or demoted.

## How Quotas Are Calculated

Each member independently inherits the group's quota limits, accumulating across all their own assistants; other members do not consume this quota. User groups do not have an additional assistant budget pool.

| Resource | Configurable Limits |
| --- | --- |
| Model | Periodic cost, call count, concurrent call count |
| Tool | Periodic call count, concurrent call count |
| Sandbox | Concurrent execution count, compute resources, network traffic and connection count, workspace storage |

Models and tools can be metered by day, week, month, or cumulative totals, with periods determined by the specified timezone. Leaving the cost limit blank, or setting call or concurrency limits to 0, indicates no group-level restriction for that item. Model calls first reserve costs, which are settled based on known costs upon completion; if costs are unknown, the reservation and concurrency occupation are automatically ended, retaining the unknown record. Calls that have been issued with unknown results continue to count towards the original period's call count and do not permanently occupy quota across periods.

Compute resources impose aggregate limits on CPU, memory, and process count for concurrent assistants under the same account, while also limiting single execution duration and temporary space. Network usage is limited by both assistant quotas and account quotas; changing groups or modifying statistical periods does not clear usage. When periods change, overlapping usage from the old period is conservatively counted towards the new period.

Storage limits require that the sandbox has a configured storage hard quota. An account's storage capacity is reserved based on the sum of workspace capacities authorized to all its assistants; revoking authorization does not automatically release the original capacity, requiring explicit reduction of assistant capacity. This limit cannot be enabled if a hard quota is not configured. Remote sandboxes also need to be upgraded to a version that supports user group quotas.

The built-in administrator group automatically has access to all existing and new resources, with no group-level limits. Administrator assistants are still subject to their own authorizations, budgets, and resource limits. Regular users undergo additional group-level resource and quota checks; deployment-side limits always apply.

## Data Isolation and Statistics

Chats, attachments, assistants, plans, memories, execution outputs, and personal settings are isolated by account. Administrators can view an account's model/tool/HTTP connection call counts, consumed and reserved costs, historical calls with unknown results, and total attachment volume; the product does not provide an entry point or unauthorized interface to view detailed data of other users. Server administrators can still directly log into the server to read disk data; the project does not counteract this access.

User Settings → Usage aggregates tokens and spend by date, assistant and model; see [Personal Settings](../../user/guides/settings.md#usage). Lifetime usage and allowances is expanded by default and shows lifetime usage and current member allowances. Models, tools and connections display calls, concurrent holdings and reset times in their respective periods. Running calls carried across periods retain their holdings; ended calls with unknown outcomes remain historical records. Sandbox entries show member limits; live usage remains available in assistant permission details. Administrators' personal pages omit member allowances; assistant budgets still apply.

Admin Settings → Usage Statistics supports dates, user group, member and model filters, plus username/display-name search. The overview and daily calendar follow these filters. Group the paginated breakdown by member, user group or model and sort descending by tokens, spend or calls. Select a member to inspect their model distribution. Statistics never return other users' assistant names, conversations, tasks or receipt bodies. Groups reflect current membership. Dates use the viewer's timezone; money uses their preferred currency and one exchange-rate snapshot.

Lifetime usage and resources is expanded by default and shows independent global context storage and lifetime totals, including user message counts, for all or the selected member. Date, group and model filters do not affect this section. HTTP connection calls are included in tool calls and must not be added again. Active reservations and unknown costs are excluded from confirmed spend.

The "Usage" in User Settings displays the context data volume and deduplicated message node count for this account; identical objects within the account are counted only once. The "Usage Statistics" in Admin Settings displays global context storage deduplicated across accounts, including data awaiting reclamation, and does not change with member filters. Shared data is counted separately for each account referencing it, so the sum of individual account usages may exceed the global usage. This statistics covers storage objects for messages, tool definitions, and debug details, excluding database indexes, reference metadata, and free space.

Regular users cannot currently create public port forwards that do not require authentication. This entry point directly exposes services within the assistant workspace to visitors, so currently only the existing port forwarding capability for administrators is retained.
