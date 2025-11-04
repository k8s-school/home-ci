# Home-CI

This Go program automatically monitors a Git repository to detect new commits on all branches and launches e2e tests sequentially.

## Features

- **Automatic monitoring**: Periodically checks for new commits on all remote branches
- **Concurrent execution**: Configurable number of concurrent test runs
- **Sequential testing**: Prevents resource conflicts by managing concurrent execution
- **Flexible configuration**: Customizable options via YAML file
- **State persistence**: Saves state between restarts
- **Commit age filtering**: Only processes commits within a specified time window

## Installation

```bash
cd home-ci
go mod tidy
make build
```

Or manually:

```bash
go build -o home-ci ./cmd/home-ci
```

## Configuration

Create a `config.yaml` file:

```yaml
repo_path: "/path/to/your-repo"
check_interval: 5m
test_script: "./e2e/your-test-script.sh"
max_concurrent_runs: 2
options: "-c -i ztf"
recent_commits_within: 240h
test_timeout: 5m
fetch_remote: true

cleanup:
  after_e2e: true
  script: "./cleanup.sh"

github_actions_dispatch:
  enabled: false
  github_repo: "owner/repo"
```

### Parameters

- `repo_path`: Path to the repository to monitor
- `check_interval`: Check interval (Go duration format: "5m", "1h", etc.)
- `test_script`: Test script to execute
- `max_concurrent_runs`: Maximum number of concurrent test runs
- `options`: Options to pass to the test script
- `recent_commits_within`: Time window for processing recent commits (e.g., "24h" for last 24 hours, "240h" for 10 days)
- `test_timeout`: Maximum duration for test execution before timeout (e.g., "30s", "5m")
- `fetch_remote`: Whether to fetch from remote repositories

### Test Script Options

According to the test scripts, available options include:

- `-c`: Clean up cluster if tests succeed
- `-s`: Use scientific algorithms during tests
- `-i <survey>`: Specify input survey (default: ztf)
- `-b <branch>`: Branch name (automatically added by the monitor)
- `-m`: Enable monitoring

Option examples:
- `"-c -i ztf"`: Cleanup + ZTF survey
- `"-c -s -i ztf"`: Cleanup + science + ZTF survey
- `"-c -s -m -i ztf"`: Cleanup + science + monitoring + ZTF survey

## Usage

### Starting

```bash
# With configuration file
./home-ci -c config.yaml

# With verbose logging
./home-ci -c config.yaml -v

# Show help
./home-ci --help
```

### Required Environment Variables

The test scripts may require:

```bash
export TOKEN="your-github-token"
export USER="your-username"
```

### How it Works

1. **Monitoring**: The program periodically checks remote branches
2. **Detection**: When a new commit is detected, it's added to the queue
3. **Filtering**: Only processes commits within the specified age limit
4. **Execution**: Launches the test script with appropriate options
5. **Concurrency**: Manages multiple test runs based on configuration

### Generated Files

- `.home-ci/state.json`: Persistent state (last commits, daily counters)
- `.home-ci/*.log`: Test execution logs

## Logs

The program displays detailed logs:

```
2024/01/15 10:00:00 Starting Git CI Monitor...
2024/01/15 10:00:00 Repository: /path/to/your-repo
2024/01/15 10:00:00 Check interval: 5m0s
2024/01/15 10:00:00 Max concurrent runs: 2
2024/01/15 10:00:00 Options: -c -i ztf
2024/01/15 10:00:00 Starting test runner...
2024/01/15 10:00:00 Checking for updates...
2024/01/15 10:05:00 New commit detected on branch feature-xyz: abcd1234
2024/01/15 10:05:00 Queued test job for branch feature-xyz
2024/01/15 10:05:00 Starting tests for branch feature-xyz, commit abcd1234
```

## Graceful Shutdown

The program can be stopped cleanly with Ctrl+C. It automatically saves its state before closing.

## Architecture

- **Monitor**: Main structure that manages monitoring
- **TestJob**: Test job in the queue
- **BranchState**: State of a branch (last commit, daily runs)
- **Config**: Configuration loaded from YAML

The program uses goroutines for:
- Periodic branch monitoring
- Concurrent test execution management
- State management and persistence

## Testing

Run integration tests to validate home-ci functionality using the unified e2e test harness:

```bash
# Build the e2e test harness
make build-e2e

# Quick test (recommended for development) - 30 seconds
make test-quick

# Standard test - 3 minutes
make test

# Extended test - 10 minutes
make test-long

# Timeout validation test
make test-timeout
```

The e2e test harness creates a temporary git repository, launches home-ci, simulates commits and branches, and verifies that tests are executed properly. All test scripts and configurations are embedded as resources, making the test harness completely self-contained.