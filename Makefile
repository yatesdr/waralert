# WarAlert Makefile

BINARY_NAME=waralert
VERSION?=0.1.0
BUILD_DIR=build

LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all clean linux windows macos build run test

all: clean linux windows macos

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/waralert

run:
	go run ./cmd/waralert

test:
	go test -v ./...

linux: linux-amd64 linux-arm64

linux-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/waralert

linux-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/waralert

windows: windows-amd64

windows-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/waralert

macos: macos-amd64 macos-arm64

macos-amd64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/waralert

macos-arm64:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/waralert

clean:
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY_NAME)

install:
	go install $(LDFLAGS) ./cmd/waralert

deps:
	go mod tidy
	go mod download

fmt:
	go fmt ./...

help:
	@echo "WarAlert Build Targets:"
	@echo "  make build     - Build for current platform"
	@echo "  make run       - Run the application"
	@echo "  make all       - Build for all platforms"
	@echo "  make linux     - Build for Linux (amd64, arm64)"
	@echo "  make windows   - Build for Windows (amd64)"
	@echo "  make macos     - Build for macOS (amd64, arm64)"
	@echo "  make clean     - Remove build artifacts"
	@echo "  make install   - Install to GOPATH/bin"
	@echo "  make test      - Run tests"
	@echo "  make deps      - Update dependencies"
	@echo "  make fmt       - Format code"
