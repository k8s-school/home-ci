package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k8s-school/home-ci/internal/config"
)

func TestLoadGitHubToken(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "github_dispatch_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test case 1: Valid secret file
	secretFile := filepath.Join(tempDir, "secret.yaml")
	secretContent := "github_token: github_pat_test_token_123456789"
	err = os.WriteFile(secretFile, []byte(secretContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write secret file: %v", err)
	}

	// Test with absolute path
	token, err := loadGitHubToken(secretFile, "")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expectedToken := "github_pat_test_token_123456789"
	if token != expectedToken {
		t.Errorf("Expected token %s, got %s", expectedToken, token)
	}

	// Test with relative path and config directory
	relativeSecretFile := "secret.yaml"
	token, err = loadGitHubToken(relativeSecretFile, tempDir)
	if err != nil {
		t.Fatalf("Expected no error with relative path, got: %v", err)
	}
	if token != expectedToken {
		t.Errorf("Expected token %s with relative path, got %s", expectedToken, token)
	}

	// Test case 2: File not found - absolute path
	_, err = loadGitHubToken("nonexistent.yaml", "")
	if err == nil {
		t.Error("Expected error for nonexistent file with absolute path, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read secret file") {
		t.Errorf("Expected 'failed to read secret file' error, got: %v", err)
	}

	// Test case 3: File not found - relative path in non-existent directory
	_, err = loadGitHubToken("secret.yaml", "/nonexistent/directory")
	if err == nil {
		t.Error("Expected error for nonexistent directory, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read secret file") {
		t.Errorf("Expected 'failed to read secret file' error, got: %v", err)
	}

	// Test case 4: File not found - relative path in existing directory but file doesn't exist
	_, err = loadGitHubToken("nonexistent.yaml", tempDir)
	if err == nil {
		t.Error("Expected error for nonexistent file in valid directory, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read secret file") {
		t.Errorf("Expected 'failed to read secret file' error, got: %v", err)
	}

	// Test case 5: Invalid YAML
	invalidFile := filepath.Join(tempDir, "invalid.yaml")
	err = os.WriteFile(invalidFile, []byte("invalid: yaml: content: ["), 0600)
	if err != nil {
		t.Fatalf("Failed to write invalid file: %v", err)
	}

	_, err = loadGitHubToken(invalidFile, "")
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse secret file") {
		t.Errorf("Expected 'failed to parse secret file' error, got: %v", err)
	}

	// Test case 6: Missing github_token field
	emptyFile := filepath.Join(tempDir, "empty.yaml")
	err = os.WriteFile(emptyFile, []byte("other_field: value"), 0600)
	if err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	_, err = loadGitHubToken(emptyFile, "")
	if err == nil {
		t.Error("Expected error for missing github_token, got nil")
	}
	if !strings.Contains(err.Error(), "github_token not found in secret file") {
		t.Errorf("Expected 'github_token not found' error, got: %v", err)
	}
}


func TestIntegrationNotifyGitHubActionsValidation(t *testing.T) {
	// This test validates the notifyGitHubActions function with real dispatch including artifacts
	secretPath := "../../secret.yaml"
	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		t.Skip("Skipping GitHub Actions integration test: secret.yaml not found")
	}

	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "github_dispatch_artifacts_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test log file
	logFilePath := filepath.Join(tempDir, "test_20241015_123456_main_abcdef12.log")
	logContent := `Starting test run...
Running tests for branch main
Test 1: PASS
Test 2: FAIL - Expected value 42, got 24
Test 3: PASS
Test summary: 2/3 tests passed
`
	err = os.WriteFile(logFilePath, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write log file: %v", err)
	}

	// Create test result JSON file
	resultFilePath := filepath.Join(tempDir, "test_20241015_123456_main_abcdef12.json")
	resultContent := `{
  "timestamp": "2024-10-15T12:34:56Z",
  "branch": "main",
  "commit": "abcdef123456",
  "tests": {
    "total": 3,
    "passed": 2,
    "failed": 1,
    "skipped": 0
  },
  "duration": "45.2s",
  "success": false,
  "failures": [
    {
      "test": "Test 2",
      "error": "Expected value 42, got 24"
    }
  ]
}`
	err = os.WriteFile(resultFilePath, []byte(resultContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write result file: %v", err)
	}

	// Test with valid configuration and artifacts
	cfg := &config.Config{
		GitHubActionsDispatch: config.GitHubActionsDispatch{
			Enabled:         true,
			GitHubRepo:      "k8s-school/home-ci",
			GitHubTokenFile: "secret.yaml", // Use relative path so it gets resolved via config directory
			DispatchType:    "test-home-ci-with-artifacts",
		},
	}

	tr := &TestRunner{
		config:     *cfg,
		configPath: "/home/fjammes/src/github.com/k8s-school/home-ci/some-config.yaml", // Mock config path in project root
	}
	err = tr.notifyGitHubActions("main", "abcdef123456", false, logFilePath, resultFilePath)
	if err != nil {
		t.Fatalf("Expected no error for valid dispatch with artifacts, got: %v", err)
	}

	t.Logf("Successfully sent GitHub dispatch with artifacts")
	t.Logf("Log file: %s (%d bytes)", filepath.Base(logFilePath), len(logContent))
	t.Logf("Result file: %s (%d bytes)", filepath.Base(resultFilePath), len(resultContent))
	t.Logf("Check GitHub Actions tab for the dispatch event with artifacts")
}

func TestIntegrationLoadGitHubToken(t *testing.T) {
	// This test reads the actual secret.yaml file if it exists
	secretPath := "../../secret.yaml"
	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		t.Skip("Skipping integration test: secret.yaml not found")
	}

	// Test loading the real secret file
	token, err := loadGitHubToken(secretPath, "")
	if err != nil {
		t.Fatalf("Failed to load GitHub token from real secret.yaml: %v", err)
	}

	if token == "" {
		t.Error("Token should not be empty")
	}

	if !strings.HasPrefix(token, "github_pat_") {
		t.Errorf("Token should start with 'github_pat_', got: %s", token[:20]+"...")
	}

	t.Logf("Successfully loaded token from secret.yaml (length: %d)", len(token))
}
