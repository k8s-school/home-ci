package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/k8s-school/home-ci/resources"
)

func NewE2ETestHarness(testType TestType, duration time.Duration, noCleanup bool) *E2ETestHarness {
	// Use a fixed directory name for simplicity
	tempRunDir := "/tmp/home-ci/e2e/repo"

	// Use the temp run directory directly as the repository path
	repoPath := tempRunDir

	return &E2ETestHarness{
		testType:     testType,
		duration:     duration,
		testRepoPath: repoPath,
		tempRunDir:   tempRunDir,
		noCleanup:    noCleanup,
	}
}

// writeFileFromResource writes an embedded resource to a file
func (th *E2ETestHarness) writeFileFromResource(content, filePath string, executable bool) error {
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return err
	}

	if executable {
		return os.Chmod(filePath, 0755)
	}
	return nil
}

// setupTestRepo creates a test repository using the embedded setup script or manual setup
func (th *E2ETestHarness) setupTestRepo() error {
	if th.testType != TestTimeout {
		log.Printf("🚀 Setting up test environment (%s)...", th.tempRunDir)
	}

	// Clean up and initialize the entire /tmp/home-ci/e2e/ directory
	e2eBaseDir := "/tmp/home-ci/e2e"
	if _, err := os.Stat(e2eBaseDir); err == nil {
		if th.testType != TestTimeout {
			log.Printf("Cleaning up existing e2e directory at %s", e2eBaseDir)
		}
		if err := os.RemoveAll(e2eBaseDir); err != nil {
			return fmt.Errorf("failed to remove existing e2e directory: %w", err)
		}
	}

	// Create the e2e base directory structure
	if err := os.MkdirAll(e2eBaseDir, 0755); err != nil {
		return fmt.Errorf("failed to create e2e base directory: %w", err)
	}

	// Create the temp run directory structure
	if err := os.MkdirAll(th.tempRunDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp run directory: %w", err)
	}

	// Create data subdirectory for test data files
	dataDir := "/tmp/home-ci/e2e/data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Create the repository directory
	if err := os.MkdirAll(th.testRepoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repo directory: %w", err)
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

	log.Printf("✅ Test repository created at %s", th.testRepoPath)
	return nil
}

// startHomeCI starts home-ci with the appropriate configuration
func (th *E2ETestHarness) startHomeCI(configPath string) error {
	if th.testType != TestTimeout {
		log.Println("🚀 Starting home-ci process...")
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
	dataDir := "/tmp/home-ci/e2e/data"
	th.homeCIProcess.Env = append(os.Environ(), fmt.Sprintf("HOME_CI_DATA_DIR=%s", dataDir))

	if err := th.homeCIProcess.Start(); err != nil {
		return fmt.Errorf("failed to start home-ci: %w", err)
	}

	if th.testType != TestTimeout {
		logPath := filepath.Join(th.testRepoPath, ".home-ci")
		log.Printf("✅ home-ci started with PID %d, logs will be in %s/", th.homeCIProcess.Process.Pid, logPath)
	}

	// Wait a bit for home-ci to start
	time.Sleep(3 * time.Second)
	return nil
}

// simulateActivity simulates development activity
func (th *E2ETestHarness) simulateActivity() {
	if th.testType == TestTimeout {
		// For timeout tests, just create one commit to trigger the test
		log.Println("📝 Creating commit to trigger timeout test...")
		if err := th.createCommit("main"); err != nil {
			log.Printf("❌ Failed to create commit: %v", err)
		}
		return
	}

	log.Printf("🎯 Starting activity simulation for %v", th.duration)

	ticker := time.NewTicker(45 * time.Second) // Create a commit every 45 seconds
	defer ticker.Stop()

	timeout := time.After(th.duration)

	branches := []string{"main", "feature/new-feature", "bugfix/critical-fix", "feature/enhancement"}
	branchIndex := 0

	for {
		select {
		case <-timeout:
			log.Println("⏰ Activity simulation completed")
			return
		case <-ticker.C:
			branch := branches[branchIndex%len(branches)]
			if err := th.createCommit(branch); err != nil {
				log.Printf("❌ Failed to create commit on %s: %v", branch, err)
			}
			branchIndex++
		}
	}
}

// countTestsFromResults counts the number of tests by counting JSON result files
func (th *E2ETestHarness) countTestsFromResults() int {
	homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
	files, err := os.ReadDir(homeCIDir)
	if err != nil {
		return 0
	}

	count := 0
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			// Skip state.json file, only count test result files
			if file.Name() != "state.json" {
				count++
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

	// Use the data directory within our temp run directory
	dataDir := "/tmp/home-ci/e2e/data"

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
func (th *E2ETestHarness) analyzeTestResults() {
	log.Println("")
	log.Println("=== Test Results Analysis ===")

	// Read the home-ci test results
	homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
	files, err := os.ReadDir(homeCIDir)
	if err != nil {
		log.Printf("⚠️ Could not read test results directory: %v", err)
		return
	}

	totalTests := 0
	successfulTests := 0
	failedTests := 0
	timedOutTests := 0

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

			totalTests++

			// Determine expected behavior for this branch/commit
			expectedBehavior := th.determineExpectedBehavior(result.Branch, result.Commit)

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
			}

			log.Printf("Branch: %s | Commit: %.8s", result.Branch, result.Commit)
			log.Printf("  Expected: %s | Actual: %s | Status: %s", expectedBehavior, actualBehavior, status)
		}
	}

	log.Printf("")
	log.Printf("Summary: %d total tests (%d success, %d failed, %d timeout)",
		totalTests, successfulTests, failedTests, timedOutTests)
	log.Println("===============================")
}

// determineExpectedBehavior determines what the expected test outcome should be for a given branch/commit
func (th *E2ETestHarness) determineExpectedBehavior(branch, commit string) string {
	// This logic should match the logic in run-e2e.sh
	// For timeout tests, we expect timeout behavior unless overridden
	if th.testType == TestTimeout {
		return "timeout"
	}

	// Check branch patterns (simplified version of run-e2e.sh logic)
	switch branch {
	case "main":
		return "success"
	case "feature/test1":
		return "success"
	case "feature/test2":
		return "failure"
	case "bugfix/critical":
		return "timeout"
	default:
		if strings.HasPrefix(branch, "feature/") {
			return "success"
		} else if strings.HasPrefix(branch, "bugfix/") {
			return "failure"
		}
		return "success" // Default
	}
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

// getTestTypeName returns a human-readable test type name
func (th *E2ETestHarness) getTestTypeName() string {
	switch th.testType {
	case TestTimeout:
		return "Timeout Test"
	case TestQuick:
		return "Quick Test"
	case TestLong:
		return "Long Test"
	case TestDispatch:
		return "Dispatch Test"
	default:
		return "Normal Test"
	}
}