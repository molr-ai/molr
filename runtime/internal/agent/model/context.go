package model

// DefaultContextLimit applies only at resolution time; it is never saved as a
// manual model configuration. K uses 1024 tokens.
const DefaultContextLimit = 256 * 1024

func (m Model) ContextWindow() int {
	if m.EffectiveContextLimit > 0 {
		return m.EffectiveContextLimit
	}
	if m.ContextLimit > 0 {
		return m.ContextLimit
	}
	if m.ProviderContextLimit > 0 {
		return m.ProviderContextLimit
	}
	return DefaultContextLimit
}

// DefaultOutputLimit is used only when neither owner nor provider declares a limit.
const DefaultOutputLimit = 32 * 1024
const MaxOutputLimit = 1_000_000

func (m Model) OutputWindow() int {
	if m.EffectiveOutputLimit > 0 {
		return m.EffectiveOutputLimit
	}
	if m.OutputLimit > 0 {
		return m.OutputLimit
	}
	if m.ProviderOutputLimit > 0 {
		return m.ProviderOutputLimit
	}
	return DefaultOutputLimit
}
