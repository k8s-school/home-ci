.PHONY: build build-e2e build-diag install test test-success test-fail test-timeout test-dispatch-one-success test-dispatch-no-token-file test-dispatch-all test-quick test-normal test-long test-concurrent-limit test-continuous-ci test-uat-github-repo-default copy-secret-if-exists clean clean-all help

# Default target
help:
	@echo "Home-CI Build and Test Targets"
	@echo "==============================="
	@echo ""
	@echo "Build targets:"
	@echo "  build               Build everything (home-ci + e2e test harness + diagnostics tool)"
	@echo "  build-home-ci       Build the home-ci binary"
	@echo "  build-e2e           Build the e2e test harness"
	@echo "  build-diag          Build the diagnostics tool"
	@echo "  install             Install home-ci and home-ci-diag binaries to /usr/local/bin"
	@echo "  clean               Clean build artifacts"
	@echo ""
	@echo "Test targets:"
	@echo "  test                Run integration tests (normal, 3 minutes)"
	@echo "  test-success        Run single commit success test"
	@echo "  test-fail           Run single commit failure test"
	@echo "  test-timeout        Run single commit timeout test (~1 minute)"
	@echo "  test-dispatch-one-success  Run single commit dispatch test (includes artifacts)"
	@echo "  test-dispatch-no-token-file Run single commit dispatch test (no token file)"
	@echo "  test-dispatch-all   Run multi commit test with dispatch"
	@echo "  test-quick          Run multi-commit quick tests (30 seconds)"
	@echo "  test-normal         Run normal integration tests (3 minutes)"
	@echo "  test-long           Run extended integration tests (10 minutes)"
	@echo "  test-concurrent-limit Test max_concurrent_runs=2 with 4 branches"
	@echo "  test-continuous-ci  Test continuous integration with max_concurrent_runs=3"
	@echo "  test-uat-github-repo-default UAT test for GitHub repo auto-defaulting"
	@echo ""
	@echo "Examples:"
	@echo "  make build && make test"
	@echo "  make test-success      # Fast unit test"
	@echo "  make test-quick        # Fast integration test"
	@echo "  make test-timeout      # Timeout validation"

# Build everything
build: build-home-ci build-e2e build-diag

# Build the main binary
build-home-ci:
	@echo "🏗️  Building home-ci..."
	go build -o home-ci ./cmd/home-ci
	@echo "✅ Build complete: ./home-ci"

# Build the e2e test harness
build-e2e:
	@echo "🏗️  Building e2e test harness..."
	go build -o home-ci-e2e ./cmd/home-ci-e2e
	@echo "✅ Build complete: ./home-ci-e2e -v3"

# Build the diagnostics tool
build-diag:
	@echo "🏗️  Building diagnostics tool..."
	go build -o home-ci-diag ./cmd/home-ci-diag
	@echo "✅ Build complete: ./home-ci-diag"

# Install binaries to system
install: build-home-ci build-diag
	@echo "📦 Installing home-ci and home-ci-diag to /usr/local/bin..."
	@if [ ! -f "./home-ci" ]; then echo "❌ home-ci binary not found"; exit 1; fi
	@if [ ! -f "./home-ci-diag" ]; then echo "❌ home-ci-diag binary not found"; exit 1; fi
	sudo cp ./home-ci /usr/local/bin/
	sudo cp ./home-ci-diag /usr/local/bin/
	sudo chmod +x /usr/local/bin/home-ci
	sudo chmod +x /usr/local/bin/home-ci-diag
	@echo "✅ Binaries installed successfully"

# Run integration tests (default duration)
test: build
	@echo "🧪 Running integration tests..."
	./home-ci-e2e -v3 --type=normal -duration=3m
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/normal/config-normal.yaml --check-timeline
	@echo ""
	@echo "🔍 Verifying concurrency compliance:"
	./home-ci-diag --config=/tmp/home-ci/e2e/normal/config-normal.yaml --check-concurrency

# Run single commit success test
test-success: build
	@echo "✅ Running success test..."
	./home-ci-e2e -v3 --type=success
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/success/config-success.yaml --check-timeline

# Run single commit failure test
test-fail: build
	@echo "❌ Running failure test..."
	./home-ci-e2e -v3 --type=fail
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/fail/config-fail.yaml --check-timeline

# Run timeout validation test
test-timeout: build
	@echo "🕐 Running timeout validation test..."
	./home-ci-e2e -v3 --type=timeout
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/timeout/config-timeout.yaml --check-timeline

# Helper function to copy secret.yaml for dispatch tests
copy-secret-if-exists:
	@if [ -f secret.yaml ]; then \
		echo "📄 Copying secret.yaml to dispatch test directories..."; \
		mkdir -p /tmp/home-ci/e2e/dispatch-one-success; \
		mkdir -p /tmp/home-ci/e2e/dispatch-all; \
		cp secret.yaml /tmp/home-ci/e2e/dispatch-one-success/secret.yaml; \
		cp secret.yaml /tmp/home-ci/e2e/dispatch-all/secret.yaml; \
		echo "✅ secret.yaml copied to dispatch test directories"; \
	else \
		echo "⚠️  No secret.yaml found in project root - dispatch tests may fail"; \
	fi

# Run single commit dispatch test
test-dispatch-one-success: build copy-secret-if-exists
	@echo "🚀 Running single commit dispatch test..."
	./home-ci-e2e -v3 --type=dispatch-one-success
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/dispatch-one-success/config-dispatch-one-success.yaml --check-timeline

# Run single commit dispatch test with no token file
test-dispatch-no-token-file: build
	@echo "🚀 Running single commit dispatch test (no token file)..."
	./home-ci-e2e -v3 --type=dispatch-no-token-file
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/dispatch-no-token-file/config-dispatch-no-token-file.yaml --check-timeline

# Run multi commit dispatch test
test-dispatch-all: build copy-secret-if-exists
	@echo "🚀 Running multi commit dispatch test..."
	./home-ci-e2e -v3 --type=dispatch-all
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/dispatch-all/config-dispatch-all.yaml --check-timeline
	@echo ""
	@echo "🔍 Verifying concurrency compliance:"
	./home-ci-diag --config=/tmp/home-ci/e2e/dispatch-all/config-dispatch-all.yaml --check-concurrency

# Run quick tests (4 commits)
test-quick: build
	@echo "⚡ Running quick integration tests..."
	./home-ci-e2e -v3 --type=quick --duration=10s
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/quick/config-quick.yaml --check-timeline
	@echo ""
	@echo "🔍 Verifying concurrency compliance:"
	./home-ci-diag --config=/tmp/home-ci/e2e/quick/config-quick.yaml --check-concurrency

# Run normal integration tests
test-normal: build
	@echo "🧪 Running normal integration tests..."
	./home-ci-e2e -v3 --type=normal --duration=3m
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/normal/config-normal.yaml --check-timeline
	@echo ""
	@echo "🔍 Verifying concurrency compliance:"
	./home-ci-diag --config=/tmp/home-ci/e2e/normal/config-normal.yaml --check-concurrency

# Run extended tests
test-long: build
	@echo "🐌 Running extended integration tests..."
	./home-ci-e2e -v3 --type=long -duration=5m
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/long/config-long.yaml --check-timeline
	@echo ""
	@echo "🔍 Verifying concurrency compliance:"
	./home-ci-diag --config=/tmp/home-ci/e2e/long/config-long.yaml --check-concurrency

# Run concurrent limit test
test-concurrent-limit: build
	@echo "⚡ Running concurrent limit test (max_concurrent_runs=2)..."
	./home-ci-e2e -v3 --type=concurrent-limit
	@echo ""
	@echo "🔍 Verifying workflow consistency:"
	./home-ci-diag --config=/tmp/home-ci/e2e/concurrent-limit/config-concurrent-limit.yaml --check-timeline
	@echo ""
	@echo "🔍 Verifying concurrency compliance:"
	./home-ci-diag --config=/tmp/home-ci/e2e/concurrent-limit/config-concurrent-limit.yaml --check-concurrency

# Run continuous integration test
test-continuous-ci: build
	@echo "🔄 Running continuous integration test (max_concurrent_runs=3)..."
	./home-ci-e2e -v3 --type=continuous-ci
	@echo ""

test-uat-github-repo-default: build
	@echo "🧪 Running UAT: GitHub repository auto-defaulting test..."
	./tests/uat/test-github-repo-default.sh
	@echo ""

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -f home-ci
	rm -f home-ci-e2e
	rm -f home-ci-diag
	@echo "✅ Clean complete"
	@echo "💾 Test data preserved in /tmp/home-ci/e2e/*/data/"

# Clean all test environments
clean-all:
	@echo "🧹 Cleaning all build artifacts and test environments..."
	rm -f home-ci
	rm -f home-ci-e2e
	rm -f home-ci-diag
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
