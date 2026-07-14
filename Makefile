BINARY_NAME := gruh
BIN_DIR     := bin
CMD_DIR     := ./cmd/gruh
GOLANGCI_LINT_VERSION := latest
STATICCHECK_VERSION   := latest
GOSEC_VERSION         := latest
GOVULNCHECK_VERSION   := latest

.PHONY: all build test lint gosec govulncheck staticcheck security clean

all: build

## build: build the binary into bin/gruh
build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_DIR)

## test: run all tests
test:
	go test ./...

## lint: run golangci-lint v2 (installed automatically if missing)
# Built with the project's Go toolchain: golangci-lint refuses to run when it was
# built with a Go version older than the module's target (see go.mod).
lint:
	@command -v golangci-lint >/dev/null 2>&1 || \
		GOTOOLCHAIN=$(shell go env GOVERSION) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	golangci-lint run ./...

## gosec: run gosec (code vulnerability scan, installed automatically if missing)
gosec:
	@command -v gosec >/dev/null 2>&1 || \
		go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	gosec ./...

## govulncheck: run govulncheck (scans dependencies for known vulnerabilities, installed automatically if missing)
govulncheck:
	@command -v govulncheck >/dev/null 2>&1 || \
		go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	govulncheck ./...

## staticcheck: run staticcheck (static code analysis, installed automatically if missing)
staticcheck:
	@command -v staticcheck >/dev/null 2>&1 || \
		go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	staticcheck ./...

## security: run all security and static-analysis checks (gosec, govulncheck, staticcheck)
security: gosec govulncheck staticcheck

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR)
