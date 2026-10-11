# Plugins and Skills

First, enable **Show Plugins** in **User Settings → General**, then manage themes, pages, and HTTP tools via the **Plugins** sidebar (`/#/extensions`). This switch is disabled by default, saved per account, and synced across devices. When disabled, it hides the plugin entry, plugin themes, the plugin tool directory, plugin targets in login links, plugin and service connection options in user groups, and connection details in usage; directly opening a plugin page redirects to general settings. Selected plugin themes temporarily use the default theme and are restored upon re-enabling. Existing plugin configurations, authorizations, and background tool executions are unaffected. CLI and Skills are installed directly in the assistant workspace, without needing to import plugin packages.

## Using Authorized Plugins

Regular members use plugins that have been enabled by administrators and assigned to their user groups. Personal enable/disable actions only affect your own usage; if a plugin has personal settings, you can fill them in via the corresponding configuration entry. Plugin pages, themes, and HTTP tools remain subject to group permissions, personal enablement, and assistant authorization restrictions. Installation and shared service connections are [managed by administrators](../administration/plugins.md).

## Installing CLI or Skills

Tell the assistant the source and purpose directly in the chat, for example: "Install this CLI into your working directory and verify it works according to the documentation." The assistant uses standard Shell and file tools to install programs and dependencies, following the existing permission request process when computational or network resources are needed.

CLI is installed within the assistant's `/work`. Skills are placed in `/work/.agents/skills/<名称>/SKILL.md`, automatically discovered by the system, and read by the assistant on demand. Different assistants have independent workspaces, and files are preserved across executions and restarts. Login is completed according to the process supported by the CLI itself, with login files saved in the workspace; updates, checks, and deletions are also performed by the assistant using standard tools.

These installations do not generate records in the plugin list. Plugin enable/disable, version switching, and uninstallation do not manage these working files.

The Skills page in Assistant Information allows you to view discovered entries; currently, there are no entries for installation, editing, or enable/disable actions. Skills provide process guidance and do not grant additional file, network, or tool permissions.
