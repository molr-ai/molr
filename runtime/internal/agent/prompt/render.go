// Package prompt owns the model-facing presentation contract. Internal records
// must never be passed through wholesale: every structured field is selected by
// a schema and every output is rendered by a text template.
package prompt

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var files embed.FS
var templates = template.Must(template.New("prompts").Funcs(template.FuncMap{"quote": quote}).ParseFS(files, "templates/*.tmpl"))

func Text(name string, value any) string {
	var out bytes.Buffer
	if err := templates.ExecuteTemplate(&out, name+".tmpl", value); err != nil {
		panic(err)
	} // compiled templates and private presentation DTOs only
	return strings.TrimSpace(out.String())
}
func quote(value any) (string, error) {
	switch value.(type) {
	case nil, string, bool, json.Number:
	default:
		return "", fmt.Errorf("prompt scalar required")
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

type field struct {
	name      string
	shape     *shape
	keepEmpty bool
}
type shape struct {
	fields     []field
	element    *shape
	dictionary *shape
}
type pair struct {
	Key   string
	Value node
}
type node struct {
	Kind   string
	Scalar any
	Fields []pair
	Items  []node
}

var scalar = &shape{}

func object(names string, extra ...field) *shape {
	s := &shape{}
	for _, name := range strings.Fields(names) {
		s.fields = append(s.fields, field{name, scalar, false})
	}
	s.fields = append(s.fields, extra...)
	return s
}
func array(s *shape) *shape                { return &shape{element: s} }
func nested(name string, s *shape) field   { return field{name, s, false} }
func required(name string, s *shape) field { return field{name, s, true} }
func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == "" || x == "0001-01-01T00:00:00Z"
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false // false and zero are meaningful states, never silently dropped
}
func project(v any, s *shape) (node, error) {
	if v == nil {
		return node{Kind: "scalar"}, nil
	}
	if s.element != nil {
		rows, ok := v.([]any)
		if !ok {
			return node{}, fmt.Errorf("prompt array required")
		}
		n := node{Kind: "array"}
		for _, row := range rows {
			item, err := project(row, s.element)
			if err != nil {
				return node{}, err
			}
			n.Items = append(n.Items, item)
		}
		return n, nil
	}
	if s.fields != nil || s.dictionary != nil {
		values, ok := v.(map[string]any)
		if !ok {
			return node{}, fmt.Errorf("prompt object required")
		}
		n := node{Kind: "object"}
		fields := s.fields
		if s.dictionary != nil {
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				fields = append(fields, required(key, s.dictionary))
			}
		}
		for _, f := range fields {
			value, exists := values[f.name]
			if !exists || (!f.keepEmpty && empty(value)) {
				continue
			}
			child, err := project(value, f.shape)
			if err != nil {
				return node{}, fmt.Errorf("%s: %w", f.name, err)
			}
			if !f.keepEmpty && child.Kind == "object" && len(child.Fields) == 0 {
				continue
			}
			n.Fields = append(n.Fields, pair{f.name, child})
		}
		return n, nil
	}
	switch v.(type) {
	case string, bool, json.Number:
		return node{Kind: "scalar", Scalar: v}, nil
	}
	return node{}, fmt.Errorf("prompt scalar required")
}
func decode(raw string) (any, error) {
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	var tail any
	if err := d.Decode(&tail); err != io.EOF {
		return nil, fmt.Errorf("invalid trailing prompt data")
	}
	return v, nil
}
func structured(raw string, s *shape) (string, error) {
	v, err := decode(raw)
	if err != nil {
		return "", err
	}
	n, err := project(v, s)
	if err != nil {
		return "", err
	}
	return Text("json", n), nil
}

// Structured uses an explicit presentation schema, never reflection over a
// business record. New internal fields cannot accidentally cross this boundary.
func Structured(kind, raw string) (string, error) {
	s, ok := schemas[kind]
	if !ok {
		return "", fmt.Errorf("no prompt template for %s", kind)
	}
	return structured(raw, s)
}

func String(value string) string {
	out, err := quote(value)
	if err != nil {
		panic(err)
	}
	return out
}
