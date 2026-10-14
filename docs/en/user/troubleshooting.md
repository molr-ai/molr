# Troubleshooting usage problems

Read the status and error shown in the page, then decide whether to continue, change settings or contact your administrator.

## Waiting and failures

| Symptom | What you can check |
| --- | --- |
| Waiting for permission | Open the request card, review the resource, actions, limits and expiry, then approve or reject |
| Budget exhausted | Check the assistant budget, usage and reset time; contact the administrator for group quota issues |
| No available model or tool | Ask the administrator to check group resource assignments; assistants also need their own authorization |
| Model or command failed | Save the error and check the selected model, task input and permissions; ask the administrator about connections or execution environments |
| Page disconnected | Check your device network and reconnect; contact the administrator if the service is unreachable |
| Unknown result or verification needed | Inspect messages, execution records and actual deliverables before retrying to avoid repeating completed actions |

After changing permissions or budgets, continue using the actions offered by the task. The navigation connection status indicates client connectivity, not success of every task.

## Collect information for support

Check WebUI and Runtime versions in User settings → About. Record the error, time, affected task and reproduction steps; remove passwords, tokens and unrelated personal information before sharing screenshots or logs.

Ask your administrator for help if you lose your password or cannot complete two-factor authentication. For server, model or sandbox checks, share the [administrator troubleshooting guide](../admin/troubleshooting.md). If your instance exposes the developer workbench, use it to inspect your own executions as described in [Developer workbench](../dev/runtime/workbench.md).
