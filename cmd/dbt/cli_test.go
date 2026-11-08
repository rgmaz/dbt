package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionAndHelp(t *testing.T) {
	oldVersion := version
	version = "test-version"
	t.Cleanup(func() { version = oldVersion })

	code, stdout, stderr := runCLI(t, context.Background(), "--version")
	if code != exitOK {
		t.Fatalf("--version exit code = %d, want %d", code, exitOK)
	}
	if stdout != "dbt test-version\n" {
		t.Errorf("--version stdout = %q, want %q", stdout, "dbt test-version\n")
	}
	if stderr != "" {
		t.Errorf("--version stderr = %q, want empty", stderr)
	}

	code, stdout, stderr = runCLI(t, context.Background(), "--help")
	if code != exitOK {
		t.Fatalf("--help exit code = %d, want %d", code, exitOK)
	}
	for _, want := range []string{"Usage:", "--check", "--connection", "Exit codes:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("--help stdout missing %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("--help stderr = %q, want empty", stderr)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "unknown flag",
			args: []string{"--nope"},
			want: "flag provided but not defined",
		},
		{
			name: "too many arguments",
			args: []string{"a.db", "b.db"},
			want: "expected at most one DSN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, context.Background(), tt.args...)
			if code != exitUsage {
				t.Fatalf("exit code = %d, want %d", code, exitUsage)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
			}
		})
	}
}

func TestCheckSQLiteMemory(t *testing.T) {
	code, stdout, stderr := runCLI(t, context.Background(), "--check", ":memory:")
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr)
	}
	if !strings.HasPrefix(stdout, "ok sqlite ") || !strings.HasSuffix(stdout, " (read-only)\n") {
		t.Errorf("stdout = %q, want ok sqlite <version> (read-only)", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestCheckMissingSQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")

	code, stdout, stderr := runCLI(t, context.Background(), "--check", path)
	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "does not exist") {
		t.Errorf("stderr = %q, want missing-file error", stderr)
	}
}

func TestUnsupportedScheme(t *testing.T) {
	code, stdout, stderr := runCLI(t, context.Background(), "--check", "mysql://user@host/db")
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "unsupported driver scheme") {
		t.Errorf("stderr = %q, want unsupported-scheme error", stderr)
	}
}

func TestConnectionProfile(t *testing.T) {
	configPath := writeConfig(t, `
[[connection]]
name = "mem"
driver = "sqlite"
dsn = ":memory:"
`)

	code, stdout, stderr := runCLI(t, context.Background(),
		"--config", configPath, "--connection", "mem", "--check")
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr)
	}
	if !strings.HasPrefix(stdout, "ok sqlite ") {
		t.Errorf("stdout = %q, want ok sqlite ...", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestConnectionProfileAcceptsSQLitePrefix(t *testing.T) {
	configPath := writeConfig(t, `
[[connection]]
name = "mem"
driver = "sqlite"
dsn = "sqlite::memory:"
`)

	code, stdout, stderr := runCLI(t, context.Background(),
		"--config", configPath, "--connection", "mem", "--check")
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr)
	}
	if !strings.HasPrefix(stdout, "ok sqlite ") {
		t.Errorf("stdout = %q, want ok sqlite ...", stdout)
	}
}

func TestPositionalOverridesProfile(t *testing.T) {
	configPath := writeConfig(t, `
[[connection]]
name = "broken"
driver = "sqlite"
dsn = "/nonexistent/never.db"
`)

	code, stdout, stderr := runCLI(t, context.Background(),
		"--config", configPath, "--connection", "broken", "--check", ":memory:")
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr)
	}
	if !strings.HasPrefix(stdout, "ok sqlite ") {
		t.Errorf("stdout = %q, want ok sqlite ...", stdout)
	}
	if !strings.Contains(stderr, "ignoring --connection") {
		t.Errorf("stderr = %q, want ignored-profile warning", stderr)
	}
}

func TestMissingProfile(t *testing.T) {
	configPath := writeConfig(t, "")

	code, stdout, stderr := runCLI(t, context.Background(),
		"--config", configPath, "--connection", "nope", "--check")
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `connection profile "nope" not found`) {
		t.Errorf("stderr = %q, want missing-profile error", stderr)
	}
}

func TestNoConnection(t *testing.T) {
	code, stdout, stderr := runCLI(t, context.Background(), "--check")
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "no connection specified") {
		t.Errorf("stderr = %q, want no-connection error", stderr)
	}
}

func TestExplicitConfigMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")

	code, stdout, stderr := runCLI(t, context.Background(),
		"--config", path, "--check", ":memory:")
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "config") {
		t.Errorf("stderr = %q, want config error", stderr)
	}
}

func TestInvalidConfig(t *testing.T) {
	configPath := writeConfig(t, "page_size = 1\n")

	code, stdout, stderr := runCLI(t, context.Background(),
		"--config", configPath, "--check", ":memory:")
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "page_size") {
		t.Errorf("stderr = %q, want page_size validation error", stderr)
	}
}

func TestConfigWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission warning is not emitted on Windows")
	}

	configPath := writeConfig(t, "")
	if err := os.Chmod(configPath, 0o644); err != nil {
		t.Fatalf("chmod config: %v", err)
	}

	code, stdout, stderr := runCLI(t, context.Background(),
		"--config", configPath, "--check", ":memory:")
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr)
	}
	if !strings.HasPrefix(stdout, "ok sqlite ") {
		t.Errorf("stdout = %q, want ok sqlite ...", stdout)
	}
	if !strings.Contains(stderr, "readable by group or others") {
		t.Errorf("stderr = %q, want permission warning", stderr)
	}
}

func TestInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	code, stdout, stderr := runCLI(t, ctx, "--check", ":memory:")
	if code != exitInterrupt {
		t.Fatalf("exit code = %d, want %d", code, exitInterrupt)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "interrupted") {
		t.Errorf("stderr = %q, want interrupted message", stderr)
	}
}

func TestInteractiveRequiresTerminal(t *testing.T) {
	code, stdout, stderr := runCLI(t, context.Background(), ":memory:")
	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "requires a terminal") {
		t.Errorf("stderr = %q, want terminal guidance", stderr)
	}
}

func TestConnectionLabel(t *testing.T) {
	if got := connectionLabel(
		options{connection: "prod"},
		target{source: "profile", dsn: "postgres://user:secret@host/db"},
	); got != "prod" {
		t.Errorf("profile label = %q, want prod", got)
	}

	got := connectionLabel(
		options{},
		target{source: "dsn", dsn: "postgres://user:secret@host/db?password=hidden"},
	)
	for _, secret := range []string{"secret", "hidden"} {
		if strings.Contains(got, secret) {
			t.Errorf("dsn label leaked %q: %s", secret, got)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	if isTerminal(nil) {
		t.Error("nil is not a terminal")
	}
	if isTerminal(&bytes.Buffer{}) {
		t.Error("buffer is not a terminal")
	}
}

func TestDebugLog(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	code, stdout, stderr := runCLI(t, context.Background(), "--debug", "--check", ":memory:")
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr)
	}
	if !strings.HasPrefix(stdout, "ok sqlite ") {
		t.Errorf("stdout = %q, want ok sqlite ...", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	logPath := filepath.Join(stateDir, "dbt", "dbt.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	text := string(data)
	for _, want := range []string{`"msg":"check ok"`, `"kind":"sqlite"`} {
		if !strings.Contains(text, want) {
			t.Errorf("debug log missing %q:\n%s", want, text)
		}
	}
}

func TestCheckPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("DBT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set DBT_TEST_POSTGRES_DSN to run the PostgreSQL CLI integration test")
	}

	code, stdout, stderr := runCLI(t, context.Background(), "--check", dsn)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr = %q", code, exitOK, stderr)
	}
	if !strings.HasPrefix(stdout, "ok postgres ") || !strings.HasSuffix(stdout, " (read-only)\n") {
		t.Errorf("stdout = %q, want ok postgres <version> (read-only)", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func runCLI(t *testing.T, ctx context.Context, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("DBT_CONFIG", filepath.Join(t.TempDir(), "config.toml"))

	var stdout, stderr bytes.Buffer
	code := run(ctx, args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
