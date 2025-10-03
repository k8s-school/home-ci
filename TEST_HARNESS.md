# Home-CI Test Harness

This test program automates the validation of home-ci by creating a complete test environment.

## Features

The test harness automatically performs:

1. **Test git repository creation**: Initializes a new repository with a basic structure
2. **Home-CI launch**: Starts the home-ci process with a test-adapted configuration
3. **Development activity simulation**: Periodically creates new commits on different branches
4. **Test verification**: Monitors logs to ensure that e2e tests are properly executed
5. **Statistics**: Displays a summary of activities performed

## Usage

### Simple method (recommended)

```bash
./run_test.sh [duration]
```

Examples:
```bash
./run_test.sh          # 3-minute test by default
./run_test.sh 5m       # 5-minute test
./run_test.sh 30s      # 30-second test
./run_test.sh 10m      # 10-minute test
```

### Manual method

```bash
# Compile the test harness
go build -o test_home_ci test_home_ci.go

# Run the test
./test_home_ci [duration]
```

## Configuration

The test harness uses a specific configuration for home-ci:

- **Test repository**: `/tmp/test-repo-home-ci`
- **Check interval**: 30 seconds
- **Test script**: `_e2e/run-e2e.sh`
- **Logs**: `<test-repo>/.home-ci/`

## Test output

The program displays in real-time:

- ✅ Test repository creation
- 🚀 Home-ci startup
- 📝 Commits created on different branches
- 🧪 Test execution detection
- 📊 Final statistics

### Example output

```
🚀 Starting home-ci test harness...
✅ Test repository created at /tmp/test-repo-home-ci
✅ Configuration file created at /tmp/home-ci-test-config.yaml
✅ home-ci started with PID 12345, logs will be in /tmp/test-repo-home-ci/.home-ci/
🎯 Starting activity simulation for 5m0s
📝 Creating commit on branch main
✅ Created commit on main: Add file_main_1234567890.txt on branch main
🧪 Test execution detected! Total tests executed: 1
📝 Creating commit on branch feature/new-feature
✅ Created new branch: feature/new-feature
🧪 Test execution detected! Total tests executed: 2
...

📊 Test Statistics:
   Commits created: 7
   Branches created: 3
   Tests detected: 15
✅ Test execution detection working correctly!
```

## Automatically created branches

The test harness creates commits on the following branches cyclically:

1. `main`
2. `feature/new-feature`
3. `bugfix/critical-fix`
4. `feature/enhancement`

## Verification

The test verifies that:

- ✅ Home-ci starts correctly
- ✅ New commits are detected
- ✅ E2e tests are executed automatically
- ✅ The system properly handles multiple branches
- ✅ Logs are generated correctly

## Stopping the test

- **Normal stop**: The test ends automatically after the specified duration
- **Manual stop**: Press `Ctrl+C` to stop the test at any time

The program performs automatic cleanup on stop:
- Graceful shutdown of the home-ci process
- Closing of log files
- Cleanup of temporary resources

## Generated files

During execution, the following files are created:

- `/tmp/test-repo-home-ci/`: Test git repository
- `/tmp/home-ci-test-config.yaml`: Home-ci configuration for testing
- `/tmp/test-repo-home-ci/.home-ci/`: Home-ci logs and state files
- `./test_home_ci`: Compiled test harness binary

## Troubleshooting

### Common problems

1. **"home-ci binary not found"**
   ```bash
   go build -o home-ci
   ```

2. **"git is required but not installed"**
   ```bash
   sudo apt-get install git  # Ubuntu/Debian
   brew install git          # macOS
   ```

3. **Insufficient permissions on `/tmp/`**
   - Check `/tmp/` directory permissions
   - Or modify the path in `test_home_ci.go`

### Debug

For detailed debugging:
1. Check home-ci logs: `tail -f /tmp/test-repo-home-ci/.home-ci/*.log`
2. Check test repository state: `cd /tmp/test-repo-home-ci && git log --oneline`
3. Run home-ci manually with test config to reproduce issues

## Customization

To modify test behavior, edit `test_home_ci.go`:

- **Duration between commits**: Modify `45 * time.Second` in `simulateActivity()`
- **Tested branches**: Modify the `branches` slice
- **Paths**: Modify constants at the beginning of the file
- **Home-ci configuration**: Modify the template in `createConfigFile()`