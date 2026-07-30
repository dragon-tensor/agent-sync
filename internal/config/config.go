package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	DBPath  string `json:"db_path"`
	DataDir string `json:"data_dir"`

	defaultPaths map[string]string `json:"-"`
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".agent-sync")
	if override := os.Getenv("DRAGON_SYNC_DATA_DIR"); override != "" {
		dataDir = override
	}

	config := &Config{
		DBPath:  filepath.Join(dataDir, "agent-sync.db"),
		DataDir: dataDir,
		defaultPaths: map[string]string{
			"claude-code": filepath.Join(home, ".claude", "projects"),
			"opencode":    filepath.Join(home, ".local", "share", "opencode"),
			"codex":       filepath.Join(home, ".codex", "sessions"),
		},
	}
	if override := os.Getenv("DRAGON_SYNC_DB_PATH"); override != "" {
		config.DBPath = override
	}
	return config
}

func (c *Config) DefaultPath(provider string) string {
	if p, ok := c.defaultPaths[provider]; ok {
		return p
	}
	return ""
}

func Load() (*Config, error) {
	cfg := DefaultConfig()
	path := filepath.Join(cfg.DataDir, "config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func (c *Config) Save() error {
	if err := os.MkdirAll(c.DataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(c.DataDir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
