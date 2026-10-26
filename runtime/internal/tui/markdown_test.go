package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestMarkdownContentAndWrapping(t *testing.T) {
	source := "## 标题\n\n**重点**、*说明* 和 `inline`。\n\n- 第一项\n- 第二项\n\n" +
		"> 引用内容\n\n```go\nfmt.Println(\"hello\")\n```\n\n" +
		"[文档](https://example.com/docs)\n\n| 操作 | 键 |\n| --- | --- |\n| 返回 | Esc |\n\n" + strings.Repeat("中文长文本", 30)
	for _, width := range []int{38, 76, 116} {
		out := newMarkdownRenderer(width).render(source)
		plain := ansi.Strip(out)
		for _, want := range []string{"标题", "重点", "说明", "inline", "第一项", "第二项", "引用内容", `fmt.Println("hello")`, "文档", "https://example.com/docs", "操作", "Esc"} {
			if !strings.Contains(plain, want) {
				t.Errorf("width %d lost %q:\n%s", width, want, plain)
			}
		}
		for _, syntax := range []string{"## 标题", "**重点**", "*说明*", "`inline`", "```go", "[文档]("} {
			if strings.Contains(plain, syntax) {
				t.Errorf("unrendered syntax %q", syntax)
			}
		}
		if strings.HasPrefix(plain, "\n") || strings.HasSuffix(plain, "\n") {
			t.Fatal("renderer added outer blank rows")
		}
		for _, line := range strings.Split(out, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("Markdown overflowed %d columns: %q", width, line)
			}
		}
	}
}

func TestMarkdownControlsAreNotDecodedIntoTerminalCommands(t *testing.T) {
	out := newMarkdownRenderer(76).render("safe\x1b]52;c;secret\x07\n\n&#27;[2J &#x1b;]0;title&#7; &lt;literal&gt;")
	if strings.Contains(out, "\x1b]52") || strings.Contains(out, "\x1b[2J") || strings.ContainsRune(out, '\x07') || strings.Contains(out, "\x1b]0") {
		t.Fatal("Markdown emitted a terminal command")
	}
	if !strings.Contains(ansi.Strip(out), "<literal>") {
		t.Fatal("ordinary entity text was lost")
	}
}

func TestQuoteBarsCoverWrappedAndBlankLines(t *testing.T) {
	for _, width := range []int{38, 76} {
		out := ansi.Strip(newMarkdownRenderer(width).render("> " + strings.Repeat("引用文字", 30) + "\n>\n> 第二段\n\n普通正文"))
		quoteLines := 0
		for _, line := range strings.Split(out, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("quote overflowed: %q", line)
			}
			if strings.Contains(line, "普通正文") {
				break
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			if !strings.HasPrefix(line, "▌") {
				t.Fatalf("quote line lost its continuous bar: %q", line)
			}
			quoteLines++
		}
		if quoteLines < 3 {
			t.Fatalf("quote did not wrap: %s", out)
		}
	}
}

func TestMarkdownTablesUseContentWidthsAndUnderlines(t *testing.T) {
	source := "| 操作 | 说明 |\n| :--- | ---: |\n| 切换助理 | Ctrl+K |\n| 返回 | Esc |"
	for _, width := range []int{38, 76, 116} {
		out := ansi.Strip(newMarkdownRenderer(width).render(source))
		lines := strings.Split(out, "\n")
		if len(lines) != 5 || lines[1] != "━━━━━━━━━━  ━━━━━━━━" || lines[3] != "──────────  ────────" {
			t.Fatalf("wrong rules: %q", out)
		}
		if strings.ContainsAny(out, "│┼|") || strings.ContainsAny(lines[len(lines)-1], "─━") {
			t.Fatalf("unexpected separators: %s", out)
		}
		if lipgloss.Width(out) != 20 {
			t.Fatalf("table grew beyond content width: %q", out)
		}
		if !strings.HasSuffix(lines[2], "Ctrl+K ") || !strings.HasSuffix(lines[4], "Esc ") {
			t.Fatal("right alignment was lost")
		}
		for _, row := range []int{0, 2, 4} {
			if !strings.HasPrefix(lines[row], " ") || ansi.Cut(lines[row], 9, 13) != "    " || !strings.HasSuffix(lines[row], " ") {
				t.Fatalf("cell padding missing: %q", lines[row])
			}
		}
	}
	long := "这是一段需要在单元格内换行的中文说明，末尾标记"
	out := ansi.Strip(newMarkdownRenderer(38).render(source + "\n| 长内容 | " + long + " |"))
	var right strings.Builder
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > 38 {
			t.Fatalf("table overflowed: %q", line)
		}
		if !strings.ContainsAny(line, "─━") {
			right.WriteString(strings.TrimSpace(ansi.Cut(line, 13, 37)))
		}
	}
	if !strings.Contains(right.String(), long) {
		t.Fatalf("wrapped text lost: %s", out)
	}
}

func TestMarkdownTableContextsAndLiteralPipes(t *testing.T) {
	source := "before\n\n| A | B |\n| --- | --- |\n| `a\\|b` | **bold** |\n\nafter"
	out := ansi.Strip(newMarkdownRenderer(60).render(source))
	for _, want := range []string{"before", "after", "a|b", "bold", "━"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %q: %s", want, out)
		}
	}
	code := ansi.Strip(newMarkdownRenderer(60).render("```\n| A | B |\n| --- | --- |\n```"))
	if strings.Contains(code, "━") || !strings.Contains(code, "| --- | --- |") {
		t.Fatal("code fence became a table")
	}
	for _, prefix := range []string{"> ", "- ", "1. "} {
		continuation := strings.Repeat(" ", len(prefix))
		if prefix == "> " {
			continuation = prefix
		}
		input := prefix + "| A | B |\n" + continuation + "| --- | --- |\n" + continuation + "| x | y |"
		rendered := ansi.Strip(newMarkdownRenderer(38).render(input))
		if !strings.Contains(rendered, "━") {
			t.Fatalf("nested table failed: %s", rendered)
		}
		for _, r := range rendered {
			if r >= 0xe000 && r <= 0xf8ff {
				t.Fatal("table placeholder leaked")
			}
		}
	}
}

func TestMarkdownHelpAndFooterOccupyTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{42, 18}, {80, 24}, {120, 32}} {
		m := newModel()
		m.resize(size[0], size[1])
		for _, target := range []page{chatPage, assistantsPage, approvalPage, helpPage} {
			m.page = target
			view := m.View()
			lines := strings.Split(ansi.Strip(view), "\n")
			if len(lines) != size[1] {
				t.Fatalf("page %d: %d rows, want %d", target, len(lines), size[1])
			}
			if strings.TrimSpace(lines[len(lines)-1]) != "" || strings.TrimSpace(lines[len(lines)-2]) == "" || strings.TrimSpace(lines[len(lines)-3]) != "" {
				t.Fatal("footer must have exactly one blank row above and below")
			}
			if target == chatPage {
				if strings.TrimSpace(lines[len(lines)-4]) != "" || !strings.Contains(lines[len(lines)-5], "发消息") {
					t.Fatal("composer bottom padding is not the only row before footer")
				}
			}
		}
		m.help.GotoTop()
		if strings.Contains(ansi.Strip(m.help.View()), "## 聊天") || !strings.Contains(ansi.Strip(m.help.View()), "Ctrl+K") {
			t.Fatal("help was not rendered as Markdown")
		}
	}
}
