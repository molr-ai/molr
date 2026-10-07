# @molr/ui-host

Framework-independent manifest validation, fixed-version theme selection, theme-owned partial page resolution and request permission policy.

Users select one theme. Its manifest owns the page mapping; undeclared pages resolve to the default theme. External themes are installed and authorized through the Runtime plugin manager; the host resolves them alongside built-ins. The React selector and host adapter live in `apps/webui/src/ui-host`.

See [theme architecture](../../dev-docs/frontend/UI_PACKAGES.md).
