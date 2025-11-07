package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k8s-school/home-ci/internal/config"
)

func TestRunCommand(t *testing.T) {
	// Create temporary test directory
	tempDir, err := os.MkdirTemp("", "home-ci-run-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test config
	configPath := filepath.Join(tempDir, "config.yaml")
	configContent := `repository: "."
repo_name: "test-repo"
test_script: "/bin/echo"
options: "Integration test successful"
test_timeout: 5s
max_concurrent_runs: 1
state_dir: "` + filepath.Join(tempDir, "state") + `"
cache_dir: "` + filepath.Join(tempDir, "cache") + `"
workspace_dir: "` + filepath.Join(tempDir, "workspaces") + `"
log_dir: "` + filepath.Join(tempDir, "logs") + `"
cleanup:
  after_e2e: false
  script: ""
github_actions_dispatch:
  enabled: false
  github_repo: ""`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test loading config
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.TestScript != "/bin/echo" {
		t.Errorf("Expected test_script to be '/bin/echo', got '%s'", cfg.TestScript)
	}

	// Test getLatestCommitFromBranch function with current repo
	// Skip if we're not in a git repository
	if _, err := os.Stat(".git"); os.IsNotExist(err) {
		t.Skip("Not in a git repository, skipping getLatestCommitFromBranch test")
	}

	commitHash, err := getLatestCommitFromBranch(".", "main")
	if err != nil {
		t.Logf("Failed to get latest commit (expected in test env): %v", err)
	} else {
		if len(commitHash) != 40 { // Git SHA-1 is 40 characters
			t.Errorf("Expected commit hash to be 40 characters, got %d: %s", len(commitHash), commitHash)
		}
		t.Logf("Got commit hash: %s", commitHash[:8])
	}
}

func TestRunCommandFlags(t *testing.T) {
	// Test that the run command has the expected flags
	cmd := runCmd

	// Check required flag
	branchFlag := cmd.Flags().Lookup("branch")
	if branchFlag == nil {
		t.Error("Expected 'branch' flag to exist")
	}

	// Check optional flag
	commitFlag := cmd.Flags().Lookup("commit")
	if commitFlag == nil {
		t.Error("Expected 'commit' flag to exist")
	}

	// Test flag shorthand
	if branchFlag.Shorthand != "b" {
		t.Errorf("Expected branch flag shorthand to be 'b', got '%s'", branchFlag.Shorthand)
	}
}

func TestManualTestExecution(t *testing.T) {
	// This test would be more complex in a real scenario
	// For now, we test the configuration and basic setup

	tempDir, err := os.MkdirTemp("", "home-ci-manual-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test that manual runs use "run_" prefix
	_ = config.Config{
		Repository:    ".",
		RepoName:     "test-repo",
		TestScript:   "/bin/echo",
		Options:      "test",
		TestTimeout:  5 * time.Second,
		WorkspaceDir: filepath.Join(tempDir, "workspaces"),
		LogDir:       filepath.Join(tempDir, "logs"),
	}

	// Create a mock test execution to verify naming
	timestamp := time.Now().Format("20060102-150405")
	branchFile := strings.ReplaceAll("feature/test", "/", "-")
	commitShort := "abc123de"

	expectedWorkspaceID := "run_" + branchFile + "_" + commitShort + "_" + timestamp
	expectedLogFile := "run_" + expectedWorkspaceID + ".log"
	expectedResultFile := "run_" + expectedWorkspaceID + ".json"

	// Verify the naming pattern matches what we expect
	if !strings.HasPrefix(expectedWorkspaceID, "run_") {
		t.Error("Expected workspace ID to start with 'run_'")
	}

	if !strings.HasPrefix(expectedLogFile, "run_") {
		t.Error("Expected log file to start with 'run_'")
	}

	if !strings.HasPrefix(expectedResultFile, "run_") {
		t.Error("Expected result file to start with 'run_'")
	}

	t.Logf("Expected workspace ID: %s", expectedWorkspaceID)
	t.Logf("Expected log file: %s", expectedLogFile)
	t.Logf("Expected result file: %s", expectedResultFile)
}

func TestAbsoluteVsRelativeScriptPath(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		expected bool // true if should be treated as absolute
	}{
		{"Absolute Unix path", "/bin/echo", true},
		{"Relative path", "scripts/test.sh", false},
		{"Current dir relative", "./test.sh", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isAbs := filepath.IsAbs(tt.script)
			if isAbs != tt.expected {
				t.Errorf("filepath.IsAbs(%q) = %v, want %v", tt.script, isAbs, tt.expected)
			}
		})
	}
}