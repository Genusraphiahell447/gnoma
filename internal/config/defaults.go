package config

import "time"

func Defaults() Config {
	return Config{
		Provider: ProviderSection{
			Default:   "mistral",
			Model:     "",
			MaxTokens: 8192,
			APIKeys:   make(map[string]string),
			Endpoints: make(map[string]string),
		},
		Permission: PermissionSection{
			Mode: "auto",
		},
		Tools: ToolsSection{
			BashTimeout: Duration(30 * time.Second),
			MaxFileSize: 1 << 20, // 1MB
		},
		Session: SessionSection{MaxKeep: 20},
	}
}
