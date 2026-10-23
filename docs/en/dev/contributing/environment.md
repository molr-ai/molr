# Development Environment

This document defines development machine directories, shared download caches, and development services. See the [collaboration workflow](workflow.md) for task isolation and [acceptance standards](acceptance.md) for acceptance instances and delivery.

## Machine Directories and Shared Download Caches

On each development machine, including machines accessed over SSH, use `~/workspace/molr` as the recommended main working directory and `~/workspace/molr/.cache` as the fixed cache root. `~` refers to the current user's home directory on the machine running the command. Existing repositories may stay in their current location without moving running services; reusable downloads still belong in the fixed cache root.

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
