// Package plugins manages immutable extension packages, never in-process code.
package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"molr.ai/runtime/internal/core"
)

type Manifest struct {
	ManifestVersion   int                `json:"manifestVersion"`
	ID                string             `json:"id"`
	Name              string             `json:"name"`
	Description       string             `json:"description"`
	Version           string             `json:"version"`
	PluginAPI         int                `json:"pluginApi"`
	Contributes       Contributions      `json:"contributes"`
	AdminSettings     map[string]Setting `json:"adminSettings,omitempty"`
	AdminSettingsPage *Page              `json:"adminSettingsPage,omitempty"`
	Settings          map[string]Setting `json:"settings,omitempty"`
	SettingsPage      *Page              `json:"settingsPage,omitempty"`
	Providers         []HTTPProvider     `json:"providers,omitempty"`
}

type Contributions struct {
	Themes []Theme `json:"themes"`
	Pages  []Page  `json:"pages"`
	Tools  []Tool  `json:"tools"`
}
type Theme struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Pages        map[string]string `json:"pages"`
	Capabilities []string          `json:"capabilities"`
}
type Page struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Entry        string   `json:"entry"`
	Capabilities []string `json:"capabilities"`
}
type Tool struct {
	ID           string        `json:"id"`
	Description  string        `json:"description"`
	Execution    ToolExecution `json:"execution"`
	InputSchema  Schema        `json:"inputSchema"`
	Capabilities []string      `json:"capabilities"`
}

// Plugin API 3 deliberately accepts a small, fully validated JSON Schema subset.
type Schema struct {
	Type                 string              `json:"type"`
	Properties           map[string]Property `json:"properties"`
	Required             []string            `json:"required"`
	AdditionalProperties bool                `json:"additionalProperties"`
}
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}
type Package struct {
	Manifest Manifest          `json:"manifest"`
	Files    map[string]string `json:"files"`
}

var pluginID = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)
var localID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
var version = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var pageCapabilities = []string{"plans.read", "settings.read", "settings.write", "connections.read", "connections.test"}
var capabilities = append(append([]string{}, pageCapabilities...), "http.request")

func invalid(message string) error { return core.Invalid("plugin", message) }
func hash(value []byte) string     { h := sha256.Sum256(value); return hex.EncodeToString(h[:]) }
func Key(id string) core.ID        { return core.ID(hash([]byte(id))) }
func (p Package) Digest() string   { raw, _ := json.Marshal(p); return hash(raw) }
func (p Package) Validate() error {
	m := p.Manifest
	c := m.Contributes
	if m.ManifestVersion != 1 || m.PluginAPI != 3 || !pluginID.MatchString(m.ID) || len(m.ID) > 80 || strings.HasPrefix(m.ID, "molr.") || !version.MatchString(m.Version) || len(m.Version) > 32 || strings.TrimSpace(m.Name) == "" || len(m.Name) > 120 || len(m.Description) > 2000 {
		return invalid("invalid identity or incompatible plugin API")
	}
	raw, _ := json.Marshal(p)
	if len(raw) > MaxPackageBytes || len(p.Files) > 256 || len(p.Files) == 0 {
		return invalid("package exceeds limits")
	}
	for name, body := range p.Files {
		if len(name) > 160 || path.Clean(name) != name || strings.HasPrefix(name, "/") || name == "." || strings.Contains(name, "..") || strings.ContainsAny(name, "\\\x00") || !utf8.ValidString(body) || strings.ContainsRune(body, 0) || len(body) > 1<<20 {
			return invalid("invalid package file")
		}
	}
	entry := func(name string, suffix string) bool {
		_, ok := p.Files[name]
		return ok && strings.HasSuffix(name, suffix)
	}
	caps := func(items []string, allowed []string) bool {
		for _, v := range items {
			if !slices.Contains(allowed, v) {
				return false
			}
		}
		return len(items) <= len(allowed)
	}
	seen := map[string]bool{}
	unique := func(kind, id string) bool {
		k := kind + "/" + id
		if !localID.MatchString(id) || seen[k] {
			return false
		}
		seen[k] = true
		return true
	}
	if len(c.Themes)+len(c.Pages)+len(c.Tools) == 0 || len(c.Themes)+len(c.Pages)+len(c.Tools) > 24 {
		return invalid("1 to 24 contributions required")
	}
	for _, t := range c.Themes {
		if !unique("theme", t.ID) || strings.Contains(t.ID, "_") || t.Name == "" || len(t.Name) > 120 || len(t.Pages) == 0 || !caps(t.Capabilities, pageCapabilities) {
			return invalid("invalid theme")
		}
		for page, e := range t.Pages {
			if !slices.Contains([]string{"assistants", "plans", "resources", "workbench"}, page) || !entry(e, ".html") {
				return invalid("invalid theme page; settings is host-owned")
			}
		}
	}
	for _, v := range c.Pages {
		if !unique("page", v.ID) || v.Title == "" || len(v.Title) > 120 || !entry(v.Entry, ".html") || !caps(v.Capabilities, pageCapabilities) {
			return invalid("invalid page")
		}
	}
	for _, v := range c.Tools {
		if !unique("tool", v.ID) || v.Description == "" || len(v.Description) > 2000 {
			return invalid("invalid tool")
		}
		switch v.Execution.Type {
		case "http":
			if len(v.Capabilities) != 1 || v.Capabilities[0] != "http.request" {
				return invalid("HTTP tools require http.request only")
			}
		default:
			return invalid("tool execution type required")
		}
		if err := v.InputSchema.Validate(); err != nil {
			return err
		}
	}
	return p.validateHTTP()
}
func (s Schema) Validate() error {
	if s.Type != "object" || s.AdditionalProperties || len(s.Properties) > 16 || len(s.Required) > len(s.Properties) {
		return invalid("schema must be a closed object with at most 16 scalar properties")
	}
	for name, p := range s.Properties {
		if !localID.MatchString(name) || !slices.Contains([]string{"string", "number", "integer", "boolean"}, p.Type) || len(p.Description) > 500 || len(p.Enum) > 32 || (len(p.Enum) > 0 && p.Type != "string") {
			return invalid("unsupported schema property")
		}
	}
	for _, name := range s.Required {
		if _, ok := s.Properties[name]; !ok {
			return invalid("unknown required property")
		}
	}
	return nil
}
func (s Schema) Check(raw json.RawMessage) error {
	if len(raw) > 16<<10 {
		return invalid("tool arguments too large")
	}
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil || args == nil {
		return invalid("object arguments required")
	}
	for _, k := range s.Required {
		if _, ok := args[k]; !ok {
			return invalid("required argument missing")
		}
	}
	for k, v := range args {
		p, ok := s.Properties[k]
		if !ok {
			return invalid("unknown argument")
		}
		valid := false
		switch p.Type {
		case "string":
			str, ok := v.(string)
			valid = ok && len(str) <= 8192 && (len(p.Enum) == 0 || slices.Contains(p.Enum, str))
		case "boolean":
			_, valid = v.(bool)
		case "number", "integer":
			n, ok := v.(float64)
			valid = ok && (p.Type != "integer" || (n >= -9007199254740991 && n <= 9007199254740991 && n == float64(int64(n))))
		}
		if !valid {
			return invalid("argument type or value mismatch")
		}
	}
	return nil
}
func Requested(m Manifest) []string {
	out := []string{}
	add := func(v []string) {
		for _, s := range v {
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	if m.AdminSettingsPage != nil {
		add(m.AdminSettingsPage.Capabilities)
	}
	if m.SettingsPage != nil {
		add(m.SettingsPage.Capabilities)
	}
	for _, v := range m.Contributes.Pages {
		add(v.Capabilities)
	}
	for _, v := range m.Contributes.Themes {
		add(v.Capabilities)
	}
	for _, v := range m.Contributes.Tools {
		add(v.Capabilities)
	}
	slices.Sort(out)
	return out
}
