# Quick start

Start with a service address and account provided by your administrator. If you need to install Molr yourself, first follow [Set up a Molr instance](../admin/getting-started.md).

On macOS, the desktop connection manager and connected WebUI use native traffic lights at the upper left to close, minimize, and enter fullscreen, with the logo to their right. Closing the window leaves an already running Runtime active.

## Login

The first administrator logs in using the account created during initialization; regular members use accounts created by the administrator. There are no default username/password combinations or public registration entry points. If two-factor authentication is enabled, enter the verification code or recovery code as prompted on the page.

Existing logged-in devices can generate a one-time link via "User Settings → Security → Login Links" for logging in on another device. The link has an expiration time and can only be used once; if MFA is enabled, two-factor authentication is still required. Links should only be given to the individual or explicitly authorized users, and should not be placed in public documents.

## Choose an available model

Your administrator assigns available models and tools through user groups. Choose a model that supports the task and tool calling when hiring an assistant. If no model is available or its capabilities or prices are missing, contact your administrator. For your own service, follow [Model connections](../admin/resources/models.md).

## Hire an Assistant

Set a name for the assistant, a model that supports tool calling, consumption limits, and a daily or weekly reset cycle. Hiring authorizes the selected model; it does not simultaneously authorize all models under that provider or your file directory.

The budget belongs to the assistant. The assistant's chats and all planned executions share the quota; there is no separate task budget. For detailed rules, see [Resources, Permissions, and Budgets](guides/permissions-and-budget.md).

## Assign the First Task

Start with a small task whose results you can verify, such as uploading a document, asking the assistant to list key points and questions to confirm, and pointing out the basis. Specifying inputs, expected results, and originals that should not be changed helps determine whether the task is completed.

Attachment sending, model replies, or tool operations may fail; pay attention to the status and reasons on the page. Normal conversations do not force plan generation; when the model explicitly creates a task, execution is associated with a plan. The actual file types and content sizes that can be processed are limited by model and deployment capabilities.

## View Results and Continue

View related executions and artifacts in the chat and "Plans"; existing deliverables can be downloaded, and task revisions retain historical versions. Receiving a reply does not mean that materials have been modified or tasks have been accepted; verify the actual results and explicit completion status.

If the assistant needs external directories, ask the administrator to register them, then grant the assistant access; if command execution is needed, additionally grant execution permissions for the ready compute environment. For operation paths, see [Assistants, Chats, and Plans](guides/assistants-and-chat.md) and [Files and commands](guides/files-and-commands.md).
