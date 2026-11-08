// Command dbt is a terminal database viewer for SQLite and PostgreSQL.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
