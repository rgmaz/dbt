package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/rgmaz/dbt/internal/db"
)

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

	result, err := db.RunQuery(ctx, c.db, statement, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: page %s: %w", ref, err)
	}
	return result, nil
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

	result, err := db.RunQuery(ctx, c.db, statement, maxRows)
	if err != nil {
		return nil, fmt.Errorf("postgres: query: %w", err)
	}
	return result, nil
}
