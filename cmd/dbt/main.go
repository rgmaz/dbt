// Command dbt is a terminal database viewer for SQLite and PostgreSQL.
package main

import "fmt"

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	fmt.Printf("dbt %s\n", version)
}
