package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Supported driver names.
const (
	DriverPostgres = "postgres"
	DriverSQLite   = "sqlite"
)

// Config defaults and bounds.
const (
	DefaultPageSize     = 100
	MinPageSize         = 10
	MaxPageSize         = 2000
	DefaultQueryTimeout = 30 * time.Second
	DefaultTheme        = "default"
)

// Config is a validated configuration. Values are fully normalized: defaults
// are applied, driver names are lowercase, and read-only is enforced.
type Config struct {
	PageSize     int
	QueryTimeout time.Duration
	Theme        string
	Connections  []Connection

	path     string
	warnings []string
}

// Connection is a validated connection profile.
type Connection struct {
	Name          string
	Driver        string
	DSN           string
	DefaultSchema string
	ReadOnly      bool
}

// ValidationError reports one or more problems with a configuration file.
type ValidationError struct {
	Path     string
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 1 {
		return fmt.Sprintf("invalid config %s: %s", e.Path, e.Problems[0])
	}
	var b strings.Builder
	fmt.Fprintf(&b, "invalid config %s:", e.Path)
	for _, problem := range e.Problems {
		b.WriteString("\n  - ")
		b.WriteString(problem)
	}
	return b.String()
}

// Path returns the resolved path the configuration was loaded from.
func (c *Config) Path() string {
	return c.path
}

// Warnings returns non-fatal configuration advisories, such as unsafe file
// permissions. The caller owns the returned slice.
func (c *Config) Warnings() []string {
	return slices.Clone(c.warnings)
}

// Connection returns the named profile, if present.
func (c *Config) Connection(name string) (Connection, bool) {
	for _, conn := range c.Connections {
		if conn.Name == name {
			return conn, true
		}
	}
	return Connection{}, false
}

// ResolvePath determines the configuration file path. Precedence is: explicit
// path, DBT_CONFIG, XDG_CONFIG_HOME, then the platform user config directory.
func ResolvePath(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Clean(explicit), nil
	}
	if path := os.Getenv("DBT_CONFIG"); path != "" {
		return filepath.Clean(path), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "dbt", "config.toml"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "dbt", "config.toml"), nil
}

// Load reads, decodes, and validates the configuration at the given path.
// An empty path resolves via ResolvePath. A missing file is not an error: it
// yields a valid configuration with defaults and no connections.
func Load(explicitPath string) (*Config, error) {
	path, err := ResolvePath(explicitPath)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		PageSize:     DefaultPageSize,
		QueryTimeout: DefaultQueryTimeout,
		Theme:        DefaultTheme,
		path:         path,
	}

	info, err := os.Stat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("read config %s: not a regular file", path)
		}
	case errors.Is(err, fs.ErrNotExist):
		return cfg, nil
	default:
		return nil, fmt.Errorf("stat config %s: %w", path, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var raw rawConfig
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	var problems []string
	for _, key := range md.Undecoded() {
		problems = append(problems, fmt.Sprintf("unknown key %q", key.String()))
	}

	if raw.PageSize != 0 {
		if raw.PageSize < MinPageSize || raw.PageSize > MaxPageSize {
			problems = append(problems, fmt.Sprintf(
				"page_size must be between %d and %d, got %d",
				MinPageSize, MaxPageSize, raw.PageSize,
			))
		} else {
			cfg.PageSize = raw.PageSize
		}
	}

	if raw.QueryTimeout.set {
		if raw.QueryTimeout.value <= 0 {
			problems = append(problems, "query_timeout must be greater than zero")
		} else {
			cfg.QueryTimeout = raw.QueryTimeout.value
		}
	}

	if theme := strings.TrimSpace(raw.Theme); theme != "" {
		cfg.Theme = theme
	}

	seenNames := make(map[string]struct{}, len(raw.Connections))
	for i, rawConn := range raw.Connections {
		conn, connProblems := buildConnection(i, rawConn, seenNames)
		problems = append(problems, connProblems...)
		cfg.Connections = append(cfg.Connections, conn)
	}

	if len(problems) > 0 {
		return nil, &ValidationError{Path: path, Problems: problems}
	}

	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		cfg.warnings = append(cfg.warnings, fmt.Sprintf(
			"config file %s is readable by group or others; run chmod 600 %s",
			path, path,
		))
	}

	return cfg, nil
}

type rawConfig struct {
	PageSize     int             `toml:"page_size"`
	QueryTimeout durationValue   `toml:"query_timeout"`
	Theme        string          `toml:"theme"`
	Connections  []rawConnection `toml:"connection"`
}

type rawConnection struct {
	Name          string `toml:"name"`
	Driver        string `toml:"driver"`
	DSN           string `toml:"dsn"`
	DefaultSchema string `toml:"default_schema"`
	ReadOnly      any    `toml:"read_only"`
}

// durationValue distinguishes an omitted query_timeout from an explicit zero.
type durationValue struct {
	value time.Duration
	set   bool
}

func (d *durationValue) UnmarshalText(text []byte) error {
	value, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(text), err)
	}
	d.value = value
	d.set = true
	return nil
}

func buildConnection(index int, raw rawConnection, seenNames map[string]struct{}) (Connection, []string) {
	name := strings.TrimSpace(raw.Name)
	label := name
	if label == "" {
		label = fmt.Sprintf("#%d", index+1)
	}

	var problems []string
	problemf := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("connection %s: %s", label, fmt.Sprintf(format, args...)))
	}

	conn := Connection{
		Name:     name,
		Driver:   strings.ToLower(strings.TrimSpace(raw.Driver)),
		ReadOnly: true,
	}

	if name == "" {
		problemf("name is required")
	} else if _, exists := seenNames[name]; exists {
		problemf("duplicate connection name %q", name)
	} else {
		seenNames[name] = struct{}{}
	}

	driverValid := true
	switch conn.Driver {
	case DriverPostgres, DriverSQLite:
	case "":
		problemf("driver is required")
		driverValid = false
	default:
		problemf("driver must be %q or %q, got %q", DriverPostgres, DriverSQLite, conn.Driver)
		driverValid = false
	}

	dsn := strings.TrimSpace(raw.DSN)
	dsnValid := false
	if dsn == "" {
		problemf("dsn is required")
	} else {
		expanded, err := expandEnv(dsn, os.LookupEnv)
		if err != nil {
			problemf("%v", err)
		} else {
			dsn = expanded
			dsnValid = true
		}
	}

	if dsnValid && driverValid && conn.Driver == DriverSQLite {
		expanded, err := expandHome(dsn)
		if err != nil {
			problemf("%v", err)
			dsnValid = false
		} else {
			dsn = expanded
		}
	}
	if dsnValid {
		conn.DSN = dsn
	}

	if raw.ReadOnly != nil {
		readOnly, ok := raw.ReadOnly.(bool)
		switch {
		case !ok:
			problemf("read_only must be a boolean, got %T", raw.ReadOnly)
		case !readOnly:
			problemf("read_only must be true; writes are not supported in this version")
		}
	}

	if driverValid {
		conn.DefaultSchema = strings.TrimSpace(raw.DefaultSchema)
		if conn.DefaultSchema == "" {
			switch conn.Driver {
			case DriverPostgres:
				conn.DefaultSchema = "public"
			case DriverSQLite:
				conn.DefaultSchema = "main"
			}
		}
	}

	return conn, problems
}
