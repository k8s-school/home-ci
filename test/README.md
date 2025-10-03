# Home-CI Test Suite

This directory contains the integration test suite for home-ci.

## Quick Start

From the project root:

```bash
# Build and run tests
make test

# Quick test (30 seconds)
make test-quick

# Extended test (10 minutes)
make test-long
```

## Files

- `test_home_ci.go` - Main test harness program
- `run_test.sh` - Shell wrapper script for easy testing
- `README.md` - This file

## Manual Usage

From this directory:

```bash
# Run default test (3 minutes)
./run_test.sh

# Run custom duration
./run_test.sh 5m
./run_test.sh 30s
```

## What the tests do

The test harness:

1. Creates a temporary git repository with test data
2. Starts home-ci with test configuration
3. Simulates development activity (commits on multiple branches)
4. Monitors logs to verify test execution
5. Reports statistics and cleanup

See [../TEST_HARNESS.md](../TEST_HARNESS.md) for detailed documentation.