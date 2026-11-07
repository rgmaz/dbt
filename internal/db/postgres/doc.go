// Package postgres implements the PostgreSQL driver via pgx's database/sql
// adapter.
//
// Connections are opened read-only by injecting
// default_transaction_read_only=on and application_name into the pgx runtime
// parameters, then reasserting read-only mode after every connect. Catalog
// introspection uses pg_catalog queries with bound parameters for names.
package postgres
