package log

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const (
	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

var (
	urlPasswordPattern   = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^\s/:@]*):([^\s/@]*)@`)
	keywordSecretPattern = regexp.MustCompile(
		`(?i)\b(pgpassword|password|sslpassword|passfile)\s*=\s*('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"|[^\s]+)`,
	)
	secretQueryKeys = []string{"password", "sslpassword", "passfile"}
)

// Discard returns a logger that drops all records.
func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// DefaultPath returns the default debug log path. XDG_STATE_HOME wins when set;
// otherwise Unix-like systems use ~/.local/state and Windows/macOS use the user
// cache directory.
func DefaultPath() (string, error) {
	if state := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); state != "" {
		return filepath.Join(state, "dbt", "dbt.log"), nil
	}

	switch runtime.GOOS {
	case "windows", "darwin":
		dir, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("log: resolve cache directory: %w", err)
		}
		return filepath.Join(dir, "dbt", "dbt.log"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("log: resolve home directory: %w", err)
		}
		return filepath.Join(home, ".local", "state", "dbt", "dbt.log"), nil
	}
}

// Open creates or appends to the log file at path. Parent directories are
// created with mode 0700 and the file is secured to 0600. Records are written
// as JSON at debug level.
func Open(path string) (*slog.Logger, io.Closer, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil, errors.New("log: empty log path")
	}

	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return nil, nil, fmt.Errorf("log: create directory for %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		return nil, nil, fmt.Errorf("log: open %s: %w", path, err)
	}
	if err := file.Chmod(fileMode); err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("log: secure %s: %w", path, err)
	}

	handler := slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler), file, nil
}

// RedactDSN returns raw with password material masked. PostgreSQL URLs have
// userinfo passwords and password-bearing query parameters replaced; keyword
// and value DSNs are handled by Redact. Non-secret input is returned unchanged.
func RedactDSN(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}

	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" {
		return Redact(raw)
	}

	changed := false
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
			changed = true
		}
	}
	query := u.Query()
	for _, key := range secretQueryKeys {
		if query.Has(key) {
			query.Set(key, "xxxxx")
			changed = true
		}
	}
	if !changed {
		return raw
	}

	u.RawQuery = query.Encode()
	return u.String()
}

// Redact masks recognizable URL and keyword/value passwords in arbitrary text.
// It is a best-effort defense-in-depth layer for error messages and logs.
func Redact(text string) string {
	text = urlPasswordPattern.ReplaceAllString(text, "$1:xxxxx@")
	text = keywordSecretPattern.ReplaceAllString(text, "$1=xxxxx")
	return text
}
