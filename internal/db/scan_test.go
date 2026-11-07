package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

const scanDriverName = "dbt-db-scan-test"

func init() {
	sql.Register(scanDriverName, scanDriver{})
}

func TestWithQueryTimeout(t *testing.T) {
	t.Run("explicit", func(t *testing.T) {
		ctx, cancel := WithQueryTimeout(context.Background(), time.Millisecond)
		defer cancel()

		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Errorf("ctx.Err() = %v, want context.DeadlineExceeded", ctx.Err())
		}
	})

	t.Run("default", func(t *testing.T) {
		ctx, cancel := WithQueryTimeout(context.Background(), 0)
		defer cancel()

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("ctx.Deadline() ok = false, want deadline")
		}
		if remaining := time.Until(deadline); remaining > DefaultQueryTimeout {
			t.Errorf("remaining = %s, want at most %s", remaining, DefaultQueryTimeout)
		}
	})
}

func TestRunQueryTruncatesAndFormats(t *testing.T) {
	sqlDB := openScanDB(t)

	result, err := RunQuery(t.Context(), sqlDB, "SELECT fixture", 2)
	if err != nil {
		t.Fatalf("RunQuery() error = %v", err)
	}
	if len(result.Rows) != 2 || result.RowCount != 2 || !result.Truncated {
		t.Errorf("rows = %d, RowCount = %d, Truncated = %v, want 2/2/true",
			len(result.Rows), result.RowCount, result.Truncated)
	}
	if result.Statement != "SELECT fixture" {
		t.Errorf("Statement = %q, want SELECT fixture", result.Statement)
	}
	if result.Duration < 0 {
		t.Errorf("Duration = %s, want non-negative", result.Duration)
	}
	if len(result.Columns) != 6 {
		t.Fatalf("len(Columns) = %d, want 6", len(result.Columns))
	}

	id := result.Columns[0]
	if id.Name != "id" || id.DatabaseType != "INT8" || id.Nullable {
		t.Errorf("id column = %+v, want non-null INT8", id)
	}
	text := result.Columns[1]
	if text.Name != "text" || !text.Nullable {
		t.Errorf("text column = %+v, want nullable", text)
	}

	first := result.Rows[0]
	if first[0].Text != "1" {
		t.Errorf("id = %q, want 1", first[0].Text)
	}
	if !first[1].Null {
		t.Errorf("NULL text = %+v, want NULL", first[1])
	}
	if first[2].Null || first[2].Text != "" {
		t.Errorf("empty text = %+v, want empty non-NULL", first[2])
	}
	if first[3].Text != "0x00ff" {
		t.Errorf("binary blob = %q, want 0x00ff", first[3].Text)
	}
	if first[4].Text != "true" {
		t.Errorf("flag = %q, want true", first[4].Text)
	}
	if first[5].Text != "2026-01-01T00:00:00Z" {
		t.Errorf("moment = %q, want 2026-01-01T00:00:00Z", first[5].Text)
	}

	second := result.Rows[1]
	if second[1].Null || second[1].Text != "hi" {
		t.Errorf("text = %+v, want hi", second[1])
	}
	if second[3].Text != "text" {
		t.Errorf("UTF-8 bytes = %q, want text", second[3].Text)
	}
	if second[4].Text != "false" {
		t.Errorf("flag = %q, want false", second[4].Text)
	}
	if second[5].Text != "2026-01-02T03:04:05.000000006Z" {
		t.Errorf("moment = %q, want nanosecond timestamp", second[5].Text)
	}
}

func TestRunQueryExactRows(t *testing.T) {
	sqlDB := openScanDB(t)

	result, err := RunQuery(t.Context(), sqlDB, "SELECT fixture", 3)
	if err != nil {
		t.Fatalf("RunQuery() error = %v", err)
	}
	if len(result.Rows) != 3 || result.Truncated {
		t.Errorf("rows = %d, Truncated = %v, want 3/false", len(result.Rows), result.Truncated)
	}

	result, err = RunQuery(t.Context(), sqlDB, "SELECT fixture", 10)
	if err != nil {
		t.Fatalf("RunQuery(limit=10) error = %v", err)
	}
	if len(result.Rows) != 3 || result.Truncated {
		t.Errorf("rows = %d, Truncated = %v, want 3/false", len(result.Rows), result.Truncated)
	}
}

func TestRunQueryErrors(t *testing.T) {
	if _, err := RunQuery(t.Context(), nil, "SELECT 1", 1); err == nil {
		t.Error("RunQuery(nil db) error = nil, want error")
	}

	sqlDB := openScanDB(t)
	if _, err := RunQuery(t.Context(), sqlDB, "SELECT fixture", 0); err == nil {
		t.Error("RunQuery(maxRows=0) error = nil, want error")
	}
	if _, err := RunQuery(t.Context(), sqlDB, "FAIL", 1); err == nil {
		t.Error("RunQuery(FAIL) error = nil, want error")
	} else if !strings.Contains(err.Error(), "scan fixture failure") {
		t.Errorf("RunQuery(FAIL) error = %q, want fixture failure", err)
	}
}

func TestFormatCell(t *testing.T) {
	epoch := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value any
		want  Cell
	}{
		{name: "null", value: nil, want: Cell{Null: true}},
		{name: "string", value: "hi", want: Cell{Text: "hi"}},
		{name: "empty string", value: "", want: Cell{Text: ""}},
		{name: "int", value: int64(42), want: Cell{Text: "42"}},
		{name: "float", value: 1.5, want: Cell{Text: "1.5"}},
		{name: "bool", value: true, want: Cell{Text: "true"}},
		{name: "time", value: epoch, want: Cell{Text: "2026-01-01T00:00:00Z"}},
		{name: "text bytes", value: []byte("hi"), want: Cell{Text: "hi"}},
		{name: "binary bytes", value: []byte{0x00, 0xFF}, want: Cell{Text: "0x00ff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatCell(tt.value); got != tt.want {
				t.Errorf("formatCell(%v) = %+v, want %+v", tt.value, got, tt.want)
			}
		})
	}
}

func TestFormatBytesLargeBinary(t *testing.T) {
	value := make([]byte, maxBinaryPreview+1)
	for i := range value {
		value[i] = 0xFF
	}
	got := formatBytes(value)
	if !strings.HasPrefix(got, "0x") {
		t.Errorf("formatBytes() = %q, want hex prefix", got)
	}
	if !strings.HasSuffix(got, "(65 bytes)") {
		t.Errorf("formatBytes() = %q, want length suffix", got)
	}
}

func TestFormatBytesValidUTF8(t *testing.T) {
	if got := formatBytes([]byte("héllo")); got != "héllo" {
		t.Errorf("formatBytes() = %q, want text", got)
	}
}

func openScanDB(t *testing.T) *sql.DB {
	t.Helper()

	sqlDB, err := sql.Open(scanDriverName, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

type scanDriver struct{}

func (scanDriver) Open(string) (driver.Conn, error) { return scanConn{}, nil }

type scanConn struct{}

func (scanConn) Prepare(query string) (driver.Stmt, error) {
	if query == "FAIL" {
		return nil, errors.New("scan fixture failure")
	}
	return scanStmt{}, nil
}

func (scanConn) Close() error { return nil }

func (scanConn) Begin() (driver.Tx, error) { return nil, errors.New("transactions unsupported") }

type scanStmt struct{}

func (scanStmt) Close() error  { return nil }
func (scanStmt) NumInput() int { return -1 }

func (scanStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("exec unsupported")
}

func (scanStmt) Query([]driver.Value) (driver.Rows, error) { return &scanTestRows{}, nil }

type scanTestRows struct {
	index int
}

func (*scanTestRows) Columns() []string {
	return []string{"id", "text", "empty", "blob", "flag", "moment"}
}

func (*scanTestRows) Close() error { return nil }

func (r *scanTestRows) Next(dest []driver.Value) error {
	if r.index >= len(scanValues) {
		return io.EOF
	}
	copy(dest, scanValues[r.index])
	r.index++
	return nil
}

func (*scanTestRows) ColumnTypeDatabaseTypeName(i int) string { return scanTypes[i] }

func (*scanTestRows) ColumnTypeNullable(i int) (bool, bool) { return scanNullable[i], true }

var (
	scanTypes    = []string{"INT8", "TEXT", "TEXT", "BYTEA", "BOOL", "TIMESTAMPTZ"}
	scanNullable = []bool{false, true, false, true, false, true}
	scanValues   = [][]driver.Value{
		{int64(1), nil, "", []byte{0x00, 0xFF}, true, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{int64(2), "hi", "", []byte("text"), false, time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)},
		{int64(3), "bye", "", []byte("more"), true, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
)
