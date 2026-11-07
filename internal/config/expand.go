package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces ${NAME} references using lookup. All unresolved names are
// reported together, and only names (never values) appear in the error.
func expandEnv(value string, lookup func(string) (string, bool)) (string, error) {
	var missing []string
	seen := make(map[string]struct{})
	expanded := envReference.ReplaceAllStringFunc(value, func(ref string) string {
		name := ref[2 : len(ref)-1]
		replacement, ok := lookup(name)
		if !ok {
			if _, exists := seen[name]; !exists {
				seen[name] = struct{}{}
				missing = append(missing, name)
			}
			return ref
		}
		return replacement
	})

	if len(missing) == 0 {
		return expanded, nil
	}

	quoted := make([]string, len(missing))
	for i, name := range missing {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	return "", fmt.Errorf("dsn references unset environment variable(s): %s", strings.Join(quoted, ", "))
}

// expandHome expands a leading "~" or "~/" in a filesystem path.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
