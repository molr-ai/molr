# Deployment and Maintenance

This directory is intended for deployers who install and maintain Molr services. For a first-time experience, you can complete installation and the first round of delegation via [Quick Start](../getting-started.md).

## Choose an Installation Method

| Scenario | Entry Point |
| --- | --- |
| Install Linux services using distribution packages | [Installation and Service Management](installation.md) |
| Build your own deployment from the repository | [Source Code Build and Deployment](source-build.md) |
| Set up LAN access, HTTPS, or reverse proxy | [Configuration and Access](configuration.md) |
| Prepare isolated command environments, GPU, or host tools | [Linux Sandbox Deployment](../sandbox/linux.md) |
| Execute commands on another Linux machine | [Standalone Sandbox Service](../sandbox/remote.md) |
| Update versions, backup, restore, or reset accounts | [Updates and Backup/Restore](maintenance.md) |

## Complete Deployment

After installation, verify pages, authentication, models, and sandbox capabilities according to [Post-Startup Checks](installation.md#post-startup-checks). Administrators then [connect models](../models/README.md), [configure sandbox resources](../sandbox/resources.md), and assign available scopes for [user groups](../administration/users-and-groups.md).

Deployment configuration limits available capabilities, and user authorizations take effect within this scope. If you encounter issues, first check [Troubleshooting](../troubleshooting.md); for command environment failures, check [Sandbox Checks](../sandbox/linux.md#checks-and-verification).
