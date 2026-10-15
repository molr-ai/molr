# Building and Deploying from Source

## Build

Use the Go version specified in [runtime/.go-version](../../../../runtime/.go-version) and the Node.js version required by the root [package.json](../../../../package.json). Run these commands in your own deployment checkout; repository development tasks follow the [worktree rules](../../../../AGENTS.md).

```bash
mkdir -p .cache/build
cd runtime
CGO_ENABLED=0 GOTOOLCHAIN="go$(cat .go-version)" go build -o ../.cache/build/molr ./cmd/molr
chmod 0755 ../.cache/build/molr
file ../.cache/build/molr
go version -m ../.cache/build/molr
cd ..
npm ci
npm run build:webui
```

Linux artifacts must be static ELF executables. macOS artifacts are Mach-O executables using system libraries; the ELF static-link requirement does not apply. Both must report `CGO_ENABLED=0` and use `0755` permissions. When deploying the main service, also verify the target commit and `vcs.modified=false`. The WebUI output is in `apps/webui/dist/` and is served by a static server; do not use the Vite development server as a public production entry point.

After building, complete [configuration and initialization](configuration.md#configuration-and-initialization), [service installation](installation.md#linux-services-and-command-environment), and [post-start checks](installation.md#post-startup-checks), in that order.

## Native macOS execution

Runtime supports macOS ARM64 and AMD64. Workspaces, file tools, attachments, and memory are independent of command execution; `sandbox.enabled: false` disables only local commands. Controlled commands use Seatbelt; see [native macOS command permissions](../sandbox/macos.md) for configuration and capabilities. Installation, CLI updates, and uninstall support macOS; see the [installation guide](installation.md#macos-command-line-installation). In-app updates remain unavailable.

After building, create a separate configuration and replace all absolute paths. Minimal local configuration:

```json
{
  "data_dir": "/Users/example/Library/Application Support/Molr/data",
  "http": {
    "listen": "127.0.0.1:8080",
    "public_url": "http://127.0.0.1:8080",
    "tls_mode": "local"
  },
  "sandbox": { "enabled": false }
}
```

Initialize and start from the repository root:

```bash
.cache/build/molr bootstrap -config /absolute/path/server.json
.cache/build/molr serve -config /absolute/path/server.json
```

See [WebUI](../../../../apps/webui/README.md) for frontend development. To serve built pages from Runtime, copy the contents of `apps/webui/dist/` into `runtime/internal/webassets/dist/` and rebuild with `go build -tags webui`.

For controlled commands, set `sandbox.enabled: true` and grant compute access. Network and external-file access require separate grants. Setting `sandbox.allow_full_access: true` allows independently authorized Full Access. Both modes use the same native workspace. Scripts use actual paths or `$MOLR_WORKSPACE`, `$MOLR_GLOBAL`, and `$TMPDIR`; script text is never rewritten.

For persistent operation, configure an administrator-managed launchd service. Set `ProgramArguments` to the binary, `serve`, `-config`, and the absolute configuration path. Set `WorkingDirectory`, log paths, and `PATH` explicitly because launchd does not read interactive shell configuration. The service account must have access to its own configuration and data. For upgrades, stop this instance, atomically replace its executable, and restart while preserving configuration, database, and keys.

After startup, authenticate and inspect `/api/sandbox`: disabled commands report `Enabled=false` and `Storage=true`. Ready controlled execution reports `Reason=available`, `Shell=true`, `FileIsolation=true`, `NetworkIsolation=true`, and `NetworkBackend=seatbelt-toggle`. `ProcessIsolation` and `ResourceLimits` remain false. If Full Access is separately enabled, check `Capabilities.FullAccess=true`. Verify files, terminals, and revocation natively on a Mac. Remote execution nodes remain Linux-only.

## Build Release Packages

Local builds require Node.js, the repository's specified Go version, Python 3, and the system tar utility:

```bash
npm ci
npm run build:release
```

The build first checks the source and public static files to be released, then generates WebUI and statically compiles Runtime with `-trimpath` and the `webui` build tag, serving pages and APIs from the same service. Output goes into `dist/releases/`, including `molr_<版本>_linux_<架构>.tar.gz` and a matching `.sha256` file. Recommended-version catalogs are generated separately as described in [Release Maintenance](../../dev/releasing/releases.md). Packages contain Runtime, the installer, `LICENSE`, and the complete `THIRD_PARTY_NOTICES.md`; BusyBox is not bundled. The default version is `0.1.0-local`; override it with `MOLR_VERSION`, `MOLR_ARCH`, and `MOLR_DIST_DIR`. Cross-compilation does not require a target-architecture BusyBox; the installation target provides static BusyBox through its system package manager. Set `GO` to select a Go executable. `molr version` displays the build version and commit. Archives collect only the files explicitly listed in `scripts/release-policy.json`; final archives and download manifests are generated only after validation succeeds. Do not distribute directories left by failed builds or archive the entire working directory directly.

To export a source package, run `npm run build:source -- --ref HEAD` on a committed, reviewed version. This command exports only the specified Git commit; working directory changes, uncommitted new files, `.cache/`, and runtime data are excluded. See [Public and Private File Separation](../../dev/releasing/public-content.md) for release scope and validation limits.
