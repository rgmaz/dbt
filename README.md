# dbt

A keyboard-first, **read-only** terminal database viewer for SQLite and PostgreSQL, written in Go.

`dbt` opens one database — a local file or a server — and lets you explore schemas, browse and sort
table data page by page, inspect structure (columns, indexes, foreign keys, DDL), and run ad-hoc SQL.
It runs entirely in your terminal: no browser, no background server, no write access.

> Status: MVP complete. Writes, imports/exports, mouse support, and profile management are
> intentionally out of scope for this version.

## Features

- **Schema explorer** — schemas, tables, and views in a collapsible tree with live filtering and
  refresh.
- **Data grid** — paged browsing (`LIMIT`/`OFFSET`) with sortable columns, NULL rendering, horizontal
  scrolling, and per-page row counts.
- **Structure pane** — columns, indexes, foreign keys, and DDL (SQLite) for the selected relation.
- **SQL editor** — run ad-hoc read-only queries, with in-session history and the same result grid.
- **Clipboard** — copy a cell or a whole row.
- **Safe by default** — every session is enforced read-only at the driver level (see
  [Read-only guarantees](#read-only-guarantees)).
- **Responsive TUI** — works at 80×24 and up, survives terminal resizes, and lets you resize the
  sidebar.
- **Single static binary** — pure Go, `CGO_ENABLED=0`, no runtime dependencies or CDNs.

## Requirements

- **To run a release binary:** nothing beyond a terminal. The binary is static and cgo-free.
- **To build:** Go 1.26 or newer.
- **Database:** a SQLite file or a reachable PostgreSQL server.

## Install and build

```sh
# Build bin/dbt with version info from git describe
make build

# Or build directly
go build -trimpath -o dbt ./cmd/dbt
```

Static, cgo-free build:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dbt ./cmd/dbt
```

Cross-compilation is straightforward because there is no cgo:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dbt.exe ./cmd/dbt
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o dbt     ./cmd/dbt
```

`make build` injects the version through `-X main.version=$(git describe --tags --always --dirty)`.
`dbt --version` prints it.

## Quick start

```sh
# SQLite file (plain path or sqlite: prefix)
dbt ./app.db
dbt sqlite:/home/me/data/app.db

# PostgreSQL URL
dbt 'postgres://app@localhost:5432/app?sslmode=disable'

# A named profile from config.toml
dbt --connection local

# Headless connectivity check (no TUI)
dbt --check ./app.db
dbt --check 'postgres://app@localhost:5432/app?sslmode=disable'
```

Running `dbt` with no DSN in a terminal opens the **connection picker** with the profiles from your
config file. Outside a terminal, a target is required.

### Headless check

```sh
$ dbt --check ./app.db
ok sqlite 3.x.y (read-only)

$ dbt --check 'postgres://app@localhost:5432/app?sslmode=disable'
ok postgres PostgreSQL 16.4 (read-only)
```

`--check` connects, prints the server version, and exits without starting the TUI. It exits non-zero
on any connection or configuration failure, so it is safe to use from scripts and health probes.

## CLI reference

```
dbt [flags] [dsn]
```

| Flag | Description |
| --- | --- |
| `--config string` | Config file path. Defaults to `$DBT_CONFIG`, then `$XDG_CONFIG_HOME/dbt/config.toml`, then the platform user config directory. |
| `--connection string` | Named profile from the config file. |
| `--check` | Connect, print the server version, and exit (no TUI). |
| `--debug` | Write debug logs to the default log path. |
| `--version` | Print the version and exit. |
| `--help`, `-h` | Show usage and exit. |

**Target precedence:** positional DSN → `--connection` → interactive picker (TTY only) → usage
error. If both a positional DSN and `--connection` are supplied, the DSN wins and a warning is
printed.

**DSN formats**

- PostgreSQL: `postgres://…` or `postgresql://…` (passed through to `pgx`).
- SQLite: a filesystem path (`./app.db`, `/data/app.db`, `~/app.db`) or a `sqlite:` prefix.

**Exit codes**

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | Runtime or database error. |
| `2` | Usage or configuration error. |
| `130` | Interrupted (`Ctrl+C`, `SIGTERM`). |

If stdout or stdin is not a terminal, interactive mode refuses to start and suggests `--check`.

## Configuration

The configuration file is optional. A missing file means defaults and no profiles.

### Resolution order

1. `--config <path>` (must exist, otherwise exit code 2)
2. `$DBT_CONFIG`
3. `$XDG_CONFIG_HOME/dbt/config.toml`
4. The platform user config directory (`os.UserConfigDir()/dbt/config.toml`)

### Example

A copy of this example is available as [`config.example.toml`](config.example.toml):

```toml
# Number of rows fetched per page. Range: 10–2000.
page_size = 100

# Per-query deadline. Go duration syntax.
query_timeout = "30s"

# Reserved; unknown names fall back to the default theme.
theme = "default"

# SQLite profile. `~` is expanded, and ${VAR} references are interpolated.
[[connection]]
name = "notes"
driver = "sqlite"
dsn = "~/data/notes.db"
default_schema = "main"   # optional; defaults to "main"

# PostgreSQL profile. Prefer .pgpass or PGPASSWORD over inline passwords.
[[connection]]
name = "local"
driver = "postgres"
dsn = "postgres://app@localhost:5432/app?sslmode=disable"
default_schema = "public" # optional; defaults to "public"
```

### Validation rules

- Unknown keys are rejected (typos fail loudly).
- `page_size` must be between 10 and 2000.
- `query_timeout` must be a positive Go duration.
- Each connection requires a unique `name`, a `driver` (`sqlite` or `postgres`), and a `dsn`.
- `${VAR}` references in `dsn` are expanded at load time. Unset variables are reported **by name**
  and never by value.
- `read_only` may be omitted or set to `true`. Setting it to `false` is a configuration error,
  because writes are not supported in this version.
- A config file readable by group or others produces a warning suggesting `chmod 600`.

### Secrets

`dbt` never prompts for a password and never writes secrets. Prefer, in order:

1. PostgreSQL's `.pgpass` file (used automatically by `pgx`).
2. The `PGPASSWORD` environment variable.
3. A `${PGPASSWORD}` reference inside the DSN, expanded at load time.

Passwords are redacted from debug logs, error messages, and the TUI header. The header shows a
compact `user@database` label for PostgreSQL DSNs, the connection profile name when one is used,
or the SQLite file name — never the full DSN.

## Keymaps

Press `?` at any time for the help overlay.

### Global

| Key | Action |
| --- | --- |
| `?` | Help |
| `q` | Quit |
| `ctrl+c` | Force quit |
| `esc` | Close modal / leave filter or edit mode |
| `tab` | Toggle focus between sidebar and main pane |
| `1` / `2` / `3` | Data / Structure / SQL tab |

### Schema explorer (sidebar)

| Key | Action |
| --- | --- |
| `k` / `j` | Move up / down |
| `h` / `l` | Collapse / expand |
| `g` / `G` | Top / bottom |
| `/` | Filter (type to narrow; `esc` clears) |
| `enter` | Open selected relation |
| `r` | Refresh catalog |
| `+` / `-` | Widen / narrow the sidebar |

### Data grid

| Key | Action |
| --- | --- |
| `k` / `j` | Move up / down |
| `h` / `l` | Scroll left / right |
| `ctrl+d` / `ctrl+u` | Half page down / up |
| `n` / `p` | Next / previous page |
| `s` | Sort by the current column |
| `enter` | Inspect the selected cell |
| `y` / `Y` | Copy cell / row |
| `r` | Reload the current page |

### Structure pane

| Key | Action |
| --- | --- |
| `k` / `j` | Scroll line |
| `ctrl+d` / `ctrl+u` | Page down / up |
| `g` / `G` | Top / bottom |

### SQL editor

| Key | Action |
| --- | --- |
| `i` | Enter insert mode |
| `esc` | Return to normal mode |
| `ctrl+j` | Execute the query |
| `ctrl+r` | Cycle session history |
| `ctrl+l` | Clear the editor |

Queries are bounded by `query_timeout`. Results open in the same data grid and are limited to 2000
rows per execution.

## Read-only guarantees

Read-only mode is enforced before the TUI starts, not by UI convention:

- The connection layer rejects `ReadOnly=false` outright; there is no configuration or flag that
  enables writes.
- **SQLite** sessions are opened with `mode=ro` plus `_query_only=1` and a `busy_timeout`, using the
  pure-Go `modernc.org/sqlite` driver.
- **PostgreSQL** sessions inject `default_transaction_read_only=on` into pgx runtime parameters and
  reassert `SET default_transaction_read_only = on` after every connect.
- Any statement that attempts a write (`INSERT`, `UPDATE`, `DELETE`, DDL, `SELECT … FOR UPDATE`)
  fails with the database's read-only error. The SQL editor does not bypass this.

Catalog introspection uses parameterized `pg_catalog` queries (PostgreSQL) and pragma table-valued
functions (SQLite); identifiers are quoted, never string-interpolated from user input.

### SQLite WAL caveat

A database in WAL mode needs shared-memory (`-shm`) coordination. Opening one **read-only** can fail
if neither an existing `-shm` file nor a writable directory is available, typically surfacing as an
open error such as `unable to open database file`.

Mitigations:

- Make the database directory writable so SQLite can create `-shm` (the database itself remains
  read-only).
- Checkpoint and truncate the WAL with a writer before opening:
  `sqlite3 app.db 'PRAGMA wal_checkpoint(TRUNCATE);'`
- Or switch the database to rollback journal mode with a writer.

`--check` produces the same error text, which makes this easy to diagnose before launching the TUI.

## Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| `interactive mode requires a terminal; use --check` | stdin/stdout is a pipe or file. Run in a real terminal, or use `--check`. |
| `no connection specified` | Pass a DSN, use `--connection`, or run in a terminal to get the picker. |
| `connection profile "x" not found` | The name must match a `[[connection]]` entry exactly. |
| `invalid config …` with a list of problems | Fix the reported keys; unknown keys are rejected on purpose. |
| Warning about config permissions | Run `chmod 600 <config>`; secrets in a world-readable file are unsafe. |
| Picker says “No connections configured” | Add at least one `[[connection]]` to the config file shown on screen. |
| Exit code `130` | The process received `Ctrl+C` or `SIGTERM`; in-flight queries are cancelled. |
| SQLite `does not exist` | Plain paths are opened read-only and are never created. Point at an existing file. |

### Debug logs

`--debug` writes JSON logs to:

- `$XDG_STATE_HOME/dbt/dbt.log`, if set;
- `~/.local/state/dbt/dbt.log` on Linux/BSD;
- the user cache directory (`…/dbt/dbt.log`) on macOS and Windows.

Log files are created with mode `0600`, and password material is redacted.

## Development

```sh
make check          # gofmt check, go vet, tests
make test-race      # go test -race ./...
make build          # build bin/dbt
make run            # go run ./cmd/dbt
make offline-check  # verify a warm module cache is sufficient (GOPROXY=off)
make tidy           # go mod tidy
make clean          # remove bin/
```

Linting (not part of `make check`):

```sh
golangci-lint run ./...
```

PostgreSQL integration tests are opt-in and skipped when the DSN is unset:

```sh
DBT_TEST_POSTGRES_DSN='postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable' \
  go test -race -count=1 ./...
```

Golden snapshots live under `internal/tui/*/testdata/golden/`. Regenerate them only after
reviewing the diff:

```sh
UPDATE_GOLDEN=1 go test ./internal/tui/...
```

## Project layout

```
cmd/dbt/                 CLI entrypoint, flag parsing, exit codes
internal/config/         TOML loading, validation, env/home expansion
internal/db/             Driver contracts, DSN parsing, bounded query execution
internal/db/sqlite/      Pure-Go SQLite driver and catalog introspection
internal/db/postgres/    pgx driver and catalog introspection
internal/db/dbtest/      Deterministic fake connection for TUI tests
internal/log/            Structured logging and secret redaction
internal/tui/            Bubble Tea app shell, screens, components, theme, keymap
  app/                   Root model, async operations, header/status bar
  screen/                Browser, connection picker, modals
  component/             Tree, data grid, structure pane, SQL editor
  keys/                  Keymap and help groups
  theme/                 Lip Gloss styles and pane borders
docs/                    Local design notes (gitignored)
```

## Limitations and non-goals

- Read-only only: no writes, DDL, imports, or exports.
- One connection at a time; no tabs for multiple sessions.
- No mouse support; the UI is keyboard-first.
- PostgreSQL DDL is not rendered (SQLite only), pending richer metadata work.
- Pagination uses `LIMIT`/`OFFSET`, so very large offsets are slower than keyset pagination.
- The `dbt` binary name overlaps with dbt-core. If that causes friction, a rename (for example
  `dbv`) is planned as a post-MVP change.

## License

No license file is included yet. Add one before distributing binaries.
