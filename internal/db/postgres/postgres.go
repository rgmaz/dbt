package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/rgmaz/dbt/internal/db"
)

const (
	defaultSchema = "public"
	maxOpenConns  = 4
)

// Connector opens read-only PostgreSQL sessions.
type Connector struct{}

// NewConnector returns a PostgreSQL connector.
func NewConnector() db.Connector { return Connector{} }

// Kind implements db.Connector.
func (Connector) Kind() db.Kind { return db.KindPostgres }

// Open implements db.Connector. It parses the DSN, forces session-level
// read-only settings, opens the pgx database/sql adapter, and verifies the
// connection.
func (Connector) Open(ctx context.Context, dsn string, opts db.Options) (db.Conn, error) {
	opts = opts.Normalize()
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("postgres: empty DSN")
	}
	if err := validateDSN(dsn); err != nil {
		return nil, err
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DSN: %w", err)
	}
	applyOptions(cfg, opts)

	sqlDB := stdlib.OpenDB(*cfg, stdlib.OptionAfterConnect(enforceReadOnly))
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxOpenConns)

	openCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()
	if err := sqlDB.PingContext(openCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("postgres: connect %s: %w", displayTarget(cfg), err)
	}

	return &Conn{db: sqlDB, opts: opts}, nil
}

// Conn is a read-only PostgreSQL session. It is safe for concurrent use because
// it delegates to database/sql and keeps its options immutable.
type Conn struct {
	db   *sql.DB
	opts db.Options
}

var (
	_ db.Connector = Connector{}
	_ db.Conn      = (*Conn)(nil)
)

// Ping implements db.Conn.
func (c *Conn) Ping(ctx context.Context) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	if err := c.db.PingContext(ctx); err != nil {
		return fmt.Errorf("postgres: ping: %w", err)
	}
	return nil
}

// ServerVersion implements db.Conn.
func (c *Conn) ServerVersion(ctx context.Context) (string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	var version string
	if err := c.db.QueryRowContext(ctx, "SELECT current_setting('server_version')").Scan(&version); err != nil {
		return "", fmt.Errorf("postgres: server version: %w", err)
	}
	return version, nil
}

// Close implements db.Conn.
func (c *Conn) Close() error {
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("postgres: close: %w", err)
	}
	return nil
}

// Quote implements db.Conn. It always quotes identifiers and is safe for SQL.
func (c *Conn) Quote(parts ...string) string {
	return pgx.Identifier(parts).Sanitize()
}

func (c *Conn) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return db.WithQueryTimeout(ctx, c.opts.QueryTimeout)
}

func validateDSN(dsn string) error {
	if i := strings.Index(dsn, "://"); i > 0 {
		scheme := strings.ToLower(dsn[:i])
		if scheme != "postgres" && scheme != "postgresql" {
			return fmt.Errorf("postgres: unsupported DSN scheme %q", scheme)
		}
	}
	return nil
}

func applyOptions(cfg *pgx.ConnConfig, opts db.Options) {
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = make(map[string]string)
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.RuntimeParams["application_name"] = opts.AppName

	timeoutMS := opts.QueryTimeout.Milliseconds()
	if timeoutMS < 1 {
		timeoutMS = 1
	}
	cfg.RuntimeParams["statement_timeout"] = strconv.FormatInt(timeoutMS, 10)
}

func enforceReadOnly(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
		return fmt.Errorf("postgres: enforce read-only: %w", err)
	}
	return nil
}

func displayTarget(cfg *pgx.ConnConfig) string {
	host := cfg.Host
	if host == "" {
		host = "local"
	}
	if cfg.Database == "" {
		return host
	}
	return host + "/" + cfg.Database
}
