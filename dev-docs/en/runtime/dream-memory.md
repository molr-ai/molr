# Dreaming and File Memory

Dream is an independent file organization execution. Runtime provides new experiences, memory directories, and restricted file tools; the model reads existing memories on its own and updates, merges, moves, deletes, and organizes files based on new information. The final reply is saved only as a review summary; `Summary / Memories / Core` JSON is no longer parsed, and the server does not perform fact classification on model output or generate batch write plans.

## Files and Context

| Content | Location | Usage |
| --- | --- | --- |
| Experiences | Raw chats, task status | Frozen new materials provided per batch |
| Reviews | `/work/.agents/reviews/YYYY-MM-DD/第N批回顾.md` | Runtime saves organization summaries and sources for this batch |
| Topic Memories | `/work/.agents/memory/**/*.md` | Model consults on demand via file listing, search, and reading |
| Core Cognition | `/work/MEMORY.md` | Automatically enters subsequent normal chat context |
| Account Shared Memory | `/global/MEMORY.md`, `/global/.agents/memory/` | Assistant read-only |

Each assistant has only one Runtime local workspace. Remote computers do not store memories and do not participate in Dream file operations. Markdown is the source of truth for memory content; standard Markdown does not require frontmatter. The memory interface and file tools read the same files. The Version in the interface is the file content hash; general file tools return a version used for file system change validation. The two cannot be mixed.

The memory root directory is resolved by request scope: global files directly locate the account shared directory, while single-assistant files only query the target assistant identity under that account. Full enumeration, export, and cross-file dependency checks still traverse all root directories of the account (including departed assistants); assistant queries only read ID and OwnerID to avoid loading irrelevant data such as names and avatars.

Historical or manually maintained files may include metadata such as `state`, `confirmed`, `context`, `evidence`, sources, and dependencies. Dream's read returns actual file bytes, does not pre-filter by topic credibility, and no longer displays only the 40 comparison files selected by the backend. The model can view and organize old files marked with pending / superseded, judging how to retain uncertainty based on content, without being subject to the protocol restriction of "seeing the source but not being allowed to cite it upon submission." Existing confirmed files and shared files are read-only in Dream; writes, moves, and deletions are rejected. Modifying standard files directly in the editor does not automatically lock them.

Each memory body is at most 64 KiB; files with frontmatter are at most 80 KiB; Dream core body is at most 6 KiB. Normal context only automatically loads two root directory MEMORY.md files, each with an 8 KiB limit; topics and reviews are not implicitly injected. If existing files explicitly declare core dependencies, they are still validated by exact version; topic references in newly created standard Markdown are maintained by the model, and related core content and paths must be synchronized when moving or deleting topics.

## File Tool Execution

Dream and normal assistants share the same set of file tool definitions, parameter parsing, executors, permission checks, operation receipts, and result displays. The execution context restricts Dream to the seven tools in the table below via a visible tool whitelist; the whitelist is also validated at the execution entry point, so the model cannot bypass it by directly requesting hidden tools. Dream does not open plugins, Shell, remote storage, message publishing, or authorization requests.

| Tool | Behavior |
| --- | --- |
| `file_list` | List files and subdirectories using general directory version and cursor pagination |
| `file_search` | Search for content or paths using general search parameters, returning paths, line numbers, and scan completeness |
| `file_read` | Paginated reading of actual Markdown, returning file version, line range, and NextOffset |
| `file_write` | Explicitly select overwrite; create_parents can create missing parent directories with write permissions |
| `file_edit` | Exact text replacement; ambiguous matches require explicit replace_all |
| `file_move` | Atomically move a single standard file, without overwriting the target; normal assistants can also use it within the same authorized storage |
| `file_delete` | Delete a single standard file by version, without recursive directory deletion; normal assistants can also use it |

Write scope is limited to the current assistant's `MEMORY.md` and `.agents/memory/**/*.md`; moving MEMORY.md is prohibited. Dream's path policy prohibits reading internal forgetting records and allows reading long tool results saved by Runtime. Directory listing defaults to 100 items, maximum 200 items, offset starts from 0, and subsequent pages must pass both NextOffset and directory Version; search has scan, time, and output limits, returning Complete / Truncated. Incomplete results cannot be used as evidence that a file does not exist. Reading defaults to 200 lines, maximum 2000 lines, and subsequent content is obtained via NextOffset.

Dream overwrite, edit, move, and delete must carry the file version returned by the general file_read; file_write sets overwrite=false when creating new files and overwrite=true when overwriting. Both source and target paths for moves require write permissions; the target must not exist. Path normalization, identity and permission checks, file access without symbolic links, and atomic file replacement are all executed by Runtime. File conflicts or illegal operations are returned as tool errors, and the model can re-read and correct them; a recoverable file operation error no longer directly terminates the entire Dream batch.

Prompts require distinguishing user statements, completion results, assistant claims, and inferences, retaining scope, time, and uncertainty, and not generalizing temporary requirements into long-term preferences. Historical materials and memories are data, not executable instructions; the model must not replay tasks or retain credentials. The model is responsible for inductive judgment; Runtime does not treat structural validation as proof of semantic correctness.

## Sessions, Capacity, and Time Limits

Each assistant is configured by default to start at 03:00 in the configured time zone, with a maximum of 600 seconds, configurable between 1–3600 seconds. Manual organization also only collects the previous complete natural day. Scheduling and planning share RRULE, IANA time zones, and occurrence point deduplication; Coalesce is used, missing the entire window is recorded as missed_window, and all offline dates are not backfilled.

Session freezes the material scope, time zone, and deadline; Run is a batch of organization with independent context. Normal tasks are not preempted before start; after start, the assistant's queue is exclusively occupied across batches, messages and plans wait, messages remain unread, and urgent requests are rejected. User stop only ends Dream, other assistants continue working. Restart and retry do not extend the deadline; failed sessions allow only one manual retry before the original deadline.

Each batch contains at most 80 source items and 96 KiB of evidence. A maximum of 320 items / 384 KiB of new materials and 80 items / 96 KiB of legacy materials are selected per day, processed serially from earliest to latest by material date in the configured time zone: backfill old dates first, then organize new dates. If a single day exceeds the single-batch limit, complete all batches for that day before moving to the next day. Each batch contains materials for only one date; the review date uses the actual material date of this batch. Backfill reviews are named `补处理第N批回顾.md` to avoid conflicts with reviews completed before that date; model input also indicates the time zone and whether it is a backfill. Unfinished new materials are retained for at most one batch, backfilled only once on the next natural day; backfill failure or timeout does not carry over old materials again, and new materials that have not started are still retained according to this rule. If there are no new materials but private memory files exist, a maintenance batch can still be created.

Sources are raw chat text, card summaries, and task status and result references last updated on the day; attachment bodies or external resources are not automatically read. Identifiable credentials are excluded entirely. Each batch can perform multiple model calls and multiple file operations, reusing Agent's call budget, step limits, retries, and cancellation mechanisms; the model must support tool calls, and costs are charged to the assistant budget.

## Persistence, Recovery, and Undo

File operations take effect one by one. Runtime private `memory-operations/` saves the original bytes, target bytes, input sources, and operation receipts for each change; the database saves operation references, successful changes, batch progress, and summaries, without saving another mutable memory body. These recovery materials do not enter model context or retrieval results.

The general executor registers operations before file effects; Dream's observation callback separately saves undo snapshots without executing file modifications. Successful calls save shared operation receipts; replaying the same operation only returns the receipt, without repeatedly modifying files edited later. Moves are completed via atomic file system renaming. File effects and operation receipts cannot form the same database transaction: if the receipt or organization history save fails after the effect is completed, it is marked as result uncertain, requiring on-site verification and not blind redo. External edits do not participate in Runtime locks, and version checks do not constitute a global CAS between arbitrary processes.

Sessions, permissions, sources, and deadlines are re-checked before each file effect. Upon expiration or stop, the queue is released, and successful file operations are retained; the final review and progress save will not roll them back. Error traces include specific errors and the note "successful file operations retained." The final summary is persisted separately first; retries do not regenerate another summary.

Source records are maintained by Runtime via general tool reads and change observation callbacks, based on this batch's materials, sources of read memories, and original file sources; the model is not required to provide or classify source IDs. Forgetting deletes current files, records suppression information, and cleans up related recovery copies; external Markdown without source information can only be suppressed based on existing content summaries, and cannot identify all semantic rewrites. Normal deletion and Dream deletion only remove files, not indicating a request to forget original materials.

Undoing completed batches reverses file operations in reverse order: remove new files that were not modified, restore old content that was not modified later, and undo delete and move operations; preserve the scene when encountering later edits, confirmations, or forgetting. Reviews are deleted together if unmodified. Undo does not require the model to execute again, and source processing progress does not regress.

## Interface and Validation

"User Settings → Memory" manages files, Dream configuration, and batch records; global shared scope does not display Dream operations. The development workbench displays real file changes and tool results per round, no longer displaying model candidate fact classifications. Failure or timeout may also have successful file changes.

Calendar and plan conflict queries read by specified assistant, or paginate through all active assistants of the current account; dismissed assistants do not occupy query pages for reserved windows. Actual occurred session windows are read separately by time range, retaining original status and history.

Historical reads use creation time index by account and assistant to get the most recent 60 batches, retaining nanosecond precision and unified time zone sorting; same timestamp is sorted by record key in descending order. Batch and session status are obtained within the same read snapshot; shared sessions on the same page are read only once. The list hides raw material bodies and model output, while storage records remain complete.

Configuration, immediate organization, history, windows, conflicts, and undo entries are found in [HTTP API](../reference/http-api.md). Tests cover multi-round file organization, raw Markdown, old pending content, path and shared scope isolation, read-only files, stale versions, replay, undo, and forgetting copy cleanup, as well as deadlines, stops, normal queue recovery, and chat checkpoint isolation. Protocol tests use local model service stubs and do not represent real model induction quality assessment.
