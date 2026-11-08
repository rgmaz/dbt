package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/rgmaz/dbt/internal/config"
	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/db/postgres"
	"github.com/rgmaz/dbt/internal/db/sqlite"
	dbtlog "github.com/rgmaz/dbt/internal/log"
)

// Exit codes defined by the CLI contract.
const (
	exitOK        = 0
	exitError     = 1
	exitUsage     = 2
	exitInterrupt = 130
)

const usageText = `dbt - terminal database viewer for SQLite and PostgreSQL

Usage:
  dbt [flags] [dsn]

Flags:
  --config string       config path (default $DBT_CONFIG, else $XDG_CONFIG_HOME/dbt/config.toml)
  --connection string   profile name from config
  --check               connect, print the server version, and exit (no TUI)
  --debug               write debug logs to $XDG_STATE_HOME/dbt/dbt.log
  --version             print version and exit
  --help                show this help

DSN:
  postgres://... or postgresql://...   PostgreSQL
  sqlite:path or /path/to/file.db      SQLite

Precedence: positional dsn > --connection > interactive picker (not yet available).

Exit codes: 0 success, 1 runtime/DB error, 2 usage/config error, 130 interrupted.
`

type options struct {
	configPath string
	connection string
	check      bool
	debug      bool
	version    bool
	help       bool
	dsn        string
}

type target struct {
	kind   db.Kind
	dsn    string
	source string
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			write(stdout, usageText)
			return exitOK
		}
		writef(stderr, "dbt: %s\n\n", err)
		write(stderr, usageText)
		return exitUsage
	}

	if opts.help {
		write(stdout, usageText)
		return exitOK
	}
	if opts.version {
		writef(stdout, "dbt %s\n", version)
		return exitOK
	}
	if ctx.Err() != nil {
		writef(stderr, "dbt: interrupted\n")
		return exitInterrupt
	}

	logger, closeLog := setupLogger(opts.debug, stderr)
	defer closeLog()
	logger.Info("start", "version", version, "check", opts.check, "debug", opts.debug)

	cfg, code := loadConfig(ctx, opts.configPath, logger, stderr)
	if code != exitOK {
		return code
	}

	if opts.dsn != "" && opts.connection != "" {
		writef(stderr, "dbt: warning: ignoring --connection %q because a positional DSN was provided\n", opts.connection)
		logger.Warn("ignoring connection profile", "profile", opts.connection)
	}

	resolved, err := resolveTarget(cfg, opts.dsn, opts.connection)
	if err != nil {
		return fail(ctx, logger, stderr, exitUsage, err)
	}
	logger.Info("connection",
		"source", resolved.source,
		"kind", resolved.kind,
		"target", dbtlog.RedactDSN(resolved.dsn),
	)

	connector, err := connectorFor(resolved.kind)
	if err != nil {
		return fail(ctx, logger, stderr, exitUsage, err)
	}

	conn, err := db.Open(ctx, connector, resolved.dsn, db.Options{
		ReadOnly:     true,
		QueryTimeout: cfg.QueryTimeout,
	})
	if err != nil {
		return fail(ctx, logger, stderr, exitError, err)
	}
	defer func() { _ = conn.Close() }()

	if opts.check {
		return runCheck(ctx, logger, conn, resolved.kind, stdout, stderr)
	}

	logger.Info("interactive mode unavailable")
	writef(stderr, "dbt: interactive TUI is not implemented yet; use --check\n")
	return exitError
}

func parseArgs(args []string) (options, error) {
	var opts options

	fs := flag.NewFlagSet("dbt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	fs.StringVar(&opts.configPath, "config", "", "config path")
	fs.StringVar(&opts.connection, "connection", "", "profile name from config")
	fs.BoolVar(&opts.check, "check", false, "connect and print the server version")
	fs.BoolVar(&opts.debug, "debug", false, "write debug logs")
	fs.BoolVar(&opts.version, "version", false, "print version")
	fs.BoolVar(&opts.help, "help", false, "show help")
	fs.BoolVar(&opts.help, "h", false, "show help")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	rest := fs.Args()
	if len(rest) > 1 {
		return options{}, fmt.Errorf("expected at most one DSN, got %d", len(rest))
	}
	if len(rest) == 1 {
		opts.dsn = rest[0]
	}
	return opts, nil
}

func setupLogger(debug bool, stderr io.Writer) (*slog.Logger, func()) {
	if !debug {
		return dbtlog.Discard(), func() {}
	}

	path, err := dbtlog.DefaultPath()
	if err != nil {
		writef(stderr, "dbt: warning: %v; debug logging disabled\n", err)
		return dbtlog.Discard(), func() {}
	}
	logger, closer, err := dbtlog.Open(path)
	if err != nil {
		writef(stderr, "dbt: warning: %v; debug logging disabled\n", err)
		return dbtlog.Discard(), func() {}
	}
	return logger, func() { _ = closer.Close() }
}

func loadConfig(ctx context.Context, path string, logger *slog.Logger, stderr io.Writer) (*config.Config, int) {
	resolved, err := config.ResolvePath(path)
	if err != nil {
		return nil, fail(ctx, logger, stderr, exitUsage, err)
	}

	if path != "" {
		if _, err := os.Stat(resolved); err != nil {
			return nil, fail(ctx, logger, stderr, exitUsage, fmt.Errorf("config %s: %w", resolved, err))
		}
	}

	cfg, err := config.Load(resolved)
	if err != nil {
		return nil, fail(ctx, logger, stderr, exitUsage, err)
	}

	logger.Info("config", "path", cfg.Path(), "connections", len(cfg.Connections))
	for _, warning := range cfg.Warnings() {
		writef(stderr, "dbt: warning: %s\n", warning)
		logger.Warn("config warning", "warning", warning)
	}
	return cfg, exitOK
}

func resolveTarget(cfg *config.Config, dsnArg, profileName string) (target, error) {
	dsnArg = strings.TrimSpace(dsnArg)
	profileName = strings.TrimSpace(profileName)

	if dsnArg != "" {
		kind, dsn, err := db.ParseDSN(dsnArg)
		if err != nil {
			return target{}, err
		}
		return target{kind: kind, dsn: dsn, source: "dsn"}, nil
	}

	if profileName != "" {
		conn, ok := cfg.Connection(profileName)
		if !ok {
			return target{}, fmt.Errorf("connection profile %q not found", profileName)
		}
		kind, err := kindForDriver(conn.Driver)
		if err != nil {
			return target{}, fmt.Errorf("connection profile %q: %w", profileName, err)
		}

		dsn := conn.DSN
		if kind == db.KindSQLite {
			parsedKind, parsedDSN, err := db.ParseDSN(dsn)
			if err != nil {
				return target{}, fmt.Errorf("connection profile %q: %w", profileName, err)
			}
			if parsedKind != db.KindSQLite {
				return target{}, fmt.Errorf(
					"connection profile %q: driver is sqlite but DSN is %s",
					profileName, parsedKind,
				)
			}
			dsn = parsedDSN
		}
		return target{kind: kind, dsn: dsn, source: "profile"}, nil
	}

	return target{}, errors.New(
		"no connection specified: pass a DSN, use --connection, or configure a profile " +
			"(the interactive picker is not implemented yet)",
	)
}

func kindForDriver(driver string) (db.Kind, error) {
	switch driver {
	case config.DriverPostgres:
		return db.KindPostgres, nil
	case config.DriverSQLite:
		return db.KindSQLite, nil
	default:
		return "", fmt.Errorf("unsupported driver %q", driver)
	}
}

func connectorFor(kind db.Kind) (db.Connector, error) {
	switch kind {
	case db.KindPostgres:
		return postgres.NewConnector(), nil
	case db.KindSQLite:
		return sqlite.NewConnector(), nil
	default:
		return nil, fmt.Errorf("unsupported database kind %q", kind)
	}
}

func runCheck(ctx context.Context, logger *slog.Logger, conn db.Conn, kind db.Kind, stdout, stderr io.Writer) int {
	pingCtx, cancel := context.WithTimeout(ctx, db.DefaultConnectTimeout)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		return fail(ctx, logger, stderr, exitError, err)
	}

	serverVersion, err := conn.ServerVersion(ctx)
	if err != nil {
		return fail(ctx, logger, stderr, exitError, err)
	}

	writef(stdout, "ok %s %s (read-only)\n", kind, serverVersion)
	logger.Info("check ok", "kind", kind, "version", serverVersion)
	return exitOK
}

func fail(ctx context.Context, logger *slog.Logger, stderr io.Writer, code int, err error) int {
	if errors.Is(ctx.Err(), context.Canceled) {
		writef(stderr, "dbt: interrupted\n")
		logger.Warn("interrupted")
		return exitInterrupt
	}

	message := dbtlog.Redact(err.Error())
	writef(stderr, "dbt: %s\n", message)
	logger.Error("command failed", "error", message)
	return code
}

func write(w io.Writer, text string) {
	_, _ = io.WriteString(w, text)
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
