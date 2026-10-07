package plugins

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
)

type ToolExecution struct {
	SettingsScope     string `json:"settingsScope,omitempty"`
	Type              string `json:"type"`
	ConnectionSetting string `json:"connectionSetting,omitempty"`
	Operation         string `json:"operation,omitempty"`
}
type Setting struct {
	Type               string          `json:"type"`
	Title              string          `json:"title"`
	Description        string          `json:"description,omitempty"`
	Default            json.RawMessage `json:"default,omitempty"`
	Enum               []string        `json:"enum,omitempty"`
	Minimum            *float64        `json:"minimum,omitempty"`
	Maximum            *float64        `json:"maximum,omitempty"`
	Providers          []string        `json:"providers,omitempty"`
	RequiredOperations []string        `json:"requiredOperations,omitempty"`
}
type HTTPProvider struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Origin     string                   `json:"origin"`
	Auth       HTTPAuth                 `json:"auth"`
	Operations map[string]HTTPOperation `json:"operations"`
	Test       *HTTPProbe               `json:"test,omitempty"`
}
type HTTPAuth struct {
	Type   string `json:"type"`
	Header string `json:"header,omitempty"`
}
type HTTPProbe struct {
	SettingsScope string          `json:"settingsScope,omitempty"`
	Operation     string          `json:"operation"`
	Arguments     json.RawMessage `json:"arguments"`
}

// A value is either a JSON constant or a reference, never executable text.
type HTTPValue struct {
	Value    json.RawMessage `json:"value,omitempty"`
	Argument string          `json:"argument,omitempty"`
	Setting  string          `json:"setting,omitempty"`
}
type HTTPOperation struct {
	Method      string               `json:"method"`
	Path        string               `json:"path"`
	InputSchema Schema               `json:"inputSchema"`
	Query       map[string]HTTPValue `json:"query,omitempty"`
	Body        map[string]HTTPValue `json:"body,omitempty"`
	Result      Projection           `json:"result"`
	Usage       *Projection          `json:"usage,omitempty"`
	ErrorField  string               `json:"errorField,omitempty"`
}

// Path is a JSON Pointer. Fields and Items form a small, bounded projection,
// and Type is also the result contract. Missing required values are errors.
type Projection struct {
	Type     string                `json:"type"`
	Path     string                `json:"path,omitempty"`
	Default  json.RawMessage       `json:"default,omitempty"`
	Fields   map[string]Projection `json:"fields,omitempty"`
	Items    *Projection           `json:"items,omitempty"`
	MaxItems int                   `json:"maxItems,omitempty"`
}

func (p Package) validateHTTP() error {
	m := p.Manifest
	if len(m.Settings) > 16 || len(m.AdminSettings) > 16 || len(m.Providers) > 8 {
		return invalid("too many settings or providers")
	}
	providers := map[string]HTTPProvider{}
	for _, v := range m.Providers {
		u, err := url.Parse(v.Origin)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || len(v.Origin) > 512 {
			return invalid("provider requires an exact HTTPS origin")
		}
		if !localID.MatchString(v.ID) || v.Name == "" || len(v.Name) > 120 || len(v.Operations) == 0 || len(v.Operations) > 16 {
			return invalid("invalid provider")
		}
		if _, ok := providers[v.ID]; ok {
			return invalid("duplicate provider")
		}
		providers[v.ID] = v
		switch v.Auth.Type {
		case "none", "bearer":
			if v.Auth.Header != "" {
				return invalid("unexpected authentication header")
			}
		case "header":
			h := http.CanonicalHeaderKey(v.Auth.Header)
			if h == "" || len(h) > 80 || strings.ContainsAny(h, "\r\n :\t") || slices.Contains([]string{"Host", "Cookie", "Content-Length", "Transfer-Encoding", "Connection", "Content-Type", "Accept", "Proxy-Authorization"}, h) {
				return invalid("invalid authentication header")
			}
			for _, ch := range h {
				if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
					return invalid("invalid authentication header")
				}
			}
		default:
			return invalid("unsupported authentication")
		}
		for name, op := range v.Operations {
			u, err := url.Parse(op.Path)
			if !localID.MatchString(name) || err != nil || !strings.HasPrefix(op.Path, "/") || strings.HasPrefix(op.Path, "//") || strings.ContainsAny(op.Path, "\\\r\n") || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || len(op.Path) > 1024 {
				return invalid("fixed operation path required")
			}
			if op.Method != "GET" && op.Method != "POST" || op.Method == "GET" && len(op.Body) > 0 || len(op.Query) > 32 || len(op.Body) > 32 {
				return invalid("invalid HTTP request")
			}
			if err := op.InputSchema.Validate(); err != nil {
				return err
			}
			for _, fields := range []map[string]HTTPValue{op.Query, op.Body} {
				for key, value := range fields {
					if key == "" || len(key) > 128 || strings.ContainsAny(key, "\r\n") {
						return invalid("invalid parameter name")
					}
					count := 0
					if value.Value != nil {
						count++
						if !json.Valid(value.Value) || len(value.Value) > 8192 {
							return invalid("invalid constant")
						}
					}
					if value.Argument != "" {
						count++
						if _, ok := op.InputSchema.Properties[value.Argument]; !ok {
							return invalid("unknown argument reference")
						}
					}
					if value.Setting != "" {
						count++
						f, ok := m.Settings[value.Setting]
						if !ok {
							f, ok = m.AdminSettings[value.Setting]
						}
						if !ok || f.Type == "connection" {
							return invalid("unknown setting reference")
						}
					}
					if count != 1 {
						return invalid("exactly one value source required")
					}
				}
			}
			if err := op.Result.validate(0); err != nil {
				return err
			}
			if op.Usage != nil {
				if err := op.Usage.validate(0); err != nil {
					return err
				}
			}
			if !validPointer(op.ErrorField) {
				return invalid("invalid error pointer")
			}
		}
		if v.Test != nil {
			schema, err := settingsSchema(m, v.Test.SettingsScope)
			if err != nil {
				return err
			}
			op, ok := v.Operations[v.Test.Operation]
			if !ok {
				return invalid("unknown probe operation")
			}
			if err := operationSettings(op, schema); err != nil {
				return err
			}
			if err := op.InputSchema.Check(v.Test.Arguments); err != nil {
				return err
			}
		}
	}
	for _, schema := range []map[string]Setting{m.Settings, m.AdminSettings} {
		for name, f := range schema {
			if !localID.MatchString(name) || f.Title == "" || len(f.Title) > 120 || len(f.Description) > 500 || len(f.Enum) > 32 {
				return invalid("invalid setting")
			}
			if f.Type == "connection" {
				if f.Default != nil || len(f.Providers) == 0 || len(f.Providers) > 8 || len(f.RequiredOperations) > 16 || len(f.Enum) > 0 || f.Minimum != nil || f.Maximum != nil {
					return invalid("invalid connection setting")
				}
				for _, id := range f.Providers {
					v, ok := providers[id]
					if !ok {
						return invalid("unknown provider")
					}
					for _, op := range f.RequiredOperations {
						if _, ok := v.Operations[op]; !ok {
							return invalid("provider missing required operation")
						}
					}
				}
			} else {
				if len(f.Providers) > 0 || len(f.RequiredOperations) > 0 || !slices.Contains([]string{"string", "boolean", "number", "integer"}, f.Type) {
					return invalid("unsupported setting type")
				}
				if f.Type != "string" && len(f.Enum) > 0 || (f.Minimum != nil || f.Maximum != nil) && f.Type != "number" && f.Type != "integer" || f.Minimum != nil && f.Maximum != nil && *f.Minimum > *f.Maximum {
					return invalid("invalid setting constraints")
				}
				if f.Default == nil {
					return invalid("non-connection setting requires a default")
				}
				var value any
				if json.Unmarshal(f.Default, &value) != nil || !validSettingValue(f, value) {
					return invalid("invalid setting default")
				}
			}
		}
	}
	for _, v := range []*Page{m.SettingsPage, m.AdminSettingsPage} {
		if v == nil {
			continue
		}
		if !localID.MatchString(v.ID) || v.Title == "" || len(v.Title) > 120 || !strings.HasSuffix(v.Entry, ".html") {
			return invalid("invalid settings page")
		}
		if _, ok := p.Files[v.Entry]; !ok {
			return invalid("missing settings page")
		}
		for _, cap := range v.Capabilities {
			if !slices.Contains(pageCapabilities, cap) {
				return invalid("invalid page capability")
			}
		}
	}
	for _, t := range m.Contributes.Tools {
		if t.Execution.Type != "http" {
			continue
		}
		schema, err := settingsSchema(m, t.Execution.SettingsScope)
		if err != nil {
			return err
		}
		f, ok := schema[t.Execution.ConnectionSetting]
		if !ok || f.Type != "connection" || !localID.MatchString(t.Execution.Operation) {
			return invalid("HTTP tool requires a connection setting and operation")
		}
		var shape string
		for _, id := range f.Providers {
			op, ok := providers[id].Operations[t.Execution.Operation]
			if !ok {
				return invalid("provider missing tool operation")
			}
			if err := operationSettings(op, schema); err != nil {
				return err
			}
			if !sameSchema(t.InputSchema, op.InputSchema) {
				return invalid("provider input contracts differ")
			}
			current := op.Result.shape()
			if shape != "" && shape != current {
				return invalid("provider result contracts differ")
			}
			shape = current
		}
	}
	return nil
}
func sameSchema(a, b Schema) bool {
	if len(a.Required) == 0 {
		a.Required = nil
	}
	if len(b.Required) == 0 {
		b.Required = nil
	}
	return reflect.DeepEqual(a, b)
}
func validPointer(p string) bool {
	if len(p) > 512 || p != "" && !strings.HasPrefix(p, "/") {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] == '~' {
			i++
			if i == len(p) || (p[i] != '0' && p[i] != '1') {
				return false
			}
		}
	}
	return true
}
func (p Projection) validate(depth int) error {
	if depth > 4 || !validPointer(p.Path) || len(p.Fields) > 32 {
		return invalid("projection exceeds limits")
	}
	if p.Default != nil {
		var v any
		if json.Unmarshal(p.Default, &v) != nil || len(p.Default) > 8192 {
			return invalid("invalid projection default")
		}
		if _, err := project(p, v, true); err != nil {
			return invalid("default does not match projection")
		}
	}
	switch p.Type {
	case "object":
		if p.Items != nil || p.MaxItems != 0 {
			return invalid("invalid object projection")
		}
		for k, v := range p.Fields {
			if k == "" || len(k) > 128 {
				return invalid("invalid projected field")
			}
			if err := v.validate(depth + 1); err != nil {
				return err
			}
		}
	case "array":
		if len(p.Fields) > 0 || p.Items == nil || p.MaxItems < 1 || p.MaxItems > 50 {
			return invalid("bounded array projection required")
		}
		return p.Items.validate(depth + 1)
	case "string", "number", "boolean":
		if len(p.Fields) > 0 || p.Items != nil || p.MaxItems != 0 {
			return invalid("invalid scalar projection")
		}
	default:
		return invalid("unsupported projection type")
	}
	return nil
}
func (p Projection) shape() string {
	fields := map[string]string{}
	for k, v := range p.Fields {
		fields[k] = v.shape()
	}
	item := ""
	if p.Items != nil {
		item = p.Items.shape()
	}
	raw, _ := json.Marshal(struct {
		Type, Item string
		Fields     map[string]string
	}{p.Type, item, fields})
	return string(raw)
}
func validSettingValue(f Setting, v any) bool {
	raw, _ := json.Marshal(map[string]any{"value": v})
	s := Schema{Type: "object", Properties: map[string]Property{"value": {Type: f.Type, Enum: f.Enum}}, Required: []string{"value"}}
	if s.Check(raw) != nil {
		return false
	}
	if n, ok := v.(float64); ok {
		if f.Minimum != nil && n < *f.Minimum || f.Maximum != nil && n > *f.Maximum {
			return false
		}
	}
	return true
}

func settingsSchema(m Manifest, scope string) (map[string]Setting, error) {
	switch scope {
	case "":
		return m.Settings, nil
	case "admin":
		return m.AdminSettings, nil
	default:
		return nil, invalid("invalid settings scope")
	}
}
func operationSettings(op HTTPOperation, schema map[string]Setting) error {
	for _, fields := range []map[string]HTTPValue{op.Query, op.Body} {
		for _, v := range fields {
			if v.Setting != "" {
				f, ok := schema[v.Setting]
				if !ok || f.Type == "connection" {
					return invalid("operation setting unavailable in selected scope")
				}
			}
		}
	}
	return nil
}
