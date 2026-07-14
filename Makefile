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

## lint: run golangci-lint (installed automatically if missing)
lint:
	@command -v golangci-lint >/dev/null 2>&1 || \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
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
