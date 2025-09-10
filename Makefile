# FileBox Makefile

.PHONY: help build test test-coverage test-bench lint clean run dev

# Default target
help: ## Show this help message
	@echo "FileBox - Available commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the filebox binary
	@echo "Building filebox..."
	go build -o filebox .
	@echo "✅ Build complete: ./filebox"

test: ## Run tests
	@echo "Running tests..."
	go test -v .

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out .
	go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

test-bench: ## Run benchmarks
	@echo "Running benchmarks..."
	go test -bench=. -benchmem .

lint: ## Run linters
	@echo "Running linters..."
	go vet .
	@echo "Checking code formatting..."
	@if [ "$$(gofmt -s -l . | wc -l)" -gt 0 ]; then \
		echo "❌ Code is not formatted. Run 'make fmt' to fix."; \
		gofmt -s -l .; \
		exit 1; \
	else \
		echo "✅ Code is properly formatted"; \
	fi

fmt: ## Format code
	@echo "Formatting code..."
	go fmt .
	@echo "✅ Code formatted"

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -f filebox
	rm -f coverage.out coverage.html
	rm -f filebox-*
	rm -f checksums.txt
	@echo "✅ Cleaned"

run: build ## Build and run filebox
	@echo "Starting filebox..."
	@echo "Make sure to set S3_BUCKET environment variable"
	./filebox

dev: ## Run in development mode with hot reload
	@echo "Starting development server..."
	@if [ -z "$$S3_BUCKET" ]; then \
		echo "⚠️  S3_BUCKET not set, using test-bucket"; \
		export S3_BUCKET=test-bucket; \
	fi
	@if command -v air >/dev/null 2>&1; then \
		air; \
	else \
		echo "Installing air for hot reload..."; \
		go install github.com/cosmtrek/air@latest; \
		air; \
	fi

install-tools: ## Install development tools
	@echo "Installing development tools..."
	go install github.com/cosmtrek/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "✅ Tools installed"

ci: test lint ## Run CI checks locally
	@echo "✅ All CI checks passed"

# Docker targets
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t filebox:latest .

docker-run: ## Run in Docker container
	@echo "Running in Docker..."
	docker run --rm -p 8080:8080 \
		-e S3_BUCKET=$${S3_BUCKET:-test-bucket} \
		-e AWS_PROFILE=$${AWS_PROFILE:-default} \
		filebox:latest

# Release targets
release-check: ## Check if ready for release
	@echo "Checking release readiness..."
	@make test
	@make lint
	@echo "✅ Ready for release"

release-build: ## Build release binaries
	@echo "Building release binaries..."
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -o dist/filebox-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o dist/filebox-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -o dist/filebox-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -o dist/filebox-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build -o dist/filebox-windows-amd64.exe .
	@echo "✅ Release binaries built in dist/"

# Default target
.DEFAULT_GOAL := help
