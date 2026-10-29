package agent

import "molr.ai/runtime/internal/core"

type CatalogSkill struct {
	Path        string
	Name        string
	Description string
	Source      string
	AssistantID core.ID
	Version     string
}
type SkillAssistant struct {
	ID   core.ID
	Name string
}
type SkillsCatalog struct {
	Items      []CatalogSkill
	Assistants []SkillAssistant
}

type SkillDocument struct{ Text, Version string }
