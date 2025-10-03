#!/bin/bash
set -e

echo "=== Slow Test Suite (for timeout testing) ==="
echo "Current directory: $(pwd)"
echo "Current branch: $(git branch --show-current 2>/dev/null || echo 'detached')"
echo "Current commit: $(git rev-parse HEAD)"
echo "Timestamp: $(date)"

# Check passed arguments
if [ $# -gt 0 ]; then
    echo "Arguments received: $*"
fi

echo ""
echo "=== Starting long-running test ==="
echo "This test will run for 2 minutes to test timeout behavior..."

# Long-running operation that should trigger timeout if timeout < 2 minutes
for i in {1..120}; do
    echo "Test step $i/120 - $(date)"
    sleep 1

    # Allow early termination if the script receives SIGTERM
    if [ -f "/tmp/stop_slow_test" ]; then
        echo "Early termination requested"
        rm -f "/tmp/stop_slow_test"
        exit 0
    fi
done

echo ""
echo "✅ Long test completed successfully!"
echo "This should only appear if timeout is > 2 minutes"
exit 0