package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/runtimeclient"
)

func (m model) liveApprovalView() string {
	if m.review == nil {
		return "当前助理没有待处理的授权申请。\n\n" + muted.Render("Esc 返回聊天")
	}
	detail := m.liveApprovalDetails()
	visible := max(1, m.bodyHeight()-5)
	start := min(m.approvalScroll, max(0, len(detail)-visible))
	body := append([]string(nil), detail[start:min(len(detail), start+visible)]...)
	for len(body) < visible {
		body = append(body, "")
	}
	status := m.notice
	if m.resolving {
		status = "正在提交…"
	} else if status == "" && len(detail) > visible {
		status = fmt.Sprintf("详情 %d–%d / %d · PgUp/PgDn 滚动", start+1, min(len(detail), start+visible), len(detail))
	}
	body = append(body, muted.Render(ansi.Truncate(status, m.contentWidth(), "…")), "")
	for i, label := range []string{"批准", "拒绝"} {
		if m.approvalChoice == i {
			label = accent.Bold(true).Render("› " + label)
		} else {
			label = "  " + label
		}
		body = append(body, label)
	}
	return strings.Join(body, "\n")
}

func (m model) liveApprovalDetails() []string {
	q := m.review
	lines := []string{"申请 " + string(q.ID), ""}
	for i, item := range append([]policy.PermissionRequestInput{q.Input}, q.Input.Additional...) {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, cleanText(item.ResourceDescription)), "资源："+string(item.ResourceID)+" · "+item.ResourceKind, "操作："+fmt.Sprint(item.Actions), "原因："+cleanText(item.Reason))
		if item.FullAccess {
			lines = append(lines, "权限：完全访问（Full Access）")
		}
		if item.ModelID != "" {
			lines = append(lines, "模型："+string(item.ModelID))
		}
		if len(item.PathPrefixes) > 0 {
			lines = append(lines, "范围："+strings.Join(item.PathPrefixes, ", "))
		}
		if q.Input.ExpiresAt != nil {
			lines = append(lines, "有效期至："+q.Input.ExpiresAt.Local().Format("2006-01-02 15:04:05 MST"))
		}
		if item.GrantID != "" {
			lines = append(lines, fmt.Sprintf("修改现有授权：%s · 版本 %d", item.GrantID, item.GrantVersion))
		}
		if item.Constraints != nil {
			data, _ := json.MarshalIndent(item.Constraints, "", "  ")
			lines = append(lines, "额度：\n"+string(data))
		}
		if item.Temporary != nil {
			data, _ := json.MarshalIndent(item.Temporary, "", "  ")
			lines = append(lines, "临时额度：\n"+string(data))
		}
	}
	return strings.Split(ansi.Hardwrap(cleanText(strings.Join(lines, "\n")), m.contentWidth(), true), "\n")
}

func (m model) updateLiveApproval(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.review == nil || m.resolving || m.live.expired {
		return m, nil
	}
	switch key.String() {
	case "pgup":
		m.approvalScroll = max(0, m.approvalScroll-max(1, m.bodyHeight()-5))
	case "pgdown":
		m.approvalScroll = min(max(0, len(m.liveApprovalDetails())-max(1, m.bodyHeight()-5)), m.approvalScroll+max(1, m.bodyHeight()-5))
	case "up", "down", "tab", "shift+tab":
		m.approvalChoice = (m.approvalChoice + 1) % 2
	case "enter":
		q := *m.review
		if m.reviewKey == "" || m.reviewDecision != m.approvalChoice {
			m.reviewKey = runtimeclient.NewKey()
			m.reviewDecision = m.approvalChoice
		}
		resolution := policy.PermissionResolution{Mutation: core.Mutation{IdempotencyKey: m.reviewKey, ExpectedVersion: q.Version}, RequestID: q.ID, Decision: "reject"}
		if m.approvalChoice == 0 {
			resolution = approveInput(q.Input)
			resolution.Mutation = core.Mutation{IdempotencyKey: m.reviewKey, ExpectedVersion: q.Version}
			resolution.RequestID = q.ID
			resolution.Decision = "approve"
			for _, item := range q.Input.Additional {
				resolution.Additional = append(resolution.Additional, approveInput(item))
			}
		}
		m.resolving = true
		client, ctx := m.live.client, m.live.ctx
		return m, func() tea.Msg {
			// Submit the exact request reviewed, with optimistic concurrency. The
			// server's idempotency receipt also makes uncertain retries safe.
			var result policy.PermissionRequest
			err := client.Do(ctx, "POST", "/api/permission-requests/resolve", resolution, &result)
			var apiErr *runtimeclient.Error
			if errors.As(err, &apiErr) && apiErr.Status == 409 {
				err = errors.New("申请已变化或已处理，请返回聊天后重新打开审批")
			}
			return resolvedMsg{id: q.ID, request: result, err: err}
		}
	}
	return m, nil
}
func approveInput(input policy.PermissionRequestInput) policy.PermissionResolution {
	return policy.PermissionResolution{ResourceID: input.ResourceID, Actions: input.Actions, Scope: policy.Scope{ReadOnly: !slices.Contains(input.Actions, policy.Write) && !slices.Contains(input.Actions, policy.Execute), FullAccess: input.FullAccess, ModelID: input.ModelID, PathPrefixes: input.PathPrefixes}, ExpiresAt: input.ExpiresAt, Constraints: input.Constraints, Temporary: input.Temporary, GrantID: input.GrantID, GrantVersion: input.GrantVersion}
}
