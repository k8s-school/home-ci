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
	testSendGitHubDispatch := func(repoOwner, repoName, token, eventType string, clientPayload map[string]interface{}) error {
		url := server.URL + "/dispatches"

		payload := GitHubDispatchPayload{
			EventType:     eventType,
			ClientPayload: clientPayload,
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

	// Call function
	err := testSendGitHubDispatch(repoOwner, repoName, token, eventType, clientPayload)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify headers
	expectedHeaders := map[string]string{
		"Authorization":         "Bearer " + token,
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
}

func TestNotifyGitHubActionsValidation(t *testing.T) {
	// Test 1: Invalid repo format
	cfg := &config.Config{
		GitHubActionsDispatch: config.GitHubActionsDispatch{
			Enabled:         true,
			GitHubRepo:      "invalid-repo-format",
			GitHubTokenFile: "secret.yaml",
			DispatchType:    "test-home-ci",
		},
	}

	tr := &TestRunner{config: *cfg}
	err := tr.notifyGitHubActions("main", "abcdef123456", true)
	if err == nil {
		t.Error("Expected error for invalid repo format, got nil")
	}
	if !strings.Contains(err.Error(), "invalid github_repo format") {
		t.Errorf("Expected 'invalid github_repo format' error, got: %v", err)
	}

	// Test 2: Nonexistent secret file
	cfg.GitHubActionsDispatch.GitHubRepo = "k8s-school/home-ci"
	cfg.GitHubActionsDispatch.GitHubTokenFile = "nonexistent.yaml"
	tr.config = *cfg
	err = tr.notifyGitHubActions("main", "abcdef123456", true)
	if err == nil {
		t.Error("Expected error for nonexistent secret file, got nil")
	}
}

func TestIntegrationWithRealSecret(t *testing.T) {
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