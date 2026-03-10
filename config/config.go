package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/store"
	"gopkg.in/yaml.v3"
)

// resolvedPath tracks the config file path found by Resolve, so Save
// can write back to the same location.
var resolvedPath string

// Vocabulary defines the enforced set of artifact kinds.
// If Kinds is nil or empty, validation is off (backward-compatible).
type Vocabulary struct {
	Kinds []string `yaml:"kinds"`
}

// Config is the top-level configuration loaded from scribe.yaml.
type Config struct {
	DB               string              `yaml:"db"`
	Transport        string              `yaml:"transport"`
	Addr             string              `yaml:"addr"`
	Scopes           []string            `yaml:"scopes"`
	Workspaces       map[string][]string `yaml:"workspaces,omitempty"`
	Schema           *model.Schema       `yaml:"schema"`
	Vocabulary       *Vocabulary         `yaml:"vocabulary"`
	IDFormat         string              `yaml:"id_format"`
	ScopeKeys        map[string]string   `yaml:"scope_keys"`
	KindCodes        map[string]string   `yaml:"kind_codes"`
	MutableCreatedAt *bool               `yaml:"mutable_created_at"`
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
	if err := cfg.ValidateIDConfig(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}
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
			resolvedPath = path
			return Load(path)
		}
	}

	cfg := defaults()
	if err := cfg.ValidateIDConfig(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}
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

func (c *Config) IsMutableCreatedAt() bool {
	if c.MutableCreatedAt != nil {
		return *c.MutableCreatedAt
	}
	return c.IDFormat == "scoped"
}

func (c *Config) ProtocolIDConfig() protocol.IDConfig {
	return protocol.IDConfig{
		IDFormat:         c.IDFormat,
		ScopeKeys:        c.ScopeKeys,
		KindCodes:        c.KindCodes,
		MutableCreatedAt: c.IsMutableCreatedAt(),
	}
}

func (c *Config) ValidateIDConfig() error {
	if c.IDFormat != "" && c.IDFormat != "scoped" && c.IDFormat != "legacy" {
		return fmt.Errorf("id_format must be \"scoped\" or \"legacy\", got %q", c.IDFormat)
	}

	keyPattern := regexp.MustCompile(`^[A-Z0-9]{2,6}$`)

	if err := validateUniqueKeys(c.ScopeKeys, "scope_keys", keyPattern); err != nil {
		return err
	}
	if err := validateUniqueKeys(c.KindCodes, "kind_codes", keyPattern); err != nil {
		return err
	}
	return nil
}

func validateUniqueKeys(m map[string]string, label string, pattern *regexp.Regexp) error {
	if len(m) == 0 {
		return nil
	}
	seen := make(map[string]string, len(m))
	for name, code := range m {
		if !pattern.MatchString(code) {
			return fmt.Errorf("%s: %q has invalid code %q (must match [A-Z0-9]{2,6})", label, name, code)
		}
		if prev, dup := seen[code]; dup {
			return fmt.Errorf("%s collision: %s=%s, %s=%s", label, prev, code, name, code)
		}
		seen[code] = name
	}
	return nil
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

// Save writes the config to the resolved path. It uses the same resolution
// order as Resolve to find the target file, falling back to ~/.scribe/scribe.yaml.
func Save(cfg *Config) error {
	path := resolvedPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		dir := filepath.Join(home, ".scribe")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
		path = filepath.Join(dir, "scribe.yaml")
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
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
	if v := os.Getenv("SCRIBE_ID_FORMAT"); v != "" {
		c.IDFormat = v
	}
}
