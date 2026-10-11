//go:build webui

package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var content embed.FS
var assets, _ = fs.Sub(content, "dist")

const Embedded = true
