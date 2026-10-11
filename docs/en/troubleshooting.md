# Troubleshooting

First, check account permissions, model connections, and service status based on the page symptoms. Contact the instance administrator if resource configuration changes are needed, and let the deployer handle service configuration issues.

## Waiting and Failures

| Page Symptom | What to Check |
| --- | --- |
| Model unauthorized | Whether the current assistant has been authorized to call the model, and whether the authorization has expired |
| Model price missing | Whether the model configuration has a clear price; do not arbitrarily set it to zero to bypass waiting |
| Insufficient budget | Current cycle usage, reserved amounts for running tasks, assistant quota, and the quota granted to this account by the current user group |
| Provider authentication failure or rate limiting | Connection address, Key, and provider quota; wait before deciding whether to retry |
| Local model connection refused | Whether the address points to the Runtime host, and whether the deployment network policy allows it |
| Command environment unavailable | See [Linux Command Environment Diagnostics](deployment/sandbox.md#checks-and-verification); do not switch to non-isolated execution |
| Client disconnection | Whether the network and Runtime are online; after the page reconnects, verify messages and execution records |
| Result pending verification | First confirm the actual operations and artifacts; do not directly repeat operations that may have already taken effect |

The navigation connection pattern reflects the client connection status and does not indicate that all tasks have succeeded. After adjusting permissions, resources, or budgets, continue with the actions currently provided by the task; do not assume that old tasks have recovered just because saving settings was successful.

## Collecting Diagnostic Information

In "User Settings → About", verify the versions and commits of the WebUI and Runtime; if the frontend and backend are inconsistent, save the input first, then refresh or ask the administrator to verify the deployment version.

The developer workbench requires the deployer to enable `debug.enabled`. The "Show Developer Workbench" option in regular user settings only controls the visibility of the entry point and cannot restore records that were not collected or have been cleaned up. To view collected model requests, tool results, and context, refer to [Developer Workbench](../../dev-docs/en/runtime/workbench.md).

The deployer can temporarily record raw responses according to [Model Response Diagnostic Logs](deployment/maintenance.md#model-response-diagnostic-logs). Use the [Local Recovery Process](deployment/maintenance.md#backup-recovery-and-upgrades) when an account cannot log in.
