package log

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRedactDSN(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "url userinfo",
			in:   "postgres://user:secret@host:5432/db?sslmode=disable",
			want: "postgres://user:xxxxx@host:5432/db?sslmode=disable",
		},
		{
			name: "url query password",
			in:   "postgres://host/db?password=secret&sslmode=disable",
			want: "postgres://host/db?password=xxxxx&sslmode=disable",
		},
		{
			name: "url query sslpassword",
			in:   "postgres://host/db?sslpassword=secret",
			want: "postgres://host/db?sslpassword=xxxxx",
		},
		{
			name: "keyword value",
			in:   "host=localhost user=app password=secret dbname=app",
			want: "host=localhost user=app password=xxxxx dbname=app",
		},
		{
			name: "quoted keyword value",
			in:   "password = 'se cret'",
			want: "password=xxxxx",
		},
		{
			name: "no secret url",
			in:   "postgres://user@host/db",
			want: "postgres://user@host/db",
		},
		{
			name: "sqlite path",
			in:   "/home/user/notes.db",
			want: "/home/user/notes.db",
		},
		{
			name: "memory",
			in:   ":memory:",
			want: ":memory:",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactDSN(tt.in); got != tt.want {
				t.Errorf("RedactDSN(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "url",
			in:   "cannot parse `postgres://user:secret@host/db`: bad",
			want: "cannot parse `postgres://user:xxxxx@host/db`: bad",
		},
		{
			name: "keyword",
			in:   "password=secret host=localhost",
			want: "password=xxxxx host=localhost",
		},
		{
			name: "pgpassword",
			in:   "PGPASSWORD=secret",
			want: "PGPASSWORD=xxxxx",
		},
		{
			name: "none",
			in:   "connection refused",
			want: "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.in); got != tt.want {
				t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDefaultPathXDG(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if want := filepath.Join(state, "dbt", "dbt.log"); path != want {
		t.Errorf("DefaultPath() = %q, want %q", path, want)
	}
}

func TestDefaultPathFallback(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("fallback uses the user cache directory on this platform")
	}

	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if want := filepath.Join(home, ".local", "state", "dbt", "dbt.log"); path != want {
		t.Errorf("DefaultPath() = %q, want %q", path, want)
	}
}

func TestOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dbt.log")

	logger, closer, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	logger.Info("hello", "key", "value")
	if err := closer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("log file mode = %o, want no group/other bits", perm)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat log directory: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("log directory mode = %o, want no group/other bits", perm)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	text := string(data)
	for _, want := range []string{`"msg":"hello"`, `"key":"value"`} {
		if !strings.Contains(text, want) {
			t.Errorf("log file = %q, want %q", text, want)
		}
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, _, err := Open("   "); err == nil {
		t.Fatal("Open(empty) error = nil, want error")
	}
}

func TestOpenRejectsUnwritableParent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, _, err := Open(filepath.Join(file, "dbt.log")); err == nil {
		t.Fatal("Open(file as parent) error = nil, want error")
	}
}
