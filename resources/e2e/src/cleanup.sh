#!/bin/bash
set -e

# Find the data directory: use environment variable if set, otherwise auto-detect
if [ -n "$HOME_CI_DATA_DIR" ]; then
    DATA_DIR="$HOME_CI_DATA_DIR"
else
    # Try to find the data directory by going up from the current repo
    REPO_DIR=$(pwd)
    if [[ "$REPO_DIR" =~ /tmp/home-ci-[0-9]{8}-[0-9]{6}/ ]]; then
        # Extract the base temp directory
        TEMP_BASE=$(echo "$REPO_DIR" | grep -o '/tmp/home-ci-[0-9]\{8\}-[0-9]\{6\}')
        DATA_DIR="$TEMP_BASE/data"
    else
        # Fallback to old behavior
        DATA_DIR="/tmp/home-ci-data"
    fi
fi

echo "=== E2E Cleanup Script ==="
echo "Scanning for data files in: $DATA_DIR"

# Check if data directory exists
if [ ! -d "$DATA_DIR" ]; then
    echo "ℹ️  Data directory $DATA_DIR does not exist - nothing to clean"
    exit 0
fi

# Find all JSON result files created by e2e tests (support all naming conventions)
json_files=$(find "$DATA_DIR" -name "*_run-product.json" -o -name "*_test-run.json" -o -name "test-run-*.json" 2>/dev/null || true)


if [ -z "$json_files" ]; then
    echo "ℹ️  No e2e data files found to clean"
    exit 0
fi

cleaned_count=0
for json_file in $json_files; do
    # Extract base filename without extension
    base_name=$(basename "$json_file" .json)
    cleaned_file="${DATA_DIR}/${base_name}.CLEANED"

    # Check if already cleaned
    if [ -f "$cleaned_file" ]; then
        echo "⏭️  Already cleaned: $(basename "$json_file")"
        continue
    fi

    # Read test information from JSON file
    if [ -f "$json_file" ]; then
        branch=$(grep '"branch":' "$json_file" | cut -d'"' -f4 || echo "unknown")
        commit=$(grep '"commit":' "$json_file" | cut -d'"' -f4 || echo "unknown")
        test_type=$(grep '"test_type":' "$json_file" | cut -d'"' -f4 || echo "unknown")
        timestamp=$(grep '"timestamp":' "$json_file" | cut -d'"' -f4 || echo "$(date -Iseconds)")

        echo "🧹 Cleaning up: $(basename "$json_file")"
        echo "   Branch: $branch"
        echo "   Commit: $commit"
        echo "   Type: $test_type"

        # Create cleaned marker file with metadata
        cat > "$cleaned_file" << EOF
{
  "original_file": "$json_file",
  "branch": "$branch",
  "commit": "$commit",
  "test_type": "$test_type",
  "original_timestamp": "$timestamp",
  "cleanup_timestamp": "$(date -Iseconds)",
  "cleanup_status": "completed"
}
EOF

        # Also clean up associated marker files (SUCCESS, FAILURE, TIMEOUT)
        # Handle both old format (*_branch_commit.txt) and new format (branch-commit_*.txt)
        old_marker_pattern="${DATA_DIR}/*_${branch}_${commit}.txt"
        new_marker_pattern="${DATA_DIR}/${branch}-${commit}_*.txt"

        for marker_pattern in "$old_marker_pattern" "$new_marker_pattern"; do
            for marker_file in $marker_pattern; do
                if [ -f "$marker_file" ]; then
                    echo "   🗑️  Removing marker: $(basename "$marker_file")"
                    rm -f "$marker_file"
                fi
            done
        done

        echo "   ✅ Created: $(basename "$cleaned_file")"
        cleaned_count=$((cleaned_count + 1))
    else
        echo "⚠️  Warning: Could not read $json_file"
    fi
done

echo ""
echo "🎯 Cleanup completed: $cleaned_count files processed"

# Optional: Remove old cleaned files (older than 7 days)
echo ""
echo "🗑️  Removing old cleaned files (>7 days)..."
old_cleaned_files=$(find "$DATA_DIR" -name "*.CLEANED" -mtime +7 2>/dev/null || true)
if [ -n "$old_cleaned_files" ]; then
    echo "$old_cleaned_files" | xargs rm -f
    echo "   Removed old cleaned files"
else
    echo "   No old cleaned files to remove"
fi

exit 0