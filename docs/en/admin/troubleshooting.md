# Administrator troubleshooting

Instance administrators check resource connections and group assignments first. An operator with server access handles service processes, configuration files and system dependencies.

## Locate the failure

| Problem | Checks and actions |
| --- | --- |
| Page unreachable or versions differ | Check service status, access URL, proxy and frontend/backend versions; see [Configuration and access](deployment/configuration.md) |
| Account cannot sign in | Check account status, group assignment and two-factor authentication; use the [maintenance guide](deployment/maintenance.md#backup-recovery-and-upgrades) for local recovery |
| Model connection fails | Check the provider address, credentials, capabilities and Runtime network permissions; see [Model connections](resources/models.md) |
| Member lacks resources or quota | Check group model, tool and sandbox references and quotas; see [Users and groups](administration/users-and-groups.md) |
| Command environment unavailable | Verify dependencies and actual isolation capabilities with [Sandbox checks](sandbox/linux.md#checks-and-verification) |
| Remote sandbox offline | Check the remote service, network, certificate fingerprint and token; see [Standalone sandbox service](sandbox/remote.md) |
| Browser component fails | Inspect the component task status and error, then update or repair using [Component management](administration/components.md) |
| WeChat connection or link fails | Check external networking and the public access URL; see [WeChat connectivity](deployment/configuration.md#wechat-connectivity) |

## Diagnostic information

After installing, updating or restarting, follow [Post-startup checks](deployment/installation.md#post-startup-checks) to verify the page, authentication and sandbox capabilities. Listening ports or active services do not replace capability checks.

For execution details, an operator enables `debug.enabled`, then consults the [developer workbench](../dev/runtime/workbench.md) for captured records. The personal visibility setting cannot recover records that were never captured. Temporarily enable [model response diagnostic logs](deployment/maintenance.md#model-response-diagnostic-logs) when needed, disable them afterward, and review sensitive content before sharing.
