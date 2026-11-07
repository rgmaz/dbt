package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rgmaz/dbt/internal/db"
)

const maxBinaryPreview = 64

// Page implements db.Conn.
func (c *Conn) Page(ctx context.Context, ref db.RelationRef, order []db.Order, limit, offset int) (*db.Result, error) {
	if limit <= 0 {
		return nil, errors.New("postgres: page limit must be positive")
	}
	if offset < 0 {
		return nil, errors.New("postgres: page offset must not be negative")
	}
	ref = normalizeRef(ref)
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	order = append([]db.Order(nil), order...)
	if len(order) == 0 {
		primaryKey, err := c.primaryKeyColumns(ctx, ref)
		if err != nil {
			return nil, err
		}
		for _, column := range primaryKey {
			order = append(order, db.Order{Column: column})
		}
	}

	statement := "SELECT * FROM " + c.Quote(ref.Schema, ref.Name)
	if len(order) > 0 {
		parts := make([]string, len(order))
		for i, item := range order {
			if item.Column == "" {
				return nil, errors.New("postgres: page order column is required")
			}
			parts[i] = c.Quote(item.Column)
			if item.Desc {
				parts[i] += " DESC"
			}
		}
		statement += " ORDER BY " + strings.Join(parts, ", ")
	}
	statement += " LIMIT " + strconv.Itoa(limit+1) + " OFFSET " + strconv.Itoa(offset)

	start := time.Now()
	rows, err := c.db.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("postgres: page %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	columns, data, truncated, err := scanRows(rows, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: page %s: %w", ref, err)
	}
	return &db.Result{
		Columns:   columns,
		Rows:      data,
		RowCount:  len(data),
		Truncated: truncated,
		Duration:  time.Since(start),
		Statement: statement,
	}, nil
}

// Count implements db.Conn. PostgreSQL exposes an approximate row count in
// pg_class.reltuples, so Exact is always false. A negative estimate means the
// table has never been analyzed and the count is unknown.
func (c *Conn) Count(ctx context.Context, ref db.RelationRef) (db.Count, error) {
	ref = normalizeRef(ref)
	if err := validateRef(ref); err != nil {
		return db.Count{}, err
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	var estimate float64
	err := c.db.QueryRowContext(ctx, `
SELECT c.reltuples::double precision
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1
  AND c.relname = $2
  AND c.relkind IN ('r', 'p', 'v', 'm', 'f')`, ref.Schema, ref.Name).Scan(&estimate)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Count{}, fmt.Errorf("postgres: relation %s not found", ref)
	}
	if err != nil {
		return db.Count{}, fmt.Errorf("postgres: count %s: %w", ref, err)
	}
	if estimate < 0 {
		return db.Count{Exact: false}, nil
	}
	return db.Count{Value: int64(estimate), Exact: false}, nil
}

// Query implements db.Conn. It runs a read-only statement and stops after
// maxRows rows, reporting whether more rows were available.
func (c *Conn) Query(ctx context.Context, statement string, maxRows int) (*db.Result, error) {
	if strings.TrimSpace(statement) == "" {
		return nil, errors.New("postgres: query statement is required")
	}
	if maxRows <= 0 || maxRows > c.opts.MaxRows {
		maxRows = c.opts.MaxRows
	}
	if maxRows <= 0 {
		maxRows = db.DefaultMaxRows
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	start := time.Now()
	rows, err := c.db.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("postgres: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, data, truncated, err := scanRows(rows, maxRows)
	if err != nil {
		return nil, fmt.Errorf("postgres: query: %w", err)
	}
	return &db.Result{
		Columns:   columns,
		Rows:      data,
		RowCount:  len(data),
		Truncated: truncated,
		Duration:  time.Since(start),
		Statement: statement,
	}, nil
}

func scanRows(rows *sql.Rows, max int) ([]db.ResultColumn, [][]db.Cell, bool, error) {
	if max <= 0 {
		return nil, nil, false, errors.New("postgres: max rows must be positive")
	}

	columns, err := resultColumns(rows)
	if err != nil {
		return nil, nil, false, err
	}

	data := make([][]db.Cell, 0, min(max, 64))
	truncated := false
	for rows.Next() {
		if len(data) == max {
			truncated = true
			break
		}
		cells, err := scanRow(rows, len(columns))
		if err != nil {
			return nil, nil, false, err
		}
		data = append(data, cells)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, err
	}
	return columns, data, truncated, nil
}

func resultColumns(rows *sql.Rows) ([]db.ResultColumn, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("postgres: result columns: %w", err)
	}

	columns := make([]db.ResultColumn, len(types))
	for i, typ := range types {
		nullable, known := typ.Nullable()
		columns[i] = db.ResultColumn{
			Name:         typ.Name(),
			DatabaseType: typ.DatabaseTypeName(),
			Nullable:     known && nullable,
		}
	}
	return columns, nil
}

func scanRow(rows *sql.Rows, count int) ([]db.Cell, error) {
	values := make([]any, count)
	pointers := make([]any, count)
	for i := range values {
		pointers[i] = &values[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, fmt.Errorf("postgres: scan row: %w", err)
	}

	cells := make([]db.Cell, count)
	for i, value := range values {
		cells[i] = formatCell(value)
	}
	return cells, nil
}

func formatCell(value any) db.Cell {
	if value == nil {
		return db.Cell{Null: true}
	}

	switch v := value.(type) {
	case string:
		return db.Cell{Text: v}
	case []byte:
		return db.Cell{Text: formatBytes(v)}
	case int64:
		return db.Cell{Text: strconv.FormatInt(v, 10)}
	case float64:
		return db.Cell{Text: strconv.FormatFloat(v, 'g', -1, 64)}
	case bool:
		return db.Cell{Text: strconv.FormatBool(v)}
	case time.Time:
		return db.Cell{Text: v.Format(time.RFC3339Nano)}
	default:
		return db.Cell{Text: fmt.Sprint(v)}
	}
}

func formatBytes(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	if len(b) > maxBinaryPreview {
		return fmt.Sprintf("0x%x… (%d bytes)", b[:maxBinaryPreview], len(b))
	}
	return fmt.Sprintf("0x%x", b)
}
