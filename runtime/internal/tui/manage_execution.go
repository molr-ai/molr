package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/business/plan"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/shellsession"
	"net/url"
	"strings"
	"time"
)

func (m *model) plansScreen(id core.ID) tea.Cmd {
	return m.loadScreen("计划", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var items []plan.Summary
		if err := c.Do(ctx, "GET", "/api/plans?scope="+url.QueryEscape(string(id)), nil, &items); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "计划", body: "暂停后续安排不会取消已启动的业务任务。"}
		for _, p := range items {
			label := p.Title
			if p.Links.Paused {
				label += " · 已暂停"
			}
			if p.Schedule != nil && p.Schedule.NextAt != nil {
				label += " · " + p.Schedule.NextAt.Local().Format(time.DateTime)
			}
			s.rows = append(s.rows, manageRow{label, func(m *model) tea.Cmd { return m.planScreen(p.Ref) }})
		}
		if len(items) == 0 {
			s.body = "暂无计划；可在聊天中安排后续工作。"
		}
		return s, nil
	})
}
func (m *model) planScreen(ref core.Object) tea.Cmd {
	endpoint := "/api/plans/" + ref.Kind + "/" + string(ref.ID)
	return m.loadScreen("计划详情", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var p plan.Summary
		if err := c.Do(ctx, "GET", endpoint, nil, &p); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "计划详情", body: p.Title}
		if p.Schedule != nil {
			s.body += "\n" + p.Schedule.Instructions
			if p.Schedule.NextAt != nil {
				s.body += "\n下次：" + p.Schedule.NextAt.Local().Format(time.DateTime)
			}
		}
		label := "暂停后续安排"
		if p.Links.Paused {
			label = "恢复后续安排"
		}
		op := mutation(p.Links.Version)
		s.rows = []manageRow{{label, func(m *model) tea.Cmd {
			return m.confirmAction(label, p.Title+"\n已经启动的任务需在任务详情中单独管理。", label, func(ctx context.Context, c *runtimeclient.Client) error {
				return c.Do(ctx, "POST", "/api/plans/configure", plan.ConfigureRequest{Mutation: op, Ref: ref, Dependencies: p.Links.Dependencies, Handoff: p.Links.Handoff, Paused: !p.Links.Paused}, nil)
			}, "/plans")
		}}}
		history, err := getAll[plan.ExecutionSummary](ctx, c, endpoint+"/history")
		if err != nil {
			return s, err
		}
		for _, e := range history {
			s.rows = append(s.rows, manageRow{e.StartedAt.Local().Format(time.DateTime) + " · " + stateText(e.State), func(m *model) tea.Cmd {
				return m.loadScreen("执行记录", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
					var run plan.Execution
					if err := c.Do(ctx, "GET", endpoint+"/executions/"+string(e.ID), nil, &run); err != nil {
						return manageScreen{}, err
					}
					detail := manageScreen{title: "执行记录", body: run.Title + "\n" + stateText(run.State) + "\n\n" + run.Instructions + "\n\n" + run.Result}
					if run.Task != nil {
						detail.rows = []manageRow{{"查看任务与交付物", func(m *model) tea.Cmd { return m.taskScreen(run.Task.ID) }}}
					}
					return detail, nil
				})
			}})
		}
		return s, nil
	})
}
func (m *model) processesScreen(id core.ID) tea.Cmd {
	return m.loadScreen("后台进程与终端", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var page struct{ Items []shellsession.Task }
		if err := c.Do(ctx, "GET", "/api/assistants/"+string(id)+"/shell-tasks", nil, &page); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "后台进程与终端", body: "停止当前助理一轮不会关闭已分离的后台任务。"}
		for _, t := range page.Items {
			s.rows = append(s.rows, manageRow{stateText(t.State) + " · " + t.Mode + " · " + strings.ReplaceAll(t.Script, "\n", " "), func(m *model) tea.Cmd { return m.processScreen(id, t) }})
		}
		if len(s.rows) == 0 {
			s.body = "没有后台进程或终端会话"
		}
		return s, nil
	})
}
func (m *model) processScreen(id core.ID, t shellsession.Task) tea.Cmd {
	s := manageScreen{title: "进程详情", body: fmt.Sprintf("%s\n状态：%s\n资源：%s\n开始：%s\n原因：%s", t.Script, stateText(t.State), t.ComputeID, t.StartedAt.Local().Format(time.DateTime), t.Failure)}
	for _, stream := range []string{"stdout", "stderr"} {
		s.rows = append(s.rows, manageRow{"查看 " + stream, func(m *model) tea.Cmd {
			return m.loadScreen("进程输出", processOutput(id, t.ID, stream, 0, "", true))
		}})
	}
	if t.State == "running" {
		if t.Terminal == nil {
			s.rows = append(s.rows, manageRow{"停止此进程", func(m *model) tea.Cmd {
				return m.confirmAction("停止进程", t.Script+"\n已写入的文件保留。", "停止", func(ctx context.Context, c *runtimeclient.Client) error {
					return c.Do(ctx, "POST", "/api/assistants/"+string(id)+"/shell-tasks", tools.ShellTaskRequest{Action: "stop", TaskID: t.ID}, nil)
				}, "/processes")
			}})
		} else {
			clientID := runtimeclient.NewKey()
			generation := t.Terminal.Generation
			s.rows = append(s.rows, manageRow{"接管并结束此终端", func(m *model) tea.Cmd {
				return m.confirmAction("接管并结束终端", t.Script+"\n将从助理或其他窗口接管，并结束整个终端及其子进程。", "接管并结束", func(ctx context.Context, c *runtimeclient.Client) error {
					endpoint := "/api/assistants/" + string(id) + "/shell-tasks"
					var result struct{ Terminal *shellsession.TerminalState }
					if err := c.Do(ctx, "POST", endpoint, tools.ShellTaskRequest{Action: "takeover", TaskID: t.ID, Generation: generation, ClientID: clientID}, &result); err != nil {
						return err
					}
					if result.Terminal == nil {
						return fmt.Errorf("接管结果未确认，请刷新终端状态")
					}
					if err := c.Do(ctx, "POST", endpoint, tools.ShellTaskRequest{Action: "stop", TaskID: t.ID, Generation: result.Terminal.Generation, Sequence: 1, ClientID: clientID}, nil); err != nil {
						return fmt.Errorf("已接管，结束结果未确认；请刷新后核对：%w", err)
					}
					return nil
				}, "/processes")
			}})
		}
	}
	return m.showScreen(s)
}
func processOutput(id, taskID core.ID, stream string, cursor int64, previous string, follow bool) func(context.Context, *runtimeclient.Client) (manageScreen, error) {
	return func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var output struct {
			Output         string
			NextCursor     int64
			Finished       bool
			OutputComplete *bool
			Path           string
		}
		if err := c.Do(ctx, "POST", "/api/assistants/"+string(id)+"/shell-tasks", tools.ShellTaskRequest{Action: "read", TaskID: taskID, Stream: stream, Cursor: cursor, Encoding: "base64"}, &output); err != nil {
			return manageScreen{}, err
		}
		data, err := base64.StdEncoding.DecodeString(output.Output)
		if err != nil {
			return manageScreen{}, fmt.Errorf("输出编码无效")
		}
		text := previous + string(data)
		if len(text) > 64<<10 {
			text = "[只保留最近 64 KiB 输出]\n" + text[len(text)-(64<<10):]
		}
		body := cleanText(text)
		if output.Finished {
			body += "\n[进程已结束]"
		}
		if output.OutputComplete != nil && !*output.OutputComplete {
			body += "\n[输出归档不完整]"
		}
		if output.Path != "" {
			body += "\n输出路径：" + output.Path
		}
		s := manageScreen{title: "进程输出 · " + stream, body: body, poll: follow && !output.Finished}
		s.reload = processOutput(id, taskID, stream, output.NextCursor, text, follow)
		s.rows = []manageRow{{"读取下一段／切换输出跟随", func(m *model) tea.Cmd {
			return m.loadScreen("进程输出", processOutput(id, taskID, stream, output.NextCursor, text, !follow))
		}}, {"保存当前输出到本机", func(m *model) tea.Cmd { return m.saveTextScreen(text) }}}
		return s, nil
	}
}
