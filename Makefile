.PHONY: build build-e2e test test-quick test-long test-timeout clean help

# Default target
help:
	@echo "Home-CI Build and Test Targets"
	@echo "==============================="
	@echo ""
	@echo "Build targets:"
	@echo "  build               Build the home-ci binary"
	@echo "  build-e2e           Build the e2e test harness"
	@echo "  clean               Clean build artifacts"
	@echo ""
	@echo "Test targets:"
	@echo "  test                Run integration tests (3 minutes)"
	@echo "  test-quick          Run quick integration tests (30 seconds)"
	@echo "  test-long           Run extended integration tests (10 minutes)"
	@echo "  test-timeout        Run timeout validation test (~1 minute)"
	@echo ""
	@echo "Examples:"
	@echo "  make build && make test"
	@echo "  make test-quick"

# Build the main binary
build:
	@echo "🏗️  Building home-ci..."
	go build -o home-ci ./cmd/home-ci
	@echo "✅ Build complete: ./home-ci"

# Build the e2e test harness
build-e2e:
	@echo "🏗️  Building e2e test harness..."
	go build -o e2e-home-ci ./cmd/e2e-home-ci
	@echo "✅ Build complete: ./e2e-home-ci"

# Run integration tests (default duration)
test: build build-e2e
	@echo "🧪 Running integration tests..."
	./e2e-home-ci -type=normal -duration=3m

# Run quick tests
test-quick: build build-e2e
	@echo "⚡ Running quick integration tests..."
	./e2e-home-ci -type=quick -duration=30s

# Run extended tests
test-long: build build-e2e
	@echo "🐌 Running extended integration tests..."
	./e2e-home-ci -type=long -duration=10m

# Run timeout validation test
test-timeout: build build-e2e
	@echo "🕐 Running timeout validation test..."
	./e2e-home-ci -type=timeout

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -f home-ci
	rm -f e2e-home-ci
	rm -rf /tmp/test-repo-home-ci
	rm -rf /tmp/test-repo-timeout
	rm -f /tmp/home-ci-test-config-*.yaml
	@echo "✅ Clean complete"
	@echo "💾 Test data preserved in /tmp/home-ci-data/"

# Development helpers
dev-deps:
	@echo "📦 Installing development dependencies..."
	go mod tidy
	@echo "✅ Dependencies updated"

# Show project structure
tree:
	@echo "📁 Project structure:"
	@tree -I '.git|log' -L 3 || ls -la
