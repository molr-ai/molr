# Use Codex

Your assistant can delegate code reading, file changes, and tests to [Codex CLI](https://developers.openai.com/codex/cli/), then inspect the changes and test results. This uses Molr's existing Shell, background tasks, and file tools.

## Installation and Authorization

Complete [execution environment preparation](README.md#prepare-the-execution-environment), then send this request:

> [!PROMPT]
> Follow the official Codex instructions to install the appropriate CLI inside your /work. Check its version, dependencies, and network access, keep its configuration separate, and guide me through login. First verify reading files, changing a file, and running tests in a small synthetic project, then report the results.

Use an official release binary, or install `@openai/codex` into the workspace when Node.js and npm are available. Software in the host's personal directories is not automatically available inside the sandbox. The commands below use `codex` for the installed executable; use its absolute path when it is not on PATH, and prepare the required PATH explicitly for each non-interactive call.

```bash
codex --version
codex exec --help
codex login --device-auth
codex login status
```

Complete authorization in your browser. Alternatively, follow the [official authentication guide](https://developers.openai.com/codex/auth/) to use an API key or configure a supported model service. Keep keys out of chat, task instructions, and shared scripts. In an ordinary Linux sandbox, HOME is `/work`, so the default configuration and sessions live in `/work/.codex`. Keep this directory separate from project deliverables and retain it between calls. Credentials stored in the workspace are readable by that assistant and programs it runs.

Codex accounts, model configuration, and usage are separate from the Molr assistant's model connection. External CLI model costs are not automatically included in Molr model usage. A local service must support the Responses protocol required by Codex. To reach a host loopback service from the Linux sandbox, use `host.molr.internal` and grant the relevant network resource; `localhost` refers to the sandbox itself.

## Execute Inside the Molr Sandbox

First confirm that the selected Linux sandbox has working command execution, file, process, and network isolation, and resource limits. Grant the compute, network, and directory access needed by the task. The automation examples below require ordinary isolated Shell execution, with `full_access` omitted or false.

Codex's own `workspace-write` sandbox may fail to create nested namespaces inside Molr. The verified Linux invocation uses `--sandbox danger-full-access`, relying on the outer Molr sandbox. This disables Codex's inner isolation; its child processes remain subject to Molr mounts, network grants, and resource limits. Do not copy these commands into host execution, a node with isolation disabled, or Molr Full Access. The working directory also does not prevent access to other files visible to the same assistant.

Have the assistant prepare a separate project and task instructions specifying editable files, the test command, and acceptance criteria. For example, save the task to `/work/outputs/codex/task.txt`, then run:

```bash
mkdir -p /work/outputs/codex
codex exec --sandbox danger-full-access --json \
  -C /work/projects/demo \
  -o /work/outputs/codex/result.txt \
  - < /work/outputs/codex/task.txt \
  > /work/outputs/codex/events.jsonl \
  2> /work/outputs/codex/stderr.txt
```

Initialize the project as a Git repository. If the task deliberately does not use Git, consult the official instructions for `--skip-git-repo-check`. Set `background: true` and a suitable `timeout_seconds` on `shell_execute`. The assistant can read the result when the completion event arrives, or use `shell_task` to inspect output or stop execution. There is no need to detach the process using `nohup` or `&`.

`events.jsonl` contains structured events, `result.txt` holds the final response, and `stderr.txt` provides diagnostics. Check the CLI exit status and actual files or Git diff, and rerun tests independently. A completion claim alone is insufficient. See [non-interactive mode](https://developers.openai.com/codex/noninteractive/).

## Continue the Same Task

Save the `thread_id` from the JSONL `thread.started` event. Continue using the same assistant, configuration directory, and project:

```bash
cd /work/projects/demo
codex exec --sandbox danger-full-access resume SESSION_ID --json \
  'Review the changes, add any missing boundary tests, and run the tests.' \
  > /work/outputs/codex/follow-up.jsonl \
  2> /work/outputs/codex/follow-up-stderr.txt
```

Replace `SESSION_ID` with the exact ID for that task. Use separate directories or worktrees for concurrent tasks to avoid two processes changing the same files. Inspect the previous execution's files and state before continuing; stopping or timing out does not roll back completed changes.

## Example Requests

> [!PROMPT]
> Use Codex to read this repository and explain its login flow and related tests. Analyze only, and cite the relevant file locations.

<!-- -->

> [!PROMPT]
> Have Codex fix this failing test in a separate worktree. Give it the reproduction steps and acceptance criteria. When it finishes, inspect the diff, rerun the tests yourself, and deliver a change summary.

## Verification and Troubleshooting

Start with a small project covering reading, editing, testing, background completion, and result retrieval. Then verify continuation by session ID. Linux sandbox execution has been verified with Codex CLI 0.163.0 and a local model supporting tool calls. Other operating systems, releases, and account login methods require validation in their actual environment.

For namespace errors, check whether Codex's inner sandbox is enabled and whether outer Molr isolation meets the requirements above; do not change host security settings to force nesting. For authentication errors, check CLI login and configuration storage. For network errors, check the model endpoint and grants. If the CLI exits successfully but the deliverable fails validation, use the observed failure as the next task input. Check CLI dependencies, project dependencies, and test tools separately.
