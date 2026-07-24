.PHONY: build install clean run serve test lint release-build

BINARY=agent-sync
VERSION?=0.1.0-dev
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/agent-sync/

install: build
	install -m 755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed to /usr/local/bin/$(BINARY)"

run: build
	./$(BINARY)

serve: build
	./$(BINARY) serve

clean:
	rm -f $(BINARY)
	rm -rf dist/

test:
	go test ./...

lint:
	go vet ./...

release-build:
	goreleaser build --snapshot --clean

release-publish:
	goreleaser release --clean

build-all:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/agent-sync-linux-amd64 ./cmd/agent-sync/
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/agent-sync-linux-arm64 ./cmd/agent-sync/
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/agent-sync-darwin-amd64 ./cmd/agent-sync/
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/agent-sync-darwin-arm64 ./cmd/agent-sync/
	@echo "Built to dist/"
