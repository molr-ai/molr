# Molr administrator guide

This guide is for instance administrators and server operators. Resource, member and component management in WebUI requires an administrator account; installation, configuration files, services and backups require server access.

## Set up an instance

Follow [Set up a Molr instance](getting-started.md) for installation, initialization, resource connections and member setup.

- [Installation and service management](deployment/installation.md): requirements, channels, pinned versions, offline installation and service lifecycle.
- [Docker deployment](deployment/docker.md): run Runtime and WebUI in one container with persistent data and optional command execution.
- [Building from source](deployment/source-build.md): build programs and release packages yourself.
- [Configuration and access](deployment/configuration.md): LAN access, HTTPS, model networking and WeChat connectivity.

## Connect resources

- [Model connections](resources/models.md): providers, capabilities, prices and credentials.
- [Sandbox resources](resources/sandbox.md): connect execution machines and register storage, compute and network scopes.
- [Linux sandbox deployment](sandbox/linux.md), [Standalone sandbox service](sandbox/remote.md): system dependencies, isolation, GPUs and remote execution nodes.

## Manage members and extensions

- [Users and groups](administration/users-and-groups.md): accounts, resource allocation, quotas and aggregate usage.
- [Plugin administration](administration/plugins.md): installation, shared connections and versions.
- [Component management](administration/components.md): browser installation, proxy configuration, updates and repairs.

## Maintain and troubleshoot

[Updates, backups and recovery](deployment/maintenance.md) covers versions, persistent data and recovery; [Administrator troubleshooting](troubleshooting.md) helps diagnose service, authentication, model and sandbox failures.

After setup, use the [user guide](../user/README.md) to manage your own assistants. Implementation and APIs are documented in the [developer guide](../dev/README.md).
