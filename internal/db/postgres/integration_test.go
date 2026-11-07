package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rgmaz/dbt/internal/db"
)

func TestIntegrationIntrospection(t *testing.T) {
	conn, schema := setupFixture(t)

	if err := conn.Ping(t.Context()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	version, err := conn.ServerVersion(t.Context())
	if err != nil {
		t.Fatalf("ServerVersion() error = %v", err)
	}
	if version == "" {
		t.Error("ServerVersion() = empty, want version")
	}

	schemas, err := conn.ListSchemas(t.Context())
	if err != nil {
		t.Fatalf("ListSchemas() error = %v", err)
	}
	hasSchema := false
	for _, item := range schemas {
		if item.Name == schema {
			hasSchema = true
		}
		if strings.HasPrefix(item.Name, "pg_") || item.Name == "information_schema" {
			t.Errorf("ListSchemas() included system schema %q", item.Name)
		}
	}
	if !hasSchema {
		t.Errorf("ListSchemas() missing fixture schema %q", schema)
	}

	relations, err := conn.ListRelations(t.Context(), schema)
	if err != nil {
		t.Fatalf("ListRelations() error = %v", err)
	}
	byName := make(map[string]db.RelationKind, len(relations))
	for _, relation := range relations {
		byName[relation.Name] = relation.Kind
	}
	if byName["users"] != db.RelationTable {
		t.Errorf("users kind = %q, want table", byName["users"])
	}
	if byName["user_totals"] != db.RelationView {
		t.Errorf("user_totals kind = %q, want view", byName["user_totals"])
	}

	users := describe(t, conn, schema, "users")
	if users.Relation.Kind != db.RelationTable {
		t.Errorf("users kind = %q, want table", users.Relation.Kind)
	}
	if users.DDLSupported || users.DDL != "" {
		t.Errorf("users DDLSupported = %v, DDL = %q, want unsupported/empty", users.DDLSupported, users.DDL)
	}
	if len(users.Columns) != 7 {
		t.Fatalf("len(users.Columns) = %d, want 7", len(users.Columns))
	}
	id := findColumn(t, users.Columns, "id")
	if !id.IsPrimaryKey || id.Nullable || id.Position != 1 {
		t.Errorf("id column = %+v, want primary key at position 1", id)
	}
	email := findColumn(t, users.Columns, "email")
	if !email.Nullable || email.IsPrimaryKey {
		t.Errorf("email column = %+v, want nullable non-key", email)
	}
	name := findColumn(t, users.Columns, "name")
	if name.Nullable || name.Default == nil || !strings.Contains(*name.Default, "anonymous") {
		t.Errorf("name column = %+v, want NOT NULL default anonymous", name)
	}
	fullName := findColumn(t, users.Columns, "full_name")
	if !fullName.IsGenerated {
		t.Errorf("full_name column = %+v, want generated", fullName)
	}

	primary := findIndex(t, users.Indexes, "users_pkey")
	if !primary.Primary || !primary.Unique || len(primary.Columns) != 1 || primary.Columns[0] != "id" {
		t.Errorf("users_pkey = %+v, want primary unique id", primary)
	}
	emailIndex := findIndex(t, users.Indexes, "users_email_key")
	if !emailIndex.Unique || len(emailIndex.Columns) != 1 || emailIndex.Columns[0] != "email" {
		t.Errorf("users_email_key = %+v, want unique email", emailIndex)
	}
	expressionIndex := findIndex(t, users.Indexes, "users_email_lower_idx")
	if !expressionIndex.Unique || len(expressionIndex.Columns) != 1 || !strings.Contains(expressionIndex.Columns[0], "lower(") {
		t.Errorf("users_email_lower_idx = %+v, want unique lower(email)", expressionIndex)
	}

	orders := describe(t, conn, schema, "orders")
	if len(orders.ForeignKeys) != 1 {
		t.Fatalf("len(orders.ForeignKeys) = %d, want 1", len(orders.ForeignKeys))
	}
	fk := orders.ForeignKeys[0]
	if fk.Name != "orders_user_id_fkey" || fk.RefSchema != schema || fk.RefTable != "users" {
		t.Errorf("foreign key = %+v, want orders_user_id_fkey -> %s.users", fk, schema)
	}
	if len(fk.Columns) != 1 || fk.Columns[0] != "user_id" || len(fk.RefColumns) != 1 || fk.RefColumns[0] != "id" {
		t.Errorf("foreign key columns = %v -> %v, want user_id -> id", fk.Columns, fk.RefColumns)
	}
	if fk.OnDelete != "CASCADE" || fk.OnUpdate != "RESTRICT" {
		t.Errorf("foreign key actions = delete %q update %q, want CASCADE/RESTRICT", fk.OnDelete, fk.OnUpdate)
	}

	view := describe(t, conn, schema, "user_totals")
	if view.Relation.Kind != db.RelationView {
		t.Errorf("view kind = %q, want view", view.Relation.Kind)
	}
	if len(view.Columns) != 2 || len(view.Indexes) != 0 || len(view.ForeignKeys) != 0 {
		t.Errorf("view columns/indexes/FKs = %d/%d/%d, want 2/0/0",
			len(view.Columns), len(view.Indexes), len(view.ForeignKeys))
	}

	composite := describe(t, conn, schema, "composite")
	var compositePK *db.Index
	for i := range composite.Indexes {
		if composite.Indexes[i].Primary {
			compositePK = &composite.Indexes[i]
		}
	}
	if compositePK == nil {
		t.Fatal("no primary index reported for composite table")
	}
	if len(compositePK.Columns) != 2 || compositePK.Columns[0] != "a" || compositePK.Columns[1] != "b" {
		t.Errorf("composite primary index columns = %v, want [a b]", compositePK.Columns)
	}

	weird := describe(t, conn, schema, "weird table")
	for _, columnName := range []string{"select", `a"b`, "with space"} {
		column := findColumn(t, weird.Columns, columnName)
		if column.Name != columnName {
			t.Errorf("column name = %q, want %q", column.Name, columnName)
		}
	}
	if got, want := conn.Quote(schema, "weird table", `a"b`), (pgx.Identifier{schema, "weird table", `a"b`}).Sanitize(); got != want {
		t.Errorf("Quote() = %q, want %q", got, want)
	}

	if _, err := conn.Describe(t.Context(), db.RelationRef{Schema: schema, Name: "missing"}); err == nil {
		t.Error("Describe(missing) error = nil, want error")
	}
}

func TestIntegrationPagingAndQuery(t *testing.T) {
	conn, schema := setupFixture(t)
	users := db.RelationRef{Schema: schema, Name: "users"}

	result, err := conn.Page(t.Context(), users, nil, 10, 0)
	if err != nil {
		t.Fatalf("Page(first) error = %v", err)
	}
	if len(result.Rows) != 10 || !result.Truncated {
		t.Errorf("rows = %d, Truncated = %v, want 10/true", len(result.Rows), result.Truncated)
	}
	if got := result.Rows[0][0].Text; got != "1" {
		t.Errorf("first id = %q, want 1", got)
	}

	result, err = conn.Page(t.Context(), users, nil, 10, 245)
	if err != nil {
		t.Fatalf("Page(last) error = %v", err)
	}
	if len(result.Rows) != 5 || result.Truncated {
		t.Errorf("rows = %d, Truncated = %v, want 5/false", len(result.Rows), result.Truncated)
	}
	if got := result.Rows[0][0].Text; got != "246" {
		t.Errorf("first id = %q, want 246", got)
	}

	result, err = conn.Page(t.Context(), users, []db.Order{{Column: "id", Desc: true}}, 5, 0)
	if err != nil {
		t.Fatalf("Page(desc) error = %v", err)
	}
	if got := result.Rows[0][0].Text; got != "250" {
		t.Errorf("first id = %q, want 250", got)
	}

	result, err = conn.Page(t.Context(), db.RelationRef{Schema: schema, Name: "composite"}, nil, 2, 0)
	if err != nil {
		t.Fatalf("Page(composite) error = %v", err)
	}
	if len(result.Rows) != 2 || !result.Truncated {
		t.Fatalf("composite rows = %d, Truncated = %v, want 2/true", len(result.Rows), result.Truncated)
	}
	if got := result.Rows[0][0].Text + "," + result.Rows[0][1].Text; got != "1,1" {
		t.Errorf("first composite key = %q, want 1,1", got)
	}
	if got := result.Rows[1][0].Text + "," + result.Rows[1][1].Text; got != "1,2" {
		t.Errorf("second composite key = %q, want 1,2", got)
	}

	result, err = conn.Page(t.Context(), db.RelationRef{Schema: schema, Name: "no_pk"}, nil, 10, 0)
	if err != nil {
		t.Fatalf("Page(no_pk) error = %v", err)
	}
	if len(result.Rows) != 3 {
		t.Errorf("no_pk rows = %d, want 3", len(result.Rows))
	}

	result, err = conn.Page(t.Context(), db.RelationRef{Schema: schema, Name: "weird table"}, []db.Order{{Column: `a"b`}}, 10, 0)
	if err != nil {
		t.Fatalf("Page(weird table) error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0].Text != "x" {
		t.Errorf("weird table rows = %+v, want one x row", result.Rows)
	}

	count, err := conn.Count(t.Context(), users)
	if err != nil {
		t.Fatalf("Count(users) error = %v", err)
	}
	if count.Exact {
		t.Errorf("Count(users) = %+v, want estimate", count)
	}
	if count.Value < 200 || count.Value > 300 {
		t.Errorf("Count(users).Value = %d, want near 250", count.Value)
	}

	count, err = conn.Count(t.Context(), db.RelationRef{Schema: schema, Name: "user_totals"})
	if err != nil {
		t.Fatalf("Count(view) error = %v", err)
	}
	if count.Exact {
		t.Errorf("Count(view) = %+v, want estimate", count)
	}

	result, err = conn.Query(t.Context(), `
SELECT 1 AS "one",
       NULL::text AS "nothing",
       ''::text AS "empty",
       decode('00ff', 'hex') AS "blob",
       'hello'::text AS "text"`, 10)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(result.Rows))
	}
	row := result.Rows[0]
	if got := row[0].Text; got != "1" {
		t.Errorf("one = %q, want 1", got)
	}
	if !row[1].Null {
		t.Errorf("nothing = %+v, want NULL", row[1])
	}
	if row[2].Null || row[2].Text != "" {
		t.Errorf("empty = %+v, want empty non-NULL", row[2])
	}
	if got := row[3].Text; got != "0x00ff" {
		t.Errorf("blob = %q, want 0x00ff", got)
	}
	if got := row[4].Text; got != "hello" {
		t.Errorf("text = %q, want hello", got)
	}

	result, err = conn.Query(t.Context(), `SELECT id FROM `+conn.Quote(schema, "users")+` ORDER BY id`, 3)
	if err != nil {
		t.Fatalf("Query(truncated) error = %v", err)
	}
	if len(result.Rows) != 3 || !result.Truncated {
		t.Errorf("rows = %d, Truncated = %v, want 3/true", len(result.Rows), result.Truncated)
	}

	capped, err := db.Open(t.Context(), NewConnector(), integrationDSN(t), db.Options{
		ReadOnly:     true,
		QueryTimeout: 10 * time.Second,
		MaxRows:      5,
	})
	if err != nil {
		t.Fatalf("db.Open(capped) error = %v", err)
	}
	t.Cleanup(func() { _ = capped.Close() })
	result, err = capped.Query(t.Context(), `SELECT id FROM `+conn.Quote(schema, "users")+` ORDER BY id`, 100)
	if err != nil {
		t.Fatalf("Query(capped) error = %v", err)
	}
	if len(result.Rows) != 5 || !result.Truncated {
		t.Errorf("capped rows = %d, Truncated = %v, want 5/true", len(result.Rows), result.Truncated)
	}

	if _, err := conn.Query(t.Context(), `INSERT INTO `+conn.Quote(schema, "misc")+` (id) VALUES (99)`, 10); err == nil {
		t.Error("Query(INSERT) error = nil, want read-only error")
	} else {
		message := strings.ToLower(err.Error())
		if !strings.Contains(message, "read-only") && !strings.Contains(message, "read only") {
			t.Errorf("Query(INSERT) error = %q, want read-only error", err)
		}
	}
}

func TestIntegrationCountCancellation(t *testing.T) {
	conn, schema := setupFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := conn.Count(ctx, db.RelationRef{Schema: schema, Name: "users"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Count(cancelled) error = %v, want context.Canceled", err)
	}
}

func setupFixture(t *testing.T) (db.Conn, string) {
	t.Helper()

	dsn := integrationDSN(t)
	schema := setupSchema(t, dsn)

	conn, err := db.Open(t.Context(), NewConnector(), dsn, db.Options{
		ReadOnly:     true,
		QueryTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, schema
}

func integrationDSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("DBT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set DBT_TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}
	return dsn
}

func setupSchema(t *testing.T, dsn string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect for fixture: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = admin.Close(cleanupCtx)
	})

	schema := fmt.Sprintf("dbt_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatalf("create fixture schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = admin.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE")
	})

	for _, statement := range fixtureStatements(schema) {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatalf("exec fixture: %v\n%s", err, statement)
		}
	}
	return schema
}

func fixtureStatements(schema string) []string {
	q := func(parts ...string) string { return pgx.Identifier(parts).Sanitize() }

	users := q(schema, "users")
	orders := q(schema, "orders")
	return []string{
		fmt.Sprintf(`CREATE TABLE %s (
	"id" bigint PRIMARY KEY,
	"email" text UNIQUE,
	"name" text NOT NULL DEFAULT 'anonymous',
	"created_at" timestamptz,
	"score" double precision,
	"avatar" bytea,
	"full_name" text GENERATED ALWAYS AS ("name" || '!') STORED
)`, users),
		fmt.Sprintf(`CREATE TABLE %s (
	"id" bigint PRIMARY KEY,
	"user_id" bigint NOT NULL REFERENCES %s ("id") ON DELETE CASCADE ON UPDATE RESTRICT,
	"total" numeric(10, 2)
)`, orders, users),
		fmt.Sprintf(`CREATE INDEX %s ON %s ("user_id")`, q("orders_user_id_idx"), orders),
		fmt.Sprintf(`CREATE UNIQUE INDEX %s ON %s (lower("email"))`, q("users_email_lower_idx"), users),
		fmt.Sprintf(`CREATE VIEW %s AS
SELECT "user_id", sum("total") AS "total" FROM %s GROUP BY "user_id"`, q(schema, "user_totals"), orders),
		fmt.Sprintf(`CREATE TABLE %s ("a" integer, "b" integer, "value" text, PRIMARY KEY ("a", "b"))`, q(schema, "composite")),
		fmt.Sprintf(`CREATE TABLE %s ("value" text)`, q(schema, "no_pk")),
		fmt.Sprintf(`CREATE TABLE %s ("id" integer PRIMARY KEY, "text" text, "empty" text, "blob" bytea)`, q(schema, "misc")),
		fmt.Sprintf(`CREATE TABLE %s ("select" text, "a""b" integer, "with space" text)`, q(schema, "weird table")),
		fmt.Sprintf(`INSERT INTO %s ("id", "email", "name", "created_at", "score", "avatar")
SELECT g.i,
       CASE WHEN g.i = 3 THEN NULL ELSE 'user' || g.i || '@example.com' END,
       'User ' || g.i,
       CASE WHEN g.i = 7 THEN NULL ELSE timestamptz '2026-01-01 00:00:00+00' END,
       CASE WHEN g.i = 4 THEN NULL ELSE g.i::double precision / 10 END,
       CASE WHEN g.i = 5 THEN NULL ELSE decode('00ff', 'hex') END
FROM generate_series(1, 250) AS g(i)`, users),
		fmt.Sprintf(`INSERT INTO %s ("id", "user_id", "total") VALUES
	(1, 1, 10.50),
	(2, 2, 20.00),
	(3, 2, 5.25)`, orders),
		fmt.Sprintf(`INSERT INTO %s ("a", "b", "value") VALUES (1, 1, 'x'), (1, 2, 'y'), (2, 1, 'z')`, q(schema, "composite")),
		fmt.Sprintf(`INSERT INTO %s ("value") VALUES ('a'), ('b'), ('c')`, q(schema, "no_pk")),
		fmt.Sprintf(`INSERT INTO %s ("id", "text", "empty", "blob") VALUES (1, NULL, '', decode('00ff', 'hex'))`, q(schema, "misc")),
		fmt.Sprintf(`INSERT INTO %s ("select", "a""b", "with space") VALUES ('x', 1, 'y')`, q(schema, "weird table")),
		fmt.Sprintf(`ANALYZE %s`, users),
	}
}

func describe(t *testing.T, conn db.Conn, schema, name string) db.RelationDetail {
	t.Helper()
	detail, err := conn.Describe(context.Background(), db.RelationRef{Schema: schema, Name: name})
	if err != nil {
		t.Fatalf("Describe(%s.%s) error = %v", schema, name, err)
	}
	return detail
}

func findColumn(t *testing.T, columns []db.Column, name string) db.Column {
	t.Helper()
	for _, column := range columns {
		if column.Name == name {
			return column
		}
	}
	t.Fatalf("column %q not found in %+v", name, columns)
	return db.Column{}
}

func findIndex(t *testing.T, indexes []db.Index, name string) db.Index {
	t.Helper()
	for _, index := range indexes {
		if index.Name == name {
			return index
		}
	}
	t.Fatalf("index %q not found in %+v", name, indexes)
	return db.Index{}
}
