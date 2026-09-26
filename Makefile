.DEFAULT_GOAL := help

.PHONY: help
help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-9s\033[0m %s\n", $$1, $$2}'

.PHONY: lint
lint: ## Run golangci-lint (config: .golangci.yml)
	golangci-lint run ./...

.PHONY: lint-fix
lint-fix: ## Auto-fix lint issues where possible
	golangci-lint run --fix ./...

.PHONY: mocks
mocks: ## Regenerate gomock mocks (go:generate)
	go generate ./...

.PHONY: fmt
fmt: ## Apply the golangci-lint formatters
	golangci-lint fmt ./...

.PHONY: test
test: ## Run the full test suite with the race detector
	go test -race ./...

.PHONY: dev
dev: ## Run the app in dev mode (hot reload on both sides)
	wails3 dev -config ./build/config.yml
