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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/rgmaz/dbt/internal/config"
	"github.com/rgmaz/dbt/internal/db"
	"github.com/rgmaz/dbt/internal/db/postgres"
	"github.com/rgmaz/dbt/internal/db/sqlite"
	dbtlog "github.com/rgmaz/dbt/internal/log"
	"github.com/rgmaz/dbt/internal/tui/app"
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

Precedence: positional dsn > --connection > interactive picker (TTY).

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

var errNoTarget = errors.New("no connection specified")

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
	switch {
	case errors.Is(err, errNoTarget):
		return runWithoutTarget(ctx, logger, opts, cfg, stdin, stdout, stderr)
	case err != nil:
		return fail(ctx, logger, stderr, exitUsage, err)
	}

	logger.Info("connection",
		"source", resolved.source,
		"kind", resolved.kind,
		"target", dbtlog.RedactDSN(resolved.dsn),
	)

	if opts.check {
		conn, _, err := openTarget(ctx, cfg, resolved)
		if err != nil {
			return fail(ctx, logger, stderr, exitError, err)
		}
		defer func() { _ = conn.Close() }()
		return runCheck(ctx, logger, conn, resolved.kind, stdout, stderr)
	}

	if !isTerminal(stdin) || !isTerminal(stdout) {
		writef(stderr, "dbt: interactive mode requires a terminal; use --check for non-interactive use\n")
		logger.Warn("interactive mode requires a terminal")
		return exitError
	}

	conn, serverVersion, err := openTarget(ctx, cfg, resolved)
	if err != nil {
		return fail(ctx, logger, stderr, exitError, err)
	}
	defer func() { _ = conn.Close() }()

	return runBrowser(ctx, logger, opts, cfg, resolved, conn, serverVersion, stdin, stdout, stderr)
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
		return targetForProfile(conn)
	}

	return target{}, errNoTarget
}

func targetForProfile(conn config.Connection) (target, error) {
	kind, err := kindForDriver(conn.Driver)
	if err != nil {
		return target{}, fmt.Errorf("connection profile %q: %w", conn.Name, err)
	}

	dsn := conn.DSN
	if kind == db.KindSQLite {
		parsedKind, parsedDSN, err := db.ParseDSN(dsn)
		if err != nil {
			return target{}, fmt.Errorf("connection profile %q: %w", conn.Name, err)
		}
		if parsedKind != db.KindSQLite {
			return target{}, fmt.Errorf(
				"connection profile %q: driver is sqlite but DSN is %s",
				conn.Name, parsedKind,
			)
		}
		dsn = parsedDSN
	}

	return target{kind: kind, dsn: dsn, source: "profile"}, nil
}

func profilesFromConfig(cfg *config.Config) ([]app.Profile, error) {
	profiles := make([]app.Profile, 0, len(cfg.Connections))
	for _, conn := range cfg.Connections {
		resolved, err := targetForProfile(conn)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, app.Profile{
			Name:   conn.Name,
			Driver: conn.Driver,
			Kind:   resolved.kind,
			Detail: dbtlog.RedactDSN(conn.DSN),
			DSN:    resolved.dsn,
		})
	}
	return profiles, nil
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

func runWithoutTarget(
	ctx context.Context,
	logger *slog.Logger,
	opts options,
	cfg *config.Config,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	if opts.check {
		return fail(ctx, logger, stderr, exitUsage,
			errors.New("no connection specified: --check requires a DSN or --connection"))
	}
	if !isTerminal(stdin) || !isTerminal(stdout) {
		return fail(ctx, logger, stderr, exitUsage,
			errors.New("no connection specified: pass a DSN, use --connection, or run in a terminal to use the picker"))
	}
	return runPicker(ctx, logger, cfg, stdin, stdout, stderr)
}

func openTarget(ctx context.Context, cfg *config.Config, resolved target) (db.Conn, string, error) {
	connector, err := connectorFor(resolved.kind)
	if err != nil {
		return nil, "", err
	}

	conn, err := db.Open(ctx, connector, resolved.dsn, db.Options{
		ReadOnly:     true,
		QueryTimeout: cfg.QueryTimeout,
	})
	if err != nil {
		return nil, "", err
	}

	versionCtx, cancel := context.WithTimeout(ctx, db.DefaultConnectTimeout)
	version, err := conn.ServerVersion(versionCtx)
	cancel()
	if err != nil {
		_ = conn.Close()
		return nil, "", err
	}
	return conn, version, nil
}

func runBrowser(
	ctx context.Context,
	logger *slog.Logger,
	opts options,
	cfg *config.Config,
	resolved target,
	conn db.Conn,
	serverVersion string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	model := app.New(app.Options{
		Context:      ctx,
		Connection:   connectionLabel(opts, resolved),
		Kind:         resolved.kind,
		Version:      serverVersion,
		ReadOnly:     true,
		Theme:        cfg.Theme,
		Conn:         conn,
		QueryTimeout: cfg.QueryTimeout,
	})

	logger.Info("tui start", "mode", "browser", "kind", resolved.kind, "version", serverVersion)
	return runProgram(ctx, logger, model, stdin, stdout, stderr)
}

func runPicker(
	ctx context.Context,
	logger *slog.Logger,
	cfg *config.Config,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	profiles, err := profilesFromConfig(cfg)
	if err != nil {
		return fail(ctx, logger, stderr, exitUsage, err)
	}

	open := func(openCtx context.Context, profile app.Profile) (app.ConnectResult, error) {
		logger.Info("connect", "profile", profile.Name, "kind", profile.Kind)
		conn, version, err := openTarget(openCtx, cfg, target{
			kind:   profile.Kind,
			dsn:    profile.DSN,
			source: "profile",
		})
		if err != nil {
			return app.ConnectResult{}, err
		}
		return app.ConnectResult{
			Conn:    conn,
			Kind:    profile.Kind,
			Version: version,
			Label:   profile.Name,
		}, nil
	}

	model := app.New(app.Options{
		Context:      ctx,
		Picker:       true,
		Profiles:     profiles,
		ConfigPath:   cfg.Path(),
		Open:         open,
		Theme:        cfg.Theme,
		QueryTimeout: cfg.QueryTimeout,
	})

	logger.Info("tui start", "mode", "picker", "profiles", len(profiles), "config", cfg.Path())
	return runProgram(ctx, logger, model, stdin, stdout, stderr)
}

func runProgram(
	ctx context.Context,
	logger *slog.Logger,
	model app.Model,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	program := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithAltScreen(),
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
	)

	final, err := program.Run()
	if closer, ok := final.(interface{ Close() error }); ok {
		if closeErr := closer.Close(); closeErr != nil {
			logger.Warn("close session", "error", dbtlog.Redact(closeErr.Error()))
		}
	}

	if err != nil {
		if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
			writef(stderr, "dbt: interrupted\n")
			logger.Warn("interrupted")
			return exitInterrupt
		}
		return fail(ctx, logger, stderr, exitError, err)
	}
	if ctx.Err() != nil {
		writef(stderr, "dbt: interrupted\n")
		logger.Warn("interrupted")
		return exitInterrupt
	}

	logger.Info("tui exit")
	return exitOK
}

func connectionLabel(opts options, resolved target) string {
	if resolved.source == "profile" && strings.TrimSpace(opts.connection) != "" {
		return opts.connection
	}
	return db.DSNLabel(resolved.dsn)
}

func isTerminal(v any) bool {
	file, ok := v.(*os.File)
	if !ok {
		return false
	}
	fd := file.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
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
