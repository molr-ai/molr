# Manage Sandbox Resources

This page is for administrators. In "Resources → Sandbox", connect execution machines and add storage, compute, and network resources allowed for deployment. Assistant workspaces always reside locally in the Runtime.

## Local Resources and Member Scope

Local sandboxes are built into the Runtime and do not require starting a separate service. Compute resources are automatically registered; external directories and networks are added in the corresponding sandbox partitions, and the page can only select scopes allowed by the deployment. Connecting and adding resources does not automatically grant assistant permissions.

Assign sandboxes and tools via [User Groups](users-and-groups.md); members then approve directory read/write, compute, and network permissions for their own assistants. GPU devices must first be opened by the deployer, then selected in the user group's compute configuration; an empty selection means GPUs are not allowed. Full Access also requires deployment enablement and explicit administrator permission to grant it to the corresponding user groups.

For deployment dependencies, GPU, and network candidate scopes, see [Linux Sandbox Deployment](../deployment/sandbox.md). For user authorization and usage, see [Permissions and Budgets](../guides/permissions-and-budget.md).

## Connect in the WebUI

1. Go to "Resources → Sandbox → Connect Sandbox", fill in the name, HTTPS service address, certificate fingerprint, and token, click "Test Connection" first, and save after it passes. Re-testing is required after modifying the address, fingerprint, or token.
2. Go to the "Storage / Compute / Network" partitions of that sandbox and add resources that have been opened on the server side. Compute limits are displayed as read-only; directory and network scopes are managed by deployment configuration and cannot be expanded on the page.
3. Use the "Authorize to Assistant" option on resource cards to grant external storage read/write, compute execution, or network usage permissions. You can also view, modify, and revoke existing authorizations; management can also continue on the assistant permissions page. Connecting to a remote computer does not automatically grant any resource permissions, nor does it create an assistant workspace.
4. Assistants work locally by default. Remote file and Shell calls must explicitly pass `sandbox_id` and use the resources of that node; omitting the node but referencing remote resources will be rejected. There is no assistant `/work` on the remote side, and remote AGENTS.md, skills, or memory are not discovered.

The page displays connection, storage, and compute status separately; a successful connection does not imply that compute isolation is ready. "Re-check" refreshes actual capabilities, and when offline, the configuration snapshot from connection time is retained. "Rename" in the sandbox menu only changes the display name and does not require re-entering the token; "Update Connection Credentials" and "Disconnect Sandbox" are handled independently. The desktop uses a sandbox list, while the mobile version uses a selection box; the current sandbox and partition are remembered within the same browser tab.

The Runtime's `network.allowed_private_ips` continues to restrict it from connecting to private network services. If the remote Sandbox is located on a private network, the deployer must explicitly allow that address; the WebUI cannot modify this restriction. Redirects are prohibited, and dialing after resolution still passes through the existing network guard. The network resources of the Sandbox describe the network where the execution machine resides, where loopback addresses refer to that execution machine.
