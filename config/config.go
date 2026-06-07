package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	"gopkg.in/yaml.v3"
)

// resolvedPath tracks the config file path found by Resolve, so Save
// can write back to the same location.
var resolvedPath string

// Defaults holds tunable numeric parameters with sane zero-value fallbacks.
type Defaults struct {
	VacuumDays        int `yaml:"vacuum_days"`         // default: 90
	DashboardStale    int `yaml:"dashboard_stale"`     // default: 30
	DashboardStaleCap int `yaml:"dashboard_stale_cap"` // default: 10
	BriefRecentHours  int `yaml:"brief_recent_hours"`  // default: 48
	TreeMaxDepth      int `yaml:"tree_max_depth"`      // default: 10
}

// OrDefault returns the value if non-zero, otherwise the fallback.
func orDefault(val, fallback int) int {
	if val > 0 {
		return val
	}
	return fallback
}

func (d Defaults) GetVacuumDays() int        { return orDefault(d.VacuumDays, 90) }
func (d Defaults) GetDashboardStale() int    { return orDefault(d.DashboardStale, 30) }
func (d Defaults) GetDashboardStaleCap() int { return orDefault(d.DashboardStaleCap, 10) }
func (d Defaults) GetBriefRecentHours() int  { return orDefault(d.BriefRecentHours, 48) }
func (d Defaults) GetTreeMaxDepth() int      { return orDefault(d.TreeMaxDepth, 10) }

// DBConfig supports both a simple path string and a structured SQLite config.
type DBConfig struct {
	SQLite parchment.SQLiteConfig `yaml:"sqlite,omitempty"`
}

// ScopeConfig defines per-scope settings in YAML.
type ScopeConfig struct {
	Key             string   `yaml:"key,omitempty"`
	Path            string   `yaml:"path,omitempty"`
	Labels          []string `yaml:"labels,omitempty"`
	AllowedKinds    []string `yaml:"allowed_kinds,omitempty"`
	DefaultPriority string   `yaml:"default_priority,omitempty"`
}

// Config is the top-level configuration loaded from scribe.yaml.
// EmbedConfig controls the background embedding worker.
// All fields are optional; zero value disables embeddings.
type EmbedConfig struct {
	URL              string `yaml:"url,omitempty"`                // e.g. http://localhost:11434
	Model            string `yaml:"model,omitempty"`              // e.g. nomic-embed-text
	DelayMs          int    `yaml:"delay_ms,omitempty"`           // ms between embed calls (default 200)
	SweepIntervalSec int    `yaml:"sweep_interval_sec,omitempty"` // sweep period (default 300)
}

// Enabled reports whether embedding is configured.
func (e EmbedConfig) Enabled() bool { return e.URL != "" }

// EmbedDelay returns the inter-call delay, defaulting to 200ms.
func (e EmbedConfig) EmbedDelay() int {
	if e.DelayMs <= 0 {
		return 200
	}
	return e.DelayMs
}

// SweepInterval returns the sweep period in seconds, defaulting to 300.
func (e EmbedConfig) SweepInterval() int {
	if e.SweepIntervalSec <= 0 {
		return 300
	}
	return e.SweepIntervalSec
}

type Config struct {
	DB               DBConfig               `yaml:"db"`
	LogLevel         string                 `yaml:"log_level,omitempty"`
	Transport        string                 `yaml:"transport"`
	Addr             string                 `yaml:"addr"`
	ScopeConfigs     map[string]ScopeConfig `yaml:"scope_configs,omitempty"`
	Schema           *parchment.Schema      `yaml:"schema"`
	IDFormat         string                 `yaml:"id_format"`
	IDTemplate       *parchment.IDTemplate  `yaml:"id_template,omitempty"`
	ScopeKeys        map[string]string      `yaml:"scope_keys"`
	KindCodes        map[string]string      `yaml:"kind_codes"`
	MutableCreatedAt *bool                  `yaml:"mutable_created_at"`
	SeedDir          string                 `yaml:"seed_dir,omitempty"`
	Defaults         Defaults               `yaml:"defaults,omitempty"`
	Embed            EmbedConfig            `yaml:"embed,omitempty"`
}

// DBPath returns the resolved database path.
func (c *Config) DBPath() string {
	if c.DB.SQLite.Path != "" {
		return c.DB.SQLite.Path
	}
	return parchment.DefaultSQLitePath()
}

// SQLiteConfig returns the full SQLite configuration.
// Path is handled by OpenSQLiteConfig which falls back to DefaultSQLitePath if empty.
func (c *Config) SQLiteConfig() parchment.SQLiteConfig {
	return c.DB.SQLite
}

// Load reads a config file from path and returns a merged Config.
// Environment variables override file values for db/transport/addr.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied via flag or env var
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
//  3. $SCRIBE_ROOT/scribe.yaml
//  4. $XDG_CONFIG_HOME/scribe/scribe.yaml  (default: ~/.config/scribe/scribe.yaml)
//  5. ~/.scribe/scribe.yaml  (legacy)
//  6. no file → built-in defaults
func Resolve(explicit string) (*Config, error) {
	candidates := []string{explicit}
	if v := os.Getenv("SCRIBE_CONFIG"); v != "" {
		candidates = append(candidates, v)
	}
	if root := os.Getenv("SCRIBE_ROOT"); root != "" {
		candidates = append(candidates, filepath.Join(root, "scribe.yaml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfigHome == "" {
			xdgConfigHome = filepath.Join(home, ".config")
		}
		candidates = append(candidates,
			filepath.Join(xdgConfigHome, "scribe", "scribe.yaml"),
			filepath.Join(home, ".scribe", "scribe.yaml"),
		)
	}

	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil { //nolint:gosec // path is operator-supplied via flag or env var
			resolvedPath = path
			return Load(path)
		}
	}

	cfg := defaults()
	if err := cfg.ValidateIDConfig(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}
	cfg.applyEnvOverrides()

	if err := generateFirstRun(&cfg); err != nil {
		slog.WarnContext(context.Background(), "failed to generate scribe.yaml on first run", slog.Any("error", err)) //nolint:sloglint // non-request context; no ctx available at config resolution time
	}

	return &cfg, nil
}

func generateFirstRun(cfg *Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// Skip if legacy path already exists — don't migrate automatically.
	legacy := filepath.Join(home, ".scribe", "scribe.yaml")
	if _, err := os.Stat(legacy); err == nil {
		return nil
	}
	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfigHome == "" {
		xdgConfigHome = filepath.Join(home, ".config")
	}
	dir := filepath.Join(xdgConfigHome, "scribe")
	path := filepath.Join(dir, "scribe.yaml")
	if _, err := os.Stat(path); err == nil { //nolint:gosec // path is operator-supplied via XDG config, not user input
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // config dir; 0755 is intentional for user readability
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644) //nolint:gosec // config file; 0644 is intentional for user readability
}

func defaults() Config {
	return Config{
		DB:        DBConfig{SQLite: parchment.SQLiteConfig{Path: parchment.DefaultSQLitePath()}},
		Transport: "stdio",
		Addr:      ":8080",
		Schema:    parchment.KnowledgeSchema(),
	}
}

func (c *Config) IsMutableCreatedAt() bool {
	if c.MutableCreatedAt != nil {
		return *c.MutableCreatedAt
	}
	return c.IDFormat == "scoped"
}

func (c *Config) ProtocolIDConfig() parchment.ProtocolConfig {
	mc := c.ModelIDConfig()
	return parchment.ProtocolConfig{
		IDFormat:         mc.IDFormat,
		IDTemplate:       mc.IDTemplate,
		ScopeKeys:        mc.ScopeKeys,
		KindCodes:        mc.KindCodes,
		MutableCreatedAt: mc.MutableCreatedAt,
		Defaults:         c.Defaults,
		ScopePolicies:    c.ScopePolicies(),
	}
}

func (c *Config) ModelIDConfig() parchment.IDConfig {
	mc := parchment.IDConfig{
		IDFormat:         c.IDFormat,
		ScopeKeys:        c.ScopeKeys,
		KindCodes:        c.KindCodes,
		MutableCreatedAt: c.IsMutableCreatedAt(),
	}
	if c.IDTemplate != nil {
		mc.IDTemplate = c.IDTemplate
	} else {
		t := parchment.PresetScoped()
		mc.IDTemplate = &t
	}
	return mc
}

// ErrInvalidIDFormat is returned when the config specifies an unknown id_format.
var ErrInvalidIDFormat = errors.New("id_format must be \"scoped\", \"uuid\", or empty")

// ErrNestedScopePath is returned when one scope's path is a prefix of another's.
var ErrNestedScopePath = errors.New("scope_configs: nested scope paths")

func (c *Config) ValidateIDConfig() error {
	if c.IDFormat != "" && c.IDFormat != "scoped" && c.IDFormat != "uuid" {
		return fmt.Errorf("%w (got %q)", ErrInvalidIDFormat, c.IDFormat)
	}

	keyPattern := regexp.MustCompile(`^[A-Z0-9]{2,6}$`)

	if err := validateUniqueKeys(c.ScopeKeys, "scope_keys", keyPattern); err != nil {
		return err
	}
	if err := validateUniqueKeys(c.KindCodes, "kind_codes", keyPattern); err != nil {
		return err
	}
	if err := c.validateScopePaths(); err != nil {
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
			return fmt.Errorf("%s: %q has invalid code %q (must match [A-Z0-9]{2,6})", label, name, code) //nolint:err113 // runtime values in config validation; static sentinel would lose the detail
		}
		if prev, dup := seen[code]; dup {
			return fmt.Errorf("%s collision: %s=%s, %s=%s", label, prev, code, name, code) //nolint:err113 // runtime values in config validation
		}
		seen[code] = name
	}
	return nil
}

func (c *Config) applyDefaults() {
	// Path is not set here - OpenSQLiteConfig handles the default
	if c.Transport == "" {
		c.Transport = "stdio"
	}
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	// KnowledgeSchema already merges DefaultSchema — it is additive.
	// Any schema loaded from config (via YAML) merges against KnowledgeSchema
	// so knowledge kinds are always present regardless of config file contents.
	if c.Schema == nil {
		c.Schema = parchment.KnowledgeSchema()
	} else {
		c.Schema.MergeDefaults(parchment.KnowledgeSchema())
	}
}

// Save writes the config to the resolved path. It uses the same resolution
// order as Resolve to find the target file, falling back to the XDG config
// path ($XDG_CONFIG_HOME/scribe/scribe.yaml).
func Save(cfg *Config) error {
	path := resolvedPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfigHome == "" {
			xdgConfigHome = filepath.Join(home, ".config")
		}
		dir := filepath.Join(xdgConfigHome, "scribe")
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // config dir; 0755 is intentional for user readability
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
		path = filepath.Join(dir, "scribe.yaml")
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0o644) //nolint:gosec // config file; 0644 is intentional for user readability
}

// ResolvedScopes returns all scope names defined in ScopeConfigs, sorted.
func (c *Config) ResolvedScopes() []string {
	scopes := make([]string, 0, len(c.ScopeConfigs))
	for name := range c.ScopeConfigs {
		scopes = append(scopes, name)
	}
	sort.Strings(scopes)
	return scopes
}

// WorkspaceScopesFor resolves a ?workspace= query parameter to a scope list.
// Resolution order:
//  1. If the value is a single known scope name, return [name].
//  2. Otherwise, split on comma and return the resulting list.
//
// Named workspaces (single scope lookups) are intentionally kept simple — a
// workspace is just a scope name or a comma-separated list of scope names.
// No pre-declaration required: unknown names pass through as literal scopes.
func (c *Config) WorkspaceScopesFor(workspace string) []string {
	if workspace == "" {
		return nil
	}
	// Single token that names a known scope — pass through as-is (common case).
	if !strings.Contains(workspace, ",") {
		return []string{workspace}
	}
	// Comma-separated list of scope names.
	parts := strings.Split(workspace, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ScopeForDir returns the scope whose configured path is the longest prefix of dir.
// Returns "" when no scope has a path configured or none match.
func (c *Config) ScopeForDir(dir string) string {
	best, bestLen := "", 0
	for name, sc := range c.ScopeConfigs {
		p := expandHome(sc.Path)
		if p == "" {
			continue
		}
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		if strings.HasPrefix(dir+"/", p) && len(p) > bestLen {
			best, bestLen = name, len(p)
		}
	}
	return best
}

func expandHome(path string) string {
	if path == "" {
		return ""
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func (c *Config) validateScopePaths() error {
	type entry struct {
		name string
		path string
	}
	var paths []entry
	for name, sc := range c.ScopeConfigs {
		if sc.Path == "" {
			continue
		}
		p := expandHome(sc.Path)
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		paths = append(paths, entry{name, p})
	}
	for i, a := range paths {
		for j, b := range paths {
			if i == j {
				continue
			}
			if strings.HasPrefix(a.path, b.path) {
				return fmt.Errorf("%w: %q is nested under %q", ErrNestedScopePath, a.name, b.name)
			}
		}
	}
	return nil
}

// ScopePolicies converts ScopeConfigs to parchment.ScopePolicy map.
func (c *Config) ScopePolicies() map[string]parchment.ScopePolicy {
	if len(c.ScopeConfigs) == 0 {
		return nil
	}
	policies := make(map[string]parchment.ScopePolicy, len(c.ScopeConfigs))
	for name, sc := range c.ScopeConfigs {
		policies[name] = parchment.ScopePolicy{
			AllowedKinds:    sc.AllowedKinds,
			DefaultPriority: sc.DefaultPriority,
		}
	}
	return policies
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("SCRIBE_DB"); v != "" {
		c.DB.SQLite.Path = v
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
