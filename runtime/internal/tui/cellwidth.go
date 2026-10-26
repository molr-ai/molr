package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var keycapEmoji = regexp.MustCompile("[0-9#*]\ufe0f?\u20e3")

// The pinned terminal libraries count emoji keycaps as one cell, while Windows
// Terminal displays them in two. Use collision-free wide glyphs during layout,
// then restore the exact original text before displaying or copying it.
func layoutKeycaps(text string) (string, func(string) string) {
	if !strings.ContainsRune(text, '\u20e3') || !keycapEmoji.MatchString(text) {
		return text, func(s string) string { return s }
	}
	replacements := []string{}
	tokens := map[string]string{}
	candidate := rune(0x3400)
	masked := keycapEmoji.ReplaceAllStringFunc(text, func(cluster string) string {
		if token, ok := tokens[cluster]; ok {
			return token
		}
		for strings.ContainsRune(text, candidate) || ansi.StringWidth(string(candidate)) != 2 {
			candidate++
		}
		token := string(candidate)
		candidate++
		tokens[cluster] = token
		replacements = append(replacements, token, cluster)
		return token
	})
	restore := strings.NewReplacer(replacements...)
	return masked, restore.Replace
}

func cellWidth(text string) int {
	masked, _ := layoutKeycaps(text)
	return lipgloss.Width(masked)
}
func cellCut(text string, start, end int) string {
	masked, restore := layoutKeycaps(text)
	return restore(ansi.Cut(masked, start, end))
}
func cellTruncate(text string, width int, tail string) string {
	masked, restore := layoutKeycaps(text)
	return restore(ansi.Truncate(masked, width, tail))
}
