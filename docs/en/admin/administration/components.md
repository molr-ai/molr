# Component management

## Manage in WebUI

Administrators can open Admin Settings → Components from the User Settings page header to view browser component versions and installation progress, and install, update, or repair components. Click Settings on the browser component row to open its proxy settings dialog. Saving closes the dialog; cancelling discards the draft. For browser usage, see [Operating the Browser](../../user/best-practices/browser.md).

## Command-line maintenance

`molr-setup` manages both Molr and optional components. **Install and start** in the browser panel installs the tested agent-browser and Chrome for Testing (based on Chromium) combination for this instance. Assistants share read-only programs while keeping separate website data. The automatic download catalog currently supports Linux amd64; administrators can configure supported external program directories on other platforms.

```bash
molr-setup components status --json
molr-setup components install browser
molr-setup components update browser
molr-setup components repair browser
molr-setup tasks status <task-id> --json
molr-setup tasks cancel <task-id>
```

Component updates select the compatible version embedded in the installed setup tool. Repair creates a new directory instead of replacing files used by running tasks; old directories are not automatically removed. Explicit browser program paths remain administrator-managed and are never overwritten by automatic installation.

An instance-wide exclusive lock serializes application and component maintenance. Duplicate requests reuse the existing task; other mutations report that maintenance is busy. Status reads do not take that lock. Task stages and results live in private instance cache storage, separate from the Molr installation receipt. Downloads can be cancelled; file commit and service restart cannot. Failed or interrupted work can be retried, and unverified files never become an active installation.

WebUI maintenance runs as an independent systemd user service under the service account. Closing the page or restarting Runtime does not cancel installation. A working user service manager is required; otherwise use the CLI. Administrator-configured proxy and certificate environment variables are forwarded from Runtime without putting their values in command lines or task responses. Component directories and their ancestors must not be writable by other users.

Ordinary component installation neither invokes a system package manager nor automatically uses sudo. To prepare missing browser libraries, an administrator explicitly runs `molr-setup components install browser --install-deps`; automatic dependency preparation currently supports APT. System-wide Molr updates remain an installation-owner CLI operation. For eligible user installations, an administrator can explicitly choose **Update and restart Molr** in WebUI. This interrupts running tasks; the updater never automatically downgrades or restores browser profiles.

## Browser Proxy

Administrators open Admin Settings → Components and click Settings on the browser component row to configure the proxy in a dialog. All users’ newly started browser tasks use this instance-wide setting. HTTP proxies support optional username/password authentication and HTTPS website traffic. SOCKS5 supports unauthenticated connections. Enter a scheme, host and port, for example `http://host.molr.internal:7890`. Use `host.molr.internal` for host services; `localhost` belongs to the sandbox itself.

Changes apply on the next browser start. Existing tasks keep their launch configuration and are not interrupted automatically. Close and restart the browser to apply changes; Runtime does not need to restart. Passwords are never returned. Leave the password blank to keep it when the server and username are unchanged; changing either requires re-entering the password. Selecting no authentication or direct access clears credentials.

The assistant still needs sandbox network grants for the proxy endpoint; settings never grant access automatically. Separate bypass entries with commas; hosts, wildcard domains and IP/CIDR entries are supported. Bypassed connections require grants for their actual destinations. For proxied traffic, the sandbox checks the proxy endpoint and the proxy controls downstream website access. Offline mode, revocation and network quotas remain effective. Proxy failures never trigger automatic fallback to direct access.

The browser title bar shows a “Proxy enabled” badge when the current session uses a proxy. Direct connections and browsers that have not started show no network badge. The assistant receives the same sanitized proxy summary from `browser_session` start/status, without authentication usernames or passwords. Failed navigation reports a browser error code and any sandbox event observed during that action. Such an event may be unrelated to the failure; undetermined causes are labeled explicitly. The last failure includes its task and time and is updated after successful navigation or a new task. Saved configuration does not establish connectivity.
