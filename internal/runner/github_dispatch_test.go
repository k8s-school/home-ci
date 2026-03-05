package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
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

func TestCreateClientPayload(t *testing.T) {
	testCases := []struct {
		name                 string
		branch               string
		commit               string
		success              bool
		expectedArtifactName string
	}{
		{
			name:                 "main branch with long commit",
			branch:               "main",
			commit:               "9db6aa4a3510ebb74d92600c37dd1a529dd3d28e",
			success:              true,
			expectedArtifactName: "log-main-9db6aa4a",
		},
		{
			name:                 "feature branch with slash",
			branch:               "feature/test-fail",
			commit:               "e5b1eb34d902e067acf832dc97ecd407ab8988bc",
			success:              false,
			expectedArtifactName: "log-feature_test-fail-e5b1eb34",
		},
		{
			name:                 "bugfix branch with slash",
			branch:               "bugfix/timeout",
			commit:               "699226c8754caa8ca73bcdea567633342559c01e",
			success:              true,
			expectedArtifactName: "log-bugfix_timeout-699226c8",
		},
		{
			name:                 "short commit hash",
			branch:               "develop",
			commit:               "abcd123",
			success:              true,
			expectedArtifactName: "log-develop-abcd123",
		},
		{
			name:                 "multiple slashes in branch name",
			branch:               "feature/nested/branch",
			commit:               "1234567890abcdef",
			success:              false,
			expectedArtifactName: "log-feature_nested_branch-12345678",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := createClientPayload(tc.branch, tc.commit, tc.success, "", "", false, 20*1024, 1000)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Check that artifact_name is present in payload
			artifactName, exists := payload["artifact_name"]
			if !exists {
				t.Error("Expected artifact_name to be present in payload")
				return
			}

			artifactNameStr, ok := artifactName.(string)
			if !ok {
				t.Errorf("Expected artifact_name to be string, got %T", artifactName)
				return
			}

			if artifactNameStr != tc.expectedArtifactName {
				t.Errorf("Expected artifact_name %s, got %s", tc.expectedArtifactName, artifactNameStr)
			}

			// Verify other expected fields are present
			expectedFields := []string{"branch", "commit", "success", "timestamp", "source", "artifacts", "metadata"}
			for _, field := range expectedFields {
				if _, exists := payload[field]; !exists {
					t.Errorf("Expected field %s to be present in payload", field)
				}
			}

			// Verify branch and commit values
			if payload["branch"] != tc.branch {
				t.Errorf("Expected branch %s, got %s", tc.branch, payload["branch"])
			}
			if payload["commit"] != tc.commit {
				t.Errorf("Expected commit %s, got %s", tc.commit, payload["commit"])
			}
			if payload["success"] != tc.success {
				t.Errorf("Expected success %v, got %v", tc.success, payload["success"])
			}
		})
	}
}

func TestCreateCompressedArtifactsArchive(t *testing.T) {
	// Test data
	testFiles := []FileToArchive{
		{
			Name:         "test.log",
			Data:         []byte("This is a test log file\nwith multiple lines\nof content"),
			Type:         "log",
			Truncated:    false,
			OriginalSize: 55,
		},
		{
			Name:         "result.json",
			Data:         []byte(`{"test": "passed", "duration": "5s"}`),
			Type:         "result",
			Truncated:    false,
			OriginalSize: 35,
		},
		{
			Name:         "report.yaml",
			Data:         []byte("status: success\ncount: 42"),
			Type:         "e2e-report",
			Truncated:    true,
			OriginalSize: 100,
		},
	}

	// Create archive
	artifact, err := createCompressedArtifactsArchive(testFiles)
	if err != nil {
		t.Fatalf("Failed to create archive: %v", err)
	}

	// Check artifact properties
	if artifact.Type != "archive" {
		t.Errorf("Expected type 'archive', got '%s'", artifact.Type)
	}
	if !artifact.Compressed {
		t.Error("Expected artifact to be marked as compressed")
	}
	if !artifact.Truncated {
		t.Error("Expected artifact to be marked as truncated (since one file was truncated)")
	}
	if artifact.OriginalSize != 190 {
		t.Errorf("Expected original size 190, got %d", artifact.OriginalSize)
	}
	if len(artifact.Files) != 3 {
		t.Errorf("Expected 3 file metadata entries, got %d", len(artifact.Files))
	}

	// Decode and verify archive content
	decodedData, err := base64.StdEncoding.DecodeString(artifact.Content)
	if err != nil {
		t.Fatalf("Failed to decode base64 content: %v", err)
	}

	// Decompress gzip
	gz, err := gzip.NewReader(bytes.NewReader(decodedData))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gz.Close()

	// Read tar archive
	tr := tar.NewReader(gz)
	filesFound := make(map[string][]byte)

	for {
		hdr, err := tr.Next()
		if err != nil {
			break // End of archive
		}

		var buf bytes.Buffer
		if _, err := buf.ReadFrom(tr); err != nil {
			t.Fatalf("Failed to read file data from tar: %v", err)
		}
		filesFound[hdr.Name] = buf.Bytes()
	}

	// Verify all files are present with correct content
	if len(filesFound) != 3 {
		t.Errorf("Expected 3 files in archive, found %d", len(filesFound))
	}

	for _, expectedFile := range testFiles {
		actualData, found := filesFound[expectedFile.Name]
		if !found {
			t.Errorf("File %s not found in archive", expectedFile.Name)
			continue
		}
		if !bytes.Equal(actualData, expectedFile.Data) {
			t.Errorf("Content mismatch for file %s. Expected %q, got %q",
				expectedFile.Name, string(expectedFile.Data), string(actualData))
		}
	}

	// Verify file metadata
	for i, expectedFile := range testFiles {
		if i >= len(artifact.Files) {
			t.Errorf("Missing metadata for file %s", expectedFile.Name)
			continue
		}
		meta := artifact.Files[i]
		if meta.Name != expectedFile.Name {
			t.Errorf("Metadata name mismatch: expected %s, got %s", expectedFile.Name, meta.Name)
		}
		if meta.Type != expectedFile.Type {
			t.Errorf("Metadata type mismatch for %s: expected %s, got %s", expectedFile.Name, expectedFile.Type, meta.Type)
		}
		if meta.Truncated != expectedFile.Truncated {
			t.Errorf("Metadata truncated mismatch for %s: expected %v, got %v", expectedFile.Name, expectedFile.Truncated, meta.Truncated)
		}
		if meta.OriginalSize != expectedFile.OriginalSize {
			t.Errorf("Metadata original size mismatch for %s: expected %d, got %d", expectedFile.Name, expectedFile.OriginalSize, meta.OriginalSize)
		}
	}
}

func TestCreateCompressedArtifactsArchiveEmpty(t *testing.T) {
	_, err := createCompressedArtifactsArchive([]FileToArchive{})
	if err == nil {
		t.Error("Expected error for empty files list")
	}
	if !strings.Contains(err.Error(), "no files to archive") {
		t.Errorf("Expected 'no files to archive' error, got: %v", err)
	}
}

func TestReadFileForArchive(t *testing.T) {
	// Create temporary file for test
	tempDir, err := os.MkdirTemp("", "archive_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testContent := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	testFile := filepath.Join(tempDir, "test.log")
	err = os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test reading without truncation
	file, err := readFileForArchive(testFile, 1000, 1000, "log")
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if file.Name != "test.log" {
		t.Errorf("Expected name 'test.log', got '%s'", file.Name)
	}
	if file.Type != "log" {
		t.Errorf("Expected type 'log', got '%s'", file.Type)
	}
	if file.Truncated {
		t.Error("Expected file not to be truncated")
	}
	if string(file.Data) != testContent {
		t.Errorf("Content mismatch. Expected %q, got %q", testContent, string(file.Data))
	}
	if file.OriginalSize != len(testContent) {
		t.Errorf("Expected original size %d, got %d", len(testContent), file.OriginalSize)
	}

	// Test reading with line truncation (keep last 2 lines)
	file, err = readFileForArchive(testFile, 1000, 2, "log")
	if err != nil {
		t.Fatalf("Failed to read file with line truncation: %v", err)
	}

	if !file.Truncated {
		t.Error("Expected file to be truncated")
	}
	expectedTruncated := "Line 4\nLine 5"
	if string(file.Data) != expectedTruncated {
		t.Errorf("Truncated content mismatch. Expected %q, got %q", expectedTruncated, string(file.Data))
	}
	if file.OriginalSize != len(testContent) {
		t.Errorf("Expected original size %d, got %d", len(testContent), file.OriginalSize)
	}

	// Test reading with byte truncation
	file, err = readFileForArchive(testFile, 10, 1000, "log")
	if err != nil {
		t.Fatalf("Failed to read file with byte truncation: %v", err)
	}

	if !file.Truncated {
		t.Error("Expected file to be truncated")
	}
	if len(file.Data) > 10 {
		t.Errorf("Expected data length <= 10, got %d", len(file.Data))
	}
	if file.OriginalSize != len(testContent) {
		t.Errorf("Expected original size %d, got %d", len(testContent), file.OriginalSize)
	}
}

func TestCreateArtifactsMapCombinedArchive(t *testing.T) {
	// Create temporary directory and files for test
	tempDir, err := os.MkdirTemp("", "artifacts_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files
	logFile := filepath.Join(tempDir, "test.log")
	logContent := "Test log content\nwith multiple lines"
	err = os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write log file: %v", err)
	}

	resultFile := filepath.Join(tempDir, "result.json")
	resultContent := `{"status": "success", "count": 5}`
	err = os.WriteFile(resultFile, []byte(resultContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write result file: %v", err)
	}

	// Test with combined archive mode (now the only mode)
	artifacts, err := createArtifactsMap("main", "abc123", true, logFile, resultFile, false, 1000, 100)
	if err != nil {
		t.Fatalf("Failed to create artifacts map: %v", err)
	}

	// Check that we have the combined archive
	archiveArtifact, found := artifacts["combined-archive.tar.gz"]
	if !found {
		t.Error("Expected combined archive artifact not found")
		return
	}

	artifact, ok := archiveArtifact.(Artifact)
	if !ok {
		t.Errorf("Expected Artifact type, got %T", archiveArtifact)
		return
	}

	if artifact.Type != "archive" {
		t.Errorf("Expected archive type, got %s", artifact.Type)
	}
	if !artifact.Compressed {
		t.Error("Expected archive to be compressed")
	}
	if len(artifact.Files) < 2 {
		t.Errorf("Expected at least 2 files in archive metadata, got %d", len(artifact.Files))
	}

	// Check metadata artifact is still present
	if _, found := artifacts["metadata"]; !found {
		t.Error("Expected metadata artifact not found")
	}

}
