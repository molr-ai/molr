# Use ZCode

Your assistant can delegate code analysis, changes, and tests to the CLI from [Z.ai's official ZCode](https://github.com/zai-org/ZCode), then read results, inspect files, and continue the task. Calls use Molr's existing Shell and background tasks.

## Installation and Authorization

Complete [execution environment preparation](README.md#prepare-the-execution-environment), then send this request:

> [!PROMPT]
> Install the appropriate ZCode CLI from an official Z.ai source inside the sandbox. Keep the program, Node.js, and configuration under /work. Check the release and command help, and guide me through connecting a model. Verify code changes and tests in a synthetic project before using my repository.

Use `zai-org/ZCode` or the [ZCode website](https://zcode.z.ai/en/docs/install). Do not install a similarly named third-party tool based only on the name `zcode`. Retain the complete runtime resources from the same release, rather than copying only `zcode.cjs`. Use the Node.js version required by that release.

The verified Linux setup extracts CLI 0.16.9 from the official ZCode 3.15.1 AppImage and uses Node.js 24.14.0. It does not require starting the desktop application or installing into system directories. Have the assistant download the official AppImage for the correct architecture to `/work/tools/zcode/ZCode.AppImage`, then run:

```bash
cd /work/tools/zcode
chmod +x ZCode.AppImage
./ZCode.AppImage --appimage-extract 'resources/glm/*'
./ZCode.AppImage --appimage-extract 'resources/config/provider/*'
mkdir -p agent/provider
cp -R squashfs-root/resources/glm/. agent/
cp squashfs-root/resources/config/provider/zcode-builtin.json agent/provider/
node /work/tools/zcode/agent/zcode.cjs --version
node /work/tools/zcode/agent/zcode.cjs --help
```

Here, `node` should come from the prepared workspace installation. Set the same PATH for every non-interactive invocation so ZCode's child processes can also find Node.js. If a newer release changes the layout, check the package and [official running instructions](https://github.com/zai-org/ZCode/blob/main/README.en.md); do not mix files from different versions.

An ordinary Linux sandbox stores login state, sessions, and user configuration under `/work/.zcode` by default. Keep this separate from project deliverables and retain it between calls. Its credentials are readable by the assistant and programs it runs. To use a platform account, run the appropriate login command in the terminal and complete official authorization in your browser:

```bash
node /work/tools/zcode/agent/zcode.cjs login zai --no-browser
# For a BigModel account, use login bigmodel --no-browser
```

Alternatively, connect your own model service using the Provider configuration format for the installed version. Check the CLI, personal model configuration, and default model together. Molr model connections are not automatically passed to ZCode. Use `host.molr.internal` and the relevant network grant to reach a host loopback model service from a Linux sandbox. External CLI model costs are not automatically included in Molr model usage.

## Delegate Tasks and Read Results

First confirm that the selected Linux sandbox has working command execution, file, process, and network isolation, and resource limits. The following automation example requires ordinary isolated Shell execution, with `full_access` omitted or false.

ZCode's `--mode yolo` allows autonomous ordinary tool operations, with isolation supplied by Molr. Do not copy this command into host execution, a node with isolation disabled, or Molr Full Access. Setting `--cwd` alone does not restrict file access to that directory. For manual approvals or read-only planning, check the installed version's modes and non-interactive behavior first. See the [official permission explanation](https://github.com/zai-org/ZCode/blob/main/NOTICE.md).

Have the assistant run the task in a separate project, for example:

```bash
mkdir -p /work/outputs/zcode
node /work/tools/zcode/agent/zcode.cjs \
  --cwd /work/projects/demo --mode yolo --json \
  --prompt 'Read the project and fix the specified addition function without changing existing tests. Run the tests and report actual changes and results.' \
  > /work/outputs/zcode/result.json \
  2> /work/outputs/zcode/stderr.txt
```

Set `background: true` and a suitable `timeout_seconds` on `shell_execute`, then have the assistant read results after the background completion event. Use `shell_task` to inspect output or stop a running task. Neither `nohup` nor additional backgrounding is necessary.

In CLI 0.16.9, `--json` emits a final JSON object containing `sessionId`, `response`, `usage`, and `projection`. This is not Codex's JSONL event stream, and the file may remain empty while the task runs. Check Molr task status and stderr first. Inspect the exit status and actual files or Git diff, and rerun tests independently.

## Continue the Same Task

Save `sessionId` from the result and continue with the same assistant, project, and configuration:

```bash
node /work/tools/zcode/agent/zcode.cjs \
  --cwd /work/projects/demo --mode yolo --json \
  --resume SESSION_ID \
  --prompt 'Review the changes, add any missing boundary tests, and run the tests.' \
  > /work/outputs/zcode/follow-up.json \
  2> /work/outputs/zcode/follow-up-stderr.txt
```

Replace `SESSION_ID` with the exact ID returned for that task. Use separate directories or worktrees for concurrent tasks. After a timeout, stop, or authentication failure, inspect completed changes before deciding whether to continue.

## Example Requests

> [!PROMPT]
> Have ZCode implement this small feature in a separate worktree. I will provide the editable scope and acceptance criteria. When it finishes, inspect the diff, rerun project tests, and prepare a delivery summary.

<!-- -->

> [!PROMPT]
> Continue the previous ZCode task using its saved session ID. Give it this failing test log to fix, without overwriting any other ongoing work.

## Verification and Troubleshooting

Linux sandbox execution has been verified with CLI 0.16.9 from the official release, Node.js 24.14.0, and a local model supporting tool calls. Browser login with platform accounts, other operating systems, and different releases require validation in their actual environment.

If the built-in Provider configuration is missing, check that `agent/provider/zcode-builtin.json` matches the CLI release. If a custom model reports that no model is selected, check the installed version's personal Provider schema, model list, and default selection. Do not copy configuration from another CLI or a newer repository version unchanged. To diagnose failures, have the assistant read the relevant task log under `/work/.zcode/cli/log/`; avoid sharing complete logs publicly.

When using custom configuration paths, set both `ZCODE_BUILTIN_PROVIDER_CONFIG_FILE` and `ZCODE_PERSONAL_PROVIDER_CONFIG_FILE` according to the version's instructions, and keep them consistent between calls. Check authentication, model endpoints, project dependencies, and network grants separately. Retain command exit status and test results as acceptance evidence.
