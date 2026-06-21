.PHONY: help build test e2e lint clean

help: ## Show this help
	@grep -E '^[a-z0-9]+:.*##' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-8s %s\n", $$1, $$2}'

build: ## Build the vikunja-mcp binary
	go build -o vikunja-mcp .

test: ## Run unit tests
	go test -v -count=1 ./...

e2e: ## Run e2e integration tests (requires podman)
	go test -tags e2e -v -count=1 -timeout 120s .

lint: ## Run golangci-lint
	golangci-lint run ./...
	golangci-lint run --build-tags e2e ./...

clean: ## Remove build artifacts
	rm -f vikunja-mcp
