package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/storage/files"
	"molr.ai/runtime/internal/workspace"
)

func (m *model) artifactScreen(id core.ID) tea.Cmd {
	return m.loadScreen("下载附件／交付物", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var f files.File
		if err := c.Do(ctx, "GET", "/api/files/"+string(id), nil, &f); err != nil {
			return manageScreen{}, err
		}
		return manageScreen{title: "附件／交付物", body: fmt.Sprintf("%s\n大小：%d 字节", f.Name, f.Size), rows: []manageRow{{"下载到运行 TUI 的机器", func(m *model) tea.Cmd {
			return m.downloadScreen(f.Name, func(ctx context.Context, c *runtimeclient.Client, w io.Writer) error {
				return c.Download(ctx, f, w, nil)
			})
		}}}}, nil
	})
}
func (m *model) downloadScreen(name string, download func(context.Context, *runtimeclient.Client, io.Writer) error) tea.Cmd {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == ".." || name == "/" || name == "" {
		name = "download"
	}
	cwd, _ := os.Getwd()
	s := manageScreen{title: "保存到本机", body: "路径属于运行 TUI 的机器；通过 SSH 运行时保存到远端机器。", fields: []formField{field("path", "完整保存路径", filepath.Join(cwd, name))}, submitLabel: "下载"}
	s.localSubmit = func(m *model, v map[string]string) tea.Cmd {
		target, err := localPath(v["path"])
		if err != nil {
			m.manage.screen.notice = "保存路径无效"
			return nil
		}
		info, err := os.Lstat(target)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			m.manage.screen.notice = err.Error()
			return nil
		}
		if info != nil && !info.Mode().IsRegular() {
			m.manage.screen.notice = "只能替换普通文件"
			return nil
		}
		action := func(ctx context.Context, c *runtimeclient.Client) error {
			return publishFile(target, info, func(w io.Writer) error { return download(ctx, c, w) })
		}
		detail := "下载到：" + target
		if info != nil {
			detail += "\n目标已经存在，确认替换此文件。"
		}
		cmd := m.confirmAction("确认保存", detail, "开始下载", action, "")
		m.manage.screen.cancellable = true
		m.manage.screen.afterSuccess = func(m *model) tea.Cmd {
			m.closeScreen()
			m.manage.screen.dirty = false
			m.manage.screen.notice = "已保存：" + target
			return nil
		}
		return cmd
	}
	return m.showScreen(s)
}
func publishFile(target string, expected os.FileInfo, write func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(target), ".molr-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = write(f); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	current, err := os.Lstat(target)
	if expected == nil {
		if err == nil {
			return fmt.Errorf("目标文件刚刚被创建，请重新确认")
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err = os.Link(f.Name(), target); err != nil {
			return err
		}
		return nil
	}
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(expected, current) || current.Size() != expected.Size() || !current.ModTime().Equal(expected.ModTime()) {
		return fmt.Errorf("目标文件已变化，请重新确认替换")
	}
	return os.Rename(f.Name(), target)
}
func (m *model) saveTextScreen(text string) tea.Cmd {
	return m.downloadScreen("message.txt", func(_ context.Context, _ *runtimeclient.Client, w io.Writer) error {
		_, err := io.WriteString(w, text)
		return err
	})
}
func workspacePath(id core.ID, p, action string) string {
	return "/api/assistants/" + string(id) + "/workspace?path=" + url.QueryEscape(p) + "&action=" + action
}
func (m *model) workspaceScreen(id core.ID, p string) tea.Cmd {
	return m.workspacePage(id, p, 0, "", false)
}
func (m *model) workspacePage(id core.ID, p string, offset int, version string, hidden bool) tea.Cmd {
	return m.loadScreen("助理工作区", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
		var listing workspace.Listing
		if err := c.Do(ctx, "GET", workspacePath(id, p, "list")+fmt.Sprintf("&offset=%d&limit=100&version=%s", offset, url.QueryEscape(version)), nil, &listing); err != nil {
			return manageScreen{}, err
		}
		s := manageScreen{title: "助理工作区", body: "/work/" + strings.TrimPrefix(p, ".")}
		s.rows = []manageRow{{"上传本机文件到此目录", func(m *model) tea.Cmd { return m.workspaceUploadForm(id, p) }}, {"切换隐藏文件显示", func(m *model) tea.Cmd { return m.workspacePage(id, p, 0, "", !hidden) }}}
		if p != "." {
			s.rows = append(s.rows, manageRow{".. 上级目录", func(m *model) tea.Cmd { return m.workspacePage(id, path.Dir(p), 0, "", hidden) }})
		}
		slices.SortStableFunc(listing.Entries, func(a, b workspace.Entry) int {
			if (a.Type == "directory") != (b.Type == "directory") {
				if a.Type == "directory" {
					return -1
				}
				return 1
			}
			return strings.Compare(a.Name, b.Name)
		})
		for _, e := range listing.Entries {
			if !hidden && strings.HasPrefix(e.Name, ".") {
				continue
			}
			label := e.Name
			if e.Type == "directory" {
				label += "/"
			} else {
				label += fmt.Sprintf(" · %d 字节", e.Size)
			}
			s.rows = append(s.rows, manageRow{label, func(m *model) tea.Cmd {
				if e.Type == "directory" {
					return m.workspacePage(id, e.Path, 0, "", hidden)
				}
				if e.Type != "file" {
					m.manage.screen.notice = "此条目不可访问"
					return nil
				}
				return m.workspaceFile(id, e)
			}})
		}
		if listing.Truncated {
			s.rows = append(s.rows, manageRow{"下一页", func(m *model) tea.Cmd { return m.workspacePage(id, p, listing.NextOffset, listing.Version, hidden) }})
		}
		return s, nil
	})
}
func (m *model) workspaceFile(id core.ID, e workspace.Entry) tea.Cmd {
	s := manageScreen{title: e.Name, body: fmt.Sprintf("/work/%s\n大小：%d 字节\n修改：%s", e.Path, e.Size, e.ModifiedAt.Local().Format("2006-01-02 15:04:05"))}
	s.rows = []manageRow{{"预览文本", func(m *model) tea.Cmd {
		return m.loadScreen("文本预览", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
			var t workspace.Text
			if err := c.Do(ctx, "GET", workspacePath(id, e.Path, "read"), nil, &t); err != nil {
				return manageScreen{}, err
			}
			body := t.Text
			if t.Truncated {
				body += "\n\n[内容已截断，请下载查看完整文件]"
			}
			return manageScreen{title: e.Name, body: body}, nil
		})
	}}, {"下载", func(m *model) tea.Cmd {
		return m.downloadScreen(e.Name, func(ctx context.Context, c *runtimeclient.Client, w io.Writer) error {
			return c.WorkspaceDownload(ctx, id, e, w)
		})
	}}, {"复制助理路径", func(m *model) tea.Cmd {
		if m.writeClipboard != nil {
			if err := m.writeClipboard("/work/" + e.Path); err != nil {
				m.manage.screen.notice = err.Error()
			} else {
				m.manage.screen.notice = "复制请求已发送"
			}
		}
		return nil
	}}, {"把路径加入聊天输入", func(m *model) tea.Cmd {
		if m.assistantID() != id {
			m.manage.screen.notice = "请切换回此助理后再添加路径"
			return nil
		}
		text := m.composerText()
		if text != "" {
			text += "\n"
		}
		text += "/work/" + e.Path
		if len([]rune(text)) > maxMessageCharacters {
			m.manage.screen.notice = "输入已超限，路径未添加"
			return nil
		}
		m.resetComposer()
		m.input.SetValue(text)
		m.resizeComposer()
		m.manage.stack = nil
		m.back = nil
		m.page = chatPage
		return m.input.Focus()
	}}}
	return m.showScreen(s)
}
func (m *model) workspaceUploadForm(id core.ID, dir string) tea.Cmd {
	s := manageScreen{title: "上传到助理工作区", body: "上传不发送聊天消息。结果未知时先查看目标文件，不能自动重试。", fields: []formField{field("local", "本机文件路径", ""), field("name", "目标文件名（空为原文件名）", "")}, submitLabel: "检查目标"}
	s.localSubmit = func(m *model, v map[string]string) tea.Cmd {
		local, err := localPath(v["local"])
		if err != nil {
			m.manage.screen.notice = err.Error()
			return nil
		}
		info, err := os.Stat(local)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
			m.manage.screen.notice = "请选择不超过 64 MiB 的普通文件"
			return nil
		}
		name := strings.TrimSpace(v["name"])
		if name == "" {
			name = filepath.Base(local)
		}
		if name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
			m.manage.screen.notice = "目标文件名不能包含目录"
			return nil
		}
		target := path.Join(dir, name)
		return m.loadScreen("检查上传目标", func(ctx context.Context, c *runtimeclient.Client) (manageScreen, error) {
			f, err := os.Open(local)
			if err != nil {
				return manageScreen{}, err
			}
			defer f.Close()
			data, err := io.ReadAll(io.LimitReader(f, 64<<20+1))
			if err != nil || len(data) > 64<<20 {
				return manageScreen{}, fmt.Errorf("文件读取失败或超过 64 MiB")
			}
			var entry workspace.Entry
			err = c.Do(ctx, "GET", workspacePath(id, target, "stat"), nil, &entry)
			var apiErr *runtimeclient.Error
			if err != nil && (!errors.As(err, &apiErr) || apiErr.Status != 404) {
				return manageScreen{}, err
			}
			exists := err == nil
			screen := manageScreen{title: "确认上传", body: local + "\n→ /work/" + target}
			add := func(label, destination, version string) {
				screen.rows = append(screen.rows, manageRow{label, func(m *model) tea.Cmd {
					return m.confirmAction("确认上传", local+"\n→ /work/"+destination, label, func(ctx context.Context, c *runtimeclient.Client) error {
						_, err := c.WorkspaceUpload(ctx, id, destination, version, data)
						return err
					}, "/files")
				}})
			}
			if exists {
				screen.body += "\n同名文件已存在。"
				if entry.Type == "file" {
					add("替换此版本", target, entry.Version)
				}
				ext := path.Ext(name)
				copyName := strings.TrimSuffix(name, ext) + "-" + runtimeclient.NewKey()[:8] + ext
				add("保留两份", path.Join(dir, copyName), "")
			} else {
				add("上传", target, "")
			}
			return screen, nil
		})
	}
	return m.showScreen(s)
}
