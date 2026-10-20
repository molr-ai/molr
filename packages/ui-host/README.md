# @molr/ui-host

Framework-independent fixed-version theme selection and theme-owned partial page resolution. Plugin manifest validation and capability checks belong to the plugin system.

Users select one theme. Its manifest owns the page mapping; undeclared pages resolve to the default theme. External themes are installed and authorized through the Runtime plugin manager; the host resolves them alongside built-ins. The React selector and host adapter live in `apps/webui/src/ui-host`.

See [theme architecture](../../docs/dev/frontend/themes.md).

The `@molr/ui-host/app` declaration defines the desktop main-frame bridge. `getInfo()` includes desktop build identity and the active connection kind. `updates` exposes desktop release status, manual checks, the fixed download page, and a native-confirmed local upgrade for managed local connections. Status events never expose other connections or local installation paths to remote pages.
