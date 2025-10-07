#!/bin/bash
set -e

# Create unique result file in /tmp/home-ci-data for cleanup validation
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
COMMIT_HASH=$(git rev-parse HEAD 2>/dev/null | head -c 8 || echo "unknown")
BRANCH_NAME=$(git branch --show-current 2>/dev/null || echo "detached")
DATA_DIR="/tmp/home-ci-data"
RESULT_FILE="$DATA_DIR/timeout-test-${TIMESTAMP}-${BRANCH_NAME}-${COMMIT_HASH}.json"

# Ensure data directory exists
mkdir -p "$DATA_DIR"

echo "=== Slow Test (timeout validation) ==="
echo "Branch: $BRANCH_NAME | Commit: $COMMIT_HASH"

# Save run information to result file
cat > "$RESULT_FILE" << EOF
{
  "working_dir": "$(pwd)",
  "test_type": "timeout_validation"
}
EOF

echo "📁 Test result: $RESULT_FILE"
echo "⏳ Running for 2 minutes (should timeout after 30s)..."

# Long-running operation that should trigger timeout if timeout < 2 minutes
for i in {1..120}; do
    if [ $((i % 15)) -eq 0 ]; then
        echo "Step $i/120..."
    fi
    sleep 1

    # Allow early termination
    if [ -f "/tmp/stop_slow_test" ]; then
        echo "Early termination"
        rm -f "/tmp/stop_slow_test"
        exit 0
    fi
done

echo "✅ Test completed (should not appear with 30s timeout)"
exit 0