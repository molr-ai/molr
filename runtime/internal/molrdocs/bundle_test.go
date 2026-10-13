package molrdocs

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestBundleFilesystemAndReferences(t *testing.T) {
	var entries []struct {
		Path, ContentLanguage string
		Titles, Topics, URLs  map[string]string
	}
	body, err := fs.ReadFile(FS, directory+"/references/index.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(body, &entries); err != nil {
		t.Fatal(err)
	}
	paths := []string{directory + "/SKILL.md", directory + "/references/index.json", directory + "/revision.txt"}
	for _, entry := range entries {
		if entry.ContentLanguage != "zh" || entry.Titles["zh"] == "" || entry.Titles["en"] == "" || entry.Topics["zh"] == "" || entry.Topics["en"] == "" ||
			!strings.HasPrefix(entry.URLs["zh"], "https://molr.ai/zh/") ||
			entry.URLs["en"] != strings.Replace(entry.URLs["zh"], "/zh/", "/en/", 1) {
			t.Fatal(entry)
		}
		name := strings.TrimPrefix(entry.Path, Root+"/")
		paths = append(paths, name)
		if _, err = fs.ReadFile(FS, name); err != nil {
			t.Fatal(err)
		}
	}
	if err = fstest.TestFS(FS, paths...); err != nil {
		t.Fatal(err)
	}
	if len(Revision) != 64 || !strings.Contains(Skill, Revision) {
		t.Fatal("missing bundle revision")
	}
}
