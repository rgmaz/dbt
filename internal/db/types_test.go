package db

import "testing"

func TestKindValid(t *testing.T) {
	for _, kind := range []Kind{KindPostgres, KindSQLite} {
		if !kind.Valid() {
			t.Errorf("Kind(%q).Valid() = false, want true", kind)
		}
	}
	for _, kind := range []Kind{"", "mysql", "POSTGRES"} {
		if kind.Valid() {
			t.Errorf("Kind(%q).Valid() = true, want false", kind)
		}
	}
}

func TestRelationRef(t *testing.T) {
	relation := Relation{Schema: "public", Name: "users", Kind: RelationTable}
	ref := relation.Ref()
	if ref != (RelationRef{Schema: "public", Name: "users"}) {
		t.Errorf("Ref() = %+v", ref)
	}
	if got, want := ref.String(), "public.users"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
