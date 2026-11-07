package db

import (
	"strings"
	"testing"
)

func TestParseDSN(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantKind Kind
		wantDSN  string
		wantErr  string
	}{
		{
			name:     "postgres url",
			raw:      "postgres://user:pass@localhost:5432/app",
			wantKind: KindPostgres,
			wantDSN:  "postgres://user:pass@localhost:5432/app",
		},
		{
			name:     "postgresql url",
			raw:      "postgresql://user@localhost/app",
			wantKind: KindPostgres,
			wantDSN:  "postgresql://user@localhost/app",
		},
		{
			name:     "uppercase scheme",
			raw:      "POSTGRES://user@localhost/app",
			wantKind: KindPostgres,
			wantDSN:  "POSTGRES://user@localhost/app",
		},
		{
			name:     "surrounding whitespace is trimmed",
			raw:      "  postgres://user@localhost/app  ",
			wantKind: KindPostgres,
			wantDSN:  "postgres://user@localhost/app",
		},
		{
			name:     "sqlite url absolute",
			raw:      "sqlite:///tmp/app.db",
			wantKind: KindSQLite,
			wantDSN:  "/tmp/app.db",
		},
		{
			name:     "sqlite url escaped path",
			raw:      "sqlite:///tmp/app%20data.db",
			wantKind: KindSQLite,
			wantDSN:  "/tmp/app data.db",
		},
		{
			name:     "sqlite single slash absolute",
			raw:      "sqlite:/tmp/app.db",
			wantKind: KindSQLite,
			wantDSN:  "/tmp/app.db",
		},
		{
			name:     "sqlite relative",
			raw:      "sqlite:data/app.db",
			wantKind: KindSQLite,
			wantDSN:  "data/app.db",
		},
		{
			name:     "sqlite memory",
			raw:      "sqlite::memory:",
			wantKind: KindSQLite,
			wantDSN:  ":memory:",
		},
		{
			name:     "plain absolute path",
			raw:      "/tmp/app.db",
			wantKind: KindSQLite,
			wantDSN:  "/tmp/app.db",
		},
		{
			name:     "plain relative path",
			raw:      "app.db",
			wantKind: KindSQLite,
			wantDSN:  "app.db",
		},
		{
			name:     "plain memory database",
			raw:      ":memory:",
			wantKind: KindSQLite,
			wantDSN:  ":memory:",
		},
		{
			name:     "windows drive path",
			raw:      `C:\data\app.db`,
			wantKind: KindSQLite,
			wantDSN:  `C:\data\app.db`,
		},
		{
			name:    "empty",
			raw:     "   ",
			wantErr: "empty DSN",
		},
		{
			name:    "unsupported scheme",
			raw:     "mysql://user:secret@localhost/app",
			wantErr: `unsupported driver scheme "mysql"`,
		},
		{
			name:    "sqlite host",
			raw:     "sqlite://host/tmp/app.db",
			wantErr: "must not specify a host",
		},
		{
			name:    "sqlite query",
			raw:     "sqlite:///tmp/app.db?mode=ro",
			wantErr: "must not contain a query or fragment",
		},
		{
			name:    "sqlite single slash query",
			raw:     "sqlite:/tmp/app.db?mode=ro",
			wantErr: "must not contain a query or fragment",
		},
		{
			name:    "sqlite missing path",
			raw:     "sqlite:",
			wantErr: "no path",
		},
		{
			name:    "sqlite options without path",
			raw:     "sqlite:?mode=ro",
			wantErr: "no path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, dsn, err := ParseDSN(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseDSN() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ParseDSN() error = %q, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDSN() error = %v", err)
			}
			if kind != tt.wantKind {
				t.Errorf("ParseDSN() kind = %q, want %q", kind, tt.wantKind)
			}
			if dsn != tt.wantDSN {
				t.Errorf("ParseDSN() dsn = %q, want %q", dsn, tt.wantDSN)
			}
		})
	}
}

func TestParseDSNErrorsDoNotLeakCredentials(t *testing.T) {
	_, _, err := ParseDSN("mysql://user:super-secret@localhost/app")
	if err == nil {
		t.Fatal("ParseDSN() error = nil, want error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Errorf("ParseDSN() error leaked credentials: %q", err)
	}
}
