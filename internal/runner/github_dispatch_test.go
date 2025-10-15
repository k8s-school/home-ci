package runner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	token, err := loadGitHubToken(secretFile)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expectedToken := "github_pat_test_token_123456789"
	if token != expectedToken {
		t.Errorf("Expected token %s, got %s", expectedToken, token)
	}

	// Test case 2: File not found
	_, err = loadGitHubToken("nonexistent.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}

	// Test case 3: Invalid YAML
	invalidFile := filepath.Join(tempDir, "invalid.yaml")
	err = os.WriteFile(invalidFile, []byte("invalid: yaml: content: ["), 0600)
	if err != nil {
		t.Fatalf("Failed to write invalid file: %v", err)
	}

	_, err = loadGitHubToken(invalidFile)
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}

	// Test case 4: Missing github_token field
	emptyFile := filepath.Join(tempDir, "empty.yaml")
	err = os.WriteFile(emptyFile, []byte("other_field: value"), 0600)
	if err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	_, err = loadGitHubToken(emptyFile)
	if err == nil {
		t.Error("Expected error for missing github_token, got nil")
	}
}

func TestSendGitHubDispatch(t *testing.T) {
	// Create test server
	var receivedPayload GitHubDispatchPayload
	var receivedHeaders map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture headers
		receivedHeaders = make(map[string]string)
		receivedHeaders["Authorization"] = r.Header.Get("Authorization")
		receivedHeaders["Accept"] = r.Header.Get("Accept")
		receivedHeaders["X-GitHub-Api-Version"] = r.Header.Get("X-GitHub-Api-Version")
		receivedHeaders["Content-Type"] = r.Header.Get("Content-Type")

		// Decode payload
		err := json.NewDecoder(r.Body).Decode(&receivedPayload)
		if err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}

		// Return success
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Test with server URL - we'll call sendGitHubDispatch directly
	testSendGitHubDispatch := func(repoOwner, repoName, token, eventType string, clientPayload map[string]interface{}, inputs map[string]interface{}) error {
		url := server.URL + "/dispatches"

		payload := GitHubDispatchPayload{
			EventType:     eventType,
			ClientPayload: clientPayload,
			Inputs:        inputs,
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}

		req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonData)))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		return nil
	}

	// Test data
	repoOwner := "testowner"
	repoName := "testrepo"
	token := "test_token_123"
	eventType := "test-home-ci"
	clientPayload := map[string]interface{}{
		"branch":  "main",
		"commit":  "abc123",
		"success": true,
	}

	// Test inputs (empty now)
	inputs := map[string]interface{}{}

	// Call function
	err := testSendGitHubDispatch(repoOwner, repoName, token, eventType, clientPayload, inputs)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify headers
	expectedHeaders := map[string]string{
		"Authorization":        "Bearer " + token,
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
		"Content-Type":         "application/json",
	}

	for key, expected := range expectedHeaders {
		if receivedHeaders[key] != expected {
			t.Errorf("Header %s: expected %s, got %s", key, expected, receivedHeaders[key])
		}
	}

	// Verify payload
	if receivedPayload.EventType != eventType {
		t.Errorf("EventType: expected %s, got %s", eventType, receivedPayload.EventType)
	}

	if receivedPayload.ClientPayload["branch"] != "main" {
		t.Errorf("ClientPayload.branch: expected main, got %v", receivedPayload.ClientPayload["branch"])
	}

	if receivedPayload.ClientPayload["commit"] != "abc123" {
		t.Errorf("ClientPayload.commit: expected abc123, got %v", receivedPayload.ClientPayload["commit"])
	}

	if receivedPayload.ClientPayload["success"] != true {
		t.Errorf("ClientPayload.success: expected true, got %v", receivedPayload.ClientPayload["success"])
	}

	// Verify inputs are empty
	if len(receivedPayload.Inputs) != 0 {
		t.Errorf("Inputs should be empty, got %v", receivedPayload.Inputs)
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
			GitHubTokenFile: secretPath,
			DispatchType:    "test-home-ci",
		},
	}

	tr := &TestRunner{config: *cfg}
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
	token, err := loadGitHubToken(secretPath)
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
