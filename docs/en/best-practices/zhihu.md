# Use Zhihu

The official `zhihu-cli` from the [Zhihu Data Open Platform](https://developer.zhihu.com/) supports Zhihu and web search, trending topics, Zhihu Direct Answers, and reading the current account's content, follows, and favorites. This guide covers these query capabilities.

## Install and Authorize

Complete the [environment setup](README.md#prepare-the-execution-environment), then open “Zhihu CLI” on the platform website for official installation instructions. You can ask:

> [!PROMPT]
> Follow the official Zhihu CLI and Skill instructions at https://developer.zhihu.com/ to install them in your workspace. Check available commands, guide me through Access Secret configuration, then verify access with a small search.

Generate an Access Secret in the [platform profile](https://developer.zhihu.com/profile). With a desktop keychain, use `auth set --secret-stdin`; a sandbox without a system keychain can use the process environment variable `ZHIHU_ACCESS_SECRET`. Have the assistant follow the official setup instructions, keeping the Secret out of command arguments and output. API calls may consume platform quota; check usage in your profile.

The official Skill installer returns an absolute CLI path. Below, `zhihu-cli` means that executable; have the assistant substitute the returned absolute path if it is not on PATH:

```bash
zhihu-cli capabilities
zhihu-cli auth status
zhihu-cli search zhihu --query '知识管理' --count 3
```

`auth status` checks local configuration; a successful search verifies actual access.

## Example Requests

> [!PROMPT]
> Search Zhihu for personal knowledge management practices. Compare three different approaches and their uses, preserving titles, authors, and original links.

<!-- -->

> [!PROMPT]
> Check Zhihu's trending topics for software development discussions. Explain which deserve further reading and include sources.

<!-- -->

> [!PROMPT]
> Group my ten most recent Zhihu favorites by topic and suggest a reading order.

For comparing views or checking facts, ask for search results with sources. Direct Answers suits a quick synthesis. Specify the type and amount when requesting personal data.

## Verify and Troubleshoot

Personal queries use the account owning the Access Secret. Content summaries are not full text, and recent favorites are not the complete history. Ask the assistant to check pagination support before retrieving more.

For authentication failures, check the Secret's source and validity; for unavailable keychains, check the process environment. Follow platform guidance for quota or rate limits. Check `zhihu-cli <command> --help` for parameters, and `capabilities` and the [official documentation](https://developer.zhihu.com/) for supported operations.
