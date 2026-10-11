// Package buildinfo exposes the Runtime build identity.
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

// Release builds inject these values using -ldflags -X.
var Version = "development"
var Commit = ""
var BuiltAt = ""

type Info struct {
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	BuiltAt       string `json:"built_at"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	EmbeddedWebUI bool   `json:"embedded_webui"`
}

func Current() Info {
	info := Info{Version: Version, Commit: Commit, BuiltAt: BuiltAt, OS: runtime.GOOS, Arch: runtime.GOARCH}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range build.Settings {
			if setting.Key == "vcs.revision" && info.Commit == "" {
				info.Commit = setting.Value
			}
			if setting.Key == "vcs.time" && info.BuiltAt == "" {
				info.BuiltAt = setting.Value
			}
		}
	}
	return info
}
func UserAgent() string { return "molr/" + Version }
