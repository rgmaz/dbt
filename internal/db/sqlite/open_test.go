package sqlite

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rgmaz/dbt/internal/db"
)

func TestOpenAndPing(t *testing.T) {
	conn := newConn(t, newFixture(t))

	if err := conn.Ping(t.Context()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	version, err := conn.ServerVersion(t.Context())
	if err != nil {
		t.Fatalf("ServerVersion() error = %v", err)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(version) {
		t.Errorf("ServerVersion() = %q, want numeric version", version)
	}
}

func TestOpenRejectsReadWriteOptions(t *testing.T) {
	path := newFixture(t)
	_, err := NewConnector().Open(t.Context(), path, db.Options{})
	if err == nil {
		t.Fatal("Open() error = nil, want read-only error")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("Open() error = %q, want read-only error", err)
	}
}

func TestOpenRejectsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	_, err := NewConnector().Open(t.Context(), path, db.Options{ReadOnly: true})
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Open() error = %q, want missing-file error", err)
	}
}

func TestOpenRejectsDirectory(t *testing.T) {
	_, err := NewConnector().Open(t.Context(), t.TempDir(), db.Options{ReadOnly: true})
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("Open() error = %q, want directory error", err)
	}
}

func TestOpenMemory(t *testing.T) {
	conn, err := db.Open(t.Context(), NewConnector(), ":memory:", db.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("db.Open(:memory:) error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	schemas, err := conn.ListSchemas(t.Context())
	if err != nil {
		t.Fatalf("ListSchemas() error = %v", err)
	}
	if len(schemas) != 1 || schemas[0].Name != "main" {
		t.Errorf("ListSchemas() = %+v, want main", schemas)
	}
	relations, err := conn.ListRelations(t.Context(), "main")
	if err != nil {
		t.Fatalf("ListRelations() error = %v", err)
	}
	if len(relations) != 0 {
		t.Errorf("ListRelations() = %+v, want empty", relations)
	}
}

func TestReadOnlyRejectsWrites(t *testing.T) {
	conn := newConn(t, newFixture(t))
	_, err := conn.Query(t.Context(), `INSERT INTO "misc" ("id") VALUES (99)`, 10)
	if err == nil {
		t.Fatal("Query(INSERT) error = nil, want read-only error")
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "readonly") && !strings.Contains(message, "read-only") {
		t.Errorf("Query(INSERT) error = %q, want read-only error", err)
	}

}

func TestClose(t *testing.T) {
	conn := newConn(t, newFixture(t))
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := conn.Ping(context.Background()); err == nil {
		t.Error("Ping() after Close error = nil, want error")
	}
}
