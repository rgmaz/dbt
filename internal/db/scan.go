package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"
)

const maxBinaryPreview = 64

// WithQueryTimeout returns a context bounded by timeout. Non-positive values
// fall back to DefaultQueryTimeout.
func WithQueryTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = DefaultQueryTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

// RunQuery executes statement and scans at most maxRows rows. The returned
// result records truncation, duration, and the original statement. Callers are
// responsible for wrapping the error with driver context.
func RunQuery(ctx context.Context, sqlDB *sql.DB, statement string, maxRows int) (*Result, error) {
	if sqlDB == nil {
		return nil, errors.New("db: nil database")
	}
	if maxRows <= 0 {
		return nil, errors.New("db: max rows must be positive")
	}

	start := time.Now()
	rows, err := sqlDB.QueryContext(ctx, statement)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	columns, data, truncated, err := scanRows(rows, maxRows)
	if err != nil {
		return nil, err
	}
	return &Result{
		Columns:   columns,
		Rows:      data,
		RowCount:  len(data),
		Truncated: truncated,
		Duration:  time.Since(start),
		Statement: statement,
	}, nil
}

func scanRows(rows *sql.Rows, max int) ([]ResultColumn, [][]Cell, bool, error) {
	if max <= 0 {
		return nil, nil, false, errors.New("db: max rows must be positive")
	}

	columns, err := resultColumns(rows)
	if err != nil {
		return nil, nil, false, err
	}

	data := make([][]Cell, 0, min(max, 64))
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

func resultColumns(rows *sql.Rows) ([]ResultColumn, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("db: result columns: %w", err)
	}

	columns := make([]ResultColumn, len(types))
	for i, typ := range types {
		nullable, known := typ.Nullable()
		columns[i] = ResultColumn{
			Name:         typ.Name(),
			DatabaseType: typ.DatabaseTypeName(),
			Nullable:     known && nullable,
		}
	}
	return columns, nil
}

func scanRow(rows *sql.Rows, count int) ([]Cell, error) {
	values := make([]any, count)
	pointers := make([]any, count)
	for i := range values {
		pointers[i] = &values[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, fmt.Errorf("db: scan row: %w", err)
	}

	cells := make([]Cell, count)
	for i, value := range values {
		cells[i] = formatCell(value)
	}
	return cells, nil
}

func formatCell(value any) Cell {
	if value == nil {
		return Cell{Null: true}
	}

	switch v := value.(type) {
	case string:
		return Cell{Text: v}
	case []byte:
		return Cell{Text: formatBytes(v)}
	case int64:
		return Cell{Text: strconv.FormatInt(v, 10)}
	case float64:
		return Cell{Text: strconv.FormatFloat(v, 'g', -1, 64)}
	case bool:
		return Cell{Text: strconv.FormatBool(v)}
	case time.Time:
		return Cell{Text: v.Format(time.RFC3339Nano)}
	default:
		return Cell{Text: fmt.Sprint(v)}
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
