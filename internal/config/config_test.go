package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

const minimalConnection = `
[[connection]]
name = "notes"
driver = "sqlite"
dsn = "notes.db"
`

func writeConfig(t *testing.T, dir, body string, perm os.FileMode) string {
	t.Helper()

	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// WriteFile is subject to umask; force the mode under test.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	return path
}

func TestResolvePath(t *testing.T) {
	t.Run("explicit wins", func(t *testing.T) {
		t.Setenv("DBT_CONFIG", "/env/config.toml")
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := ResolvePath("/tmp/explicit.toml")
		if err != nil {
			t.Fatal(err)
		}
		if want := "/tmp/explicit.toml"; got != want {
			t.Fatalf("ResolvePath() = %q, want %q", got, want)
		}
	})

	t.Run("DBT_CONFIG wins over XDG", func(t *testing.T) {
		t.Setenv("DBT_CONFIG", "/env/config.toml")
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := ResolvePath("")
		if err != nil {
			t.Fatal(err)
		}
		if want := "/env/config.toml"; got != want {
			t.Fatalf("ResolvePath() = %q, want %q", got, want)
		}
	})

	t.Run("XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("DBT_CONFIG", "")
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := ResolvePath("")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/xdg", "dbt", "config.toml"); got != want {
			t.Fatalf("ResolvePath() = %q, want %q", got, want)
		}
	})

	t.Run("user config directory fallback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("DBT_CONFIG", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", home)

		got, err := ResolvePath("")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".config", "dbt", "config.toml"); got != want {
			t.Fatalf("ResolvePath() = %q, want %q", got, want)
		}
	})
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PageSize != DefaultPageSize {
		t.Errorf("PageSize = %d, want %d", cfg.PageSize, DefaultPageSize)
	}
	if cfg.QueryTimeout != DefaultQueryTimeout {
		t.Errorf("QueryTimeout = %s, want %s", cfg.QueryTimeout, DefaultQueryTimeout)
	}
	if cfg.Theme != DefaultTheme {
		t.Errorf("Theme = %q, want %q", cfg.Theme, DefaultTheme)
	}
	if len(cfg.Connections) != 0 {
		t.Errorf("Connections = %v, want none", cfg.Connections)
	}
	if cfg.Path() != path {
		t.Errorf("Path() = %q, want %q", cfg.Path(), path)
	}
	if warnings := cfg.Warnings(); len(warnings) != 0 {
		t.Errorf("Warnings() = %v, want none", warnings)
	}
}

func TestLoadValid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APP_NAME", "dbt-test")

	body := `
page_size = 250
query_timeout = "45s"
theme = "mono"

[[connection]]
name = "local-pg"
driver = "Postgres"
dsn = "postgres://app@localhost:5432/app?application_name=${APP_NAME}"

[[connection]]
name = "notes"
driver = "sqlite"
dsn = "~/notes.db"
read_only = true
`
	path := writeConfig(t, t.TempDir(), body, 0o600)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.PageSize != 250 {
		t.Errorf("PageSize = %d, want 250", cfg.PageSize)
	}
	if cfg.QueryTimeout != 45*time.Second {
		t.Errorf("QueryTimeout = %s, want 45s", cfg.QueryTimeout)
	}
	if cfg.Theme != "mono" {
		t.Errorf("Theme = %q, want mono", cfg.Theme)
	}

	want := []Connection{
		{
			Name:          "local-pg",
			Driver:        DriverPostgres,
			DSN:           "postgres://app@localhost:5432/app?application_name=dbt-test",
			DefaultSchema: "public",
			ReadOnly:      true,
		},
		{
			Name:          "notes",
			Driver:        DriverSQLite,
			DSN:           filepath.Join(home, "notes.db"),
			DefaultSchema: "main",
			ReadOnly:      true,
		},
	}
	if !reflect.DeepEqual(cfg.Connections, want) {
		t.Errorf("Connections = %#v, want %#v", cfg.Connections, want)
	}
	if warnings := cfg.Warnings(); len(warnings) != 0 {
		t.Errorf("Warnings() = %v, want none", warnings)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown top-level key",
			body: "nope = true\n",
			want: `unknown key "nope"`,
		},
		{
			name: "unknown connection key",
			body: minimalConnection + "unknown_thing = 1\n",
			want: "unknown key",
		},
		{
			name: "missing driver",
			body: `
[[connection]]
name = "x"
dsn = "x.db"
`,
			want: "driver is required",
		},
		{
			name: "unsupported driver",
			body: `
[[connection]]
name = "x"
driver = "mysql"
dsn = "x.db"
`,
			want: `driver must be "postgres" or "sqlite", got "mysql"`,
		},
		{
			name: "missing name",
			body: `
[[connection]]
driver = "sqlite"
dsn = "x.db"
`,
			want: "name is required",
		},
		{
			name: "duplicate name",
			body: `
[[connection]]
name = "dup"
driver = "sqlite"
dsn = "a.db"

[[connection]]
name = "dup"
driver = "sqlite"
dsn = "b.db"
`,
			want: `duplicate connection name "dup"`,
		},
		{
			name: "missing dsn",
			body: `
[[connection]]
name = "x"
driver = "sqlite"
`,
			want: "dsn is required",
		},
		{
			name: "read_only false",
			body: `
[[connection]]
name = "x"
driver = "sqlite"
dsn = "x.db"
read_only = false
`,
			want: "read_only must be true",
		},
		{
			name: "read_only wrong type",
			body: `
[[connection]]
name = "x"
driver = "sqlite"
dsn = "x.db"
read_only = "no"
`,
			want: "read_only must be a boolean, got string",
		},
		{
			name: "page_size too small",
			body: "page_size = 5\n",
			want: "page_size must be between 10 and 2000, got 5",
		},
		{
			name: "page_size too large",
			body: "page_size = 5000\n",
			want: "page_size must be between 10 and 2000, got 5000",
		},
		{
			name: "query_timeout zero",
			body: "query_timeout = \"0s\"\n",
			want: "query_timeout must be greater than zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, t.TempDir(), tt.body, 0o600)

			_, err := Load(path)
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("Load() error type = %T, want *ValidationError: %v", err, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	path := writeConfig(t, t.TempDir(), "[[connection]\n", 0o600)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Errorf("Load() error = %q, want parse context", err)
	}

	var validation *ValidationError
	if errors.As(err, &validation) {
		t.Errorf("Load() error type = *ValidationError, want parse error")
	}
}

func TestLoadUnsetEnvironmentVariable(t *testing.T) {
	const name = "DBT_TEST_UNSET_CONFIG_VAR"

	old, hadValue := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv(name, old)
			return
		}
		_ = os.Unsetenv(name)
	})

	body := fmt.Sprintf(`
[[connection]]
name = "x"
driver = "sqlite"
dsn = "/tmp/${%s}.db"
`, name)
	path := writeConfig(t, t.TempDir(), body, 0o600)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unresolved variable error")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%q", name)) {
		t.Errorf("Load() error = %q, want variable name %q", err, name)
	}
	if !strings.Contains(err.Error(), "unset environment variable") {
		t.Errorf("Load() error = %q, want unset environment explanation", err)
	}
}

func TestLoadPermissionWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file permissions are not available on windows")
	}

	t.Run("group or world readable", func(t *testing.T) {
		path := writeConfig(t, t.TempDir(), minimalConnection, 0o644)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		warnings := cfg.Warnings()
		if len(warnings) != 1 {
			t.Fatalf("Warnings() = %v, want one warning", warnings)
		}
		if !strings.Contains(warnings[0], "chmod 600") {
			t.Errorf("warning = %q, want chmod advice", warnings[0])
		}
	})

	t.Run("private file", func(t *testing.T) {
		path := writeConfig(t, t.TempDir(), minimalConnection, 0o600)

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if warnings := cfg.Warnings(); len(warnings) != 0 {
			t.Errorf("Warnings() = %v, want none", warnings)
		}
	})
}

func TestConnectionLookup(t *testing.T) {
	cfg := &Config{
		Connections: []Connection{
			{Name: "a", Driver: DriverSQLite},
			{Name: "b", Driver: DriverPostgres},
		},
	}

	got, ok := cfg.Connection("b")
	if !ok {
		t.Fatal("Connection(b) not found")
	}
	if got.Driver != DriverPostgres {
		t.Errorf("Connection(b).Driver = %q, want %q", got.Driver, DriverPostgres)
	}

	if _, ok := cfg.Connection("missing"); ok {
		t.Error("Connection(missing) found, want not found")
	}
}

func TestExpandEnv(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		env     map[string]string
		want    string
		wantErr string
	}{
		{
			name:  "single reference",
			input: "a-${FOO}-b",
			env:   map[string]string{"FOO": "bar"},
			want:  "a-bar-b",
		},
		{
			name:  "multiple references",
			input: "${A}:${B}",
			env:   map[string]string{"A": "one", "B": "two"},
			want:  "one:two",
		},
		{
			name:  "empty value is set",
			input: "x${EMPTY}y",
			env:   map[string]string{"EMPTY": ""},
			want:  "xy",
		},
		{
			name:  "no reference",
			input: "plain",
			env:   map[string]string{},
			want:  "plain",
		},
		{
			name:  "dollar without braces is literal",
			input: "$FOO",
			env:   map[string]string{"FOO": "bar"},
			want:  "$FOO",
		},
		{
			name:    "missing variable",
			input:   "x-${MISSING}",
			env:     map[string]string{},
			wantErr: `"MISSING"`,
		},
		{
			name:    "missing variables are deduplicated",
			input:   "${A}${A}${B}",
			env:     map[string]string{},
			wantErr: `"A", "B"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(name string) (string, bool) {
				value, ok := tt.env[name]
				return value, ok
			}

			got, err := expandEnv(tt.input, lookup)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expandEnv() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expandEnv() error = %q, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expandEnv() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("expandEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "relative path", input: "notes.db", want: "notes.db"},
		{name: "absolute path", input: "/tmp/notes.db", want: "/tmp/notes.db"},
		{name: "home only", input: "~", want: home},
		{name: "home prefix", input: "~/notes.db", want: filepath.Join(home, "notes.db")},
		{name: "other user is untouched", input: "~other/notes.db", want: "~other/notes.db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandHome(tt.input)
			if err != nil {
				t.Fatalf("expandHome() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("expandHome() = %q, want %q", got, tt.want)
			}
		})
	}
}
