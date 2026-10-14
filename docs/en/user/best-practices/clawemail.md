# Use ClawEmail

[NetEase ClawEmail](https://claw.163.com/projects/doc/) gives your assistant a dedicated mailbox for finding messages, reading content, downloading attachments, and sending email. In Molr, we recommend the official CLI (`mail-cli`), which the assistant runs through Shell to carry out your requests.

## Installation and Authorization

Complete the [execution environment setup](README.md#prepare-the-execution-environment), create an Agent mailbox on the [ClawEmail website](https://claw.163.com/), then follow the “Mail-CLI” section of the official documentation. You can ask:

> [!PROMPT]
> Follow the official Mail-CLI documentation at https://claw.163.com/projects/doc/ to install mail-cli and its dependencies in your workspace and connect to ClawEmail through the CLI. Check the Node.js version and available commands, guide me through API Key setup and mailbox login, then retrieve a few messages to verify access.

The official CLI requires Node.js 18 or later. Use the installation source and steps currently provided by the official website, keeping the program and configuration inside `/work`. Use `--config <path>` to specify a configuration file and `--profile <name>` to select a mailbox.

Claw mailbox operations require an API Key. The official flow configures it with `auth apikey set`, then signs in with `auth login`. The documentation stores the Key in the system keychain; if the sandbox has no usable keychain, check whether the current CLI supports an appropriate credential method before continuing setup.

## Example Requests

> [!PROMPT]
> Review the ten most recent unread messages in my ClawEmail inbox. Group summaries and action items by topic, retaining the sender, time, and message ID.

<!-- -->

> [!PROMPT]
> Search the specified ClawEmail mailbox for this week's project reports, download relevant attachments to the workspace, and summarize progress and blockers with references to the source messages.

<!-- -->

> [!PROMPT]
> Draft a reply to the message I specify. Show me the recipients, subject, body, and attachments for confirmation before sending it through ClawEmail.

Specify the mailbox, time range, and number of messages. For recurring summaries, verify a single query first, then schedule execution through [Plans & Follow-up](../guides/plans.md).

## Verification and Troubleshooting

After installation and configuration, check authentication and folders:

```bash
mail-cli auth test
mail-cli folder list --json
```

Confirm the actual inbox ID before querying it; this example uses `1`:

```bash
mail-cli mail list --fid 1 --unread --limit 5 --json
```

After authentication passes, a successful query verifies message access. With multiple mailboxes, check the active profile. For authentication failures, check the API Key, login state, and credential storage availability. If external messages cannot arrive, check the mailbox's communication rules and trusted contacts in the ClawEmail dashboard.

Check `mail-cli --help` and the relevant subcommand's help for parameters. See the [official documentation](https://claw.163.com/projects/doc/) for the full capabilities and configuration details.
