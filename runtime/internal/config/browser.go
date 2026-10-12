package config

// Browser programs are deployment assets, mounted read-only into browser tasks.
// Empty paths resolve to libexec/browser next to the Runtime executable.
type Browser struct {
	AgentDirectory    string `json:"agent_directory"`
	ChromiumDirectory string `json:"chromium_directory"`
}
