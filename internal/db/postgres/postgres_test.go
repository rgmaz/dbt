package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rgmaz/dbt/internal/db"
)

func TestValidateDSN(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{name: "postgres url", dsn: "postgres://user@localhost/app"},
		{name: "postgresql url", dsn: "postgresql://user@localhost/app"},
		{name: "keyword value", dsn: "host=localhost dbname=app"},
		{name: "mysql url", dsn: "mysql://user@localhost/app", wantErr: true},
		{name: "sqlite url", dsn: "sqlite:///tmp/app.db", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDSN(tt.dsn)
			if tt.wantErr && err == nil {
				t.Fatalf("validateDSN(%q) error = nil, want error", tt.dsn)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateDSN(%q) error = %v", tt.dsn, err)
			}
		})
	}
}

func TestApplyOptionsOverridesDSN(t *testing.T) {
	cfg, err := pgx.ParseConfig("postgres://user:secret@localhost:5432/app" +
		"?application_name=evil&default_transaction_read_only=off&statement_timeout=1")
	if err != nil {
		t.Fatalf("pgx.ParseConfig() error = %v", err)
	}

	opts := db.Options{ReadOnly: true, QueryTimeout: 1500 * time.Millisecond}.Normalize()
	applyOptions(cfg, opts)

	if got := cfg.RuntimeParams["default_transaction_read_only"]; got != "on" {
		t.Errorf("default_transaction_read_only = %q, want on", got)
	}
	if got := cfg.RuntimeParams["application_name"]; got != db.DefaultAppName {
		t.Errorf("application_name = %q, want %q", got, db.DefaultAppName)
	}
	if got := cfg.RuntimeParams["statement_timeout"]; got != "1500" {
		t.Errorf("statement_timeout = %q, want 1500", got)
	}
}

func TestApplyOptionsMinimumTimeout(t *testing.T) {
	cfg, err := pgx.ParseConfig("postgres://user@localhost/app")
	if err != nil {
		t.Fatalf("pgx.ParseConfig() error = %v", err)
	}

	applyOptions(cfg, db.Options{ReadOnly: true, QueryTimeout: time.Nanosecond}.Normalize())
	if got := cfg.RuntimeParams["statement_timeout"]; got != "1" {
		t.Errorf("statement_timeout = %q, want 1", got)
	}
}

func TestRelationKind(t *testing.T) {
	tests := map[string]db.RelationKind{
		"r": db.RelationTable,
		"p": db.RelationTable,
		"v": db.RelationView,
		"m": db.RelationMaterializedView,
		"f": db.RelationForeignTable,
	}
	for relkind, want := range tests {
		got, ok := relationKind(relkind)
		if !ok || got != want {
			t.Errorf("relationKind(%q) = %q, %v; want %q, true", relkind, got, ok, want)
		}
	}
	if _, ok := relationKind("S"); ok {
		t.Error("relationKind(sequence) ok = true, want false")
	}
}

func TestForeignKeyAction(t *testing.T) {
	tests := map[string]string{
		"a": "NO ACTION",
		"r": "RESTRICT",
		"c": "CASCADE",
		"n": "SET NULL",
		"d": "SET DEFAULT",
		"x": "x",
	}
	for code, want := range tests {
		if got := foreignKeyAction(code); got != want {
			t.Errorf("foreignKeyAction(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestQuote(t *testing.T) {
	conn := &Conn{}
	if got := conn.Quote(); got != "" {
		t.Errorf("Quote() = %q, want empty", got)
	}
	if got, want := conn.Quote("weird schema", `a"b`), `"weird schema"."a""b"`; got != want {
		t.Errorf("Quote() = %q, want %q", got, want)
	}
}

func TestFormatCell(t *testing.T) {
	epoch := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value any
		want  db.Cell
	}{
		{name: "null", value: nil, want: db.Cell{Null: true}},
		{name: "string", value: "hi", want: db.Cell{Text: "hi"}},
		{name: "empty string", value: "", want: db.Cell{Text: ""}},
		{name: "int", value: int64(42), want: db.Cell{Text: "42"}},
		{name: "float", value: 1.5, want: db.Cell{Text: "1.5"}},
		{name: "bool", value: true, want: db.Cell{Text: "true"}},
		{name: "time", value: epoch, want: db.Cell{Text: "2026-01-01T00:00:00Z"}},
		{name: "text bytes", value: []byte("hi"), want: db.Cell{Text: "hi"}},
		{name: "binary bytes", value: []byte{0x00, 0xFF}, want: db.Cell{Text: "0x00ff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatCell(tt.value); got != tt.want {
				t.Errorf("formatCell(%v) = %+v, want %+v", tt.value, got, tt.want)
			}
		})
	}
}

func TestPageValidation(t *testing.T) {
	conn := &Conn{}
	ref := db.RelationRef{Schema: "public", Name: "users"}

	if _, err := conn.Page(t.Context(), ref, nil, 0, 0); err == nil {
		t.Error("Page(limit=0) error = nil, want error")
	}
	if _, err := conn.Page(t.Context(), ref, nil, 10, -1); err == nil {
		t.Error("Page(offset=-1) error = nil, want error")
	}
	if _, err := conn.Page(t.Context(), db.RelationRef{Schema: "public"}, nil, 10, 0); err == nil {
		t.Error("Page(empty name) error = nil, want error")
	}
	if _, err := conn.Page(t.Context(), ref, []db.Order{{}}, 10, 0); err == nil {
		t.Error("Page(empty order column) error = nil, want error")
	}
}

func TestQueryValidation(t *testing.T) {
	conn := &Conn{}
	if _, err := conn.Query(t.Context(), "   ", 10); err == nil {
		t.Error("Query(empty) error = nil, want error")
	}
}

func TestOpenRejectsReadWriteOptions(t *testing.T) {
	_, err := NewConnector().Open(t.Context(), "postgres://localhost/app", db.Options{})
	if err == nil {
		t.Fatal("Open() error = nil, want read-only error")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("Open() error = %q, want read-only error", err)
	}
}

func TestOpenRejectsUnsupportedScheme(t *testing.T) {
	_, err := NewConnector().Open(t.Context(), "mysql://localhost/app", db.Options{ReadOnly: true})
	if err == nil {
		t.Fatal("Open() error = nil, want unsupported scheme error")
	}
	if !strings.Contains(err.Error(), "unsupported DSN scheme") {
		t.Errorf("Open() error = %q, want unsupported scheme error", err)
	}
}

func TestOpenRejectsEmptyDSN(t *testing.T) {
	_, err := NewConnector().Open(t.Context(), "   ", db.Options{ReadOnly: true})
	if err == nil {
		t.Fatal("Open() error = nil, want empty DSN error")
	}
	if !strings.Contains(err.Error(), "empty DSN") {
		t.Errorf("Open() error = %q, want empty DSN error", err)
	}
}
