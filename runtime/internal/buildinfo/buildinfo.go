// Package buildinfo exposes the Runtime build identity.
package buildinfo

// Version is set by the release build using -ldflags -X.
var Version = "development"

func UserAgent() string { return "molr/" + Version }
