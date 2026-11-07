package db

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ParseDSN classifies a command-line DSN and returns the value a driver should
// use. PostgreSQL URLs are passed through unchanged. SQLite DSNs may use a
// "sqlite:" prefix and are reduced to a filesystem path or ":memory:".
func ParseDSN(raw string) (Kind, string, error) {
	dsn := strings.TrimSpace(raw)
	if dsn == "" {
		return "", "", errors.New("db: empty DSN")
	}

	switch scheme := dsnScheme(dsn); scheme {
	case "postgres", "postgresql":
		return KindPostgres, dsn, nil
	case "sqlite":
		path, err := sqlitePath(dsn)
		if err != nil {
			return "", "", err
		}
		return KindSQLite, path, nil
	case "":
		return KindSQLite, dsn, nil
	default:
		return "", "", fmt.Errorf("db: unsupported driver scheme %q", scheme)
	}
}

// dsnScheme extracts a URL scheme, returning "" for plain paths. Single-letter
// schemes are treated as Windows drive letters so "C:\data\app.db" remains a
// path on every platform.
func dsnScheme(dsn string) string {
	colon := strings.IndexByte(dsn, ':')
	if colon <= 0 {
		return ""
	}

	scheme := dsn[:colon]
	if !isASCIILetter(scheme[0]) {
		return ""
	}
	for i := 1; i < len(scheme); i++ {
		if !isSchemeChar(scheme[i]) {
			return ""
		}
	}
	if len(scheme) == 1 {
		return ""
	}
	return strings.ToLower(scheme)
}

func isASCIILetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isSchemeChar(c byte) bool {
	return isASCIILetter(c) || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'
}

func sqlitePath(dsn string) (string, error) {
	rest := dsn[strings.IndexByte(dsn, ':')+1:]
	switch {
	case rest == "":
		return "", errors.New("db: sqlite DSN has no path")
	case strings.HasPrefix(rest, "?") || strings.HasPrefix(rest, "#"):
		return "", errors.New("db: sqlite DSN has no path")
	case strings.HasPrefix(rest, "//"):
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", errors.New("db: invalid sqlite DSN")
		}
		if parsed.Host != "" {
			return "", errors.New("db: sqlite DSN must not specify a host")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", errors.New("db: sqlite DSN must not contain a query or fragment")
		}
		if parsed.Path == "" {
			return "", errors.New("db: sqlite DSN has no path")
		}
		return parsed.Path, nil
	case strings.ContainsAny(rest, "?#"):
		return "", errors.New("db: sqlite DSN must not contain a query or fragment")
	default:
		return rest, nil
	}
}
