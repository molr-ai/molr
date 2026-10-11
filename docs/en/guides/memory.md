# Memory and Automatic Organization

In "User Settings → Memory", select a sharing scope or an assistant to manage long-term facts and preferences.

## Storage Location and Editing

Memory stores facts, communication preferences, and long-term cognition. Shared memory and assistant-private memory within the same account have different scopes and are not shared across accounts; modifying or deleting memory affects subsequent usage but does not equal deleting the original chat history. Detailed execution context is a separate type of internal state.

Memory is saved as Markdown files in the assistant's local workspace: `/work/MEMORY.md` holds core cognition that is automatically loaded, while `/work/.agents/memory/` holds topic-specific memory for on-demand reference; shared files for the same account are located in the directory corresponding to `/global`. File tools and Shell read the same content, and `rg` can be used for searching. It supports category directories like `用户偏好/沟通偏好.md` and semantic file names. The interface allows creating directories, renaming, moving, and deleting empty directories; creating a file with the same name will not overwrite it. After directly modifying files, refresh the memory page to see the changes; `/global` is read-only for the assistant's normal commands. Remote computers do not store the assistant's workspace or memory.

## Memory Management

The local MEMORY.md core cognition is automatically included in the context with a size limit; topic-specific memory is actively retrieved by the assistant using file search and read tools. When the model organizes topics, it is responsible for synchronizing related core content and file references. Directories serve as categories; after renaming, old paths are no longer valid; current explicit requirements take precedence over historical preferences.

You can view, edit, confirm, delete, or forget memory. Normal deletion only removes files, which may be re-organized from original materials later. Forgetting also records content summaries and known sources, and cleans up related undo copies; for external Markdown without source information, it cannot be guaranteed that all rewrites will be identified. Original chats are not deleted as a result.

Undoing a completed batch of organization restores its file operations in reverse order, including creation, editing, moving, and deletion, provided they were not modified later. Content that was edited, confirmed, or forgotten later is protected; specific retained files are displayed in the batch record.

## Dreaming (Automatic Organization)

Dreaming provides the model with new experiences from the previous complete natural day and file tools. The model searches and reads existing memory on its own, updates, merges, moves, or deletes topic files, and maintains core cognition in `MEMORY.md`; finally, it saves a review of this batch of organization. Organization can still proceed if there are no new experiences but private memory files exist. The model is responsible for judging the scope and reliability of information, should retain uncertainty, and avoid treating assistant claims or temporary requests as long-term facts.

In "User Settings → Memory", select an active assistant to set the daily dreaming time, duration limit, or trigger immediate organization. Global sharing scopes do not display dreaming operations. Dreaming uses the model currently selected by the assistant that supports tool calling; each batch can involve multiple rounds of calls, and usage counts against that assistant's budget. Historical materials are provided as data and are not replayed as new instructions.

The development workbench displays actual file writes, edits, moves, deletions, and tool results for each round. When file operations encounter errors, the model can re-read and correct based on the error. Files saved or confirmed by you in the memory interface are marked as read-only, and shared memory is also read-only; directly editing normal Markdown does not automatically lock the file.

During dreaming, the assistant displays "Dreaming", new messages and plans are queued, messages remain unread, and urgent processing is unavailable. When the time slot ends or is manually stopped, successful file operations are retained even if the current batch is not yet complete. If a single dreaming session involves multiple material dates, they are organized from earliest to latest according to the set time zone, processing older dates first before newer ones; if a single day is split into multiple batches, complete all batches for that day before processing the next day. Reviews are saved according to the date of the material. Unprocessed materials are retried at most once the next day; if the entire reserved time slot is missed, it is skipped, and failed organization can be retried once within the original deadline.
