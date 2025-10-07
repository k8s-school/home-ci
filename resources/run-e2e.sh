#!/bin/bash
set -e

# Create unique result file in /tmp/home-ci-data for cleanup validation
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
COMMIT_HASH=$(git rev-parse HEAD 2>/dev/null | head -c 8 || echo "unknown")
BRANCH_NAME=$(git branch --show-current 2>/dev/null || echo "detached")
DATA_DIR="/tmp/home-ci-data"
RESULT_FILE="$DATA_DIR/test-run-${TIMESTAMP}-${BRANCH_NAME}-${COMMIT_HASH}.json"

# Ensure data directory exists
mkdir -p "$DATA_DIR"

echo "=== E2E Test Suite ==="
echo "Branch: $BRANCH_NAME | Commit: $COMMIT_HASH"

# Save run information to result file
cat > "$RESULT_FILE" << EOF
{
  "working_dir": "$(pwd)",
  "test_type": "e2e"
}
EOF

echo "📁 Test result: $RESULT_FILE"

# Simplified test execution
echo "▶️ Environment setup..."
sleep 1
echo "▶️ Running tests..."
sleep 3
echo "▶️ Performance validation..."
sleep 1

# Simulate test results (90% success rate for e2e)
SUCCESS_RATE=9
if [ $((RANDOM % 10)) -lt $SUCCESS_RATE ]; then
    echo "✅ All tests passed"
    exit 0
else
    echo "❌ Some tests failed"
    exit 1
fi