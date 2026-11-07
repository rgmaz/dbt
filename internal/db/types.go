package db

import (
	"context"
	"io"
	"time"
)

// Kind identifies a supported database driver.
type Kind string

// Supported database kinds.
const (
	KindPostgres Kind = "postgres"
	KindSQLite   Kind = "sqlite"
)

// Valid reports whether k is a supported database kind.
func (k Kind) Valid() bool {
	return k == KindPostgres || k == KindSQLite
}

// Schema is a database namespace.
type Schema struct {
	Name     string
	IsSystem bool
}

// RelationKind classifies a relation.
type RelationKind string

// Supported relation kinds.
const (
	RelationTable            RelationKind = "table"
	RelationView             RelationKind = "view"
	RelationMaterializedView RelationKind = "materialized_view"
	RelationForeignTable     RelationKind = "foreign_table"
)

// Relation is a table, view, materialized view, or foreign table.
type Relation struct {
	Schema string
	Name   string
	Kind   RelationKind
}

// Ref returns the relation's address.
func (r Relation) Ref() RelationRef {
	return RelationRef{Schema: r.Schema, Name: r.Name}
}

// RelationRef identifies a relation without its kind.
type RelationRef struct {
	Schema string
	Name   string
}

// String returns an unquoted display name. It is not safe for SQL.
func (r RelationRef) String() string {
	return r.Schema + "." + r.Name
}

// Column describes a relation column.
type Column struct {
	Name         string
	DataType     string
	Nullable     bool
	Default      *string
	Position     int
	IsPrimaryKey bool
	IsGenerated  bool
	Comment      string
}

// Index describes a relation index.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
	Primary bool
	Method  string
}

// ForeignKey describes a relation's foreign key constraint.
type ForeignKey struct {
	Name       string
	Columns    []string
	RefSchema  string
	RefTable   string
	RefColumns []string
	OnDelete   string
	OnUpdate   string
}

// RelationDetail is the metadata rendered by the structure pane.
type RelationDetail struct {
	Relation     Relation
	Columns      []Column
	Indexes      []Index
	ForeignKeys  []ForeignKey
	DDL          string
	DDLSupported bool
}

// Order is one column in a page sort.
type Order struct {
	Column string
	Desc   bool
}

// Count is a row count, which may be an estimate.
type Count struct {
	Value int64
	Exact bool
}

// Cell is one display value. Null distinguishes NULL from the empty string.
type Cell struct {
	Text string
	Null bool
}

// ResultColumn describes a query result column.
type ResultColumn struct {
	Name         string
	DatabaseType string
	Nullable     bool
}

// Result is a bounded query or page result.
type Result struct {
	Columns   []ResultColumn
	Rows      [][]Cell
	RowCount  int
	Truncated bool
	Duration  time.Duration
	Statement string
}

// Connector opens connections for a database kind.
type Connector interface {
	Kind() Kind
	Open(ctx context.Context, dsn string, opts Options) (Conn, error)
}

// Conn is a read-only database session.
type Conn interface {
	io.Closer

	Ping(ctx context.Context) error
	ServerVersion(ctx context.Context) (string, error)

	ListSchemas(ctx context.Context) ([]Schema, error)
	ListRelations(ctx context.Context, schema string) ([]Relation, error)
	Describe(ctx context.Context, ref RelationRef) (RelationDetail, error)

	Page(ctx context.Context, ref RelationRef, order []Order, limit, offset int) (*Result, error)
	Count(ctx context.Context, ref RelationRef) (Count, error)
	Query(ctx context.Context, sql string, maxRows int) (*Result, error)

	Quote(parts ...string) string
}
