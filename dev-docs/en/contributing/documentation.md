# Documentation Maintenance Conventions

Public documentation describes the current implementation, limitations, and reproducible verification methods. When adding or modifying features, synchronously update user operations and the development documentation responsible for that topic.

## Directory and Content Ownership

| Location | Maintenance Content |
| --- | --- |
| `docs/guides/` | Operations, results, and feature boundaries for regular users |
| `docs/best-practices/` | Scenario guides for tool setup, example requests, and verification |
| `docs/administration/` | Administrator configuration of resources, user groups, and plugins |
| `docs/deployment/` | Host installation, configuration, dependencies, services, and disaster recovery |
| `dev-docs/architecture/` | System composition, module responsibilities, and technical choices |
| `dev-docs/runtime/` | Execution, context, memory, and model mechanisms |
| `dev-docs/security/` | Authentication, authorization, quotas, and isolation mechanisms |
| `dev-docs/reference/` | Invocation contracts for HTTP, modules, and model tools |
| `dev-docs/extensions/` | Plugin and channel extension protocols |
| `dev-docs/frontend/` | Frontend architecture, themes, branding, design, and interaction conventions |
| `dev-docs/releasing/` | Releases, public content review, and licensing |
| App and Package READMEs | Local startup, testing, and code entry points |
| `internal-docs/` (locally ignored) | Planning, research, historical proposals, one-off operations, and private acceptance records |

When a topic has multiple audiences, user documentation maintains operations and results, reference documentation maintains fields, and specialized development documentation maintains mechanisms. Use links to associate content, avoiding duplication of interface lists, default values, and complete feature lists. Component APIs are maintained by `packages/ui/COMPONENTS.md`.

Files use lowercase hyphenated naming, and directory entries use `README.md`. Split content by reader task; retain a single file when content is minimal and responsibilities are consistent; do not create a directory for every feature. Directly replace outdated body text; do not append historical correction records. Save detailed acceptance results in internal directories, and note the date for test results.

## Indexing and Official Website Publishing

The root README guides reading by task. `apps/website/src/docs-routes.ts` is the explicit list of reviewed documentation for the official website, maintaining URLs, groups, and reading order. New files are not automatically published; repository-specific development collaboration standards may remain unregistered on the official website.

Chinese body text remains in the existing directory. English body text is placed separately in `docs/en/` and `dev-docs/en/`, maintaining the same relative filenames and section hierarchy. Each registered entry on the official website publishes `/zh/docs/…`, `/en/docs/…`, or the corresponding `dev-docs` path; the build fails if any language file is missing. When modifying public body text, synchronously maintain both languages, section correspondence, and relative links, keeping code and commands consistent. The URL determines the document language; switching languages jumps to the corresponding section of the same document, and search and previous/next navigation use only the current language.

When moving, splitting, or merging, synchronously modify relative links in the body, section anchors, app and package READMEs, references in code, and the `AGENTS.md` mandatory entry. The official website directory structure must remain consistent with physical categorization; internal materials must not register routes, import pages, or become dependencies for public reading.

After completion, run `npm run test:website`, `npm run build:website`, `npm run check:release`, and `npm run check:licenses` to check navigation, search, previous/next links, deep links, and the public build scope. See [Development Collaboration Standards](../../contributing/development.md#公开内容与内部资料) for publishing review requirements.

## Documentation Expression Standards

Documentation organizes content around the information readers currently need to understand and the operations they need to complete. Prohibit meaningless emphasis on unimportant content, including repetitive reminders, deliberate contrasts, generalized disclaimers, and setting up prominent paragraphs, bold text, or warning boxes for secondary information.

- Emphasized content must affect current operations, key decisions, or result understanding, and specify concrete conditions and consequences; common sense, obvious prerequisites, and limitations unrelated to the current topic are not emphasized separately.
- Necessary capability boundaries, configuration prerequisites, and risks are written near the corresponding feature or operation description, using direct and specific expressions; overviews and READMEs do not repeat details already present in specialized documentation.
- Delete reminders and modifiers that do not add effective information; do not artificially amplify secondary content with phrases like "Important", "Pay special attention", or "This does not mean". During review, check whether the expression helps readers take action or avoid specific misunderstandings; if it has no effect, delete it.

## Handling Documentation Conflicts

Current user operations use the product guide as the entry point, and implementation constraints are verified against the corresponding code and tests; when inconsistencies are found, correct the documentation rather than defaulting to the priority of an older scheme. Internal requirements and research do not constitute claims of implementation, nor are they dependencies that external contributors must obtain. Do not copy interface lists, configuration defaults, and complete feature lists into multiple overviews.
