BINARY  := whatsapp-tui
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

export CGO_ENABLED := 0

.DEFAULT_GOAL := run
.PHONY: run build vet test check windows clean help

run: build ## Build and start the app
	./$(BINARY)

build: ## Build the binary for this machine
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

vet: ## Run go vet
	go vet ./...

test: ## Run the tests
	go test ./...

check: vet test ## Run vet and tests

windows: ## Build dist/whatsapp-tui.exe for 64-bit Windows
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY).exe .

clean: ## Remove build output
	rm -f $(BINARY)
	rm -rf dist

help: ## List the available targets
	@grep -E '^[a-z]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  make %-8s %s\n", $$1, $$2}'
