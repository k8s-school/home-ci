# Enhanced GitHub Actions Dispatch Payload with Build Steps

This document demonstrates the enhanced payload structure with detailed build steps tracking for GitHub Actions dispatch notifications.

## Basic Payload Structure (Original)

The basic payload includes essential test information:

```json
{
  "branch": "main",
  "commit": "e07938b8c12345678",
  "success": true,
  "timestamp": "1672531200",
  "source": "home-ci",
  "artifact_name": "log-main-e07938b",
  "artifacts": {
    "test.log": {
      "content": "base64-encoded-content",
      "type": "log"
    },
    "result.txt": {
      "content": "base64-encoded-content",
      "type": "result"
    }
  },
  "metadata": {
    "branch": "main",
    "commit": "e07938b8c12345678",
    "success": true
  }
}
```

## Enhanced Payload Structure with Build Steps (New)

When `TestResult` information is available, the payload includes additional detailed information including individual build steps:

```json
{
  "branch": "main",
  "commit": "e07938b8c12345678",
  "success": true,
  "timestamp": "1672531200",
  "source": "home-ci",
  "artifact_name": "log-main-e07938b",
  "artifacts": {
    "test.log": {
      "content": "base64-encoded-content",
      "type": "log"
    },
    "result.txt": {
      "content": "base64-encoded-content",
      "type": "result"
    }
  },
  "metadata": {
    "branch": "main",
    "commit": "e07938b8c12345678",
    "success": true
  },
  "execution": {
    "start_time": "2024-01-02T15:04:05Z",
    "end_time": "2024-01-02T15:06:30Z",
    "duration": "2m25s",
    "duration_seconds": 145.0,
    "timed_out": false
  },
  "status": {
    "test_success": true,
    "cleanup_executed": true,
    "cleanup_success": true,
    "github_actions_notified": true
  },
  "error": {
    "test_error": "Optional error message if test failed",
    "cleanup_error": "Optional cleanup error message"
  },
  "repository": {
    "branch": "main",
    "commit": "e07938b8c12345678",
    "ref": "refs/heads/main"
  },
  "environment": {
    "log_file": "/tmp/home-ci/logs/main-e07938b.log",
    "result_file": "/tmp/home-ci/logs/main-e07938b.result",
    "source": "home-ci",
    "version": "1.0"
  },
  "steps": [
    {
      "step_number": 1,
      "name": "Install Dependencies",
      "start_time": "2024-01-02T15:04:05Z",
      "end_time": "2024-01-02T15:04:25Z",
      "duration": "20s",
      "duration_seconds": 20.0,
      "success": true,
      "exit_code": 0,
      "output_lines": 15,
      "error_output": ""
    },
    {
      "step_number": 2,
      "name": "Build",
      "start_time": "2024-01-02T15:04:25Z",
      "end_time": "2024-01-02T15:05:10Z",
      "duration": "45s",
      "duration_seconds": 45.0,
      "success": true,
      "exit_code": 0,
      "output_lines": 32,
      "error_output": ""
    },
    {
      "step_number": 3,
      "name": "Test",
      "start_time": "2024-01-02T15:05:10Z",
      "end_time": "2024-01-02T15:06:00Z",
      "duration": "50s",
      "duration_seconds": 50.0,
      "success": true,
      "exit_code": 0,
      "output_lines": 28,
      "error_output": ""
    },
    {
      "step_number": 4,
      "name": "Docker Build",
      "start_time": "2024-01-02T15:06:00Z",
      "end_time": "2024-01-02T15:06:20Z",
      "duration": "20s",
      "duration_seconds": 20.0,
      "success": true,
      "exit_code": 0,
      "output_lines": 12,
      "error_output": ""
    },
    {
      "step_number": 5,
      "name": "Docker Push",
      "start_time": "2024-01-02T15:06:20Z",
      "end_time": "2024-01-02T15:06:30Z",
      "duration": "10s",
      "duration_seconds": 10.0,
      "success": false,
      "exit_code": 1,
      "output_lines": 5,
      "error_output": "Error: authentication failed\nPush failed with exit code 1"
    }
  ]
}
```

## Usage in GitHub Actions Workflows

The receiving workflow can access this enhanced information including build steps:

```yaml
- name: Display enhanced test metadata
  run: |
    echo "⏱️  Start Time: ${{ github.event.client_payload.execution.start_time }}"
    echo "⏱️  Duration: ${{ github.event.client_payload.execution.duration }}"
    echo "🔧 Cleanup Success: ${{ github.event.client_payload.status.cleanup_success }}"

    if [ "${{ github.event.client_payload.error.test_error }}" != "null" ]; then
      echo "❌ Test Error: ${{ github.event.client_payload.error.test_error }}"
    fi

- name: Display build steps
  run: |
    # Check if steps information is available
    if [ "${{ toJson(github.event.client_payload.steps) }}" != "null" ]; then
      echo "📋 Build Steps Summary:"
      echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

      # Parse and display each step
      echo '${{ toJson(github.event.client_payload.steps) }}' | jq -r '.[] |
        "🔸 Step \(.step_number): \(.name)" +
        "   ⏱️  Duration: \(.duration) (\(.duration_seconds)s)" +
        "   " + (if .success then "✅ Success" else "❌ Failed (exit \(.exit_code))") +
        (if .error_output != "" then "\n   ⚠️  Error: \(.error_output)" else "")'

      echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    fi

- name: Create step summary table
  run: |
    # Generate a markdown table for GitHub Actions summary
    echo "| Step | Name | Duration | Status |" >> $GITHUB_STEP_SUMMARY
    echo "|------|------|----------|--------|" >> $GITHUB_STEP_SUMMARY

    echo '${{ toJson(github.event.client_payload.steps) }}' | jq -r '.[] |
      "| " + (.step_number|tostring) + " | " + .name + " | " + .duration + " | " +
      (if .success then "✅ Success" else "❌ Failed") + " |"' >> $GITHUB_STEP_SUMMARY
```

## Build Steps Parsing

The system automatically parses build steps from log files by looking for common patterns:

### Recognized Step Patterns
- `Step:` - Explicit step markers
- `Installing` - Package installation steps
- `Building`, `go build` - Build steps
- `Testing`, `go test` - Test execution
- `docker build` - Docker image building
- `docker push`, `Pushing` - Docker image pushing
- `kubectl` - Kubernetes deployment
- `npm install` - NPM package installation
- `npm run` - NPM script execution
- `make` - Makefile targets

### Step Information Extracted
For each detected step:
- **Step number**: Sequential numbering
- **Name**: Clean, descriptive step name
- **Timing**: Start time, end time, duration
- **Status**: Success/failure with exit codes
- **Output**: Number of output lines
- **Errors**: Error messages if step failed

## Customization Options

You can further customize the payload and step parsing:

### 1. Add Custom Step Patterns
Modify `stepPatterns` in `parseBuildStepsFromLog()`:

```go
stepPatterns := []string{
    "Step:",
    "Running:",
    "=== ",
    // Add your custom patterns
    "Deploying to",
    "Running integration tests",
    "Publishing artifacts",
}
```

### 2. Custom Step Name Extraction
Enhance `extractStepName()` function:

```go
if strings.Contains(cleaned, "your-custom-command") {
    return "Custom Step Name"
}
```

### 3. Add Build Information
```go
// Add build information
payload["build"] = map[string]interface{}{
    "version":     getBuildVersion(),
    "build_number": getBuildNumber(),
    "git_sha":      getGitSHA(),
}

// Add test metrics
payload["metrics"] = map[string]interface{}{
    "test_count":    getTestCount(),
    "failed_tests":  getFailedTestCount(),
    "coverage":      getCoveragePercentage(),
}

// Add environment information
payload["environment"]["os"] = runtime.GOOS
payload["environment"]["arch"] = runtime.GOARCH
payload["environment"]["go_version"] = runtime.Version()
```

## Implementation Details

- The enhanced payload is automatically used when `TestResult` is available
- Backward compatibility is maintained - existing workflows will continue to work
- The basic payload is used as fallback when enhanced information is not available
- All timestamp fields use RFC3339 format for consistency
- Duration is provided in both human-readable format and seconds for flexibility