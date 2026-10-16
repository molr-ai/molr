# Use Weibo

The official [Weibo CLI](https://open.weibo.com/cli/quickstart) lets your assistant retrieve posts, read comments, publish posts, and reply to comments. The assistant runs `weibo-cli` through Shell; available operations depend on account authorization and the services you have activated.

## Install and Authorize

Complete the [environment setup](README.md#prepare-the-execution-environment), then send:

> [!PROMPT]
> Follow https://open.weibo.com/cli/quickstart to install @weibo-ai/weibo-cli and its dependencies in your workspace. Check the version and available commands, then guide me through developer verification, service activation, and device-code authorization. Send any links I need to open. Start by reading the current account's user information to verify access.

The official npm package is `@weibo-ai/weibo-cli`, requiring Node.js 18 or later and npm. Set the installation prefix to a workspace directory, for example:

```bash
npm install --global --prefix /work/.local @weibo-ai/weibo-cli
/work/.local/bin/weibo-cli version
```

The examples below use `weibo-cli` for the installed program; use `/work/.local/bin/weibo-cli` if it is not on PATH. Node.js must also be available for every invocation; do not rely solely on an interactive terminal's PATH. Keep configuration and login credentials inside the assistant's workspace and check the current CLI's help for their actual storage location.

Complete developer facial verification through the Weibo mobile app on the [open platform](https://open.weibo.com/cli), then activate a trial or purchase a credit package. Check the console for current service coverage, API prices, and quotas. Trial access covers only some capabilities, and queries can also consume quota.

The Molr sandbox and your browser typically run on different computers. Use device-code login:

```bash
weibo-cli auth login --device
weibo-cli doctor
weibo-cli me --output table
```

Open the official authorization link returned by the CLI in your browser, enter the device code as instructed, and confirm the account. Have the assistant check verification and service status afterward. Do not paste access or refresh tokens into the conversation.

## Example Requests

> [!PROMPT]
> Search Weibo for this week's discussions about the product I specify. Summarize a small selection of representative feedback, retaining authors, timestamps, and original post links.

<!-- -->

> [!PROMPT]
> Read the ten most recent comments on the Weibo post I specify, group the questions, and draft replies. Show me the target comments and complete replies for confirmation first.

<!-- -->

> [!PROMPT]
> Draft a Weibo post from this product update. Show me the account, text, and images for confirmation before publishing, then return the post ID and link.

Specify the target account, post links, time range, and quantity. When publishing AI-generated content, declare that attribute using the current publishing command's parameters. For recurring summaries or scheduled posts, verify the corresponding operation once before arranging it through [Plans & Follow-up](../guides/plans.md).

## Verify and Troubleshoot

Inspect the commands available to the current account and their parameters, then make one read-only query:

```bash
weibo-cli commands list --available
weibo-cli commands show users show/biz
weibo-cli users show/biz --output json
```

Confirm that the returned user information belongs to the intended account. A successful login or `doctor` check does not replace an actual API query. Check command availability and required parameters separately for search, comments, and publishing.

| Symptom | What to check |
| --- | --- |
| CLI or Node.js cannot be found | Check the workspace installation path and the current Shell's PATH; use the CLI's absolute path |
| Calls fail after login | Run `doctor` to check developer verification, service activation, and quota, then confirm that the target command is available |
| Authorization has expired or been revoked | Repeat device-code login and confirm the intended account |
| Rate limit or insufficient credits | Follow the platform's retry guidance or manage credits in the console; reduce query scope and frequency |
| Publishing or replying times out | Retain any returned post or comment ID and verify the outcome before retrying; do not resend while the result is unknown |

After upgrading, check usage with `weibo-cli version`, `weibo-cli --help`, and `commands show`. To disconnect, revoke the session in the open platform's authorization list and remove the related workspace credentials. See the [official manual](https://open.weibo.com/cli/quickstart) for complete installation and service details.
