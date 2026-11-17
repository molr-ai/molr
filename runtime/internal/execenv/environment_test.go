package execenv

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCaptureDefaultAndExplicitSelection(t *testing.T) {
	path := t.TempDir()
	env := []string{"PATH=" + path, "JAVA_HOME=first", "JAVA_HOME=second", "SECRET=not-selected", "EMPTY="}
	got, err := Capture("", env)
	if err != nil || !reflect.DeepEqual(got, map[string]string{"PATH": path}) {
		t.Fatal(got, err)
	}
	got, err = Capture("JAVA_HOME, EMPTY,JAVA_HOME", env)
	if err != nil || !reflect.DeepEqual(got, map[string]string{"JAVA_HOME": "second", "EMPTY": ""}) {
		t.Fatal(got, err)
	}
	if runtime.GOOS == "windows" {
		got, err = Capture("Path", []string{"Path=" + path})
		if err != nil || got["PATH"] != path {
			t.Fatal(got, err)
		}
	}
	for _, tc := range []struct {
		names string
		env   []string
	}{
		{"MISSING", env}, {"PATH,", env}, {"PATH=wrong", env}, {"MOLR_WORKSPACE", []string{"MOLR_WORKSPACE=bad"}},
		{"", []string{"PATH="}}, {"", []string{"PATH=relative"}}, {"", []string{"PATH=" + path + string(os.PathListSeparator)}},
		{"CUSTOM", []string{"CUSTOM=secret\x00value"}}, {"CUSTOM", []string{"CUSTOM=" + strings.Repeat("x", maxBytes)}},
	} {
		if _, err = Capture(tc.names, tc.env); err == nil {
			t.Fatalf("accepted invalid selection %q", tc.names)
		}
	}
}

func save(t *testing.T, directory string, values map[string]string) {
	t.Helper()
	data, err := EncodeUpdate(directory, values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, Filename), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEnvironmentRefreshPreservesUnselectedAndInheritedValues(t *testing.T) {
	dir := privateDirectory(t)
	inherited := []string{"PATH=" + filepath.Join(dir, "original"), "OTHER=preserved"}
	before := append([]string{}, inherited...)
	got, err := Environment(dir, inherited)
	if err != nil || !reflect.DeepEqual(got, inherited) {
		t.Fatal(got, err)
	}
	save(t, dir, map[string]string{"PATH": filepath.Join(dir, "first"), "JAVA_HOME": "jdk", "EMPTY": ""})
	save(t, dir, map[string]string{"PATH": filepath.Join(dir, "second")})
	got, err = Environment(dir, inherited)
	want := []string{"OTHER=preserved", "EMPTY=", "JAVA_HOME=jdk", "PATH=" + filepath.Join(dir, "second")}
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(inherited, before) {
		t.Fatal(got, err)
	}
	values, err := Read(dir)
	if err != nil || values["JAVA_HOME"] != "jdk" {
		t.Fatal(values, err)
	}
	if _, err = EncodeUpdate(dir, map[string]string{"PATH": "relative"}); err == nil {
		t.Fatal("invalid update accepted")
	}
	values, err = Read(dir)
	if err != nil || values["PATH"] != filepath.Join(dir, "second") {
		t.Fatal("failed update changed saved values", err)
	}
}

func TestReadRejectsUnsafeOrMalformedEnvironment(t *testing.T) {
	dir := privateDirectory(t)
	path := filepath.Join(dir, Filename)
	for _, content := range []string{`null`, `[]`, `{"PATH":"relative"}`, `{"BAD-NAME":"value"}`, `{"CUSTOM":"private-value"`, strings.Repeat(" ", maxBytes+1)} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Environment(dir, nil); err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatal("malformed environment accepted or value leaked", err)
		}
	}
	if runtime.GOOS != "windows" {
		os.WriteFile(path, []byte(`{}`), 0600)
		os.Chmod(path, 0644)
		if _, err := Read(dir); err == nil {
			t.Fatal("public environment accepted")
		}
		os.Remove(path)
		target := filepath.Join(dir, "target")
		os.WriteFile(target, []byte(`{}`), 0600)
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(dir); err == nil {
			t.Fatal("symlink accepted")
		}
		os.Remove(path)
		if err := os.Link(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(dir); err == nil {
			t.Fatal("hard link accepted")
		}
	}
	if _, err := Read("relative"); err == nil {
		t.Fatal("relative private root accepted")
	}
}

func privateDirectory(t *testing.T) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}
