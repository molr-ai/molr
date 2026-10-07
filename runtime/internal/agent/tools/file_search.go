package tools

import (
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

type FileSearchRequest struct {
	FileTarget
	Pattern       string   `json:"pattern,omitempty"`
	Regex         bool     `json:"regex,omitempty"`
	CaseSensitive *bool    `json:"case_sensitive,omitempty"`
	Glob          []string `json:"glob,omitempty"`
	ExcludeGlob   []string `json:"exclude_glob,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	ContextLines  int      `json:"context_lines,omitempty"`
	IncludeHidden bool     `json:"include_hidden,omitempty"`
	MaxResults    int      `json:"max_results,omitempty"`
}

func (r FileSearchRequest) SearchOptions() textfiles.SearchOptions {
	sensitive := true
	if r.CaseSensitive != nil {
		sensitive = *r.CaseSensitive
	}
	return textfiles.SearchOptions{Pattern: r.Pattern, Regex: r.Regex, CaseSensitive: sensitive, Glob: r.Glob, ExcludeGlob: r.ExcludeGlob, Mode: r.Mode, ContextLines: r.ContextLines, IncludeHidden: r.IncludeHidden, MaxResults: r.MaxResults}
}
func (r FileSearchRequest) Validate() error {
	if len(r.Path) > 4096 {
		return core.Invalid("path", "path exceeds limit")
	}
	return r.SearchOptions().Validate()
}
