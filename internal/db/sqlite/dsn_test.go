package sqlite

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyDSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a b#c?d%e.db")
	dsn, err := readOnlyDSN(path)
	if err != nil {
		t.Fatalf("readOnlyDSN() error = %v", err)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", dsn, err)
	}
	if parsed.Scheme != "file" {
		t.Errorf("scheme = %q, want file", parsed.Scheme)
	}
	if parsed.Path != path {
		t.Errorf("path = %q, want %q", parsed.Path, path)
	}

	query := parsed.Query()
	if got := query.Get("mode"); got != "ro" {
		t.Errorf("mode = %q, want ro", got)
	}
	if got := query.Get("_query_only"); got != "1" {
		t.Errorf("_query_only = %q, want 1", got)
	}
	if got := query.Get("_pragma"); got != "busy_timeout(5000)" {
		t.Errorf("_pragma = %q, want busy_timeout(5000)", got)
	}
}

func TestReadOnlyDSNMemory(t *testing.T) {
	dsn, err := readOnlyDSN(":memory:")
	if err != nil {
		t.Fatalf("readOnlyDSN() error = %v", err)
	}
	if got, want := dsn, "file::memory:?cache=shared&_query_only=1"; got != want {
		t.Errorf("readOnlyDSN() = %q, want %q", got, want)
	}
}

func TestReadOnlyDSNRejectsRelativePath(t *testing.T) {
	_, err := readOnlyDSN("relative.db")
	if err == nil {
		t.Fatal("readOnlyDSN() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "must be absolute") {
		t.Errorf("readOnlyDSN() error = %q, want absolute-path error", err)
	}
}

func TestResolvePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	writeFile(t, path, "")

	got, err := resolvePath(path)
	if err != nil {
		t.Fatalf("resolvePath() error = %v", err)
	}
	if got != path {
		t.Errorf("resolvePath() = %q, want %q", got, path)
	}

	got, err = resolvePath(":memory:")
	if err != nil {
		t.Fatalf("resolvePath(:memory:) error = %v", err)
	}
	if got != ":memory:" {
		t.Errorf("resolvePath(:memory:) = %q", got)
	}
}

func TestResolvePathMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	_, err := resolvePath(path)
	if err == nil {
		t.Fatal("resolvePath() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("resolvePath() error = %q, want missing-file error", err)
	}
}

func TestResolvePathDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := resolvePath(dir)
	if err == nil {
		t.Fatal("resolvePath() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("resolvePath() error = %q, want directory error", err)
	}
}
