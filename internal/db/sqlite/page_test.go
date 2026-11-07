package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rgmaz/dbt/internal/db"
)

func TestPageFirstPage(t *testing.T) {
	conn := newConn(t, newFixture(t))
	ref := db.RelationRef{Schema: "main", Name: "users"}

	result, err := conn.Page(t.Context(), ref, nil, 10, 0)
	if err != nil {
		t.Fatalf("Page() error = %v", err)
	}
	if len(result.Rows) != 10 || result.RowCount != 10 {
		t.Errorf("rows = %d, RowCount = %d, want 10/10", len(result.Rows), result.RowCount)
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true")
	}
	if got := result.Rows[0][0].Text; got != "1" {
		t.Errorf("first id = %q, want 1", got)
	}
	if result.Columns[0].Name != "id" {
		t.Errorf("first column = %q, want id", result.Columns[0].Name)
	}
	if result.Statement == "" {
		t.Error("Statement is empty")
	}
	if result.Duration < 0 {
		t.Errorf("Duration = %s, want non-negative", result.Duration)
	}
}

func TestPageLastPage(t *testing.T) {
	conn := newConn(t, newFixture(t))
	ref := db.RelationRef{Schema: "main", Name: "users"}

	result, err := conn.Page(t.Context(), ref, nil, 10, 245)
	if err != nil {
		t.Fatalf("Page() error = %v", err)
	}
	if len(result.Rows) != 5 {
		t.Errorf("len(Rows) = %d, want 5", len(result.Rows))
	}
	if result.Truncated {
		t.Error("Truncated = true, want false")
	}
	if got := result.Rows[0][0].Text; got != "246" {
		t.Errorf("first id = %q, want 246", got)
	}
}

func TestPageBeyondEnd(t *testing.T) {
	conn := newConn(t, newFixture(t))
	ref := db.RelationRef{Schema: "main", Name: "users"}

	result, err := conn.Page(t.Context(), ref, nil, 10, 1000)
	if err != nil {
		t.Fatalf("Page() error = %v", err)
	}
	if len(result.Rows) != 0 || result.Truncated {
		t.Errorf("rows = %d, Truncated = %v, want 0/false", len(result.Rows), result.Truncated)
	}
}

func TestPageExplicitSort(t *testing.T) {
	conn := newConn(t, newFixture(t))
	users := db.RelationRef{Schema: "main", Name: "users"}
	orders := db.RelationRef{Schema: "main", Name: "orders"}

	result, err := conn.Page(t.Context(), users, []db.Order{{Column: "id", Desc: true}}, 5, 0)
	if err != nil {
		t.Fatalf("Page(users desc) error = %v", err)
	}
	if got := result.Rows[0][0].Text; got != "250" {
		t.Errorf("first id = %q, want 250", got)
	}

	result, err = conn.Page(t.Context(), orders, []db.Order{{Column: "total", Desc: true}}, 10, 0)
	if err != nil {
		t.Fatalf("Page(orders desc) error = %v", err)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("len(Rows) = %d, want 3", len(result.Rows))
	}
	wantTotals := []string{"20", "10.5", "5.25"}
	for i, want := range wantTotals {
		if got := result.Rows[i][2].Text; got != want {
			t.Errorf("row %d total = %q, want %q", i, got, want)
		}
	}
}

func TestPageCompositePrimaryKey(t *testing.T) {
	conn := newConn(t, newFixture(t))
	ref := db.RelationRef{Schema: "main", Name: "composite"}

	result, err := conn.Page(t.Context(), ref, nil, 2, 0)
	if err != nil {
		t.Fatalf("Page() error = %v", err)
	}
	if len(result.Rows) != 2 || !result.Truncated {
		t.Fatalf("rows = %d, Truncated = %v, want 2/true", len(result.Rows), result.Truncated)
	}
	if got := result.Rows[0][0].Text + "," + result.Rows[0][1].Text; got != "1,1" {
		t.Errorf("first key = %q, want 1,1", got)
	}
	if got := result.Rows[1][0].Text + "," + result.Rows[1][1].Text; got != "1,2" {
		t.Errorf("second key = %q, want 1,2", got)
	}
}

func TestPageNoPrimaryKey(t *testing.T) {
	conn := newConn(t, newFixture(t))
	ref := db.RelationRef{Schema: "main", Name: "no_pk"}

	result, err := conn.Page(t.Context(), ref, nil, 2, 0)
	if err != nil {
		t.Fatalf("Page() error = %v", err)
	}
	if len(result.Rows) != 2 || !result.Truncated {
		t.Errorf("rows = %d, Truncated = %v, want 2/true", len(result.Rows), result.Truncated)
	}
}

func TestPageWeirdIdentifiers(t *testing.T) {
	conn := newConn(t, newFixture(t))
	ref := db.RelationRef{Schema: "main", Name: "weird table"}

	result, err := conn.Page(t.Context(), ref, []db.Order{{Column: `a"b`}}, 10, 0)
	if err != nil {
		t.Fatalf("Page(weird table) error = %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(result.Rows))
	}
	if got := result.Rows[0][0].Text; got != "x" {
		t.Errorf("select column = %q, want x", got)
	}
}

func TestPageValidation(t *testing.T) {
	conn := newConn(t, newFixture(t))
	ref := db.RelationRef{Schema: "main", Name: "users"}

	if _, err := conn.Page(t.Context(), ref, nil, 0, 0); err == nil {
		t.Error("Page(limit=0) error = nil, want error")
	}
	if _, err := conn.Page(t.Context(), ref, nil, 10, -1); err == nil {
		t.Error("Page(offset=-1) error = nil, want error")
	}
	if _, err := conn.Page(t.Context(), db.RelationRef{Schema: "main"}, nil, 10, 0); err == nil {
		t.Error("Page(empty name) error = nil, want error")
	}
	if _, err := conn.Page(t.Context(), ref, []db.Order{{}}, 10, 0); err == nil {
		t.Error("Page(empty order column) error = nil, want error")
	}
}

func TestCount(t *testing.T) {
	conn := newConn(t, newFixture(t))

	count, err := conn.Count(t.Context(), db.RelationRef{Schema: "main", Name: "users"})
	if err != nil {
		t.Fatalf("Count(users) error = %v", err)
	}
	if count.Value != 250 || !count.Exact {
		t.Errorf("Count(users) = %+v, want 250 exact", count)
	}

	count, err = conn.Count(t.Context(), db.RelationRef{Schema: "main", Name: "user_totals"})
	if err != nil {
		t.Fatalf("Count(view) error = %v", err)
	}
	if count.Value != 2 || !count.Exact {
		t.Errorf("Count(view) = %+v, want 2 exact", count)
	}
}

func TestCountParentCancellation(t *testing.T) {
	conn := newConn(t, newFixture(t))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := conn.Count(ctx, db.RelationRef{Schema: "main", Name: "users"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Count(cancelled) error = %v, want context.Canceled", err)
	}
}

func TestCountUnknownOnTimeout(t *testing.T) {
	path := newFixture(t)
	conn := newRawConn(t, path, db.Options{
		ReadOnly:     true,
		QueryTimeout: time.Nanosecond,
		MaxRows:      10,
	})

	count, err := conn.Count(t.Context(), db.RelationRef{Schema: "main", Name: "users"})
	if err != nil {
		t.Fatalf("Count() error = %v, want unknown count", err)
	}
	if count.Exact {
		t.Errorf("Count() = %+v, want inexact on timeout", count)
	}
}

func TestQuery(t *testing.T) {
	conn := newConn(t, newFixture(t))

	result, err := conn.Query(t.Context(), `
SELECT 1 AS "one", NULL AS "nothing", '' AS "empty", X'00FF' AS "blob", 'hello' AS "text"`, 10)
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
}

func TestQueryTruncated(t *testing.T) {
	conn := newConn(t, newFixture(t))

	result, err := conn.Query(t.Context(), `SELECT "id" FROM "users" ORDER BY "id"`, 3)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(result.Rows) != 3 || result.RowCount != 3 || !result.Truncated {
		t.Errorf("rows = %d, RowCount = %d, Truncated = %v, want 3/3/true",
			len(result.Rows), result.RowCount, result.Truncated)
	}
}

func TestQueryMaxRowsCap(t *testing.T) {
	conn := newRawConn(t, newFixture(t), db.Options{
		ReadOnly:     true,
		QueryTimeout: 5 * time.Second,
		MaxRows:      5,
	})

	result, err := conn.Query(t.Context(), `SELECT "id" FROM "users" ORDER BY "id"`, 100)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(result.Rows) != 5 || !result.Truncated {
		t.Errorf("rows = %d, Truncated = %v, want 5/true", len(result.Rows), result.Truncated)
	}
}

func TestQueryValidation(t *testing.T) {
	conn := newConn(t, newFixture(t))

	if _, err := conn.Query(t.Context(), "   ", 10); err == nil {
		t.Error("Query(empty) error = nil, want error")
	}

	result, err := conn.Query(t.Context(), "SELECT 1 WHERE 0", 10)
	if err != nil {
		t.Fatalf("Query(no rows) error = %v", err)
	}
	if len(result.Rows) != 0 || result.Truncated {
		t.Errorf("rows = %d, Truncated = %v, want 0/false", len(result.Rows), result.Truncated)
	}
	if len(result.Columns) != 1 {
		t.Errorf("len(Columns) = %d, want 1", len(result.Columns))
	}
}

func TestQueryStatementPreserved(t *testing.T) {
	conn := newConn(t, newFixture(t))
	statement := `SELECT "id" FROM "users" LIMIT 1`

	result, err := conn.Query(t.Context(), statement, 10)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if result.Statement != statement {
		t.Errorf("Statement = %q, want %q", result.Statement, statement)
	}
	if !strings.Contains(result.Statement, "SELECT") {
		t.Errorf("Statement = %q, want SQL text", result.Statement)
	}
}
