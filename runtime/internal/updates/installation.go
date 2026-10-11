package updates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

// Installation is administrator-only metadata used to produce an exact upgrade command.
type Installation struct {
	Name        string `json:"name"`
	User        string `json:"user"`
	System      bool   `json:"system"`
	ProgramRoot string `json:"program_root"`
	ConfigHome  string `json:"config_home"`
	DataHome    string `json:"data_home"`
	CacheHome   string `json:"cache_home"`
	Source      string `json:"source"`
}

func ReadInstallation(configPath, source string) *Installation {
	if configPath == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "installation.json"))
	if err != nil || len(data) > 65536 {
		return nil
	}
	var record struct {
		Schema      int               `json:"schema"`
		Name        string            `json:"name"`
		User        string            `json:"user"`
		System      bool              `json:"system"`
		Complete    bool              `json:"complete"`
		ProgramRoot string            `json:"program_root"`
		Paths       map[string]string `json:"paths"`
	}
	if json.Unmarshal(data, &record) != nil {
		return nil
	}
	executable, err := os.Executable()
	if err != nil || record.Schema != 1 || !record.Complete || !regexp.MustCompile(`^molr(?:-[a-z0-9][a-z0-9-]{0,47})?$`).MatchString(record.Name) || record.Paths["prefix"] != filepath.Dir(filepath.Dir(executable)) || record.Paths["config"] != filepath.Dir(configPath) || record.ProgramRoot != filepath.Dir(record.Paths["prefix"]) {
		return nil
	}
	for _, key := range []string{"config", "data", "cache"} {
		if !filepath.IsAbs(record.Paths[key]) {
			return nil
		}
	}
	return &Installation{Name: record.Name, User: record.User, System: record.System, ProgramRoot: record.ProgramRoot, Source: source, ConfigHome: filepath.Dir(filepath.Dir(record.Paths["config"])), DataHome: filepath.Dir(filepath.Dir(record.Paths["data"])), CacheHome: filepath.Dir(filepath.Dir(record.Paths["cache"]))}
}
