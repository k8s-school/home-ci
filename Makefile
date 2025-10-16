.PHONY: build build-e2e test test-success test-fail test-timeout test-dispatch-one-success test-dispatch-all test-quick test-normal test-long clean clean-all help

# Default target
help:
	@echo "Home-CI Build and Test Targets"
	@echo "==============================="
	@echo ""
	@echo "Build targets:"
	@echo "  build               Build everything (home-ci + e2e test harness)"
	@echo "  build-home-ci       Build the home-ci binary"
	@echo "  build-e2e           Build the e2e test harness"
	@echo "  clean               Clean build artifacts"
	@echo ""
	@echo "Test targets:"
	@echo "  test                Run integration tests (normal, 3 minutes)"
	@echo "  test-success        Run single commit success test"
	@echo "  test-fail           Run single commit failure test"
	@echo "  test-timeout        Run single commit timeout test (~1 minute)"
	@echo "  test-dispatch-one-success  Run single commit dispatch test"
	@echo "  test-dispatch-all   Run multi commit test with dispatch"
	@echo "  test-quick          Run multi-commit quick tests (30 seconds)"
	@echo "  test-normal         Run normal integration tests (3 minutes)"
	@echo "  test-long           Run extended integration tests (10 minutes)"
	@echo ""
	@echo "Examples:"
	@echo "  make build && make test"
	@echo "  make test-success      # Fast unit test"
	@echo "  make test-quick        # Fast integration test"
	@echo "  make test-timeout      # Timeout validation"

# Build everything
build: build-home-ci build-e2e

# Build the main binary
build-home-ci:
	@echo "🏗️  Building home-ci..."
	go build -o home-ci ./cmd/home-ci
	@echo "✅ Build complete: ./home-ci"

# Build the e2e test harness
build-e2e:
	@echo "🏗️  Building e2e test harness..."
	go build -o e2e-home-ci ./cmd/e2e-home-ci
	@echo "✅ Build complete: ./e2e-home-ci"

# Run integration tests (default duration)
test: build
	@echo "🧪 Running integration tests..."
	./e2e-home-ci -type=normal -duration=3m

# Run single commit success test
test-success: build
	@echo "✅ Running success test..."
	./e2e-home-ci -type=success

# Run single commit failure test
test-fail: build
	@echo "❌ Running failure test..."
	./e2e-home-ci -type=fail

# Run timeout validation test
test-timeout: build
	@echo "🕐 Running timeout validation test..."
	./e2e-home-ci -type=timeout

# Run single commit dispatch test
test-dispatch-one-success: build
	@echo "🚀 Running single commit dispatch test..."
	./e2e-home-ci -type=dispatch-one-success

# Run multi commit dispatch test
test-dispatch-all: build
	@echo "🚀 Running multi commit dispatch test..."
	./e2e-home-ci -type=dispatch-all

# Run quick tests (4 commits)
test-quick: build
	@echo "⚡ Running quick integration tests..."
	./e2e-home-ci -type=quick

# Run normal integration tests
test-normal: build
	@echo "🧪 Running normal integration tests..."
	./e2e-home-ci -type=normal -duration=3m

# Run extended tests
test-long: build
	@echo "🐌 Running extended integration tests..."
	./e2e-home-ci -type=long -duration=10m

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -f home-ci
	rm -f e2e-home-ci
	@echo "✅ Clean complete"
	@echo "💾 Test data preserved in /tmp/home-ci/e2e/*/data/"

# Clean all test environments
clean-all:
	@echo "🧹 Cleaning all build artifacts and test environments..."
	rm -f home-ci
	rm -f e2e-home-ci
	rm -rf /tmp/home-ci/e2e/
	rm -rf /tmp/test-repo-home-ci
	rm -rf /tmp/test-repo-timeout
	rm -f /tmp/home-ci-test-config-*.yaml
	@echo "✅ Full clean complete"

# Development helpers
dev-deps:
	@echo "📦 Installing development dependencies..."
	go mod tidy
	@echo "✅ Dependencies updated"

# Show project structure
tree:
	@echo "📁 Project structure:"
	@tree -I '.git|log' -L 3 || ls -la
