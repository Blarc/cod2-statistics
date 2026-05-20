package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr          string
	DBPath              string
	LokiURL             string
	LokiQuery           string
	LokiUsername        string
	LokiPassword        string
	PollInterval        time.Duration
	LokiInitialLookback time.Duration
}

type yamlFile struct {
	Server struct {
		ListenAddr string `yaml:"listen_addr"`
		DBPath     string `yaml:"db_path"`
	} `yaml:"server"`
	Loki struct {
		URL             string `yaml:"url"`
		Query           string `yaml:"query"`
		Username        string `yaml:"username"`
		Password        string `yaml:"password"`
		PollInterval    string `yaml:"poll_interval"`
		InitialLookback string `yaml:"initial_lookback"`
	} `yaml:"loki"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		ListenAddr:          ":8080",
		DBPath:              "/data/cod2stats.db",
		PollInterval:        10 * time.Second,
		LokiInitialLookback: 24 * time.Hour,
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config %q: %w", path, err)
		}
		var yc yamlFile
		if err := yaml.Unmarshal(data, &yc); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if yc.Server.ListenAddr != "" {
			cfg.ListenAddr = yc.Server.ListenAddr
		}
		if yc.Server.DBPath != "" {
			cfg.DBPath = yc.Server.DBPath
		}
		if yc.Loki.URL != "" {
			cfg.LokiURL = yc.Loki.URL
		}
		if yc.Loki.Query != "" {
			cfg.LokiQuery = yc.Loki.Query
		}
		if yc.Loki.Username != "" {
			cfg.LokiUsername = yc.Loki.Username
		}
		if yc.Loki.Password != "" {
			cfg.LokiPassword = yc.Loki.Password
		}
		if yc.Loki.PollInterval != "" {
			d, err := time.ParseDuration(yc.Loki.PollInterval)
			if err != nil {
				return nil, fmt.Errorf("parse poll_interval: %w", err)
			}
			cfg.PollInterval = d
		}
		if yc.Loki.InitialLookback != "" {
			d, err := time.ParseDuration(yc.Loki.InitialLookback)
			if err != nil {
				return nil, fmt.Errorf("parse initial_lookback: %w", err)
			}
			cfg.LokiInitialLookback = d
		}
	}

	if v := os.Getenv("LOKI_URL"); v != "" {
		cfg.LokiURL = v
	}
	if v := os.Getenv("LOKI_QUERY"); v != "" {
		cfg.LokiQuery = v
	}
	if v := os.Getenv("LOKI_USERNAME"); v != "" {
		cfg.LokiUsername = v
	}
	if v := os.Getenv("LOKI_PASSWORD"); v != "" {
		cfg.LokiPassword = v
	}
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("parse POLL_INTERVAL: %w", err)
		}
		cfg.PollInterval = d
	}
	if v := os.Getenv("LOKI_INITIAL_LOOKBACK"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("parse LOKI_INITIAL_LOOKBACK: %w", err)
		}
		cfg.LokiInitialLookback = d
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}

	return cfg, nil
}
