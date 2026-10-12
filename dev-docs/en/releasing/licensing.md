# MIT License and Third-Party Distribution

Dependency license review date: 2026-10-07; workspace manifest verification date: 2026-10-08. This document explains the current scope of the MIT license, third-party compatibility, notice delivery, and the release process that does not bundle BusyBox.

## License Scope and Directory Conventions

Molr's own code, documentation, and original resources are licensed under the [MIT License](../../../LICENSE) in the root directory. Third-party code, fonts, and brand logos retain their respective licenses and are not relicensed as MIT. The `license` field in the root directory and the six npm workspaces describes only Molr's own content.

**No third-party source code directories are created, and Git submodules are not used.** Complete original licenses, copyright notices, required NOTICE files, and corresponding source code entries are centralized in [THIRD_PARTY_NOTICES.md](../../../THIRD_PARTY_NOTICES.md). This file applies to the union of content distributed for the current frontend and Linux Runtime; listing a component does not imply that every artifact includes it.

`package-lock.json` and `runtime/go.mod` continue to manage dependency versions. Submodules cannot replace delivering licenses to binary users; upstream licenses do not automatically appear in final artifacts simply because npm/Go package managers are used.

## Compatibility Verification

Under the current usage patterns of standalone applications, library calls, and external processes, no dependencies were found that require Molr's own code to change to a different license. The original conditions for each third-party component must still be observed.

| Component | Current License and Handling |
| --- | --- |
| Standard Go/npm code dependencies | Original texts and copyrights for MIT, BSD, ISC, Apache, etc., are included in the summary file; static linking does not exempt notice obligations |
| `gopkg.in/yaml.v3` | MIT **AND** Apache-2.0, applied per file; NOTICE is also retained |
| `modernc.org/libc` / `sqlite` / `memory` | In addition to the top-level BSD, retain additional notices for musl, Go, go-netdb, Nixpkgs, public domain SQLite, MIT sqlite-vec, and mmap; logo assets for unused modules |
| DOMPurify | Apache-2.0 selected from dual license; original files retain upstream terms |
| elkjs 0.9.3 | Core and ELK parts are EPL-2.0; EMF-GWT parts are EPL-1.0; GWT/Guava etc. have other licenses such as Apache. Summarize original notices and provide corresponding version source code and build entries |
| ELK source for elkjs | Official 0.9.3 release notes explicitly state it is based on ELK 0.9.1, including ELK #955 patch; the summary file provides tags, patches, and release notes simultaneously. Do not treat `elkjs 0.9.3` as a non-existent `ELK 0.9.3` tag |
| DM Sans / KaTeX fonts | Both retain OFL-1.1; original copyright and Reserved Font Names for KaTeX fonts are taken from the name table of the fonts within the package, and cannot be replaced by the MIT license of KaTeX JavaScript |
| Lucide | Retain ISC, as well as the MIT part for Feather-derived icons |
| khroma | License not filled in manifest, but the original `license` file explicitly states MIT |
| remark-math / rehype-katex | npm packages do not include license text; supplement with original MIT and Junyoung Choi copyright from the same upstream project |
| SILK / WASI SDK 34.0 | Retain complete notices for Skype SILK, wasi-libc options, musl/cloudlibc/musl-fts/dlmalloc, LLVM compiler-rt, etc. SILK terms explicitly do not grant patent rights |
| resvg / Lightning CSS and other build tools | The tools themselves are not included in the release package; using tools does not cause generated artifacts to change to MPL. If tools or development containers are distributed in the future, that artifact needs to be re-inventoried |
| GitHub logo | Used for GitHub project links on the official website, belonging to commonly allowed official uses; retain brand authorization exceptions, do not imply endorsement. Existing links point to `https://github.com/molr-ai/molr`, do not treat it as Molr MIT resource or open source obstacle |
| BusyBox / Bubblewrap / Optional host tools | Current release packages do not carry these binaries; provided by user system, installer can install via apt according to authorization. If containers/rootfs containing these programs are distributed in the future, corresponding source code obligations need to be fulfilled separately |

## Inventory and Maintenance

[license-inventory.json](../../releasing/license-inventory.json) records 337 npm third-party installation location records and 22 Go modules, covering direct, transitive, development, and platform-optional dependencies; the six workspace links are not counted as third-party records. Linux x64 actually installs 301 locations; the other 36 platform-optional build tools are verified against the same version package family and lock file, without individually verifying their binaries. The current Linux amd64 Runtime references 20 of these Go modules; notice collection additionally traverses the actual dependency package directories for amd64/arm64, retaining more specific nested licenses.

Update method:

```bash
npm ci
# GO 可以指定独立 Go 工具链路径，版本仍由 runtime/.go-version 决定。
npm run notices:update
npm run notices:check
npm run check:licenses
```

`notices:update` collects original texts from installed npm dependencies consistent with the lock file, Go modules, and specified Go toolchain, updating the generated section in the summary file; the supplementary section is retained within the same file. Vite/Rolldown notices are also included to cover the runtime helper code they generate; tool executables are not distributed with the product. Packages where original license text is not found will cause an error; the two verified remark-math packages provide text via the supplementary section.

When upgrading dependencies, license changes, additional components, and corresponding source code must be reviewed first, then regenerate the text, update the supplementary section, and update the inventory input summary. Do not just refresh the summary and skip the review. `notices:check` compares original dependency text with the committed generated section; `check:licenses` checks MIT metadata and review input/full notice summaries. The latter is integrated into the release build entry to prevent unreviewed inputs from being directly packaged. These checks do not automatically make legal judgments.

## Build and Installation Delivery

- The three Vite applications share `scripts/license-assets.ts`; both build and development services provide `/LICENSE`, `/THIRD_PARTY_NOTICES.md`, and HTML includes license link metadata. WebUI files are embedded into the Runtime along with the page.
- `scripts/build-release.mjs` copies these two files to the archive root directory, no longer packages BusyBox, and no longer requires `MOLR_BUSYBOX` or `MOLR_BUSYBOX_LICENSE`.
- `molr` and `molr-setup` are static Go programs. Maintenance uses the Go standard library and the existing `golang.org/x/sys` and `github.com/vishvananda/netlink` modules, with no new modules. Host dependencies are not copied into packages.
- The installer reads these two fixed files from the complete release package or source tree, saves them to the installation prefix root directory using atomic writes, with permissions `0644`; missing complete materials will cause an error, not silently ignored. It does not copy arbitrary directories, nor does it change the original file text.
- The installer checks that the system BusyBox must be a static, executable regular file that does not allow group/other user write access. If only a dynamic version is present, it will also prompt to install `busybox-static`. If `--install-deps` is provided, it can be installed via apt; if apt is not supported or offline, users must install system dependencies in advance.
- BusyBox is not copied to Molr's installation directory; the Runtime continues to prepare the isolated environment from standard system locations. If required dependencies are missing, the sandbox is not automatically disabled or Full Access opened.
- Specific deployment commands and post-installation file locations are described in the [Deployment Guide](../../../docs/en/deployment/README.md).

## Terms and Source Basis

- [MIT Standard Text](https://opensource.org/license/mit), [Apache-2.0 §4](https://www.apache.org/licenses/LICENSE-2.0).
- [EPL-2.0](https://www.eclipse.org/legal/epl/epl-v20.html), [EPL-1.0](https://www.eclipse.org/legal/epl-v10.html), [MPL FAQ](https://www.mozilla.org/en-US/MPL/2.0/FAQ/).
- [OFL-1.1](https://openfontlicense.org/open-font-license-official-text/), [BusyBox Distribution Conditions](https://www.busybox.net/license.html), [GitHub Brand Usage](https://brand.github.com/foundations/logo#legal).
- [elkjs 0.9.3 Release Notes](https://github.com/kieler/elkjs/releases/tag/0.9.3), [Fixed Build Configuration](https://github.com/kieler/elkjs/blob/0.9.3/build.gradle).
- [WASI SDK 34](https://github.com/WebAssembly/wasi-sdk/tree/wasi-sdk-34), [Fixed wasi-libc License](https://github.com/WebAssembly/wasi-libc/blob/2e6fb9d8ee0cdf9e431fbcabe8af3115de000a13/LICENSE).

The scope of this review is copyright licensing and material delivery under the current composition, not a legal determination of patents, trademark registration, or ownership of all source files; new target platforms, dependency versions, copying/linking methods, and external distribution artifacts require re-review.

2026-10-11 Verification of version checks and release manifest adjustments: No new third-party dependencies were added; the release package and installer continue to deliver the original `LICENSE` and `THIRD_PARTY_NOTICES.md`, and static BusyBox is still provided by the target system. The release recommendation list uses only Node.js standard library to handle version metadata; the notice copying scope for build and installation scripts remains unchanged. Corresponding input summaries have been updated.

2026-10-12 Verification of documentation directory reorganization: The inventory and this description have been moved to `dev-docs/releasing/`; the notice file only updates the inventory reference path; dependency versions, original licenses, and NOTICE text remain unchanged. Path checks and notice file summaries have been updated synchronously.
