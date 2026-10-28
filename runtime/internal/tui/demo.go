package tui

import (
	"molr.ai/runtime/internal/agent"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/notification"
	"molr.ai/runtime/internal/policy"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
)

type message struct {
	attachments []core.ID
	read        bool
	id          core.ID
	sequence    uint64
	role        string
	text        string
}

type assistant struct {
	notices                      []notification.Notice
	messageDraft                 *textarea.Model
	messagePastes                []pasteBlock
	pastes                       []pasteBlock
	clearVersion                 uint64
	channel                      string
	attachments                  []draftAttachment
	commandBusy                  bool
	commandSignature, commandKey string
	status                       agent.Status
	id, chat                     core.ID
	sequence, readSequence       uint64
	reading, loaded              bool
	older                        string
	requests                     []policy.PermissionRequest
	pending                      pendingSend
	name                         string
	role                         string
	messages                     []message
	composer                     textarea.Model
	busy                         bool
	generation                   uint64
	unread                       bool
	approval                     string
	scroll                       int
	follow                       bool
	visited                      bool
}

func demoAssistants() []assistant {
	return []assistant{
		{
			name: "研究助理", role: "资料整理与研究", approval: "pending",
			messages: []message{
				{role: "user", text: "帮我整理一下 Molr 终端版的第一版体验。"},
				{role: "assistant", text: "先把每天最常用的事情放到终端里：\n\n  01  切换助理，继续已有的对话\n  02  发送消息，查看执行进度\n  03  在需要时确认授权\n\n按 Ctrl+K 打开助理选择页，底部可以直接输入。每位助理会保留自己的草稿和聊天记录。"},
				{role: "assistant", text: "这里还有一条模拟授权申请，可以按 Ctrl+P 查看范围并体验批准或拒绝。"},
			},
		},
		{
			name: "开发助理", role: "开发与代码审查",
			messages: []message{{role: "assistant", text: "终端界面已经准备好了。\n\n可以试着输入一段需求，或粘贴多行文字。Enter 发送，Alt+Enter 换行。\n\n这是离线演示，我会返回示例回复，不会读取本机文件或执行命令。"}},
		},
		{
			name: "日常助理", role: "计划与日常事务",
			messages: []message{{role: "assistant", text: "今天想先处理哪件事？\n\n把想法写下来，试试这套终端交互。按 F1 可以查看快捷键。"}},
		},
	}
}

func demoReply(prompt string) string {
	if strings.Contains(strings.ToLower(prompt), "markdown") {
		return "## Markdown 排版\n\n支持 **粗体**、*斜体* 和 `行内代码`。\n\n" +
			"- 聊天消息自动排版\n- 帮助页也使用 Markdown\n\n" +
			"> 输入区保留原文，发送后渲染。\n\n" +
			"```sh\nmolr tui --demo\n```\n\n" +
			"| 操作 | 快捷键 |\n| --- | --- |\n| 切换助理 | Ctrl+K |\n| 返回 | Esc |\n\n" +
			"[Molr](https://molr.ai)\n\n这是预设的演示回复。"
	}
	if strings.Contains(prompt, "审批") || strings.Contains(prompt, "授权") {
		return "可以切换到研究助理，按 Ctrl+P 查看模拟申请。\n\n审批页会展示资源、操作和范围。使用方向键选择批准或拒绝，再按 Enter 确认；它不会授予任何真实权限。"
	}
	if strings.Contains(prompt, "代码") || strings.Contains(prompt, "开发") {
		return "可以先把工作拆成三个部分：\n\n  • 明确输入与预期结果\n  • 做出最小可运行版本\n  • 检查错误处理与关键交互\n\n这是预设的演示回复，尚未调用模型或访问项目文件。"
	}
	return "收到这条消息了。\n\n你可以继续输入，也可以切换到其他助理；消息和草稿会各自保留。\n\n这是预设的演示回复，真实 Runtime 连接将在后续接入。"
}
