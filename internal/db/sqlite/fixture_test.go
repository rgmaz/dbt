package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	modernsqlite "modernc.org/sqlite"

	"github.com/rgmaz/dbt/internal/db"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fixture.db")
	driverConnector, err := modernsqlite.NewConnector(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	writable := sql.OpenDB(driverConnector)
	defer func() { _ = writable.Close() }()

	for _, statement := range fixtureSchema {
		if _, err := writable.Exec(statement); err != nil {
			t.Fatalf("exec fixture schema: %v\n%s", err, statement)
		}
	}
	insertUsers(t, writable)
	if _, err := writable.Exec(`
INSERT INTO "orders" ("id", "user_id", "total") VALUES
	(1, 1, 10.5),
	(2, 2, 20.0),
	(3, 2, 5.25)`); err != nil {
		t.Fatalf("insert orders: %v", err)
	}
	if _, err := writable.Exec(`
INSERT INTO "implicit_fk" ("id", "user_id") VALUES (1, 1);
INSERT INTO "composite" ("a", "b", "value") VALUES (1, 1, 'x'), (1, 2, 'y'), (2, 1, 'z');
INSERT INTO "no_pk" ("value") VALUES ('a'), ('b'), ('c');
INSERT INTO "misc" ("id", "text", "empty", "blob") VALUES (1, NULL, '', X'00FF');
INSERT INTO "weird table" ("select", "a""b", "with space") VALUES ('x', 1, 'y');`); err != nil {
		t.Fatalf("insert fixture rows: %v", err)
	}
	return path
}

func insertUsers(t *testing.T, db *sql.DB) {
	t.Helper()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin users insert: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	statement, err := tx.Prepare(`
INSERT INTO "users" ("id", "email", "name", "created_at", "score", "avatar")
VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare users insert: %v", err)
	}
	defer func() { _ = statement.Close() }()

	for i := 1; i <= 250; i++ {
		var email any = fmt.Sprintf("user%d@example.com", i)
		if i == 3 {
			email = nil
		}
		var createdAt any = "2026-01-01T00:00:00Z"
		if i == 7 {
			createdAt = nil
		}
		var score any = float64(i) / 10
		if i == 4 {
			score = nil
		}
		var avatar any = []byte{0x00, 0xFF}
		if i == 5 {
			avatar = nil
		}
		if _, err := statement.Exec(i, email, fmt.Sprintf("User %d", i), createdAt, score, avatar); err != nil {
			t.Fatalf("insert user %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit users insert: %v", err)
	}
}

func newConn(t *testing.T, path string) db.Conn {
	t.Helper()

	conn, err := db.Open(t.Context(), NewConnector(), path, db.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func newRawConn(t *testing.T, path string, opts db.Options) *Conn {
	t.Helper()

	connDSN, err := readOnlyDSN(path)
	if err != nil {
		t.Fatalf("readOnlyDSN() error = %v", err)
	}
	driverConnector, err := modernsqlite.NewConnector(connDSN)
	if err != nil {
		t.Fatalf("modernc connector: %v", err)
	}
	sqlDB := sql.OpenDB(driverConnector)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return &Conn{db: sqlDB, opts: opts.Normalize()}
}

var fixtureSchema = []string{
	`
CREATE TABLE "users" (
	"id" INTEGER PRIMARY KEY,
	"email" TEXT UNIQUE,
	"name" TEXT NOT NULL DEFAULT 'anonymous',
	"created_at" TEXT,
	"score" REAL,
	"avatar" BLOB,
	"full_name" TEXT GENERATED ALWAYS AS ("name" || '!') VIRTUAL
)`,
	`
CREATE TABLE "orders" (
	"id" INTEGER PRIMARY KEY,
	"user_id" INTEGER NOT NULL REFERENCES "users"("id") ON DELETE CASCADE ON UPDATE RESTRICT,
	"total" REAL
)`,
	`CREATE INDEX "orders_user_id_idx" ON "orders"("user_id")`,
	`CREATE UNIQUE INDEX "users_email_lower_idx" ON "users"(lower("email"))`,
	`CREATE VIEW "user_totals" AS SELECT "user_id", sum("total") AS "total" FROM "orders" GROUP BY "user_id"`,
	`CREATE TABLE "implicit_fk" ("id" INTEGER PRIMARY KEY, "user_id" INTEGER REFERENCES "users")`,
	`CREATE TABLE "composite" ("a" INTEGER, "b" INTEGER, "value" TEXT, PRIMARY KEY ("a", "b"))`,
	`CREATE TABLE "no_pk" ("value" TEXT)`,
	`CREATE TABLE "misc" ("id" INTEGER PRIMARY KEY, "text" TEXT, "empty" TEXT, "blob" BLOB)`,
	`CREATE TABLE "weird table" ("select" TEXT, "a""b" INTEGER, "with space" TEXT)`,
}

func describe(t *testing.T, conn db.Conn, schema, name string) db.RelationDetail {
	t.Helper()
	detail, err := conn.Describe(context.Background(), db.RelationRef{Schema: schema, Name: name})
	if err != nil {
		t.Fatalf("Describe(%s.%s) error = %v", schema, name, err)
	}
	return detail
}
