package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	modernsqlite "modernc.org/sqlite"

	"github.com/rgmaz/dbt/internal/db"
)

// Connector opens read-only SQLite sessions.
type Connector struct{}

// NewConnector returns a SQLite connector.
func NewConnector() db.Connector { return Connector{} }

// Kind implements db.Connector.
func (Connector) Kind() db.Kind { return db.KindSQLite }

// Open implements db.Connector. It resolves the database path, constructs a
// read-only URI, opens the pure-Go driver, and verifies the connection.
func (Connector) Open(ctx context.Context, dsn string, opts db.Options) (db.Conn, error) {
	opts = opts.Normalize()
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("sqlite: empty DSN")
	}

	path, err := resolvePath(dsn)
	if err != nil {
		return nil, err
	}
	connDSN, err := readOnlyDSN(path)
	if err != nil {
		return nil, err
	}
	driverConnector, err := modernsqlite.NewConnector(connDSN)
	if err != nil {
		return nil, fmt.Errorf("sqlite: build connector: %w", err)
	}

	sqlDB := sql.OpenDB(driverConnector)
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(4)

	openCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()
	if err := sqlDB.PingContext(openCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}

	return &Conn{db: sqlDB, opts: opts}, nil
}

// Conn is a read-only SQLite session. It is safe for concurrent use because it
// delegates to database/sql and keeps its options immutable.
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
	if err := c.db.PingContext(ctx); err != nil {
		return fmt.Errorf("sqlite: ping: %w", err)
	}
	return nil
}

// ServerVersion implements db.Conn.
func (c *Conn) ServerVersion(ctx context.Context) (string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	var version string
	if err := c.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		return "", fmt.Errorf("sqlite: server version: %w", err)
	}
	return version, nil
}

// Close implements db.Conn.
func (c *Conn) Close() error {
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("sqlite: close: %w", err)
	}
	return nil
}

// Quote implements db.Conn. It always quotes identifiers and is safe for SQL.
func (c *Conn) Quote(parts ...string) string {
	quoted := make([]string, len(parts))
	for i, part := range parts {
		quoted[i] = quoteIdentifier(part)
	}
	return strings.Join(quoted, ".")
}

func quoteIdentifier(part string) string {
	return `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
}

func (c *Conn) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := c.opts.QueryTimeout
	if timeout <= 0 {
		timeout = db.DefaultQueryTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func resolvePath(path string) (string, error) {
	if path == ":memory:" {
		return path, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("sqlite: resolve path %q: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("sqlite: database file %q does not exist", abs)
		}
		return "", fmt.Errorf("sqlite: stat database file %q: %w", abs, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("sqlite: database path %q is a directory", abs)
	}
	return abs, nil
}

func readOnlyDSN(path string) (string, error) {
	if path == ":memory:" {
		return "file::memory:?cache=shared&_query_only=1", nil
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("sqlite: database path %q must be absolute", path)
	}

	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("_query_only", "1")
	q.Set("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
