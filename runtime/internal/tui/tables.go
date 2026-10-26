package tui

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	tableast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

type compactTable struct {
	marker     string
	rows       [][]string
	align      []tableast.Alignment
	start, end int
	prefix     string
}

const (
	tableCellPadding = 1
	tableColumnGap   = 2
)

// Parse GFM tables instead of splitting on pipes: code fences, escaped pipes,
// inline formatting and surrounding Markdown keep their normal semantics.
func (r markdownRenderer) extractTables(source string) (string, []compactTable) {
	if !strings.Contains(source, "|") {
		return source, nil
	}
	data := []byte(source)
	doc := goldmark.New(goldmark.WithExtensions(extension.Table)).Parser().Parse(text.NewReader(data))
	var tables []compactTable
	marker := rune(0xe000)
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		table, ok := node.(*tableast.Table)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		t := compactTable{align: table.Alignments, start: -1}
		firstCell := -1
		for row := table.FirstChild(); row != nil; row = row.NextSibling() {
			cells := make([]string, len(t.align))
			col := 0
			for cell := row.FirstChild(); cell != nil && col < len(cells); cell = cell.NextSibling() {
				if firstCell < 0 && cell.Lines().Len() > 0 && row == table.FirstChild() {
					firstCell = cell.Lines().At(0).Start
				}
				raw := strings.ReplaceAll(string(cell.Lines().Value(data)), `\|`, "|")
				// A zero-width prefix keeps cell text such as '# title' or '- item'
				// inline when rendered outside the table's original AST context.
				lines := strings.Split(strings.Replace(r.renderBlock("\u200b"+raw), "\u200b", "", 1), "\n")
				for i, line := range lines {
					length := cellWidth(strings.TrimRight(ansi.Strip(line), " \t"))
					lines[i] = cellTruncate(line, length, "")
				}
				cells[col] = strings.Join(lines, "\n")
				col++
			}
			t.rows = append(t.rows, cells)
		}
		if firstCell < 0 {
			return ast.WalkSkipChildren, nil
		}
		t.start = bytes.LastIndexByte(data[:firstCell], '\n') + 1
		t.prefix = string(data[t.start:firstCell])
		if pipe := strings.LastIndex(t.prefix, "|"); pipe >= 0 {
			t.prefix = t.prefix[:pipe]
		}
		t.end = t.start
		// One physical line per row, plus the Markdown header delimiter line.
		for i := 0; i < len(t.rows)+1 && t.end < len(data); i++ {
			if newline := bytes.IndexByte(data[t.end:], '\n'); newline >= 0 {
				t.end += newline + 1
			} else {
				t.end = len(data)
			}
		}
		for strings.ContainsRune(source, marker) {
			marker++
		}
		t.marker = string(marker)
		marker++
		tables = append(tables, t)
		return ast.WalkSkipChildren, nil
	})
	var out strings.Builder
	position := 0
	for _, table := range tables {
		out.WriteString(source[position:table.start])
		out.WriteString(table.prefix + table.marker + "\n")
		position = table.end
	}
	out.WriteString(source[position:])
	return out.String(), tables
}

func (r markdownRenderer) expandTables(rendered string, tables []compactTable) string {
	for _, table := range tables {
		var result []string
		for _, line := range strings.Split(rendered, "\n") {
			position := strings.Index(line, table.marker)
			if position < 0 {
				result = append(result, line)
				continue
			}
			prefix := line[:position]
			continuation := prefix
			// Keep quote prefixes, but don't repeat a list bullet on every row.
			plain := ansi.Strip(prefix)
			if tableListPrefix.MatchString(plain) {
				continuation = tableListPrefix.ReplaceAllStringFunc(plain, func(bullet string) string { return strings.Repeat(" ", lipgloss.Width(bullet)) })
				continuation = strings.ReplaceAll(continuation, "▌", quoteBar.Render("▌"))
			}
			rows := strings.Split(table.render(max(1, r.width-lipgloss.Width(prefix))), "\n")
			for i, row := range rows {
				if i == 0 {
					result = append(result, prefix+row)
				} else {
					result = append(result, continuation+row)
				}
			}
		}
		rendered = strings.Join(result, "\n")
	}
	return rendered
}

var tableListPrefix = regexp.MustCompile(`(?:• |[0-9]+\. )$`)

func (t compactTable) render(width int) string {
	if len(t.align) == 0 {
		return ""
	}
	// Extremely wide tables are shown in consecutive column groups rather than
	// dropping columns or allowing the whole terminal to overflow.
	columns := max(1, (width+tableColumnGap)/(2+2*tableCellPadding+tableColumnGap))
	var groups []string
	for start := 0; start < len(t.align); start += columns {
		end := min(len(t.align), start+columns)
		groups = append(groups, t.renderColumns(start, end, width))
	}
	return strings.Join(groups, "\n\n")
}

func (t compactTable) renderColumns(start, end, width int) string {
	widths := make([]int, end-start)
	for _, row := range t.rows {
		for col := start; col < end; col++ {
			widths[col-start] = max(widths[col-start], cellWidth(row[col]), 1)
		}
	}
	available := max(len(widths), width-tableColumnGap*(len(widths)-1)-2*tableCellPadding*len(widths))
	for {
		total, largest := 0, 0
		for i, w := range widths {
			total += w
			if w > widths[largest] {
				largest = i
			}
		}
		if total <= available || widths[largest] <= 2 {
			break
		}
		widths[largest]--
	}
	var out []string
	for rowIndex, row := range t.rows {
		wrapped := make([][]string, len(widths))
		height := 1
		for col := start; col < end; col++ {
			masked, restore := layoutKeycaps(row[col])
			wrapped[col-start] = strings.Split(restore(ansi.Wrap(masked, widths[col-start], "")), "\n")
			height = max(height, len(wrapped[col-start]))
		}
		for line := 0; line < height; line++ {
			cells := make([]string, len(widths))
			for i, w := range widths {
				value := ""
				if line < len(wrapped[i]) {
					value = wrapped[i][line]
				}
				alignment := lipgloss.Left
				switch t.align[start+i] {
				case tableast.AlignCenter:
					alignment = lipgloss.Center
				case tableast.AlignRight:
					alignment = lipgloss.Right
				}
				style := lipgloss.NewStyle().Width(w+2*tableCellPadding).Padding(0, tableCellPadding).Align(alignment)
				if rowIndex == 0 {
					style = style.Inherit(tableHeader)
				}
				masked, restore := layoutKeycaps(value)
				cells[i] = restore(style.Render(masked))
			}
			out = append(out, strings.Join(cells, strings.Repeat(" ", tableColumnGap)))
		}
		if rowIndex == 0 || rowIndex < len(t.rows)-1 {
			stroke := "─"
			if rowIndex == 0 {
				stroke = "━"
			}
			cells := make([]string, len(widths))
			for i, w := range widths {
				cells[i] = tableRule.Render(strings.Repeat(stroke, w+2*tableCellPadding))
			}
			out = append(out, strings.Join(cells, strings.Repeat(" ", tableColumnGap)))
		}
	}
	return strings.Join(out, "\n")
}
