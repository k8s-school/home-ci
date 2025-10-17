#!/bin/bash
set -e

# Parse command line arguments
TIMEOUT_TEST_MODE=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --timeout-test)
            TIMEOUT_TEST_MODE=true
            shift
            ;;
        *)
            # Unknown option, ignore for now
            shift
            ;;
    esac
done

# Create unique result file in the run's data directory for cleanup validation
COMMIT_HASH=$(git rev-parse HEAD 2>/dev/null | head -c 8 || echo "unknown")
BRANCH_NAME=$(git branch --show-current 2>/dev/null || echo "detached")
COMMIT_MESSAGE=$(git log -1 --pretty=format:"%s" 2>/dev/null || echo "unknown")

# E2E tests use the standardized data directory (can be overridden by environment)
DATA_DIR="${HOME_CI_DATA_DIR:-/tmp/home-ci/e2e/data}"

# Sanitize branch name for file system (replace / with -)
BRANCH_SAFE=$(echo "$BRANCH_NAME" | sed 's|/|-|g')
RESULT_FILE="$DATA_DIR/${BRANCH_SAFE}-${COMMIT_HASH}_run-product.json"

# Ensure data directory exists
mkdir -p "$DATA_DIR"

# Ensure the result file's directory exists
mkdir -p "$(dirname "$RESULT_FILE")"

echo "=== E2E Test Suite ==="
echo "Branch: $BRANCH_NAME | Commit: $COMMIT_HASH"
echo "Message: $COMMIT_MESSAGE"
if [ "$TIMEOUT_TEST_MODE" = true ]; then
    echo "Mode: Timeout Test"
fi

# Determine expected behavior based on commit message and branch
determine_test_behavior() {
    # Special handling for timeout tests: check if --timeout-test flag was passed
    if [ "$TIMEOUT_TEST_MODE" = true ]; then
        # For timeout tests, force timeout behavior unless commit message overrides
        if [[ "$COMMIT_MESSAGE" =~ .*SUCCESS.* ]]; then
            echo "success"
        elif [[ "$COMMIT_MESSAGE" =~ .*FAIL.* ]]; then
            echo "failure"
        else
            echo "timeout"  # Default for timeout tests
        fi
        return
    fi

    # Check commit message patterns first (highest priority)
    if [[ "$COMMIT_MESSAGE" =~ .*FAIL.* ]]; then
        echo "failure"
    elif [[ "$COMMIT_MESSAGE" =~ .*TIMEOUT.* ]]; then
        echo "timeout"
    elif [[ "$COMMIT_MESSAGE" =~ .*SUCCESS.* ]]; then
        echo "success"
    elif [[ "$COMMIT_MESSAGE" =~ .*CONCURRENT_TEST.* ]]; then
        echo "concurrent_test"
    else
        # Branch-based behavior (fallback)
        case "$BRANCH_NAME" in
            main)
                echo "success"
                ;;
            feature/test1)
                echo "success"
                ;;
            feature/test2)
                echo "failure"
                ;;
            bugfix/critical)
                echo "timeout"
                ;;
            feature/*)
                echo "success"
                ;;
            bugfix/*)
                echo "failure"
                ;;
            *)
                echo "success"  # Default behavior
                ;;
        esac
    fi
}

EXPECTED_BEHAVIOR=$(determine_test_behavior)

echo "🎯 Expected behavior: $EXPECTED_BEHAVIOR"

# Save run information to result file
cat > "$RESULT_FILE" << EOF
{
  "working_dir": "$(pwd)",
  "test_type": "e2e",
  "branch": "$BRANCH_NAME",
  "commit": "$COMMIT_HASH",
  "commit_message": "$COMMIT_MESSAGE",
  "expected_behavior": "$EXPECTED_BEHAVIOR",
  "timestamp": "$(date -Iseconds)"
}
EOF

echo "📁 Test result: $RESULT_FILE"

# Test execution based on expected behavior
case "$EXPECTED_BEHAVIOR" in
    "success")
        echo "▶️ Environment setup..."
        sleep 1
        echo "▶️ Running tests..."
        sleep 2
        echo "▶️ Performance validation..."
        sleep 1
        echo "✅ All tests passed"

        # Create success marker file
        echo "Test completed successfully" > "$DATA_DIR/${BRANCH_SAFE}-${COMMIT_HASH}_SUCCESS.txt"
        exit 0
        ;;

    "failure")
        echo "▶️ Environment setup..."
        sleep 1
        echo "▶️ Running tests..."
        sleep 2
        echo "❌ Test suite failed: Mock error for testing"
        echo "❌ Error details: Simulated failure based on branch/commit pattern"

        # Create failure marker file
        echo "Test failed as expected" > "$DATA_DIR/${BRANCH_SAFE}-${COMMIT_HASH}_FAILURE.txt"
        exit 1
        ;;

    "timeout")
        echo "▶️ Environment setup..."
        sleep 1
        echo "▶️ Running tests..."
        echo "⏳ Long-running operation starting..."

        # Create timeout marker file
        echo "Test will timeout" > "$DATA_DIR/${BRANCH_SAFE}-${COMMIT_HASH}_TIMEOUT.txt"

        # Run longer than the typical timeout (45+ seconds)
        for i in {1..60}; do
            if [ $((i % 10)) -eq 0 ]; then
                echo "Step $i/60... (this should timeout)"
            fi
            sleep 1

            # Allow early termination for debugging
            if [ -f "/tmp/stop_e2e_test" ]; then
                echo "Early termination requested"
                rm -f "/tmp/stop_e2e_test"
                exit 0
            fi
        done

        echo "✅ Test completed (should not reach here if timeout works)"
        exit 0
        ;;

    "concurrent_test")
        echo "🔄 Starting concurrent test..."
        echo "⏳ Running 15-second test to observe concurrency..."

        # Simulate test work for 15 seconds with progress updates
        for i in {1..15}; do
            echo "🔄 Concurrent test progress: $i/15 seconds"
            sleep 1
        done

        echo "✅ Concurrent test completed"

        # Create success marker file
        echo "Concurrent test completed successfully" > "$DATA_DIR/${BRANCH_SAFE}-${COMMIT_HASH}_CONCURRENT_SUCCESS.txt"
        exit 0
        ;;
esac