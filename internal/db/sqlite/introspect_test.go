package sqlite

import (
	"strings"
	"testing"

	"github.com/rgmaz/dbt/internal/db"
)

func TestListSchemas(t *testing.T) {
	conn := newConn(t, newFixture(t))

	schemas, err := conn.ListSchemas(t.Context())
	if err != nil {
		t.Fatalf("ListSchemas() error = %v", err)
	}
	if len(schemas) != 1 || schemas[0].Name != "main" || schemas[0].IsSystem {
		t.Errorf("ListSchemas() = %+v, want main", schemas)
	}
}

func TestListRelations(t *testing.T) {
	conn := newConn(t, newFixture(t))

	relations, err := conn.ListRelations(t.Context(), "main")
	if err != nil {
		t.Fatalf("ListRelations() error = %v", err)
	}

	byName := make(map[string]db.RelationKind, len(relations))
	for _, relation := range relations {
		byName[relation.Name] = relation.Kind
		if relation.Schema != "main" {
			t.Errorf("relation %q schema = %q, want main", relation.Name, relation.Schema)
		}
		if strings.HasPrefix(strings.ToLower(relation.Name), "sqlite_") {
			t.Errorf("ListRelations() included internal relation %q", relation.Name)
		}
	}
	for _, name := range []string{
		"users", "orders", "user_totals", "implicit_fk",
		"composite", "no_pk", "misc", "weird table",
	} {
		if _, ok := byName[name]; !ok {
			t.Errorf("ListRelations() missing %q", name)
		}
	}
	if byName["users"] != db.RelationTable {
		t.Errorf("users kind = %q, want table", byName["users"])
	}
	if byName["user_totals"] != db.RelationView {
		t.Errorf("user_totals kind = %q, want view", byName["user_totals"])
	}

	relations, err = conn.ListRelations(t.Context(), "")
	if err != nil {
		t.Fatalf("ListRelations(empty schema) error = %v", err)
	}
	if len(relations) == 0 {
		t.Error("ListRelations(empty schema) returned no relations, want default main")
	}
}

func TestDescribeUsers(t *testing.T) {
	conn := newConn(t, newFixture(t))
	detail := describe(t, conn, "main", "users")

	if detail.Relation.Kind != db.RelationTable {
		t.Errorf("kind = %q, want table", detail.Relation.Kind)
	}
	if !detail.DDLSupported || !strings.Contains(strings.ToUpper(detail.DDL), "CREATE TABLE") {
		t.Errorf("DDLSupported = %v, DDL = %q", detail.DDLSupported, detail.DDL)
	}
	if len(detail.Columns) != 7 {
		t.Fatalf("len(Columns) = %d, want 7", len(detail.Columns))
	}

	id := findColumn(t, detail.Columns, "id")
	if !id.IsPrimaryKey || id.Nullable || id.Position != 1 {
		t.Errorf("id column = %+v, want primary key at position 1", id)
	}
	email := findColumn(t, detail.Columns, "email")
	if !email.Nullable || email.IsPrimaryKey {
		t.Errorf("email column = %+v, want nullable non-key", email)
	}
	name := findColumn(t, detail.Columns, "name")
	if name.Nullable || name.Default == nil || *name.Default != "'anonymous'" {
		t.Errorf("name column = %+v, want NOT NULL default 'anonymous'", name)
	}
	fullName := findColumn(t, detail.Columns, "full_name")
	if !fullName.IsGenerated {
		t.Errorf("full_name column = %+v, want generated", fullName)
	}

	expressionIndex := findIndex(t, detail.Indexes, "users_email_lower_idx")
	if !expressionIndex.Unique || expressionIndex.Primary {
		t.Errorf("users_email_lower_idx = %+v, want unique non-primary", expressionIndex)
	}
	if len(expressionIndex.Columns) != 1 || expressionIndex.Columns[0] != "(expression)" {
		t.Errorf("users_email_lower_idx columns = %v, want expression", expressionIndex.Columns)
	}
	autoIndex := findIndex(t, detail.Indexes, "sqlite_autoindex_users_1")
	if !autoIndex.Unique || len(autoIndex.Columns) != 1 || autoIndex.Columns[0] != "email" {
		t.Errorf("sqlite_autoindex_users_1 = %+v, want unique email index", autoIndex)
	}
	if len(detail.ForeignKeys) != 0 {
		t.Errorf("len(ForeignKeys) = %d, want 0", len(detail.ForeignKeys))
	}
}

func TestDescribeOrders(t *testing.T) {
	conn := newConn(t, newFixture(t))
	detail := describe(t, conn, "main", "orders")

	if len(detail.ForeignKeys) != 1 {
		t.Fatalf("len(ForeignKeys) = %d, want 1", len(detail.ForeignKeys))
	}
	fk := detail.ForeignKeys[0]
	if fk.RefSchema != "main" || fk.RefTable != "users" {
		t.Errorf("foreign key ref = %s.%s, want main.users", fk.RefSchema, fk.RefTable)
	}
	if len(fk.Columns) != 1 || fk.Columns[0] != "user_id" {
		t.Errorf("foreign key columns = %v, want user_id", fk.Columns)
	}
	if len(fk.RefColumns) != 1 || fk.RefColumns[0] != "id" {
		t.Errorf("foreign key ref columns = %v, want id", fk.RefColumns)
	}
	if fk.OnDelete != "CASCADE" || fk.OnUpdate != "RESTRICT" {
		t.Errorf("foreign key actions = delete %q update %q, want CASCADE/RESTRICT", fk.OnDelete, fk.OnUpdate)
	}

	index := findIndex(t, detail.Indexes, "orders_user_id_idx")
	if index.Unique || index.Primary || len(index.Columns) != 1 || index.Columns[0] != "user_id" {
		t.Errorf("orders_user_id_idx = %+v, want non-unique user_id index", index)
	}
}

func TestDescribeImplicitForeignKey(t *testing.T) {
	conn := newConn(t, newFixture(t))
	detail := describe(t, conn, "main", "implicit_fk")

	if len(detail.ForeignKeys) != 1 {
		t.Fatalf("len(ForeignKeys) = %d, want 1", len(detail.ForeignKeys))
	}
	fk := detail.ForeignKeys[0]
	if len(fk.RefColumns) != 1 || fk.RefColumns[0] != "id" {
		t.Errorf("implicit foreign key ref columns = %v, want resolved id", fk.RefColumns)
	}
}

func TestDescribeView(t *testing.T) {
	conn := newConn(t, newFixture(t))
	detail := describe(t, conn, "main", "user_totals")

	if detail.Relation.Kind != db.RelationView {
		t.Errorf("kind = %q, want view", detail.Relation.Kind)
	}
	if !detail.DDLSupported || !strings.Contains(strings.ToUpper(detail.DDL), "CREATE VIEW") {
		t.Errorf("DDLSupported = %v, DDL = %q", detail.DDLSupported, detail.DDL)
	}
	if len(detail.Columns) != 2 {
		t.Errorf("len(Columns) = %d, want 2", len(detail.Columns))
	}
	if len(detail.Indexes) != 0 || len(detail.ForeignKeys) != 0 {
		t.Errorf("view indexes = %v, foreign keys = %v, want empty", detail.Indexes, detail.ForeignKeys)
	}
}

func TestDescribeCompositePrimaryKey(t *testing.T) {
	conn := newConn(t, newFixture(t))
	detail := describe(t, conn, "main", "composite")

	a := findColumn(t, detail.Columns, "a")
	b := findColumn(t, detail.Columns, "b")
	if !a.IsPrimaryKey || !b.IsPrimaryKey || a.Nullable || b.Nullable {
		t.Errorf("composite key columns = %+v / %+v, want non-null primary keys", a, b)
	}
	var primary *db.Index
	for i := range detail.Indexes {
		if detail.Indexes[i].Primary {
			primary = &detail.Indexes[i]
		}
	}
	if primary == nil {
		t.Fatal("no primary index reported for composite table")
	}
	if len(primary.Columns) != 2 || primary.Columns[0] != "a" || primary.Columns[1] != "b" {
		t.Errorf("primary index columns = %v, want [a b]", primary.Columns)
	}
}

func TestDescribeWeirdIdentifiers(t *testing.T) {
	conn := newConn(t, newFixture(t))
	detail := describe(t, conn, "main", "weird table")

	for _, name := range []string{"select", `a"b`, "with space"} {
		column := findColumn(t, detail.Columns, name)
		if column.Name != name {
			t.Errorf("column name = %q, want %q", column.Name, name)
		}
	}
	if got, want := conn.Quote("main", "weird table", `a"b`), `"main"."weird table"."a""b"`; got != want {
		t.Errorf("Quote() = %q, want %q", got, want)
	}
}

func TestDescribeMissingRelation(t *testing.T) {
	conn := newConn(t, newFixture(t))
	_, err := conn.Describe(t.Context(), db.RelationRef{Schema: "main", Name: "missing"})
	if err == nil {
		t.Fatal("Describe(missing) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Describe(missing) error = %q, want not-found error", err)
	}
}

func TestQuote(t *testing.T) {
	conn := newConn(t, newFixture(t))
	if got := conn.Quote(); got != "" {
		t.Errorf("Quote() = %q, want empty", got)
	}
	if got, want := conn.Quote(`we"ird`), `"we""ird"`; got != want {
		t.Errorf("Quote() = %q, want %q", got, want)
	}
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
