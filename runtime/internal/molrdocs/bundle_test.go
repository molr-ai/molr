package molrdocs

import (
	"encoding/hex"
	"encoding/json"
	"errors"
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
	paths := []string{directory + "/SKILL.md", directory + "/references/index.json"}
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
	if digest, err := hex.DecodeString(Revision); err != nil || len(digest) != 32 {
		t.Fatal("invalid bundle revision", Revision, err)
	}
	if _, err := fs.Stat(FS, directory+"/revision.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("bundle contains a generated revision file", err)
	}
	body, err = fs.ReadFile(FS, directory+"/SKILL.md")
	if err != nil || string(body) != Skill || strings.Contains(Skill, "bundle-revision:") {
		t.Fatal("Skill must match its embedded source without generated metadata", err)
	}
}

func TestBundleRevisionTracksContentsAndPaths(t *testing.T) {
	files := map[string]string{
		"content/SKILL.md":              "Read the documentation.",
		"content/references/index.json": "[]",
		"content/references/a.md":       "First document.",
		"content/references/b.md":       "Second document.",
	}
	bundle := func() fstest.MapFS {
		out := fstest.MapFS{}
		for name, body := range files {
			out[name] = &fstest.MapFile{Data: []byte(body)}
		}
		return out
	}
	want := bundleRevision(bundle())
	for range 10 {
		if got := bundleRevision(bundle()); got != want {
			t.Fatal("revision depends on map iteration order", got, want)
		}
	}
	for name := range files {
		t.Run(name, func(t *testing.T) {
			changed := bundle()
			changed[name].Data = append(changed[name].Data, '!')
			if bundleRevision(changed) == want {
				t.Fatal("content change did not change revision")
			}
			renamed := bundle()
			renamed[name+".renamed"] = renamed[name]
			delete(renamed, name)
			if bundleRevision(renamed) == want {
				t.Fatal("path change did not change revision")
			}
			removed := bundle()
			delete(removed, name)
			if bundleRevision(removed) == want {
				t.Fatal("removal did not change revision")
			}
		})
	}
	added := bundle()
	added["content/references/c.md"] = &fstest.MapFile{Data: []byte("Third document.")}
	if bundleRevision(added) == want {
		t.Fatal("addition did not change revision")
	}
}
