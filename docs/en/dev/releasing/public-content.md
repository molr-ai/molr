# Open-Source Release and Private File Isolation

This specification applies to source code archives, Linux distribution packages, and the three frontend applications. For license scope and maintenance of the complete third-party notices, see [Licenses and Distribution](licensing.md). This document owns everyday public-content and internal-material rules, distribution checks, and history review.

## Public Content and Internal Materials

Maintain this repository for public distribution. Review actual content and the staging area before committing; these rules cover code, comments, test fixtures, images, attachments, documentation, and generated files:

- Never commit real credentials, passwords, private keys, tokens, one-time login links, databases, logs, backups, customer or personal materials, or actual deployment configuration. Examples must use synthetic data, relative paths, and explicit address placeholders. Renaming a file or changing its extension does not bypass these rules.
- Do not commit code, fonts, or assets of unknown origin, without distribution rights, or without license review. Preserve original third-party copyrights, licenses, and required NOTICE text according to [licenses and distribution](licensing.md); these are not personal information to remove.
- `docs/user/` serves ordinary users, `docs/admin/` administrators and operators, and `docs/dev/` public contributors. They describe current implementation, limitations, and reproducible verification. Internal product goals, commercial or competitive assessments, undecided roadmaps, discussion transcripts, and acceptance records containing local environment details belong in repository-root `internal-docs/`. Create it when needed. The whole directory is Git-ignored and is not a dependency of public documentation.
- When deriving public explanations from internal materials, verify implementation again and remove personal data, deployment details, and internal decision history. Public documents must not link to or depend on internal files. The website loads only reviewed routed documents; do not bypass scope through directory globs, raw imports, symlinks, or copies into `public/`.
- Before committing, inspect `git status --short`, `git diff --cached --stat`, and `git diff --cached`, and run `npm run check` for public contract manifests, bundled documentation, platform manifests, public content, and licensing. Document route or release workflow changes also require relevant builds and tests. Never force-add internal directories with `git add -f` or remove blocking rules to pass checks.
- Ignore rules do not remove tracked files or historical objects. After moving committed internal materials, verify the original files are deleted in the staging area and review historical public scope separately. Revoke or rotate leaked credentials before addressing history; moving them into an ignored directory does not resolve a leak.
- Before cleaning up a worktree, preserve valuable untracked materials such as `internal-docs/`. During merging, transfer them to the main workspace's corresponding ignored directory or private storage designated by the maintainer, verify digests, and retain both copies when names match but content differs. Git does not merge these files; do not lose them during worktree cleanup.

Release checks prevent common accidental commits; they do not replace content and provenance review. Deliver temporary acceptance URLs in the handoff message, and keep accounts, links, and detailed records only in local ignored directories. Maintainers control Git commit identity; do not rewrite history or change global identity configuration without authorization.

## Local Files and Git

Runtime databases, passwords, master keys, login links, actual deployment configurations, logs, caches, and backups should be placed in private directories ignored by Git, such as `.cache/` in the task worktree or a separate runtime directory outside the repository.

`.gitignore` covers common databases and WAL/SHM files, private key/certificate containers, initial passwords, saved login links, environment files, backups, and local configurations. `runtime/configs/` retains only `*.example.json`; `.env.example` and `.env.*.example` may be committed, but their content must still use example values. Do not rename real credentials to example file names just to pass checks.

Ignore rules do not remove files already committed or staged from Git history or the staging area. Run:

```bash
npm run check:release
npm run test:release
```

`check:release` checks raw Git objects in the staging area and tracked, non-ignored files in the working directory separately. It can detect common private files forcibly added via `git add -f` and is not bypassed by states where "keys remain in the staging area while working files have been cleaned." Ignored local runtime directories are not opened, copied, or automatically deleted; when investigating historical leaks, check history separately rather than modifying ignore rules and declaring cleanup complete.

Check errors output only paths and issue categories, not the matched credential bodies.

## Internal Documentation

The `internal-docs/` directory in the repository root stores non-public requirements, research, and local acceptance records, and is entirely ignored by Git. Release checks reject this directory in the staging area, specified source commits, and frontend outputs; it cannot be bypassed by force-adding. Public builds must be completed in a fresh checkout or source snapshot without this directory.

Official website documentation reads only from public Markdown files registered in `docs-routes.json`; unregistered files do not automatically enter client packages. Adding a route means publishing the full text; content must be reviewed before addition. Pointing to internal directories or reading them via symbolic links is prohibited.

Moving committed materials into ignored directories only changes the current file tree; it does not delete old history. Before making an old repository public, its reachable history must still be audited. Before cleaning up worktrees, preserve local internal materials according to [public content and internal materials](#public-content-and-internal-materials).

## Public Static Assets

Vite's `public` directory is copied in its entirety; Git ignore rules do not control this step. Publicly shareable raw static assets are explicitly listed in `publicFiles` within `scripts/release-policy.json`; new files must first be confirmed as suitable for public release before updating the manifest. The current manifest includes shared brand assets and two initial screen initialization scripts.

The three applications check the public directory when loading configuration via `scripts/release-assets.ts`: files not listed in the manifest, symbolic links, database content, and obvious key formats will cause the build to fail. After frontend output is written, it is scanned again; existing artifacts are also checked before the preview service starts. Directories after a build failure cannot be used for release; do not use `public` as a download, log, or temporary screenshot directory.

When the official website distributes installation packages via `MOLR_RELEASE_DIR`, it also validates the selected archives, checksum files, and download manifests: filenames matching rules does not mean the content is allowed to be public. Other files in the input directory that are not selected are not read or copied; if extra files are mixed into the final web download directory, acceptance is rejected.

## Source Code Archives

First review and commit the version to be made public, then run:

```bash
npm run build:source -- --ref HEAD
# 或选择已审查的明确提交：
# npm run build:source -- --ref <commit>
```

Output is located in `dist/sources/`, including `molr_source_<commit>.tar.gz` and SHA-256 files. The command checks all files and content of the selected commit, then exports via `git archive`; subsequently, it verifies that each archive file matches the corresponding Git blob. Symbolic links, submodules, common private paths, and obvious credentials will block export. `export-subst` that changes content or `export-ignore` that hides tracked files will not be silently accepted.

**Source packages contain only the selected commit.** Staging area changes, working directory modifications, uncommitted new files, and ignored runtime data are not included; it also does not include the `.git` database. Do not compress the entire working directory and treat it as a source package. Release builds `build:release` require a Git checkout to record the exact build commit; from source packages not containing `.git`, Go Runtime and frontend can be built separately according to the deployment guide.

This command does not change remote repository visibility, does not clean history, and does not push code; setting an existing Git repository to public will still expose its reachable history, which requires separate auditing.

## Linux Distribution Packages

`npm run build:release` runs source checks and license checks before building, then generates the WebUI, and builds the Runtime using `CGO_ENABLED=0` and `-trimpath`. `-trimpath` prevents the compiler from writing absolute paths of the local working directory and module cache into artifacts; it does not clean text written into source code itself.

When packaging, only files explicitly listed in `scripts/release-policy.json` are passed:

- `LICENSE`
- `THIRD_PARTY_NOTICES.md`
- `bin/molr`
- `bin/molr-setup`
- `release.json`

Working directories or staging directories are not packaged recursively. Generated temporary archives must pass member type, explicit manifest, duplicate path, content, and metadata checks before atomically replacing the final archive and generating checksums and download manifests. BusyBox, runtime configurations, databases, keys, and acceptance data are not part of the distribution package.

Existing artifacts can be checked independently:

```bash
python3 scripts/release_safety.py release dist/releases/molr_0.1.0-local_linux_amd64.tar.gz
python3 scripts/release_safety.py release-directory dist/releases
python3 scripts/release_safety.py frontend apps/webui/dist
```

No files are extracted or executed when checking archives. Symbolic links, hard links, out-of-bounds paths, extra files, missing required files, abnormal release metadata, or mismatched checksums in the archive will be rejected.

## Personal Information and History

Public documentation uses repository-relative paths, generic account descriptions, and explicitly labeled address placeholders. Actual working directories, acceptance addresses, accounts, and logs are saved only in ignored local acceptance directories; historical acceptance records retain test results but not machine connection information. Private address ranges in network classifications, manual test data, product domains, and third-party original copyrights and contact information are not personal data requiring batch replacement.

New commits or `.mailmap` do not delete identities or file content in old commit objects. Source archives do not contain history; directly making the original repository public will expose the corresponding reachable history. When old information needs to be removed, first determine the release method and scope, then process in an independent copy; do not directly rewrite shared repositories, force push, or automatically delete old branches.

## Verification and Scope

Changes to installation directories, installers, or uninstallers also require running:

```bash
cd runtime
CGO_ENABLED=0 go test ./internal/installation ./internal/updates
cd ..
python3 scripts/test-install-download.py
```

Installation/uninstallation tests use temporary directories and mock service managers, covering directory isolation, data retention during upgrade/reinstallation, deletion confirmation, and protection of external directories and symbolic links. Real service acceptance should use independent instances, verifying reinstallation after default uninstallation, thorough cleanup, and authenticated sandbox capabilities; system-level installation also requires a privileged isolated environment.

When modifying the release process, run `npm run test:release`, `npm run check:release`, and the corresponding application builds; verify that rejected files indeed did not enter the artifacts, and that source packages match the selected commit. Specific results, commits, and artifact summaries are saved in local acceptance records.

Source and archive checks use the Python 3 standard library; channel directories additionally call shared Node.js schema validation scripts, with no new third-party dependencies. Tests use manual sentinels, temporary Git repositories, and temporary archives, and do not load real user credentials. Verified cases include force-adding ignored files, staging area/working directory differences, files mixed into static directories, SQLite disguised with extensions, archive links and path traversal, and download manifest and checksum anomalies.

Key detection covers only high-confidence formats, such as PEM private keys, some vendor tokens, and complete Molr one-time login links; it cannot identify all passwords, encoded/encrypted content, or personal data. Allowed images and documents still require manual review; passing checks should not be interpreted as "absolutely no sensitive content." Decisions on making historical commits, account personal information, and internal documents public are not automatically handled by this script.
