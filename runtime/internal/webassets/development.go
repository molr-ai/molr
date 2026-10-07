//go:build !webui

package webassets

import (
	"embed"
	"io/fs"
)

//go:embed fallback/index.html
var fallback embed.FS
var assets, _ = fs.Sub(fallback, "fallback")
