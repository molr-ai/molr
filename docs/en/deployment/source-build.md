# Building and Deploying from Source

## Build

Use the Go version specified in [runtime/.go-version](../../../runtime/.go-version) and the Node.js version required by the root [package.json](../../../package.json). Run these commands in your own deployment checkout; repository development tasks follow the [worktree rules](../../../AGENTS.md).

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

Confirm that Runtime is statically linked, its build information includes `CGO_ENABLED=0`, and the final file permissions are `0755`. When deploying the main service, also verify the target commit and `vcs.modified=false`. The WebUI output is in `apps/webui/dist/` and is served by a static server; do not use the Vite development server as a public production entry point.

After building, complete [configuration and initialization](configuration.md#configuration-and-initialization), [service installation](installation.md#linux-services-and-command-environment), and [post-start checks](installation.md#post-startup-checks), in that order.

## Build Release Packages

Local builds require Node.js, the repository's specified Go version, Python 3, and the system tar utility:

```bash
npm ci
npm run build:release
```

The build first checks the source and public static files to be released, then generates WebUI and statically compiles Runtime with `-trimpath` and the `webui` build tag, serving pages and APIs from the same service. Output goes into `dist/releases/`, including `molr_<版本>_linux_<架构>.tar.gz` and a matching `.sha256` file. Recommended-version catalogs are generated separately as described in [Release Maintenance](../../../dev-docs/en/releasing/releases.md). Packages contain Runtime, the installer, `LICENSE`, and the complete `THIRD_PARTY_NOTICES.md`; BusyBox is not bundled. The default version is `0.1.0-local`; override it with `MOLR_VERSION`, `MOLR_ARCH`, and `MOLR_DIST_DIR`. Cross-compilation does not require a target-architecture BusyBox; the installation target provides static BusyBox through its system package manager. Set `GO` to select a Go executable. `molr version` displays the build version and commit. Archives collect only the files explicitly listed in `scripts/release-policy.json`; final archives and download manifests are generated only after validation succeeds. Do not distribute directories left by failed builds or archive the entire working directory directly.

To export a source package, run `npm run build:source -- --ref HEAD` on a committed, reviewed version. This command exports only the specified Git commit; working directory changes, uncommitted new files, `.cache/`, and runtime data are excluded. See [Public and Private File Separation](../../../dev-docs/en/releasing/public-content.md) for release scope and validation limits.
