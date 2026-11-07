package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rgmaz/dbt/internal/db"
)

// ListSchemas implements db.Conn. SQLite reports "main", "temp", and any
// attached databases; the temporary schema is excluded as a system schema.
func (c *Conn) ListSchemas(ctx context.Context) ([]db.Schema, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	rows, err := c.db.QueryContext(ctx, "SELECT name FROM pragma_database_list ORDER BY seq")
	if err != nil {
		return nil, fmt.Errorf("sqlite: list schemas: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var schemas []db.Schema
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("sqlite: scan schema: %w", err)
		}
		if name == "" || name == "temp" {
			continue
		}
		schemas = append(schemas, db.Schema{Name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list schemas: %w", err)
	}
	return schemas, nil
}

// ListRelations implements db.Conn.
func (c *Conn) ListRelations(ctx context.Context, schema string) ([]db.Relation, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	schema = schemaOrDefault(schema)
	query := fmt.Sprintf(`
SELECT name, type
FROM %s.sqlite_master
WHERE type IN ('table', 'view')
  AND lower(substr(name, 1, 7)) <> 'sqlite_'
ORDER BY name`, quoteIdentifier(schema))

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list relations in %s: %w", schema, err)
	}
	defer func() { _ = rows.Close() }()

	var relations []db.Relation
	for rows.Next() {
		var (
			name string
			typ  string
		)
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, fmt.Errorf("sqlite: scan relation in %s: %w", schema, err)
		}
		kind, ok := relationKind(typ)
		if !ok {
			continue
		}
		relations = append(relations, db.Relation{Schema: schema, Name: name, Kind: kind})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list relations in %s: %w", schema, err)
	}
	return relations, nil
}

// Describe implements db.Conn.
func (c *Conn) Describe(ctx context.Context, ref db.RelationRef) (db.RelationDetail, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	ref = normalizeRef(ref)
	if err := validateRef(ref); err != nil {
		return db.RelationDetail{}, err
	}

	detail := db.RelationDetail{Relation: db.Relation{Schema: ref.Schema, Name: ref.Name}}
	var (
		typ string
		ddl sql.NullString
	)
	query := fmt.Sprintf(
		`SELECT type, sql FROM %s.sqlite_master WHERE name = ? AND type IN ('table', 'view')`,
		quoteIdentifier(ref.Schema),
	)
	err := c.db.QueryRowContext(ctx, query, ref.Name).Scan(&typ, &ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return db.RelationDetail{}, fmt.Errorf("sqlite: relation %s not found", ref)
	}
	if err != nil {
		return db.RelationDetail{}, fmt.Errorf("sqlite: describe %s: %w", ref, err)
	}

	kind, ok := relationKind(typ)
	if !ok {
		return db.RelationDetail{}, fmt.Errorf("sqlite: relation %s has unsupported type %q", ref, typ)
	}
	detail.Relation.Kind = kind
	if ddl.Valid && ddl.String != "" {
		detail.DDL = ddl.String
		detail.DDLSupported = true
	}

	columns, err := c.describeColumns(ctx, ref)
	if err != nil {
		return db.RelationDetail{}, err
	}
	detail.Columns = columns

	if kind == db.RelationTable {
		indexes, err := c.describeIndexes(ctx, ref)
		if err != nil {
			return db.RelationDetail{}, err
		}
		detail.Indexes = indexes

		foreignKeys, err := c.describeForeignKeys(ctx, ref)
		if err != nil {
			return db.RelationDetail{}, err
		}
		detail.ForeignKeys = foreignKeys
	}

	return detail, nil
}

func (c *Conn) describeColumns(ctx context.Context, ref db.RelationRef) ([]db.Column, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT "cid", "name", "type", "notnull", "dflt_value", "pk", "hidden"
FROM pragma_table_xinfo(?, ?)
ORDER BY "cid"`, ref.Name, ref.Schema)
	if err != nil {
		return nil, fmt.Errorf("sqlite: describe columns for %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	var columns []db.Column
	for rows.Next() {
		var (
			cid      int
			name     string
			dataType sql.NullString
			notNull  int
			dflt     sql.NullString
			pk       int
			hidden   int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &dflt, &pk, &hidden); err != nil {
			return nil, fmt.Errorf("sqlite: scan column for %s: %w", ref, err)
		}
		if hidden == 1 {
			continue
		}

		column := db.Column{
			Name:         name,
			DataType:     dataType.String,
			Nullable:     notNull == 0 && pk == 0,
			Position:     cid + 1,
			IsPrimaryKey: pk > 0,
			IsGenerated:  hidden == 2 || hidden == 3,
		}
		if dflt.Valid {
			value := dflt.String
			column.Default = &value
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: describe columns for %s: %w", ref, err)
	}
	return columns, nil
}

func (c *Conn) describeIndexes(ctx context.Context, ref db.RelationRef) ([]db.Index, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT "name", "unique", "origin"
FROM pragma_index_list(?, ?)
ORDER BY "seq"`, ref.Name, ref.Schema)
	if err != nil {
		return nil, fmt.Errorf("sqlite: describe indexes for %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	var indexes []db.Index
	for rows.Next() {
		var (
			name   string
			unique int
			origin string
		)
		if err := rows.Scan(&name, &unique, &origin); err != nil {
			return nil, fmt.Errorf("sqlite: scan index for %s: %w", ref, err)
		}

		columns, err := c.indexColumns(ctx, ref.Schema, name)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, db.Index{
			Name:    name,
			Columns: columns,
			Unique:  unique != 0,
			Primary: origin == "pk",
			Method:  "btree",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: describe indexes for %s: %w", ref, err)
	}
	return indexes, nil
}

func (c *Conn) indexColumns(ctx context.Context, schema, index string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT "name"
FROM pragma_index_xinfo(?, ?)
WHERE "key" = 1
ORDER BY "seqno"`, index, schema)
	if err != nil {
		return nil, fmt.Errorf("sqlite: describe index %s: %w", index, err)
	}
	defer func() { _ = rows.Close() }()

	var columns []string
	for rows.Next() {
		var name sql.NullString
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("sqlite: scan index column for %s: %w", index, err)
		}
		if name.Valid && name.String != "" {
			columns = append(columns, name.String)
			continue
		}
		columns = append(columns, "(expression)")
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: describe index %s: %w", index, err)
	}
	return columns, nil
}

func (c *Conn) describeForeignKeys(ctx context.Context, ref db.RelationRef) ([]db.ForeignKey, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT "id", "seq", "table", "from", "to", "on_update", "on_delete"
FROM pragma_foreign_key_list(?, ?)
ORDER BY "id", "seq"`, ref.Name, ref.Schema)
	if err != nil {
		return nil, fmt.Errorf("sqlite: describe foreign keys for %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		order   []int
		byID    = make(map[int]*db.ForeignKey)
		pkCache = make(map[db.RelationRef][]string)
	)
	for rows.Next() {
		var (
			id       int
			seq      int
			refTable string
			from     string
			to       sql.NullString
			onUpdate string
			onDelete string
		)
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete); err != nil {
			return nil, fmt.Errorf("sqlite: scan foreign key for %s: %w", ref, err)
		}

		fk, ok := byID[id]
		if !ok {
			fk = &db.ForeignKey{
				Name:      fmt.Sprintf("fk_%s_%d", ref.Name, id),
				RefSchema: ref.Schema,
				RefTable:  refTable,
				OnUpdate:  onUpdate,
				OnDelete:  onDelete,
			}
			byID[id] = fk
			order = append(order, id)
		}
		fk.Columns = append(fk.Columns, from)

		target := to.String
		if !to.Valid {
			pkRef := db.RelationRef{Schema: ref.Schema, Name: refTable}
			pk, cached := pkCache[pkRef]
			if !cached {
				var pkErr error
				pk, pkErr = c.primaryKeyColumns(ctx, pkRef)
				if pkErr != nil {
					return nil, pkErr
				}
				pkCache[pkRef] = pk
			}
			if seq < len(pk) {
				target = pk[seq]
			}
		}
		fk.RefColumns = append(fk.RefColumns, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: describe foreign keys for %s: %w", ref, err)
	}

	foreignKeys := make([]db.ForeignKey, 0, len(order))
	for _, id := range order {
		foreignKeys = append(foreignKeys, *byID[id])
	}
	return foreignKeys, nil
}

func (c *Conn) primaryKeyColumns(ctx context.Context, ref db.RelationRef) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT "name"
FROM pragma_table_xinfo(?, ?)
WHERE "pk" > 0
ORDER BY "pk"`, ref.Name, ref.Schema)
	if err != nil {
		return nil, fmt.Errorf("sqlite: primary key columns for %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("sqlite: scan primary key column for %s: %w", ref, err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: primary key columns for %s: %w", ref, err)
	}
	return columns, nil
}

func relationKind(typ string) (db.RelationKind, bool) {
	switch typ {
	case "table":
		return db.RelationTable, true
	case "view":
		return db.RelationView, true
	default:
		return "", false
	}
}

func schemaOrDefault(schema string) string {
	if schema == "" {
		return "main"
	}
	return schema
}

func normalizeRef(ref db.RelationRef) db.RelationRef {
	if ref.Schema == "" {
		ref.Schema = "main"
	}
	return ref
}

func validateRef(ref db.RelationRef) error {
	if ref.Name == "" {
		return errors.New("sqlite: relation name is required")
	}
	return nil
}
