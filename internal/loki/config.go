package loki

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds connection parameters for a Loki instance.
type Config struct {
	URL      string `yaml:"url"`
	Query    string `yaml:"query"`
	Start    string `yaml:"start"` // RFC3339 or empty (defaults to 1h ago)
	End      string `yaml:"end"`   // RFC3339 or empty (defaults to now)
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// LoadConfig reads a YAML config file from path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read loki config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse loki config: %w", err)
	}
	return &cfg, nil
}
