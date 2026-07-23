.PHONY: build install clean run serve

BINARY=agent-sync

build:
	go build -o $(BINARY) ./cmd/agent-sync/

install: build
	install -m 755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed to /usr/local/bin/$(BINARY)"

run: build
	./$(BINARY)

serve: build
	./$(BINARY) serve

clean:
	rm -f $(BINARY)

test:
	go test ./...
