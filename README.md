# dbt

A keyboard-first terminal database viewer for SQLite and PostgreSQL, written in Go.

Status: early development (MVP scaffold). The TUI is not implemented yet.

## Requirements

- Go 1.26 or newer
- A local SQLite file or a reachable PostgreSQL server

## Development

```sh
make check        # gofmt check, go vet, tests
make build        # build bin/dbt
make run          # run the current binary
make offline-check # verify the module cache is sufficient (GOPROXY=off)
```

## Documentation

Usage, configuration, and keymaps will be documented here as the MVP lands.
