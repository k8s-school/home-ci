#!/bin/bash
set -e

REPO_PATH="/tmp/test-repo-local"

echo "=== E2E Cleanup ==="
echo "Cleaning up test repository at: $REPO_PATH"

if [ -d "$REPO_PATH" ]; then
    echo "Removing test repository..."
    rm -rf "$REPO_PATH"
    echo "✅ Test repository removed successfully"
else
    echo "ℹ️  Test repository not found (already cleaned up)"
fi

echo "=== E2E Cleanup Complete ==="