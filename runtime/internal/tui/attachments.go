package tui

import (
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/runtimeclient"
	"molr.ai/runtime/internal/storage/files"
)

const maxAttachmentBytes = 20 << 20
const maxDraftAttachments = 5
const maxMessageCharacters = 4000

type draftAttachment struct {
	digest    string
	key, name string
	data      []byte // Immutable snapshot: retries must upload the same bytes with the same key.
	file      files.File
	pasted    bool
	busy      bool
	err       string
	progress  *atomic.Int64
	cancel    context.CancelFunc
}

type attachmentResult struct {
	assistant core.ID
	key       string
	file      files.File
	err       error
}
type attachmentInfo struct {
	id   core.ID
	file files.File
	err  error
}
type attachmentInfos struct {
	assistant core.ID
	items     []attachmentInfo
}
type transferTick struct{}
type downloaded struct {
	path string
	err  error
}

type fileDialog struct {
	path          textinput.Model
	directory     string
	entries       []os.DirEntry
	cursor        int
	selected      map[string]bool
	pathFocused   bool
	drafts        bool
	items         []attachmentInfo
	loading       bool
	saving        bool
	overwrite     bool
	downloadFile  files.File
	progress      *atomic.Int64
	cancel        context.CancelFunc
	busy          bool
	notice        string
	preview       string
	previewData   []byte
	previewOffset int
}

func localPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if len(path) > 1 && (path[0] == '"' && path[len(path)-1] == '"' || path[0] == '\'' && path[len(path)-1] == '\'') {
		path = path[1 : len(path)-1]
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return filepath.Abs(path)
}

func (m *model) attachPath(path string) tea.Cmd {
	if m.live == nil {
		m.notice = "附件收发需要连接 Runtime"
		return nil
	}
	if notice := m.channelNotice(); notice != "" {
		m.notice = notice
		return nil
	}
	path, err := localPath(path)
	if err != nil {
		m.notice = "文件路径无效"
		return nil
	}
	// Inspect before opening so choosing a pipe or device does not block input.
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		m.notice = "请选择可读取的普通文件"
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		m.notice = "无法读取文件：" + cleanText(err.Error())
		return nil
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		m.notice = "请选择普通文件"
		return nil
	}
	if info.Size() > maxAttachmentBytes {
		m.notice = "单个附件最多 20 MiB"
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxAttachmentBytes+1))
	if err != nil || len(data) > maxAttachmentBytes {
		m.notice = "文件读取失败或超过 20 MiB"
		return nil
	}
	return m.addAttachment(filepath.Base(path), data, false)
}

func (m *model) addAttachment(name string, data []byte, pasted bool) tea.Cmd {
	if len(m.assistants) == 0 {
		m.notice = "请先选择助理"
		return nil
	}
	if len(data) > maxAttachmentBytes {
		m.notice = "单个附件最多 20 MiB；原粘贴未加入草稿"
		return nil
	}
	a := &m.assistants[m.selected]
	if len(a.attachments) >= maxDraftAttachments {
		m.notice = "每条消息最多 5 个附件，请先在 /drafts 中移除附件"
		return nil
	}
	if pasted {
		name = fmt.Sprintf("粘贴文本-%s.txt", runtimeclient.NewKey()[:8])
	}
	draft := draftAttachment{key: runtimeclient.NewKey(), name: name, data: data, pasted: pasted, digest: blobDigest(data)}
	a.attachments = append(a.attachments, draft)
	m.resizeComposer()
	if m.live == nil {
		m.notice = "演示附件已加入草稿"
		return nil
	}
	return m.uploadAttachment(len(a.attachments) - 1)
}

func (m *model) uploadAttachment(index int) tea.Cmd {
	if m.live == nil || m.live.expired || len(m.assistants) == 0 {
		return nil
	}
	a := &m.assistants[m.selected]
	if index < 0 || index >= len(a.attachments) {
		return nil
	}
	d := &a.attachments[index]
	if d.busy || d.file.ID != "" {
		return nil
	}
	d.busy, d.err, d.progress = true, "", &atomic.Int64{}
	ctx, cancel := context.WithCancel(m.live.ctx)
	d.cancel = cancel
	client, id, key, name, data, progress := m.live.client, a.id, d.key, d.name, d.data, d.progress
	if err := m.persistDrafts(); err != nil {
		cancel()
		d.busy = false
		d.err = "草稿未保存，请重试"
		m.draftError = err.Error()
		return nil
	}
	m.notice = "正在上传附件…"
	return tea.Batch(func() tea.Msg {
		defer cancel()
		file, err := client.Upload(ctx, name, data, key, progress.Store)
		return attachmentResult{assistant: id, key: key, file: file, err: err}
	}, transferTimer())
}

func transferTimer() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return transferTick{} })
}

func (m model) transferRunning() bool {
	if m.files.busy {
		return true
	}
	for _, a := range m.assistants {
		for _, d := range a.attachments {
			if d.busy {
				return true
			}
		}
	}
	return false
}

func (m *model) updateAttachments(msg tea.Msg) (tea.Cmd, bool) {
	switch v := msg.(type) {
	case attachmentResult:
		if i := m.assistantIndex(v.assistant); i >= 0 {
			for j := range m.assistants[i].attachments {
				d := &m.assistants[i].attachments[j]
				if d.key != v.key {
					continue
				}
				d.busy, d.cancel = false, nil
				if v.err != nil {
					d.err = cleanText(v.err.Error())
					if i == m.selected {
						m.liveError(v.err)
						m.notice = "附件上传失败，/drafts 查看并重试；" + m.notice
					}
				} else {
					d.file = v.file
					if i == m.selected {
						m.notice = "附件已就绪，Enter 随消息发送"
					}
				}
			}
		}
		m.resizeComposer()
		return nil, true
	case transferTick:
		if m.transferRunning() {
			return transferTimer(), true
		}
		return nil, true
	case attachmentInfos:
		if len(m.assistants) > 0 && m.assistants[m.selected].id == v.assistant && m.page == attachmentsPage && !m.files.drafts {
			m.files.items, m.files.loading = v.items, false
			for _, item := range v.items {
				if item.err != nil {
					m.liveError(item.err)
				}
			}
			if len(v.items) == 0 {
				m.files.notice = "已加载的聊天记录中没有附件；返回聊天上翻可加载更早消息。"
			}
		}
		return nil, true
	case downloaded:
		m.files.busy, m.files.cancel = false, nil
		if v.err != nil {
			m.files.notice = "下载失败：" + cleanText(v.err.Error())
		} else {
			m.files.saving = false
			m.files.notice = "已保存：" + cleanText(v.path)
		}
		return nil, true
	}
	return nil, false
}

func newFileDialog() fileDialog {
	p := textinput.New()
	p.Prompt = "路径 › "
	p.CharLimit = 4096
	return fileDialog{path: p, selected: map[string]bool{}}
}

func (m *model) openFiles() tea.Cmd {
	if m.live == nil {
		m.notice = "附件收发需要连接 Runtime"
		return nil
	}
	if notice := m.channelNotice(); notice != "" {
		m.notice = notice
		return nil
	}
	m.files = newFileDialog()
	dir, _ := os.Getwd()
	m.readDirectory(dir)
	return m.openPage(filesPage)
}

func (m *model) readDirectory(path string) {
	path, err := localPath(path)
	if err != nil {
		m.files.notice = "目录路径无效"
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		m.files.notice = cleanText(err.Error())
		return
	}
	slices.SortStableFunc(entries, func(a, b os.DirEntry) int {
		if a.IsDir() != b.IsDir() {
			if a.IsDir() {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name(), b.Name())
	})
	m.files.directory, m.files.entries, m.files.cursor = path, entries, 0
	m.files.path.SetValue(path + string(os.PathSeparator))
	m.files.path.Width = max(1, m.contentWidth()-8)
	m.files.notice = ""
}

func (m *model) openAttachments(drafts bool) tea.Cmd {
	if len(m.assistants) == 0 {
		m.notice = "请先选择助理"
		return nil
	}
	m.files = newFileDialog()
	m.files.drafts = drafts
	focus := m.openPage(attachmentsPage)
	if drafts || m.live == nil {
		return focus
	}
	a := m.assistants[m.selected]
	var ids []core.ID
	for i := len(a.messages) - 1; i >= 0; i-- {
		for _, id := range a.messages[i].attachments {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	m.files.loading = true
	client, ctx := m.live.client, m.live.ctx
	return tea.Batch(focus, func() tea.Msg {
		out := attachmentInfos{assistant: a.id}
		for _, id := range ids {
			item := attachmentInfo{id: id}
			item.err = client.Do(ctx, "GET", "/api/files/"+string(id), nil, &item.file)
			out.items = append(out.items, item)
			if ctx.Err() != nil {
				break
			}
		}
		return out
	})
}

func (m model) filesView() string {
	f := m.files
	if f.preview != "" {
		lines := strings.Split(f.preview, "\n")
		start := min(f.previewOffset, max(0, len(lines)-1))
		end := min(len(lines), start+max(1, m.bodyHeight()-3))
		return strings.Join(lines[start:end], "\n") + "\n\n" + muted.Render("PgUp/PgDn 翻页 · Esc 返回附件列表")
	}
	if f.saving {
		status := f.notice
		if f.busy {
			status = fmt.Sprintf("正在下载 %d / %d 字节…", f.progress.Load(), f.downloadFile.Size)
		}
		if f.overwrite {
			status = "目标已存在：Enter 确认替换，Esc 返回修改保存路径"
		}
		return "保存附件：" + cleanText(f.downloadFile.Name) + "\n\n" + f.path.View() + "\n\n" + warning.Render(cellTruncate(status, m.contentWidth(), "…"))
	}
	var rows []string
	if m.page == filesPage {
		rows = append(rows, f.path.View(), "")
		for _, entry := range f.entries {
			label := cleanText(entry.Name())
			if entry.IsDir() {
				label += "/"
			}
			if f.selected[filepath.Join(f.directory, entry.Name())] {
				label = "[✓] " + label
			} else {
				label = "[ ] " + label
			}
			rows = append(rows, label)
		}
	} else if f.drafts && len(m.assistants) > 0 {
		for _, d := range m.assistants[m.selected].attachments {
			rows = append(rows, attachmentLabel(d))
		}
		if len(rows) == 0 {
			rows = append(rows, "尚未添加附件。Ctrl+F 选择文件，长粘贴自动成为文本附件。")
		}
	} else {
		for _, item := range f.items {
			label := fmt.Sprintf("%s · %d 字节", cleanText(item.file.Name), item.file.Size)
			if item.err != nil {
				label = string(item.id) + " · 无法读取，请重新打开列表重试"
			}
			rows = append(rows, label)
		}
		if f.loading {
			rows = append(rows, "正在读取附件信息…")
		}
	}
	header := ""
	if m.page == filesPage {
		header = strings.Join(rows[:2], "\n")
		rows = rows[2:]
	}
	visible := max(1, m.bodyHeight()-5)
	start := max(0, f.cursor-visible+1)
	end := min(len(rows), start+visible)
	var body []string
	for i := start; i < end; i++ {
		label := cellTruncate(rows[i], m.contentWidth()-2, "…")
		if i == f.cursor {
			label = accent.Render("› " + label)
		} else {
			label = "  " + label
		}
		body = append(body, label)
	}
	return header + strings.Join(body, "\n") + "\n\n" + muted.Render(cellTruncate(f.notice, m.contentWidth(), "…"))
}

func attachmentLabel(d draftAttachment) string {
	label := fmt.Sprintf("%s · %d 字节", cleanText(d.name), len(d.data))
	if d.busy {
		label += fmt.Sprintf(" · 上传 %d%%", d.progress.Load()*100/int64(max(1, len(d.data))))
	} else if d.err != "" {
		label += " · 上传失败（/drafts 重试）"
	} else if d.file.ID != "" {
		label += " · 已就绪"
	}
	return label
}

func (m model) updateFiles(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.files
	if f.preview != "" {
		switch key.String() {
		case "esc":
			f.preview = ""
			f.previewData = nil
		case "pgdown", "down":
			f.previewOffset += max(1, m.bodyHeight()-3)
		case "pgup", "up":
			f.previewOffset = max(0, f.previewOffset-max(1, m.bodyHeight()-3))
		}
		return m, nil
	}
	if f.busy {
		if key.String() == "esc" && f.cancel != nil {
			f.cancel()
			f.notice = "正在取消下载…"
		}
		return m, nil
	}
	if f.saving {
		if key.String() == "esc" {
			if f.overwrite {
				f.overwrite = false
				return m, nil
			}
			f.saving = false
			f.path.Blur()
			return m, nil
		}
		if key.String() == "enter" {
			return m, m.startDownload()
		}
		f.overwrite = false
		var cmd tea.Cmd
		f.path, cmd = f.path.Update(key)
		return m, cmd
	}
	if key.String() == "esc" {
		return m, m.goBack()
	}
	if len(m.assistants) == 0 {
		cmd := m.goBack()
		return m, cmd
	}
	count := len(f.entries)
	if m.page == attachmentsPage {
		count = len(f.items)
		if f.drafts {
			count = len(m.assistants[m.selected].attachments)
		}
	}
	switch key.String() {
	case "up", "down":
		f.pathFocused = false
		f.path.Blur()
		delta := 1
		if key.String() == "up" {
			delta = -1
		}
		f.cursor = max(0, min(max(0, count-1), f.cursor+delta))
		return m, nil
	case "ctrl+l":
		if m.page == filesPage {
			f.pathFocused = true
			return m, f.path.Focus()
		}
	case "tab":
		if m.page == filesPage {
			f.pathFocused = true
			f.path.Focus()
			m.completePath()
			return m, nil
		}
	case "backspace":
		if m.page == filesPage && !f.pathFocused {
			m.readDirectory(filepath.Dir(f.directory))
			return m, nil
		}
	case " ":
		if m.page == filesPage && !f.pathFocused && count > 0 {
			entry := f.entries[f.cursor]
			if !entry.IsDir() {
				path := filepath.Join(f.directory, entry.Name())
				f.selected[path] = !f.selected[path]
			}
			return m, nil
		}
	case "delete", "d":
		if m.page == attachmentsPage && f.drafts && count > 0 {
			a := &m.assistants[m.selected]
			if d := a.attachments[f.cursor]; d.cancel != nil {
				d.cancel()
			}
			a.attachments = slices.Delete(a.attachments, f.cursor, f.cursor+1)
			f.cursor = max(0, min(f.cursor, len(a.attachments)-1))
			m.resizeComposer()
			return m, nil
		}
	case "r":
		if m.page == attachmentsPage && f.drafts && count > 0 {
			return m, m.uploadAttachment(f.cursor)
		}
	case "enter":
		if m.page == filesPage {
			if f.pathFocused {
				path, err := localPath(f.path.Value())
				if err != nil {
					f.notice = "路径无效"
					return m, nil
				}
				info, err := os.Stat(path)
				if err == nil && info.IsDir() {
					m.readDirectory(path)
					f.pathFocused = false
					f.path.Blur()
					return m, nil
				}
				before := len(m.assistants[m.selected].attachments)
				cmd := m.attachPath(path)
				if len(m.assistants[m.selected].attachments) > before {
					return m, tea.Batch(cmd, m.goBack())
				}
				f.notice = m.notice
				return m, cmd
			}
			var paths []string
			for path, chosen := range f.selected {
				if chosen {
					paths = append(paths, path)
				}
			}
			slices.Sort(paths)
			if len(paths) == 0 && count > 0 {
				entry := f.entries[f.cursor]
				path := filepath.Join(f.directory, entry.Name())
				if entry.IsDir() {
					m.readDirectory(path)
					return m, nil
				}
				paths = []string{path}
			}
			if len(paths)+len(m.assistants[m.selected].attachments) > maxDraftAttachments {
				f.notice = "每条消息最多 5 个附件"
				return m, nil
			}
			var cmds []tea.Cmd
			for _, path := range paths {
				before := len(m.assistants[m.selected].attachments)
				cmds = append(cmds, m.attachPath(path))
				if len(m.assistants[m.selected].attachments) == before {
					f.notice = m.notice
					return m, tea.Batch(cmds...)
				}
				delete(f.selected, path)
			}
			cmds = append(cmds, m.goBack())
			return m, tea.Batch(cmds...)
		}
		if count == 0 {
			return m, nil
		}
		if f.drafts {
			d := m.assistants[m.selected].attachments[f.cursor]
			if d.pasted {
				f.previewData = d.data
				f.preview = wrapPreview(string(d.data), m.contentWidth())
				f.previewOffset = 0
			} else {
				f.notice = cleanText(d.name) + " · " + d.err
			}
			return m, nil
		}
		item := f.items[f.cursor]
		if item.err != nil {
			f.notice = cleanText(item.err.Error())
			return m, nil
		}
		f.downloadFile = item.file
		f.saving = true
		f.overwrite = false
		f.notice = "Enter 保存 · Esc 取消"
		dir, _ := os.Getwd()
		name := filepath.Base(strings.ReplaceAll(item.file.Name, "\\", "/"))
		if name == "." || name == "/" || name == ".." || name == "" {
			name = string(item.id)
		}
		f.path.SetValue(filepath.Join(dir, name))
		f.path.Width = max(1, m.contentWidth()-8)
		return m, f.path.Focus()
	}
	if m.page == filesPage && f.pathFocused {
		var cmd tea.Cmd
		f.path, cmd = f.path.Update(key)
		return m, cmd
	}
	return m, nil
}

func (m *model) completePath() {
	path, err := localPath(m.files.path.Value())
	if err != nil {
		return
	}
	dir, prefix := filepath.Dir(path), filepath.Base(path)
	if strings.HasSuffix(m.files.path.Value(), string(os.PathSeparator)) {
		dir, prefix = path, ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var matches []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			value := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				value += string(os.PathSeparator)
			}
			matches = append(matches, value)
		}
	}
	if len(matches) == 1 {
		m.files.path.SetValue(matches[0])
		m.files.path.CursorEnd()
	} else if len(matches) > 1 {
		m.files.notice = fmt.Sprintf("%d 个匹配项，请继续输入或浏览目录", len(matches))
	}
}

func wrapPreview(text string, width int) string { return ansi.Hardwrap(cleanText(text), width, true) }

func (m *model) startDownload() tea.Cmd {
	f := &m.files
	path, err := localPath(f.path.Value())
	if err != nil {
		f.notice = "保存路径无效"
		return nil
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			f.notice = "目标不是普通文件，请修改路径"
			return nil
		}
		if !f.overwrite {
			f.overwrite = true
			return nil
		}
	} else if !os.IsNotExist(err) {
		f.notice = cleanText(err.Error())
		return nil
	}
	ctx, cancel := context.WithCancel(m.live.ctx)
	f.cancel = cancel
	f.busy = true
	f.progress = &atomic.Int64{}
	client, file, progress, overwrite := m.live.client, f.downloadFile, f.progress, f.overwrite
	return tea.Batch(func() tea.Msg {
		defer cancel()
		out := downloaded{path: path}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".molr-download-*")
		if err != nil {
			out.err = err
			return out
		}
		defer os.Remove(tmp.Name())
		err = client.Download(ctx, file, tmp, progress.Store)
		if err == nil {
			err = tmp.Sync()
		}
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if err == nil {
			if overwrite {
				// Refuse to overwrite a destination replaced since confirmation.
				now, statErr := os.Lstat(path)
				if statErr != nil || info == nil || !os.SameFile(info, now) || now.Size() != info.Size() || !now.ModTime().Equal(info.ModTime()) {
					err = fmt.Errorf("目标文件已变化，请重新确认")
				} else {
					err = os.Rename(tmp.Name(), path)
				}
			} else {
				err = os.Link(tmp.Name(), path)
			}
		}
		out.err = err
		return out
	}, transferTimer())
}
