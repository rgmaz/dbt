package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type captureConnector struct {
	kind Kind
	dsn  string
	opts Options
	conn Conn
	err  error
}

func (c *captureConnector) Kind() Kind {
	return c.kind
}

func (c *captureConnector) Open(_ context.Context, dsn string, opts Options) (Conn, error) {
	c.dsn = dsn
	c.opts = opts
	return c.conn, c.err
}

func TestOptionsNormalize(t *testing.T) {
	got := Options{}.Normalize()
	if got.ConnectTimeout != DefaultConnectTimeout {
		t.Errorf("ConnectTimeout = %s, want %s", got.ConnectTimeout, DefaultConnectTimeout)
	}
	if got.QueryTimeout != DefaultQueryTimeout {
		t.Errorf("QueryTimeout = %s, want %s", got.QueryTimeout, DefaultQueryTimeout)
	}
	if got.MaxRows != DefaultMaxRows {
		t.Errorf("MaxRows = %d, want %d", got.MaxRows, DefaultMaxRows)
	}
	if got.AppName != DefaultAppName {
		t.Errorf("AppName = %q, want %q", got.AppName, DefaultAppName)
	}
}

func TestOptionsNormalizePreservesNegativeValues(t *testing.T) {
	got := Options{
		ConnectTimeout: -time.Second,
		QueryTimeout:   -time.Second,
		MaxRows:        -1,
	}.Normalize()
	if got.ConnectTimeout != -time.Second || got.QueryTimeout != -time.Second || got.MaxRows != -1 {
		t.Errorf("Normalize() corrected negative values: %+v", got)
	}
}

func TestOptionsValidate(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "read-only required",
			opts: Options{},
			want: "read-only mode is required",
		},
		{
			name: "negative connect timeout",
			opts: Options{ReadOnly: true, ConnectTimeout: -time.Second},
			want: "connect timeout",
		},
		{
			name: "negative query timeout",
			opts: Options{ReadOnly: true, QueryTimeout: -time.Second},
			want: "query timeout",
		},
		{
			name: "negative max rows",
			opts: Options{ReadOnly: true, MaxRows: -1},
			want: "max rows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() error = %q, want substring %q", err, tt.want)
			}
		})
	}

	if err := (Options{ReadOnly: true}).Validate(); err != nil {
		t.Errorf("Validate() with defaults error = %v", err)
	}
}

func TestOpenAppliesDefaults(t *testing.T) {
	connector := &captureConnector{kind: KindPostgres}

	conn, err := Open(context.Background(), connector, "postgres://localhost/app", Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if conn != nil {
		t.Errorf("Open() conn = %v, want nil from capture connector", conn)
	}
	if connector.dsn != "postgres://localhost/app" {
		t.Errorf("connector dsn = %q", connector.dsn)
	}
	if connector.opts.ConnectTimeout != DefaultConnectTimeout {
		t.Errorf("ConnectTimeout = %s, want %s", connector.opts.ConnectTimeout, DefaultConnectTimeout)
	}
	if connector.opts.QueryTimeout != DefaultQueryTimeout {
		t.Errorf("QueryTimeout = %s, want %s", connector.opts.QueryTimeout, DefaultQueryTimeout)
	}
	if connector.opts.MaxRows != DefaultMaxRows {
		t.Errorf("MaxRows = %d, want %d", connector.opts.MaxRows, DefaultMaxRows)
	}
	if connector.opts.AppName != DefaultAppName {
		t.Errorf("AppName = %q, want %q", connector.opts.AppName, DefaultAppName)
	}
}

func TestOpenRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()

	if _, err := Open(ctx, nil, "postgres://localhost/app", Options{ReadOnly: true}); err == nil {
		t.Error("Open(nil connector) error = nil, want error")
	}
	if _, err := Open(ctx, &captureConnector{}, "", Options{ReadOnly: true}); err == nil {
		t.Error("Open(empty DSN) error = nil, want error")
	}
	if _, err := Open(ctx, &captureConnector{}, "postgres://localhost/app", Options{}); err == nil {
		t.Error("Open(read-write options) error = nil, want error")
	}
}

func TestOpenWrapsConnectorError(t *testing.T) {
	sentinel := errors.New("connection refused")
	connector := &captureConnector{kind: KindSQLite, err: sentinel}

	_, err := Open(context.Background(), connector, "/tmp/app.db", Options{ReadOnly: true})
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Open() error = %v, want wrapped sentinel", err)
	}
	if !strings.Contains(err.Error(), "db: open sqlite") {
		t.Errorf("Open() error = %q, want driver context", err)
	}
}
