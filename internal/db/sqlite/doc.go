// Package sqlite implements the SQLite driver via the pure-Go modernc driver.
//
// Connections are opened read-only using SQLite's mode=ro URI parameter and the
// query_only pragma. Catalog introspection uses pragma table-valued functions,
// so identifiers are passed as parameters rather than interpolated.
package sqlite
