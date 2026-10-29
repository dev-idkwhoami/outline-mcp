GO ?= go
VERSION ?= 1.0.0
LDFLAGS = -s -w -X main.version=$(VERSION)
BUILD_FLAGS = -trimpath -buildvcs=false

.PHONY: all build linux windows checksums test check generate
all: checksums
build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -ldflags '$(LDFLAGS)' -o bin/outline-mcp ./cmd/outline-mcp
linux:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -ldflags '$(LDFLAGS)' -o bin/outline-mcp-linux-amd64 ./cmd/outline-mcp
windows:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -ldflags '$(LDFLAGS)' -o bin/outline-mcp-windows-amd64.exe ./cmd/outline-mcp
checksums: linux windows
	$(GO) run ./scripts/checksums bin/outline-mcp-linux-amd64 bin/outline-mcp-windows-amd64.exe > bin/SHA256SUMS
test:
	$(GO) test ./...
check:
	$(GO) vet ./...
generate:
	$(GO) run ./scripts/generate.go
