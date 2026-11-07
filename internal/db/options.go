package db

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Option defaults.
const (
	DefaultConnectTimeout = 10 * time.Second
	DefaultQueryTimeout   = 30 * time.Second
	DefaultMaxRows        = 2000
	DefaultAppName        = "dbt"
)

// Options configures a connection. ReadOnly must be true in this version.
type Options struct {
	ReadOnly       bool
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration
	MaxRows        int
	AppName        string
}

// Normalize applies defaults to unset fields. Negative values are preserved so
// that Validate can reject them instead of silently correcting caller bugs.
func (o Options) Normalize() Options {
	if o.ConnectTimeout == 0 {
		o.ConnectTimeout = DefaultConnectTimeout
	}
	if o.QueryTimeout == 0 {
		o.QueryTimeout = DefaultQueryTimeout
	}
	if o.MaxRows == 0 {
		o.MaxRows = DefaultMaxRows
	}
	if o.AppName == "" {
		o.AppName = DefaultAppName
	}
	return o
}

// Validate rejects unsupported or invalid options.
func (o Options) Validate() error {
	if !o.ReadOnly {
		return errors.New("db: read-only mode is required in this version")
	}
	if o.ConnectTimeout < 0 {
		return fmt.Errorf("db: connect timeout must not be negative, got %s", o.ConnectTimeout)
	}
	if o.QueryTimeout < 0 {
		return fmt.Errorf("db: query timeout must not be negative, got %s", o.QueryTimeout)
	}
	if o.MaxRows < 0 {
		return fmt.Errorf("db: max rows must not be negative, got %d", o.MaxRows)
	}
	return nil
}

// Open normalizes options, validates the read-only guarantee, and delegates to
// the connector. Callers should prefer Open over Connector.Open directly so
// defaults and safety checks are always applied.
func Open(ctx context.Context, connector Connector, dsn string, opts Options) (Conn, error) {
	if connector == nil {
		return nil, errors.New("db: nil connector")
	}
	if dsn == "" {
		return nil, errors.New("db: empty DSN")
	}

	opts = opts.Normalize()
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	conn, err := connector.Open(ctx, dsn, opts)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", connector.Kind(), err)
	}
	return conn, nil
}
