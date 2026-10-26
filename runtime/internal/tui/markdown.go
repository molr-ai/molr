package tui

import (
	_ "embed"
	"html"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	markdownansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

//go:embed help.md
var helpMarkdown string

type markdownRenderer struct {
	terminal *glamour.TermRenderer
	width    int
}

func newMarkdownRenderer(width int) markdownRenderer {
	zero, one, two := uint(0), uint(1), uint(2)
	yes := true
	color := string(accentColor)
	style := styles.ASCIIStyleConfig
	style.Document = markdownansi.StyleBlock{Margin: &zero}
	quoteToken := quoteBar.Render("▌") + " "
	style.BlockQuote = markdownansi.StyleBlock{Indent: &one, IndentToken: &quoteToken, Margin: &zero}
	style.Heading = markdownansi.StyleBlock{StylePrimitive: markdownansi.StylePrimitive{Bold: &yes, Color: &color, BlockSuffix: "\n"}}
	style.H1, style.H2, style.H3 = markdownansi.StyleBlock{}, markdownansi.StyleBlock{}, markdownansi.StyleBlock{}
	style.H4, style.H5, style.H6 = markdownansi.StyleBlock{}, markdownansi.StyleBlock{}, markdownansi.StyleBlock{}
	style.Strong = markdownansi.StylePrimitive{Bold: &yes}
	style.Emph = markdownansi.StylePrimitive{Italic: &yes}
	style.Strikethrough = markdownansi.StylePrimitive{CrossedOut: &yes}
	style.Code = markdownansi.StyleBlock{StylePrimitive: markdownansi.StylePrimitive{Color: &color}}
	style.CodeBlock = markdownansi.StyleCodeBlock{StyleBlock: markdownansi.StyleBlock{
		StylePrimitive: markdownansi.StylePrimitive{Color: &color}, Margin: &two,
	}}
	style.Link = markdownansi.StylePrimitive{Color: &color, Underline: &yes}
	style.LinkText = markdownansi.StylePrimitive{Underline: &yes}
	width = max(1, width)
	renderer, _ := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(width),
		glamour.WithColorProfile(lipgloss.ColorProfile()), glamour.WithTableWrap(true), glamour.WithPreservedNewLines())
	return markdownRenderer{terminal: renderer, width: width}
}

var markdownEntity = regexp.MustCompile(`&(?:#[xX][0-9a-fA-F]+|#[0-9]+|[a-zA-Z][a-zA-Z0-9]+);?`)

func (r markdownRenderer) render(source string) string {
	source = cleanText(source)
	// Goldmark decodes entities. Remove encoded controls before they can become
	// terminal escape sequences, while preserving ordinary Markdown entities.
	source = markdownEntity.ReplaceAllStringFunc(source, func(entity string) string {
		decoded := html.UnescapeString(entity)
		if cleaned := cleanText(decoded); cleaned != decoded {
			return cleaned
		}
		return entity
	})
	prepared, tables := r.extractTables(source)
	return r.expandTables(r.renderBlock(prepared), tables)
}

func (r markdownRenderer) renderBlock(source string) string {
	out := source
	if r.terminal != nil {
		if rendered, err := r.terminal.Render(source); err == nil {
			out = rendered
		}
	}
	// Layout owns the outer spacing, including the user's shaded padding.
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		lines[i] = wrapMarkdownLine(line, max(1, r.width))
	}
	return strings.Join(lines, "\n")
}

func wrapMarkdownLine(line string, width int) string {
	plain := ansi.Strip(line)
	rest := strings.TrimLeft(plain, " ▌")
	prefixText := plain[:len(plain)-len(rest)]
	if strings.Contains(prefixText, "▌") {
		// Discard layout padding before rewrapping the wider quote rail.
		line = ansi.Truncate(line, lipgloss.Width(strings.TrimRight(plain, " \t")), "")
		plain = ansi.Strip(line)
		rest = strings.TrimLeft(plain, " ▌")
		prefixText = plain[:len(plain)-len(rest)]
		if rest == "" {
			return ansi.Truncate(line, width, "")
		}
	}
	prefixWidth := lipgloss.Width(prefixText)
	if !strings.Contains(prefixText, "▌") || prefixWidth >= width {
		return ansi.Hardwrap(line, width, true)
	}
	// Glamour indents physical lines; continue the quote rail when a long word
	// or CJK paragraph requires additional terminal-level wrapping.
	prefix := ansi.Cut(line, 0, prefixWidth)
	content := ansi.Cut(line, prefixWidth, lipgloss.Width(line))
	wrapped := strings.Split(ansi.Hardwrap(content, width-prefixWidth, true), "\n")
	for i, part := range wrapped {
		wrapped[i] = prefix + part
	}
	return strings.Join(wrapped, "\n")
}
