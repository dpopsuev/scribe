package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/store"
	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration loaded from scribe.yaml.
type Config struct {
	DB        string       `yaml:"db"`
	Transport string       `yaml:"transport"`
	Addr      string       `yaml:"addr"`
	Scopes    []string     `yaml:"scopes"`
	Schema    *model.Schema `yaml:"schema"`
}

// Load reads a config file from path and returns a merged Config.
// Environment variables override file values for db/transport/addr.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.applyDefaults()
	cfg.applyEnvOverrides()
	return &cfg, nil
}

// Resolve walks the resolution order to find and load a config file:
//  1. explicit path (from --config flag)
//  2. $SCRIBE_CONFIG
//  3. ./scribe.yaml
//  4. ~/.scribe/scribe.yaml
//  5. no file → built-in defaults
func Resolve(explicit string) (*Config, error) {
	candidates := []string{explicit}
	if v := os.Getenv("SCRIBE_CONFIG"); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates, "scribe.yaml")
	if root := os.Getenv("SCRIBE_ROOT"); root != "" {
		candidates = append(candidates, filepath.Join(root, "scribe.yaml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".scribe", "scribe.yaml"))
	}

	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return Load(path)
		}
	}

	cfg := defaults()
	cfg.applyEnvOverrides()
	return &cfg, nil
}

func defaults() Config {
	return Config{
		DB:        store.DefaultSQLitePath(),
		Transport: "stdio",
		Addr:      ":8080",
		Schema:    model.DefaultSchema(),
	}
}

func (c *Config) applyDefaults() {
	if c.DB == "" {
		c.DB = store.DefaultSQLitePath()
	}
	if c.Transport == "" {
		c.Transport = "stdio"
	}
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	if c.Schema == nil {
		c.Schema = model.DefaultSchema()
	}
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("SCRIBE_DB"); v != "" {
		c.DB = v
	}
	if v := os.Getenv("SCRIBE_TRANSPORT"); v != "" {
		c.Transport = v
	}
	if v := os.Getenv("SCRIBE_ADDR"); v != "" {
		c.Addr = v
	}
}
