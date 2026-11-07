package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rgmaz/dbt/internal/db"
)

// ListSchemas implements db.Conn. System namespaces are excluded per the
// introspection contract.
func (c *Conn) ListSchemas(ctx context.Context) ([]db.Schema, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	rows, err := c.db.QueryContext(ctx, `
SELECT n.nspname
FROM pg_catalog.pg_namespace n
WHERE n.nspname !~ '^pg_'
  AND n.nspname <> 'information_schema'
ORDER BY n.nspname`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list schemas: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var schemas []db.Schema
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("postgres: scan schema: %w", err)
		}
		schemas = append(schemas, db.Schema{Name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list schemas: %w", err)
	}
	return schemas, nil
}

// ListRelations implements db.Conn.
func (c *Conn) ListRelations(ctx context.Context, schema string) ([]db.Relation, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	schema = schemaOrDefault(schema)
	rows, err := c.db.QueryContext(ctx, `
SELECT c.relname, c.relkind
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1
  AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
ORDER BY c.relname`, schema)
	if err != nil {
		return nil, fmt.Errorf("postgres: list relations in %s: %w", schema, err)
	}
	defer func() { _ = rows.Close() }()

	var relations []db.Relation
	for rows.Next() {
		var (
			name    string
			relkind string
		)
		if err := rows.Scan(&name, &relkind); err != nil {
			return nil, fmt.Errorf("postgres: scan relation in %s: %w", schema, err)
		}
		kind, ok := relationKind(relkind)
		if !ok {
			continue
		}
		relations = append(relations, db.Relation{Schema: schema, Name: name, Kind: kind})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list relations in %s: %w", schema, err)
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

	var relkind string
	err := c.db.QueryRowContext(ctx, `
SELECT c.relkind
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1
  AND c.relname = $2
  AND c.relkind IN ('r', 'p', 'v', 'm', 'f')`, ref.Schema, ref.Name).Scan(&relkind)
	if errors.Is(err, sql.ErrNoRows) {
		return db.RelationDetail{}, fmt.Errorf("postgres: relation %s not found", ref)
	}
	if err != nil {
		return db.RelationDetail{}, fmt.Errorf("postgres: describe %s: %w", ref, err)
	}
	kind, ok := relationKind(relkind)
	if !ok {
		return db.RelationDetail{}, fmt.Errorf("postgres: relation %s has unsupported type %q", ref, relkind)
	}

	detail := db.RelationDetail{
		Relation:     db.Relation{Schema: ref.Schema, Name: ref.Name, Kind: kind},
		DDLSupported: false,
	}

	columns, err := c.describeColumns(ctx, ref)
	if err != nil {
		return db.RelationDetail{}, err
	}
	detail.Columns = columns

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

	return detail, nil
}

func (c *Conn) describeColumns(ctx context.Context, ref db.RelationRef) ([]db.Column, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT a.attname,
       pg_catalog.format_type(a.atttypid, a.atttypmod),
       NOT a.attnotnull,
       pg_catalog.pg_get_expr(d.adbin, d.adrelid),
       a.attnum,
       EXISTS (
           SELECT 1
           FROM pg_catalog.pg_index i
           WHERE i.indrelid = a.attrelid
             AND i.indisprimary
             AND a.attnum = ANY (i.indkey)
       ),
       a.attgenerated <> '',
       COALESCE(pg_catalog.col_description(a.attrelid, a.attnum), '')
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE n.nspname = $1
  AND c.relname = $2
  AND a.attnum > 0
  AND NOT a.attisdropped
ORDER BY a.attnum`, ref.Schema, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("postgres: describe columns for %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	var columns []db.Column
	for rows.Next() {
		var (
			name        string
			dataType    string
			nullable    bool
			defaultExpr sql.NullString
			position    int
			primary     bool
			generated   bool
			comment     string
		)
		if err := rows.Scan(&name, &dataType, &nullable, &defaultExpr, &position, &primary, &generated, &comment); err != nil {
			return nil, fmt.Errorf("postgres: scan column for %s: %w", ref, err)
		}

		column := db.Column{
			Name:         name,
			DataType:     dataType,
			Nullable:     nullable,
			Position:     position,
			IsPrimaryKey: primary,
			IsGenerated:  generated,
			Comment:      comment,
		}
		if defaultExpr.Valid {
			value := defaultExpr.String
			column.Default = &value
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: describe columns for %s: %w", ref, err)
	}
	return columns, nil
}

func (c *Conn) describeIndexes(ctx context.Context, ref db.RelationRef) ([]db.Index, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT ic.relname,
       i.indisunique,
       i.indisprimary,
       am.amname,
       pg_catalog.pg_get_indexdef(i.indexrelid, k.ord, true)
FROM pg_catalog.pg_index i
JOIN pg_catalog.pg_class tc ON tc.oid = i.indrelid
JOIN pg_catalog.pg_namespace n ON n.oid = tc.relnamespace
JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
JOIN pg_catalog.pg_am am ON am.oid = ic.relam
JOIN LATERAL generate_series(1, i.indnkeyatts) AS k(ord) ON TRUE
WHERE n.nspname = $1
  AND tc.relname = $2
ORDER BY ic.relname, k.ord`, ref.Schema, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("postgres: describe indexes for %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		indexes []db.Index
		byName  = make(map[string]int)
	)
	for rows.Next() {
		var (
			name    string
			unique  bool
			primary bool
			method  string
			column  string
		)
		if err := rows.Scan(&name, &unique, &primary, &method, &column); err != nil {
			return nil, fmt.Errorf("postgres: scan index for %s: %w", ref, err)
		}

		if i, ok := byName[name]; ok {
			indexes[i].Columns = append(indexes[i].Columns, column)
			continue
		}
		byName[name] = len(indexes)
		indexes = append(indexes, db.Index{
			Name:    name,
			Columns: []string{column},
			Unique:  unique,
			Primary: primary,
			Method:  method,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: describe indexes for %s: %w", ref, err)
	}
	return indexes, nil
}

func (c *Conn) describeForeignKeys(ctx context.Context, ref db.RelationRef) ([]db.ForeignKey, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT con.conname,
       rn.nspname,
       rc.relname,
       con.confupdtype,
       con.confdeltype,
       a.attname,
       fa.attname
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_class rc ON rc.oid = con.confrelid
JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
JOIN LATERAL generate_subscripts(con.conkey, 1) AS k(ord) ON TRUE
JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = con.conkey[k.ord]
JOIN pg_catalog.pg_attribute fa ON fa.attrelid = con.confrelid AND fa.attnum = con.confkey[k.ord]
WHERE con.contype = 'f'
  AND n.nspname = $1
  AND c.relname = $2
ORDER BY con.conname, k.ord`, ref.Schema, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("postgres: describe foreign keys for %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		foreignKeys []db.ForeignKey
		byName      = make(map[string]int)
	)
	for rows.Next() {
		var (
			name      string
			refSchema string
			refTable  string
			onUpdate  string
			onDelete  string
			column    string
			refColumn string
		)
		if err := rows.Scan(&name, &refSchema, &refTable, &onUpdate, &onDelete, &column, &refColumn); err != nil {
			return nil, fmt.Errorf("postgres: scan foreign key for %s: %w", ref, err)
		}

		if i, ok := byName[name]; ok {
			foreignKeys[i].Columns = append(foreignKeys[i].Columns, column)
			foreignKeys[i].RefColumns = append(foreignKeys[i].RefColumns, refColumn)
			continue
		}
		byName[name] = len(foreignKeys)
		foreignKeys = append(foreignKeys, db.ForeignKey{
			Name:       name,
			Columns:    []string{column},
			RefSchema:  refSchema,
			RefTable:   refTable,
			RefColumns: []string{refColumn},
			OnDelete:   foreignKeyAction(onDelete),
			OnUpdate:   foreignKeyAction(onUpdate),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: describe foreign keys for %s: %w", ref, err)
	}
	return foreignKeys, nil
}

func (c *Conn) primaryKeyColumns(ctx context.Context, ref db.RelationRef) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT a.attname
FROM pg_catalog.pg_index i
JOIN pg_catalog.pg_class c ON c.oid = i.indrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN LATERAL unnest(i.indkey::smallint[]) WITH ORDINALITY AS k(attnum, ord) ON TRUE
JOIN pg_catalog.pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
WHERE n.nspname = $1
  AND c.relname = $2
  AND i.indisprimary
ORDER BY k.ord`, ref.Schema, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("postgres: primary key columns for %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()

	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("postgres: scan primary key column for %s: %w", ref, err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: primary key columns for %s: %w", ref, err)
	}
	return columns, nil
}

func relationKind(relkind string) (db.RelationKind, bool) {
	switch relkind {
	case "r", "p":
		return db.RelationTable, true
	case "v":
		return db.RelationView, true
	case "m":
		return db.RelationMaterializedView, true
	case "f":
		return db.RelationForeignTable, true
	default:
		return "", false
	}
}

func foreignKeyAction(code string) string {
	switch code {
	case "a":
		return "NO ACTION"
	case "r":
		return "RESTRICT"
	case "c":
		return "CASCADE"
	case "n":
		return "SET NULL"
	case "d":
		return "SET DEFAULT"
	default:
		return code
	}
}

func schemaOrDefault(schema string) string {
	if schema == "" {
		return defaultSchema
	}
	return schema
}

func normalizeRef(ref db.RelationRef) db.RelationRef {
	if ref.Schema == "" {
		ref.Schema = defaultSchema
	}
	return ref
}

func validateRef(ref db.RelationRef) error {
	if ref.Name == "" {
		return errors.New("postgres: relation name is required")
	}
	return nil
}
