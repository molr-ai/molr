// Package releaseplatform reads the generated release platform registry.
package releaseplatform

import (
	_ "embed"
	"encoding/json"
)

//go:embed platforms.json
var data []byte

func Supported(os, arch string) bool {
	var registry struct {
		Platforms []struct {
			OS   string `json:"os"`
			Arch string `json:"arch"`
		} `json:"platforms"`
	}
	if json.Unmarshal(data, &registry) != nil {
		return false
	}
	for _, p := range registry.Platforms {
		if p.OS == os && p.Arch == arch {
			return true
		}
	}
	return false
}
