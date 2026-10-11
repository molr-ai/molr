# Plugin Management

Administrators must first enable **Show Plugins** in **User Settings → General**, then enter the plugin page. For member usage instructions, see [Plugins and Skills](../guides/plugins-and-skills.md).

## Adding Plugins

Administrators click **Install Plugin**, select **Upload Plugin Package** or **Install from Link** in the dialog, provide a ZIP file or a public HTTPS ZIP URL, and click **Install**. Downloads do not carry account credentials, do not follow redirects, and do not access private networks. The page search scope is limited to plugins already present in this instance; there is no online plugin directory.

Importing only saves and validates the package; it does not execute scripts. Administrators globally enable plugins, approve required permissions, allow members to use them via **Authorized User Groups**, and bind HTTP tools to assistants. Regular members only see plugins that are globally enabled and authorized for their group; individual enable/disable actions only affect their own usage.

## Plugin Configuration and Connections

Personal settings, administrator shared settings, and HTTP service connections are declared by plugins as needed. Only plugins that provide settings display a configuration entry. HTTP connections are configured by administrators; API Keys are stored encrypted and injected into fixed requests via authorized connections.

Connections support cumulative call counts, concurrency, timeouts, and user group quotas. **Test Connection** sends a request to the provider and counts against the quota; page loading and saving do not automatically test connections. In case of configuration errors or insufficient permissions, the system will not automatically switch to other connections.

## Versions and Uninstallation

Importing a new version only stages it. When administrators switch the enabled version, applicable permissions and tool bindings are retained. Re-review is required if configuration or connection summaries change.

Disabling, revoking permissions, or uninstalling closes the corresponding pages, themes, and HTTP capabilities. Uninstalling retains published versions and historical call records; deleting HTTP service accounts requires separately deleting the corresponding connections.

For developing custom packages, see [Plugin Protocol and Packaging](../../../dev-docs/en/extensions/plugins.md#writing-plugins).
