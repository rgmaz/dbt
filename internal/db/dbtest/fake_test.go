package dbtest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rgmaz/dbt/internal/db"
)

func TestFakeConnPage(t *testing.T) {
	conn := NewFakeConn()
	ctx := context.Background()
	ref := db.RelationRef{Schema: "public", Name: "users"}

	result, err := conn.Page(ctx, ref, nil, 10, 0)
	if err != nil {
		t.Fatalf("Page() error = %v", err)
	}
	if len(result.Rows) != 10 {
		t.Fatalf("len(Rows) = %d, want 10", len(result.Rows))
	}
	if result.RowCount != 10 {
		t.Errorf("RowCount = %d, want 10", result.RowCount)
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true")
	}
	if got := result.Rows[0][0].Text; got != "1" {
		t.Errorf("first id = %q, want 1", got)
	}
	if !result.Rows[3][1].Null {
		t.Error("row 3 email Null = false, want true")
	}
	if result.Columns[0].Name != "id" || result.Columns[1].Name != "email" {
		t.Errorf("Columns = %+v", result.Columns)
	}

	calls := conn.Calls()
	if len(calls) == 0 || !strings.Contains(calls[len(calls)-1], "Page public.users limit=10 offset=0 order=-") {
		t.Errorf("last call = %q, want page request", calls)
	}
}

func TestFakeConnPageLastPage(t *testing.T) {
	conn := NewFakeConn()
	ref := db.RelationRef{Schema: "public", Name: "users"}

	result, err := conn.Page(context.Background(), ref, []db.Order{{Column: "id", Desc: true}}, 10, 245)
	if err != nil {
		t.Fatalf("Page() error = %v", err)
	}
	if len(result.Rows) != 5 {
		t.Errorf("len(Rows) = %d, want 5", len(result.Rows))
	}
	if result.Truncated {
		t.Error("Truncated = true, want false on last page")
	}

	calls := conn.Calls()
	if !strings.Contains(calls[len(calls)-1], "order=id:desc") {
		t.Errorf("last call = %q, want recorded order", calls[len(calls)-1])
	}
}

func TestFakeConnMetadata(t *testing.T) {
	conn := NewFakeConn()
	ctx := context.Background()
	usersRef := db.RelationRef{Schema: "public", Name: "users"}
	ordersRef := db.RelationRef{Schema: "public", Name: "orders"}

	schemas, err := conn.ListSchemas(ctx)
	if err != nil {
		t.Fatalf("ListSchemas() error = %v", err)
	}
	if len(schemas) != 1 || schemas[0].Name != "public" {
		t.Errorf("ListSchemas() = %+v", schemas)
	}

	relations, err := conn.ListRelations(ctx, "public")
	if err != nil {
		t.Fatalf("ListRelations() error = %v", err)
	}
	if len(relations) != 2 {
		t.Errorf("len(ListRelations()) = %d, want 2", len(relations))
	}

	detail, err := conn.Describe(ctx, usersRef)
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if len(detail.Columns) != 3 || !detail.DDLSupported {
		t.Errorf("Describe() = %+v", detail)
	}

	version, err := conn.ServerVersion(ctx)
	if err != nil {
		t.Fatalf("ServerVersion() error = %v", err)
	}
	if !strings.Contains(version, "fake") {
		t.Errorf("ServerVersion() = %q, want fake marker", version)
	}

	count, err := conn.Count(ctx, usersRef)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count.Value != 250 || !count.Exact {
		t.Errorf("Count(users) = %+v", count)
	}

	conn.SetCount(ordersRef, db.Count{Value: 99})
	count, err = conn.Count(ctx, ordersRef)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count.Value != 99 || count.Exact {
		t.Errorf("Count(orders) = %+v", count)
	}
}

func TestFakeConnPageBeyondEnd(t *testing.T) {
	conn := NewFakeConn()
	ref := db.RelationRef{Schema: "public", Name: "users"}

	result, err := conn.Page(context.Background(), ref, nil, 10, 1000)
	if err != nil {
		t.Fatalf("Page() error = %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("len(Rows) = %d, want 0", len(result.Rows))
	}
	if result.Truncated {
		t.Error("Truncated = true, want false")
	}
}

func TestFakeConnPageValidation(t *testing.T) {
	conn := NewFakeConn()
	ctx := context.Background()
	ref := db.RelationRef{Schema: "public", Name: "users"}

	if _, err := conn.Page(ctx, ref, nil, 0, 0); err == nil {
		t.Error("Page(limit=0) error = nil, want error")
	}
	if _, err := conn.Page(ctx, ref, nil, 10, -1); err == nil {
		t.Error("Page(offset=-1) error = nil, want error")
	}
	if _, err := conn.Page(ctx, db.RelationRef{Schema: "public", Name: "missing"}, nil, 10, 0); err == nil {
		t.Error("Page(unknown relation) error = nil, want error")
	}
}

func TestFakeConnQuery(t *testing.T) {
	conn := NewFakeConn()
	ctx := context.Background()

	conn.SetQueryResult("select n", db.Result{
		Rows: [][]db.Cell{
			{{Text: "1"}},
			{{Text: "2"}},
			{{Text: "3"}},
		},
	})

	result, err := conn.Query(ctx, "select n", 2)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(result.Rows) != 2 || result.RowCount != 2 {
		t.Errorf("Rows = %d, RowCount = %d, want 2, 2", len(result.Rows), result.RowCount)
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true")
	}
	if result.Statement != "select n" {
		t.Errorf("Statement = %q", result.Statement)
	}

	result, err = conn.Query(ctx, "select 1", 100)
	if err != nil {
		t.Fatalf("Query(default) error = %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0].Text != "ok" {
		t.Errorf("default result = %+v", result.Rows)
	}
}

func TestFakeConnErrorsAndClose(t *testing.T) {
	conn := NewFakeConn()
	ctx := context.Background()

	sentinel := errors.New("boom")
	conn.SetError(sentinel)
	if _, err := conn.ListSchemas(ctx); !errors.Is(err, sentinel) {
		t.Errorf("ListSchemas() error = %v, want sentinel", err)
	}
	conn.SetError(nil)

	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := conn.ListSchemas(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("ListSchemas(cancelled) error = %v, want context.Canceled", err)
	}

	if err := conn.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
	if _, err := conn.ListSchemas(context.Background()); !errors.Is(err, ErrClosed) {
		t.Errorf("ListSchemas(closed) error = %v, want ErrClosed", err)
	}
}

func TestFakeConnQuote(t *testing.T) {
	conn := NewFakeConn()
	if got, want := conn.Quote("public", `we"ird`), `"public"."we""ird"`; got != want {
		t.Errorf("Quote() = %q, want %q", got, want)
	}
}

func TestFakeConnector(t *testing.T) {
	connector := NewFakeConnector()
	if got := connector.Kind(); got != db.KindPostgres {
		t.Errorf("Kind() = %q, want %q", got, db.KindPostgres)
	}

	conn, err := db.Open(context.Background(), connector, "postgres://localhost/app", db.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	if conn == nil {
		t.Fatal("db.Open() conn = nil")
	}

	dsn, opts, ok := connector.LastOpen()
	if !ok {
		t.Fatal("LastOpen() ok = false")
	}
	if dsn != "postgres://localhost/app" {
		t.Errorf("LastOpen() dsn = %q", dsn)
	}
	if !opts.ReadOnly {
		t.Error("LastOpen() options ReadOnly = false")
	}
	if opts.MaxRows != db.DefaultMaxRows {
		t.Errorf("LastOpen() MaxRows = %d, want %d", opts.MaxRows, db.DefaultMaxRows)
	}
	if connector.OpenCount() != 1 {
		t.Errorf("OpenCount() = %d, want 1", connector.OpenCount())
	}

	sentinel := errors.New("dial failed")
	connector.SetError(sentinel)
	if _, err := connector.Open(context.Background(), "x", db.Options{ReadOnly: true}); !errors.Is(err, sentinel) {
		t.Errorf("Open() error = %v, want sentinel", err)
	}
}
