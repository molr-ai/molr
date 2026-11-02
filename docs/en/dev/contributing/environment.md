# Development Environment

This document defines development machine directories, shared download caches, and development services. See the [collaboration workflow](workflow.md) for task isolation and [acceptance standards](acceptance.md) for acceptance instances and delivery.

## Machine Directories and Shared Download Caches

On each development machine, including machines accessed over SSH, use `~/workspace/molr.ai` for the recommended source workspace, a separate `~/workspace/molr` for development runtime materials, and `~/workspace/molr/.cache` for shared downloads. `~` refers to the current user's home directory on the machine running the command. Existing repositories may keep their paths; a source tree intended as a sandbox external directory must neither contain nor be contained by private instance directories.

| Contents | Fixed directory relative to `~/workspace/molr/.cache` |
| --- | --- |
| npm download cache | `npm/` |
| Go modules and automatically downloaded Go toolchains | `go-mod/` |
| Electron binary downloads | `electron/` |
| Electron Builder downloads | `electron-builder/` |
| Playwright browsers | `ms-playwright/` |
| Other toolchains, installers, and reusable downloads | `downloads/<工具>/<版本>/<系统>-<架构>/` |
| Extracted toolchains for reuse | `tools/<工具>/<版本>/<系统>-<架构>/` |

- All tasks, worktrees, and repeated acceptance runs reuse the machine's fixed download caches. Pass expanded absolute paths through supported cache options or environment variables, never paths relative to the current directory. Examples include `npm_config_cache` for npm, `GOMODCACHE` for Go, `electron_config_cache` for Electron, and `PLAYWRIGHT_BROWSERS_PATH` for Playwright. Use the same settings for installation and execution. Scope configuration to the relevant command or development session; do not change global user settings without authorization.
- Before downloading, inspect the fixed directory and the tool's existing default cache. Reuse files that match the required version, OS, architecture, and available integrity checks, and place reusable files in the fixed directory. Download only missing or invalid files. Use the tool's cache locks and integrity checks. For manual downloads, write a temporary file, verify it, and replace the destination atomically. Wait for or reuse an existing download of the same item rather than starting another.
- Shared contents include downloaded packages, tool-managed module caches, and versioned common tools. Each worktree owns its `node_modules`, application build artifacts, compilation caches, test output, and acceptance instance data. Do not share writable installation directories across tasks. Databases, credentials, login sessions, and user materials do not belong in shared download caches.
- Cache lifetime is independent of task lifetime. Preserve shared downloads when cleaning up worktrees or temporary acceptance directories. Remove only confirmed damaged or unused cache entries, without clearing directories another task might use.

## Private Instance Directories and Source Isolation

Store main and acceptance instance configuration, databases, keys, login sessions, assistant workspaces, runtime caches, and logs outside source trees. The following layout is recommended; existing managed installation directories outside the repository may remain in use without relocating programs and configuration merely to match names.

| Contents | Recommended location |
| --- | --- |
| Main service private files | `~/workspace/molr/instances/main/` |
| Independent task acceptance instance | `~/workspace/molr/instances/acceptance/<task>/` |
| Single development environment backup and rollback copy | `~/workspace/molr/backups/latest/` |
| Internal materials requiring retention beyond acceptance | `~/workspace/molr/internal-docs/` |

- Give each instance its own directory and record its task branch, source worktree, service name, ports, and actual paths. External placement preserves task isolation and cleanup responsibilities; do not share databases, keys, or writable assistant workspaces. Instance directories are not shared download caches.
- Source, `node_modules`, build artifacts, and compilation caches may remain in worktrees. Deploy running Runtime/helper binaries to an external instance or managed installation directory so that source write access does not expose running programs. Frontend development services may keep source working directories, with private logs and session files in the corresponding instance directory.
- Before exposing an entire repository, inspect all ignored directories, including `.cache/` and `internal-docs/`. Move historical files containing real credentials, databases, login links, or user data outside it; any retained internal materials must also fit the intended directory grant. Git ignore rules do not restrict sandbox reads. Do not substitute symlinks or weakened protected-path checks for physical separation.
- Before migrating an existing instance, inventory actual processes, configuration, and installation records. Stop that instance normally, then copy its complete data and verify digests, permissions, and database integrity. Update paths in configuration, installation records, and startup entries while preserving security settings, accounts, and sessions. Keep backups and rollback copies outside the directory to be exposed; leave no private data or symlink to the new location at the old path.
- After restarting, follow [Runtime acceptance](acceptance.md#runtime-builds-and-sandbox-acceptance) to verify HTTP readiness, existing sessions, and authenticated sandbox capabilities, including the WebUI proxy when present. Finally, use the directory connection flow to verify that the source root is accepted and the new private instance directory remains rejected. Disconnect temporary verification resources afterward and do not grant assistant permissions.

### Acceptance Data and Backup Retention Limits

- Acceptance materials serve the current task only. Keep the required instance and records while awaiting user acceptance or merging. After merging and verifying main service activation, or explicit task closure, delete that task’s acceptance databases, accounts and login files, logs, screenshots, recordings, test output, temporary programs, and installers. Do not retain historical acceptance materials, create archives, package them for storage, or perpetuate them by moving `internal-docs/`.
- Keep at most one latest verified complete backup for this development environment, at `~/workspace/molr/backups/latest/`. Data, necessary configuration, keys, and matching programs form one recovery set. Do not retain additional rollback directories, database copies, or `.previous` files by task or date. This development convention does not replace production backup policies.
- Build a consistent candidate backup in a temporary directory and verify file digests, permissions, database integrity, and required recovery files before replacing the retained backup and deleting the old copy and temporary files. At most one backup remains after replacement. If candidate validation fails, keep the previous valid backup and delete the failed candidate.
- Before cleanup, check task state, processes, and actual paths. Preserve running instances, materials still awaiting acceptance, shared download caches, and non-acceptance work such as product requirements, research, and promotional content. Incorporate lasting technical conclusions into the owning documentation and report current verification results in the delivery message; the full acceptance process need not be retained permanently.

## Development and Preview Services

- Use development servers for daily local work. The main workspace keeps one WebUI, one website, and one UI Design instance. Do not run duplicate development instances or build previews for the same application at the same time. The website homepage and documentation share one website service.
- The default ports below apply to the main working directory. Each independent worktree allocates available ports for its own applications, with one development server per involved frontend. “Repository root” below means the task worktree root, or the main working directory for main branch services.
- Before starting a service, check the existing processes' working directories, commands, and listening ports. Reuse matching services. When changing startup methods, stop the old instance in the same workspace instead of keeping duplicates on different ports.
- Development servers listen on `0.0.0.0` by default to allow access from devices on the same LAN. Do not bind to `127.0.0.1` or `localhost` unless the user requests it.

| Application | Command from repository root | Main workspace port |
| --- | --- | --- |
| WebUI | `npm run dev` (or `npm run dev:webui`) | `5191` |
| Website and documentation | `VITE_SITE_MODE=live npm run dev:website` | `5189` |
| UI Design | `npm run dev:design` | `5188` |

- Explicitly set `VITE_SITE_MODE=live` when developing or accepting the website locally. An unset or empty value still selects `coming-soon`. To check other modes, change the environment of the same instance and restart it rather than starting another persistent service.
- Existing `build` and `preview` commands remain available. Build previews are temporary checks for cases that require artifact verification. Stop them afterward; they are not daily or post-merge persistent services. Website previews use the mode embedded in their build artifacts.
- After starting or adjusting a service, verify its listening address, port, development mode, and instance count, and provide a working LAN URL such as `http://<本机局域网 IP>:<端口>`.
