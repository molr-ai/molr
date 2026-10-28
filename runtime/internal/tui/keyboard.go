package tui

import (
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func (w *terminalWriter) keyboardFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		w.mu.Lock()
		if !w.keyboardRequested {
			_, err := w.File.WriteString(ansi.RequestKittyKeyboard)
			w.keyboardRequested = err == nil
		}
		w.mu.Unlock()
	}
	owned, ok := msg.(keyboardSequenceMsg)
	if !ok {
		return msg
	}
	sequence := string(owned)
	if strings.HasPrefix(sequence, "\x1b[?") && strings.HasSuffix(sequence, "u") {
		if _, err := strconv.ParseUint(sequence[3:len(sequence)-1], 10, 8); err == nil {
			w.mu.Lock()
			if w.keyboardRequested && !w.keyboardEnabled {
				_, err = w.File.WriteString(ansi.PushKittyKeyboard(ansi.KittyDisambiguateEscapeCodes))
				w.keyboardEnabled = err == nil
			}
			w.mu.Unlock()
		}
		return nil
	}
	if key, handled := extendedKeyboardKey(sequence); handled {
		return key
	}
	return msg
}

// CSI-u (Kitty/iTerm2) and xterm modifyOtherKeys retain modifiers that legacy
// Return cannot encode. Release events are consumed without executing actions.
func extendedKeyboardKey(sequence string) (tea.Msg, bool) {
	if !strings.HasPrefix(sequence, "\x1b[") || len(sequence) > 96 {
		return nil, false
	}
	code, modifiers, event := 0, 1, 1
	var err error
	switch {
	case strings.HasSuffix(sequence, "u"):
		parts := strings.Split(sequence[2:len(sequence)-1], ";")
		if len(parts) > 3 {
			return nil, true
		}
		code, err = strconv.Atoi(strings.Split(parts[0], ":")[0])
		if err != nil {
			return nil, true
		}
		if len(parts) > 1 && parts[1] != "" {
			mods := strings.Split(parts[1], ":")
			modifiers, err = strconv.Atoi(mods[0])
			if err != nil || len(mods) > 2 {
				return nil, true
			}
			if len(mods) > 1 {
				event, err = strconv.Atoi(mods[1])
				if err != nil {
					return nil, true
				}
			}
		}
	case strings.HasPrefix(sequence, "\x1b[27;") && strings.HasSuffix(sequence, "~"):
		parts := strings.Split(sequence[2:len(sequence)-1], ";")
		if len(parts) != 3 {
			return nil, true
		}
		modifiers, err = strconv.Atoi(parts[1])
		if err != nil {
			return nil, true
		}
		code, err = strconv.Atoi(parts[2])
		if err != nil {
			return nil, true
		}
	default:
		return nil, false
	}
	if event != 1 && event != 2 || modifiers < 1 || modifiers > 256 || code < 0 || code > utf8.MaxRune || code >= 0xd800 && code <= 0xdfff {
		return nil, true
	}
	mods := (modifiers - 1) & 63 // Caps/Num lock do not change shortcut identity.
	if mods&(8|16) != 0 {
		return nil, true
	} // Do not turn unsupported Super/Hyper shortcuts into Enter.
	alt := mods&(2|32) != 0 // iTerm2 may report Option as Alt or Meta.
	if mods&4 != 0 {
		switch {
		case code >= 'a' && code <= 'z':
			code = code - 'a' + 1
		case code >= 'A' && code <= 'Z':
			code = code - 'A' + 1
		case code == ' ' || code == '@':
			code = 0
		case code >= '[' && code <= '_':
			code &= 31
		case code == '?':
			code = 127
		}
	}
	switch code {
	case 13:
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: alt || mods&1 != 0}, true
	case 9:
		if mods&1 != 0 {
			return tea.KeyMsg{Type: tea.KeyShiftTab, Alt: alt}, true
		}
		return tea.KeyMsg{Type: tea.KeyTab, Alt: alt}, true
	case 127:
		return tea.KeyMsg{Type: tea.KeyBackspace, Alt: alt}, true
	}
	if code <= 31 {
		return tea.KeyMsg{Type: tea.KeyType(code), Alt: alt}, true
	}
	if code >= 57344 && code <= 63743 {
		return nil, true
	}
	if code == ' ' {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: alt}, true
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(code)}, Alt: alt}, true
}
