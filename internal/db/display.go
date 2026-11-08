package db

import (
	"path/filepath"
	"strings"

	"net/url"
)

// DSNLabel returns a compact, secret-safe label for a DSN. PostgreSQL URLs
// render as "user@database"; SQLite paths render as their base name.
func DSNLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		if strings.HasPrefix(strings.ToLower(u.Scheme), "postgres") {
			user := ""
			if u.User != nil {
				user = u.User.Username()
			}
			db := strings.TrimPrefix(u.Path, "/")
			if user != "" && db != "" {
				return user + "@" + db
			}
			if db != "" {
				return db
			}
			if user != "" {
				return user
			}
		}
	}
	base := filepath.Base(raw)
	if base == "." || base == "/" {
		return raw
	}
	return base
}
