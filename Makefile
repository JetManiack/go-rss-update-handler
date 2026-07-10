BINARY_NAME := gruh
BIN_DIR     := bin
CMD_DIR     := ./cmd/gruh
GOLANGCI_LINT_VERSION := latest
STATICCHECK_VERSION   := latest
GOSEC_VERSION         := latest
GOVULNCHECK_VERSION   := latest

.PHONY: all build test lint gosec govulncheck staticcheck security clean

all: build

## build: собрать бинарь в bin/gruh
build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_DIR)

## test: запустить все тесты
test:
	go test ./...

## lint: запустить golangci-lint (устанавливается автоматически, если отсутствует)
lint:
	@command -v golangci-lint >/dev/null 2>&1 || \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	golangci-lint run ./...

## gosec: запустить gosec (проверка кода на уязвимости, устанавливается автоматически, если отсутствует)
gosec:
	@command -v gosec >/dev/null 2>&1 || \
		go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	gosec ./...

## govulncheck: запустить govulncheck (проверка зависимостей на известные уязвимости, устанавливается автоматически, если отсутствует)
govulncheck:
	@command -v govulncheck >/dev/null 2>&1 || \
		go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	govulncheck ./...

## staticcheck: запустить staticcheck (статический анализ кода, устанавливается автоматически, если отсутствует)
staticcheck:
	@command -v staticcheck >/dev/null 2>&1 || \
		go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	staticcheck ./...

## security: запустить все проверки безопасности и статического анализа (gosec, govulncheck, staticcheck)
security: gosec govulncheck staticcheck

## clean: удалить артефакты сборки
clean:
	rm -rf $(BIN_DIR)
