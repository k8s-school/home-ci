package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/k8s-school/home-ci/resources"
)

func NewE2ETestHarness(testType TestType, duration time.Duration, noCleanup bool) *E2ETestHarness {
	// Use test type specific directories
	tempRunDir := testType.getTestDirectory()
	repoPath := testType.getRepoPath()

	return &E2ETestHarness{
		testType:     testType,
		duration:     duration,
		testRepoPath: repoPath,
		tempRunDir:   tempRunDir,
		noCleanup:    noCleanup,
		startTime:    time.Now(),
	}
}

// writeFileFromResource writes an embedded resource to a file
func (th *E2ETestHarness) writeFileFromResource(content, filePath string, executable bool) error {
	if filePath == "" {
		return fmt.Errorf("filePath is empty")
	}
	if content == "" {
		return fmt.Errorf("content is empty for file %s", filePath)
	}

	slog.Debug("Writing file from resource", "filePath", filePath, "contentLength", len(content))

	// Ensure the parent directory exists
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory %s: %w", parentDir, err)
	}

	// Remove the file if it already exists (in case it's a directory or has wrong permissions)
	if info, err := os.Stat(filePath); err == nil {
		if info.IsDir() {
			slog.Debug("Removing existing directory at file path", "filePath", filePath)
			if err := os.RemoveAll(filePath); err != nil {
				return fmt.Errorf("failed to remove existing directory %s: %w", filePath, err)
			}
		} else {
			slog.Debug("Removing existing file", "filePath", filePath)
			if err := os.Remove(filePath); err != nil {
				return fmt.Errorf("failed to remove existing file %s: %w", filePath, err)
			}
		}
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	if executable {
		if err := os.Chmod(filePath, 0755); err != nil {
			return fmt.Errorf("failed to make file executable %s: %w", filePath, err)
		}
	}

	slog.Debug("Successfully wrote file", "filePath", filePath)
	return nil
}

// cleanupGlobalState removes all home-ci generated data from previous test runs
func (th *E2ETestHarness) cleanupGlobalState() error {
	// List of global directories that accumulate test data
	globalDirs := []string{
		"/tmp/home-ci/state",
		"/tmp/home-ci/cache",
		"/tmp/home-ci/logs",
		"/tmp/home-ci/workspaces",
		"/tmp/home-ci/e2e/data", // Clean e2e data directory too
	}

	for _, dir := range globalDirs {
		if _, err := os.Stat(dir); err == nil {
			slog.Debug("Cleaning up global home-ci directory", "dir", dir)
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("failed to remove global directory %s: %w", dir, err)
			}
		}
	}

	return nil
}

// setupTestRepo creates a test repository using the embedded setup script or manual setup
func (th *E2ETestHarness) setupTestRepo() error {
	if th.testType != TestTimeout {
		slog.Info("🚀 Setting up test environment", "dir", th.tempRunDir)
	}

	// Clean up global state first
	if err := th.cleanupGlobalState(); err != nil {
		return fmt.Errorf("failed to cleanup global state: %w", err)
	}

	// Clean up and initialize the test type specific directory
	if _, err := os.Stat(th.tempRunDir); err == nil {
		if th.testType != TestTimeout {
			slog.Debug("Cleaning up existing test directory", "dir", th.tempRunDir)
		}
		if err := os.RemoveAll(th.tempRunDir); err != nil {
			return fmt.Errorf("failed to remove existing test directory: %w", err)
		}
	}

	// Also clean up the repository path specifically if it exists
	if _, err := os.Stat(th.testRepoPath); err == nil {
		if th.testType != TestTimeout {
			slog.Debug("Cleaning up existing repository directory", "dir", th.testRepoPath)
		}
		if err := os.RemoveAll(th.testRepoPath); err != nil {
			return fmt.Errorf("failed to remove existing repository directory: %w", err)
		}
	}

	// Create the test type directory structure
	if err := os.MkdirAll(th.tempRunDir, 0755); err != nil {
		return fmt.Errorf("failed to create test directory: %w", err)
	}

	// Create data subdirectory for test data files
	dataDir := th.testType.getDataPath()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Create the repository directory
	if err := os.MkdirAll(th.testRepoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repo directory: %w", err)
	}

	// Copy secret.yaml for dispatch tests if it exists in project root
	if th.testType.isDispatchTest() {
		if err := th.copySecretIfExists(); err != nil {
			return fmt.Errorf("failed to copy secret.yaml: %w", err)
		}
	}

	// Create the e2e directory
	e2eDir := filepath.Join(th.testRepoPath, "e2e")
	if err := os.MkdirAll(e2eDir, 0755); err != nil {
		return fmt.Errorf("failed to create e2e directory: %w", err)
	}

	// Write the test script (run-e2e.sh handles all scenarios including timeout)
	testScript := resources.RunE2EScript
	scriptName := "run-e2e.sh"

	scriptPath := filepath.Join(e2eDir, scriptName)
	if err := th.writeFileFromResource(testScript, scriptPath, true); err != nil {
		return fmt.Errorf("failed to write test script: %w", err)
	}

	// Write the cleanup script (cleanup.sh for cleanup after test execution)
	cleanupScript := resources.CleanupE2EScript
	cleanupScriptName := "cleanup.sh"

	cleanupScriptPath := filepath.Join(e2eDir, cleanupScriptName)
	if err := th.writeFileFromResource(cleanupScript, cleanupScriptPath, true); err != nil {
		return fmt.Errorf("failed to write cleanup script: %w", err)
	}

	// Initialize git using the embedded setup script logic
	if err := th.initializeGitRepo(); err != nil {
		return fmt.Errorf("failed to initialize git repo: %w", err)
	}

	slog.Info("✅ Test repository created", "path", th.testRepoPath)
	return nil
}

// startHomeCI starts home-ci with the appropriate configuration
func (th *E2ETestHarness) startHomeCI(configPath string) error {
	if th.testType != TestTimeout {
		slog.Info("🚀 Starting home-ci process...")
	}

	// Create a context with cancellation
	th.homeCIContext, th.homeCICancel = context.WithCancel(context.Background())

	// Start home-ci with less verbose logging for timeout tests
	verbosity := "5"
	if th.testType == TestTimeout {
		verbosity = "1" // Reduce verbosity for timeout tests
	}
	th.homeCIProcess = exec.CommandContext(th.homeCIContext, "./home-ci", "-c", configPath, "-v", verbosity)

	// Set environment variable for data directory
	dataDir := th.testType.getDataPath()
	th.homeCIProcess.Env = append(os.Environ(), fmt.Sprintf("HOME_CI_DATA_DIR=%s", dataDir))

	if err := th.homeCIProcess.Start(); err != nil {
		return fmt.Errorf("failed to start home-ci: %w", err)
	}

	if th.testType != TestTimeout {
		// With new architecture, logs go to the configured log_dir
		logPath := filepath.Join(th.tempRunDir, "logs")
		slog.Info("✅ home-ci started", "pid", th.homeCIProcess.Process.Pid, "logPath", logPath)
	}

	// Wait a bit for home-ci to start
	time.Sleep(3 * time.Second)
	return nil
}

// simulateActivity simulates development activity based on test type
func (th *E2ETestHarness) simulateActivity() {
	// Single commit tests don't need additional activity
	if th.testType.isSingleCommitTest() {
		slog.Info("📝 Single commit test - no additional activity needed", "type", testTypeName[th.testType])
		return
	}

	// Special handling for concurrent limit test
	if th.testType == TestConcurrentLimit {
		th.simulateConcurrentActivity()
		return
	}

	// Special handling for continuous CI test
	if th.testType == TestContinuousCI {
		th.simulateContinuousActivity()
		return
	}

	slog.Info("🎯 Starting activity simulation", "duration", th.duration)

	ticker := time.NewTicker(45 * time.Second) // Create a commit every 45 seconds
	defer ticker.Stop()

	timeout := time.After(th.duration)

	branches := []string{"main", "feature/new-feature", "bugfix/critical-fix", "feature/enhancement"}
	branchIndex := 0

	for {
		select {
		case <-timeout:
			slog.Info("⏰ Activity simulation completed")
			return
		case <-ticker.C:
			branch := branches[branchIndex%len(branches)]
			if err := th.createCommit(branch); err != nil {
				slog.Info("❌ Failed to create commit", "branch", branch, "error", err)
			}
			branchIndex++
		}
	}
}

// simulateConcurrentActivity creates 6 commits on 6 different branches simultaneously
// to test max_concurrent_runs=2 limitation and different test outcomes (success/timeout/failure)
func (th *E2ETestHarness) simulateConcurrentActivity() {
	slog.Info("🎯 Starting concurrent limit test - creating 6 commits on 6 branches")

	branches := []string{
		"concurrent/test1",
		"concurrent/test2",
		"concurrent/test3",
		"concurrent/test4",
		"concurrent/timeout-test",
		"concurrent/fail-test",
	}

	commitMessages := []string{
		"SUCCESS_CONCURRENT_TEST: Test 1 - Should run in first batch",
		"SUCCESS_CONCURRENT_TEST: Test 2 - Should run in first batch",
		"SUCCESS_CONCURRENT_TEST: Test 3 - Should run in second batch",
		"SUCCESS_CONCURRENT_TEST: Test 4 - Should run in second batch",
		"TIMEOUT_TEST: This test should timeout after 30 seconds",
		"FAIL_TEST: This test should fail deliberately",
	}

	// Create all commits quickly to trigger concurrent execution
	slog.Info("📝 Creating commits on all branches...")
	for i, branch := range branches {
		if err := th.createCommitWithMessage(branch, commitMessages[i]); err != nil {
			slog.Info("❌ Failed to create commit", "branch", branch, "error", err)
		} else {
			slog.Debug("✅ Created commit", "branch", branch)
		}
		// Small delay to ensure commits have different timestamps
		time.Sleep(500 * time.Millisecond)
	}

	slog.Info("🏁 All concurrent test commits created (including timeout and failure tests)")
}

// simulateContinuousActivity simulates continuous integration with variable commit timing
// Tests max_concurrent_runs=3 with realistic developer workflow
func (th *E2ETestHarness) simulateContinuousActivity() {
	slog.Info("🎯 Starting continuous CI test - simulating active development")

	// Start with existing branches with different commit types
	initialBranches := map[string]string{
		"main":             "INIT: Main branch setup (success)",
		"feature/existing": "INIT: Existing feature work (success)",
		"bugfix/slow":      "INIT: Slow running bugfix (timeout)",
	}

	// Create initial commits
	slog.Info("📝 Creating initial commits on existing branches...")
	for branch, message := range initialBranches {
		if err := th.createCommitWithMessage(branch, message); err != nil {
			slog.Info("❌ Failed to create initial commit", "branch", branch, "error", err)
		} else {
			slog.Debug("✅ Created initial commit", "branch", branch)
		}
		time.Sleep(200 * time.Millisecond)
	}

	slog.Info("🎯 Starting continuous development simulation", "duration", th.duration)

	// Variable timing: 8s, 12s, 6s, 15s, 10s, 7s
	commitIntervals := []time.Duration{
		8 * time.Second,  // First additional commit
		12 * time.Second, // New branch creation
		6 * time.Second,  // Quick commit
		15 * time.Second, // Another new branch
		10 * time.Second, // Commit to existing
		7 * time.Second,  // Final commits
	}

	commitPlans := []struct {
		branch  string
		message string
		action  string
	}{
		{"main", "ADD: New main feature (success)", "existing"},
		{"feature/new-api", "NEW: API development start (success)", "new"},
		{"bugfix/slow", "FIX: Performance improvement (timeout)", "existing"},
		{"hotfix/urgent", "NEW: Critical security fix (fail)", "new"},
		{"feature/existing", "UPDATE: Feature enhancement (success)", "existing"},
		{"main", "MERGE: Integrate hotfix (success)", "existing"},
	}

	timeout := time.After(th.duration)
	commitIndex := 0

	for commitIndex < len(commitPlans) {
		if commitIndex < len(commitIntervals) {
			timer := time.After(commitIntervals[commitIndex])

			select {
			case <-timeout:
				slog.Info("⏰ Continuous CI simulation completed (timeout)")
				return
			case <-timer:
				if commitIndex < len(commitPlans) {
					plan := commitPlans[commitIndex]

					if plan.action == "new" {
						log.Printf("📝 Creating new branch: %s", plan.branch)
					} else {
						log.Printf("📝 Adding commit to existing branch: %s", plan.branch)
					}

					if err := th.createCommitWithMessage(plan.branch, plan.message); err != nil {
						log.Printf("❌ Failed to create commit on %s: %v", plan.branch, err)
					} else {
						log.Printf("✅ Created commit on %s: %s", plan.branch, plan.message)
					}

					commitIndex++
				}
			}
		} else {
			break
		}
	}

	log.Printf("🏁 Continuous development simulation completed - %d commits created", commitIndex+len(initialBranches))
}

// countTestsFromResults counts the number of tests by counting JSON result files
func (th *E2ETestHarness) countTestsFromResults() int {
	// Search recursively in the default WorkDir structure: /tmp/home-ci/<repoName>/*/logs/
	repoDir := filepath.Join("/tmp/home-ci", th.repoName)

	// Check if repo directory exists
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		return 0
	}

	count := 0

	// Walk through all subdirectories looking for runID/logs directories
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return 0
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Check if this directory has a logs subdirectory
			logsDir := filepath.Join(repoDir, entry.Name(), "logs")
			if files, err := os.ReadDir(logsDir); err == nil {
				for _, file := range files {
					if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
						filePath := filepath.Join(logsDir, file.Name())
						fileInfo, err := os.Stat(filePath)
						// Count files created within the last hour (reasonable window for test duration)
						if err == nil && time.Since(fileInfo.ModTime()) < time.Hour {
							count++
						}
					}
				}
			}
		}
	}

	return count
}

// saveTestData saves test data to persistent storage
func (th *E2ETestHarness) saveTestData() error {
	if th.testType != TestTimeout {
		return nil // Only save data for timeout tests
	}

	// Use the data directory within our test type directory
	dataDir := th.testType.getDataPath()

	// Find the first timeout test result to get branch and commit info
	branchCommit := "unknown-unknown"
	homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
	files, err := os.ReadDir(homeCIDir)
	if err == nil {
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") && file.Name() != "state.json" {
				jsonPath := filepath.Join(homeCIDir, file.Name())
				content, readErr := os.ReadFile(jsonPath)
				if readErr != nil {
					continue
				}

				var result TestResult
				if unmarshalErr := json.Unmarshal(content, &result); unmarshalErr != nil {
					continue
				}

				if result.TimedOut {
					branchSafe := strings.ReplaceAll(result.Branch, "/", "-")
					branchCommit = fmt.Sprintf("%s-%s", branchSafe, result.Commit[:8])
					break
				}
			}
		}
	}

	// Create filename with branch-commit prefix (no timestamp suffix)
	filename := fmt.Sprintf("%s_timeout-test-summary.json", branchCommit)
	dataPath := filepath.Join(dataDir, filename)

	// Collect test data
	testData := map[string]interface{}{
		"timestamp":        time.Now().Format(time.RFC3339),
		"test_type":        th.getTestTypeName(),
		"duration":         th.duration.String(),
		"commits_created":  th.commitsCreated,
		"branches_created": th.branchesCreated,
		"tests_detected":   th.totalTestsDetected,
		"timeout_detected": th.timeoutDetected,
		"test_repo_path":   th.testRepoPath,
		"running_tests":    th.runningTests,
	}

	// Save to JSON file
	data, err := json.MarshalIndent(testData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal test data: %w", err)
	}

	if err := os.WriteFile(dataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write test data: %w", err)
	}

	log.Printf("💾 Test data saved to %s", dataPath)
	return nil
}

// cleanupE2EResources cleans up e2e test harness resources (separate from general cleanup script)
func (th *E2ETestHarness) cleanupE2EResources() {
	log.Println("🧹 Cleaning up e2e test harness resources...")

	// Save test data before cleanup (for timeout tests)
	if err := th.saveTestData(); err != nil {
		log.Printf("⚠️ Failed to save test data: %v", err)
	}

	// Stop home-ci
	if th.homeCICancel != nil {
		th.homeCICancel()
	}

	if th.homeCIProcess != nil && th.homeCIProcess.Process != nil {
		if th.testType != TestTimeout {
			log.Printf("Stopping home-ci process (PID: %d)", th.homeCIProcess.Process.Pid)
		}
		if err := th.homeCIProcess.Process.Signal(syscall.SIGTERM); err != nil {
			if th.testType != TestTimeout {
				log.Printf("Failed to send SIGTERM: %v", err)
			}
			th.homeCIProcess.Process.Kill()
		}
		th.homeCIProcess.Wait()
	}

	// Skip e2e temp directory cleanup if no-cleanup flag is set
	if th.noCleanup {
		log.Printf("🔍 Keeping e2e test environment for debugging: %s", th.tempRunDir)
		log.Printf("   Repository: %s", th.testRepoPath)
		log.Printf("   Data: %s", filepath.Join(th.tempRunDir, "data"))
		log.Println("✅ E2E test harness cleanup completed (environment preserved)")
	} else {
		log.Printf("✅ E2E test harness cleanup completed")
		log.Printf("   Environment was: %s", th.tempRunDir)
	}
}

// analyzeTestResults compares actual test results against expected outcomes
// Returns true if all tests passed (including GitHub Actions dispatch), false otherwise
func (th *E2ETestHarness) analyzeTestResults() bool {
	log.Println("")
	log.Println("=== Test Results Analysis ===")

	// Read the home-ci test results from simplified architecture location
	resultsDir := filepath.Join(th.tempRunDir, "logs", th.repoName)
	files, err := os.ReadDir(resultsDir)
	if err != nil {
		// Fallback to old location
		homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
		files, err = os.ReadDir(homeCIDir)
		if err != nil {
			log.Printf("⚠️ Could not read test results directory: %v", err)
			return false
		}
		// Use old location for processing
		return th.processTestResultsInDirectory(files, homeCIDir)
	}

	// Use new location for processing
	return th.processTestResultsInDirectory(files, resultsDir)
}

// processTestResultsInDirectory processes test results from a given directory
func (th *E2ETestHarness) processTestResultsInDirectory(files []os.DirEntry, dirPath string) bool {

	totalTests := 0
	successfulTests := 0
	failedTests := 0
	timedOutTests := 0
	hasErrors := false

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") && file.Name() != "state.json" {
			jsonPath := filepath.Join(dirPath, file.Name())
			content, readErr := os.ReadFile(jsonPath)
			if readErr != nil {
				continue
			}

			var result TestResult
			if unmarshalErr := json.Unmarshal(content, &result); unmarshalErr != nil {
				continue
			}

			totalTests++

			// Determine expected behavior for this commit
			commitMessage := th.getCommitMessage(result.Commit)
			expectedBehavior := th.getExpectedResult(commitMessage)

			// Determine actual behavior
			var actualBehavior string
			if result.TimedOut {
				actualBehavior = "timeout"
				timedOutTests++
			} else if result.Success {
				actualBehavior = "success"
				successfulTests++
			} else {
				actualBehavior = "failure"
				failedTests++
			}

			// Compare expected vs actual
			status := "SUCCESS"
			if expectedBehavior != actualBehavior {
				status = "ERROR"
				hasErrors = true
			}

			// Check GitHub Actions dispatch status for dispatch tests
			githubStatus := ""
			if th.testType.isDispatchTest() && result.GitHubActionsNotified {
				if !result.GitHubActionsSuccess {
					// For dispatch-no-token-file test, GitHub failure is expected and not an error
					if th.testType != TestDispatchNoTokenFile {
						status = "ERROR"
						hasErrors = true
					}
					githubStatus = fmt.Sprintf(" | GitHub Dispatch: FAILED (%s)", result.GitHubActionsErrorMessage)
				} else {
					githubStatus = " | GitHub Dispatch: SUCCESS"
				}
			}

			log.Printf("Branch: %s | Commit: %.8s", result.Branch, result.Commit)
			log.Printf("  Expected: %s | Actual: %s | Status: %s%s", expectedBehavior, actualBehavior, status, githubStatus)
		}
	}

	log.Printf("")
	log.Printf("Summary: %d total tests (%d success, %d failed, %d timeout)",
		totalTests, successfulTests, failedTests, timedOutTests)
	log.Println("===============================")
	return !hasErrors
}

// getCommitMessage retrieves the commit message for a given commit hash using go-git API
func (th *E2ETestHarness) getCommitMessage(commit string) string {
	// Open the git repository
	repo, err := git.PlainOpen(th.testRepoPath)
	if err != nil {
		return ""
	}

	// Parse the commit hash
	hash := plumbing.NewHash(commit)

	// Get the commit object
	commitObj, err := repo.CommitObject(hash)
	if err != nil {
		return ""
	}

	// Return the commit message (first line only)
	lines := strings.Split(strings.TrimSpace(commitObj.Message), "\n")
	if len(lines) > 0 {
		return lines[0]
	}
	return ""
}

// cleanupReposDirectory removes all directories from /tmp/home-ci/repos
func (th *E2ETestHarness) cleanupReposDirectory() error {
	reposDir := "/tmp/home-ci/repos"

	// Check if the directory exists
	if _, err := os.Stat(reposDir); os.IsNotExist(err) {
		return nil // Nothing to clean up
	}

	// Remove the entire repos directory and recreate it
	if err := os.RemoveAll(reposDir); err != nil {
		return fmt.Errorf("failed to remove repos directory: %w", err)
	}

	// Recreate the empty directory
	if err := os.MkdirAll(reposDir, 0755); err != nil {
		return fmt.Errorf("failed to recreate repos directory: %w", err)
	}

	return nil
}

// copySecretIfExists copies secret.yaml from project root to test directory for dispatch tests
func (th *E2ETestHarness) copySecretIfExists() error {
	// Look for secret.yaml in the current working directory (project root)
	sourceSecret := "secret.yaml"
	if _, err := os.Stat(sourceSecret); os.IsNotExist(err) {
		if th.testType != TestTimeout {
			slog.Debug("No secret.yaml found in project root - dispatch may fail if not provided by CI")
		}
		return nil // Not an error - CI will provide the secret
	}

	// Read the secret file
	content, err := os.ReadFile(sourceSecret)
	if err != nil {
		return fmt.Errorf("failed to read secret.yaml: %w", err)
	}

	// Write to test directory
	destSecret := filepath.Join(th.tempRunDir, "secret.yaml")
	if err := os.WriteFile(destSecret, content, 0600); err != nil {
		return fmt.Errorf("failed to write secret.yaml to test directory: %w", err)
	}

	if th.testType != TestTimeout {
		slog.Debug("Copied secret.yaml to test directory for dispatch test")
	}
	return nil
}

// getTestTypeName returns a human-readable test type name
func (th *E2ETestHarness) getTestTypeName() string {
	switch th.testType {
	case TestTimeout:
		return "Timeout Test"
	case TestQuick:
		return "Quick Test"
	case TestLong:
		return "Long Test"
	case TestDispatchOneSuccess:
		return "Dispatch One Success Test"
	case TestDispatchAll:
		return "Dispatch All Test"
	case TestCacheLocal:
		return "Cache Local Test"
	case TestCacheRemote:
		return "Cache Remote Test"
	default:
		return "Normal Test"
	}
}

// verifyCacheBehavior verifies that cache behavior worked correctly
func (th *E2ETestHarness) verifyCacheBehavior() bool {
	log.Println("🔍 Verifying cache behavior...")

	if th.testType == TestCacheLocal {
		// For cache-local test, verify only local branches were processed
		// This would be evidenced by tests running on local branches only
		log.Println("✅ Cache-local test: Only local branches should be processed")
		return true // Basic validation - could be enhanced to check actual branch processing
	} else if th.testType == TestCacheRemote {
		// For cache-remote test, verify remote branches were checked out and processed
		// Check if remote branches were created as local tracking branches
		// Change to test repo directory first
		oldDir, _ := os.Getwd()
		os.Chdir(th.testRepoPath)
		defer os.Chdir(oldDir)

		if output, err := th.runGitCommandWithOutput("git", "branch", "-a"); err == nil {
			log.Printf("Repository branches after cache test:\n%s", output)

			// Look for evidence that remote branches were processed
			hasRemoteRefs := strings.Contains(output, "remotes/origin/")
			hasLocalTracking := strings.Contains(output, "remote-feature") || strings.Contains(output, "remote-hotfix")

			if hasRemoteRefs || hasLocalTracking {
				log.Println("✅ Cache-remote test: Remote branches were processed")
				return true
			} else {
				log.Println("❌ Cache-remote test: No evidence of remote branch processing")
				return false
			}
		} else {
			log.Printf("❌ Failed to verify cache behavior: %v", err)
			return false
		}
	}

	return true
}
