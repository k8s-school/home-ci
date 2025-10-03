#!/bin/bash
set -e

echo "🧪 Home-CI Test Harness"
echo "======================"

# Check that home-ci is compiled
if [ ! -f "../home-ci" ]; then
    echo "❌ home-ci binary not found. Please run 'make build' first."
    exit 1
fi

# Check that e2e test harness is compiled
if [ ! -f "../e2e_home_ci" ]; then
    echo "❌ e2e_home_ci binary not found. Please run 'make build-test' first."
    exit 1
fi

# Check dependencies
if ! command -v git &> /dev/null; then
    echo "❌ git is required but not installed."
    exit 1
fi

# Default configuration
TEST_DURATION="${1:-3m}"

echo "Configuration:"
echo "  Test duration: $TEST_DURATION"
echo "  Home-CI binary: ../home-ci"
echo "  Test harness binary: ../e2e_home_ci"
echo ""

echo "🎯 Starting test harness..."
echo "   (Press Ctrl+C to stop early)"
echo ""

# Run the test
../e2e_home_ci "$TEST_DURATION"

echo ""
echo "📋 Test completed! Check the logs above for results."
echo ""
echo "💡 Tip: You can specify a different duration like:"
echo "   ./run_test.sh 5m     # Run for 5 minutes"
echo "   ./run_test.sh 30s    # Run for 30 seconds"
echo "   ./run_test.sh 10m    # Run for 10 minutes"