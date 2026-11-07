GO      ?= go
BINARY  ?= bin/dbt
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build run test test-race vet fmt fmt-check tidy offline-check check clean

all: check build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/dbt

run:
	$(GO) run ./cmd/dbt $(ARGS)

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

fmt-check:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "gofmt required for:"; \
		echo "$$files"; \
		exit 1; \
	fi

tidy:
	$(GO) mod tidy

offline-check:
	GOPROXY=off $(GO) build ./...

check: fmt-check vet test

clean:
	rm -rf bin
