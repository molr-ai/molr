// Package molrdocs contains the reviewed documentation shipped with this Runtime.
package molrdocs

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"strings"
)

//go:generate node ../../../scripts/generate-molr-docs.mjs
//go:embed all:content
var content embed.FS

const Root = "/molr"
const SkillPath = Root + "/.agents/skills/molr-docs/SKILL.md"
const directory = ".agents/skills/molr-docs"

// Compute once during package initialization; file tools and Skill contexts reuse it.
var Revision = bundleRevision(content)
var Skill = mustRead("content/SKILL.md")

func bundleRevision(bundle fs.FS) string {
	hash := sha256.New()
	// WalkDir visits paths in lexical order, independent of generation order.
	err := fs.WalkDir(bundle, "content", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		body, err := fs.ReadFile(bundle, name)
		if err != nil {
			return err
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(body)
		hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func mustRead(name string) string {
	b, err := content.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// FS exposes only the immutable bundle, with the same paths used by file tools.
var FS fs.FS = bundleFS{}

type bundleFS struct{}

func (bundleFS) Open(name string) (fs.File, error) {
	if name == directory || strings.HasPrefix(name, directory+"/") {
		file, err := content.Open("content" + strings.TrimPrefix(name, directory))
		if err == nil && name == directory {
			return namedDirectory{file.(fs.ReadDirFile)}, nil
		}
		return file, err
	}
	// The three virtual ancestors contain exactly one directory each.
	children := map[string]string{".": ".agents", ".agents": "skills", ".agents/skills": "molr-docs"}
	if child, ok := children[name]; ok {
		return &ancestor{name: name, child: child}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

type namedDirectory struct{ fs.ReadDirFile }

func (namedDirectory) Stat() (fs.FileInfo, error) { return dirInfo{"molr-docs"}, nil }
