// Package execenv captures explicitly selected host variables for Full Access tasks.
package execenv

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/textfiles"
)

const Filename = "execution-environment.json"
const maxBytes = 128 << 10

var variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func canonical(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func validate(values map[string]string) error {
	for name, value := range values {
		if !variableName.MatchString(name) || canonical(name) != name || strings.HasPrefix(strings.ToUpper(name), "MOLR_") {
			return fmt.Errorf("invalid or reserved execution environment variable name")
		}
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("execution environment variable %s contains NUL", name)
		}
		if canonical(name) == "PATH" {
			if value == "" {
				return fmt.Errorf("PATH must contain absolute directories")
			}
			for _, path := range filepath.SplitList(value) {
				if !filepath.IsAbs(path) {
					return fmt.Errorf("PATH must not contain empty or relative entries")
				}
			}
		}
	}
	data, err := json.Marshal(values)
	if err != nil || len(data) > maxBytes {
		return fmt.Errorf("execution environment exceeds 128 KiB")
	}
	return nil
}

// Capture defaults to PATH. An explicit comma-separated list replaces that
// selection; missing variables fail the whole capture without exposing values.
func Capture(names string, environment []string) (map[string]string, error) {
	if names == "" {
		names = "PATH"
	}
	available := map[string]string{}
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			available[canonical(name)] = value
		}
	}
	selected := map[string]string{}
	for _, name := range strings.Split(names, ",") {
		name = canonical(strings.TrimSpace(name))
		if !variableName.MatchString(name) {
			return nil, fmt.Errorf("environment requires comma-separated variable names")
		}
		value, ok := available[name]
		if !ok {
			return nil, fmt.Errorf("environment variable %s is not exported in the invoking process", name)
		}
		selected[name] = value
	}
	return selected, validate(selected)
}

// Read returns an empty override for instances created before environment import.
// Malformed or accessible-to-other-users files fail closed, without logging values.
func Read(directory string) (map[string]string, error) {
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("absolute private directory required for execution environment")
	}
	f, err := textfiles.Open(directory, Filename)
	if core.CodeOf(err) == core.NotFound || os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot open execution environment: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxBytes || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("execution environment must be a private regular file of at most 128 KiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read execution environment")
	}
	var values map[string]string
	if len(data) > maxBytes || json.Unmarshal(data, &values) != nil || values == nil {
		return nil, fmt.Errorf("invalid execution environment JSON")
	}
	return values, validate(values)
}

// EncodeUpdate preserves variables outside the explicit selection. Writers must
// serialize updates and atomically replace the private file with these bytes.
func EncodeUpdate(directory string, selected map[string]string) ([]byte, error) {
	if err := validate(selected); err != nil {
		return nil, err
	}
	values, err := Read(directory)
	if err != nil {
		return nil, err
	}
	for name, value := range selected {
		values[canonical(name)] = value
	}
	if err := validate(values); err != nil {
		return nil, err
	}
	data, err := json.Marshal(values)
	return data, err
}

// Environment is read on task admission, never by a background poller. Updating
// the file affects subsequent tasks without mutating the Runtime's environment.
func Environment(directory string, inherited []string) ([]string, error) {
	values, err := Read(directory)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(inherited)+len(values))
	for _, entry := range inherited {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := values[canonical(name)]; !replaced {
			result = append(result, entry)
		}
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result, nil
}
