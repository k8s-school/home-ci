package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/k8s-school/home-ci/resources"
)

type TestType int

const (
	TestNormal TestType = iota
	TestTimeout
	TestQuick
	TestLong
)

// RunningTest represents a test currently in progress
type RunningTest struct {
	Branch    string    `json:"branch"`
	Commit    string    `json:"commit"`
	LogFile   string    `json:"log_file"`
	StartTime time.Time `json:"start_time"`
}

type E2ETestHarness struct {
	testType      TestType
	duration      time.Duration
	testRepoPath  string
	homeCIProcess *exec.Cmd
	homeCIContext context.Context
	homeCICancel  context.CancelFunc
	noCleanup     bool // Skip cleanup for debugging

	// Statistics
	commitsCreated     int
	branchesCreated    int
	runningTests       []RunningTest
	totalTestsDetected int // Total number of tests detected (for statistics)
	timeoutDetected    bool
	logCheckCount      int  // Counter for periodic display
	stateFileRead      bool // Track if we've successfully read state.json
}

func NewE2ETestHarness(testType TestType, duration time.Duration, noCleanup bool) *E2ETestHarness {
	repoPath := "/tmp/test-repo-home-ci"
	if testType == TestTimeout {
		repoPath = "/tmp/test-repo-timeout"
	}

	return &E2ETestHarness{
		testType:     testType,
		duration:     duration,
		testRepoPath: repoPath,
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
		log.Printf("🚀 Setting up test repository (%s)...", th.testRepoPath)
	}

	// Clean up existing repository
	if _, err := os.Stat(th.testRepoPath); err == nil {
		if th.testType != TestTimeout {
			log.Printf("Removing existing test repository at %s", th.testRepoPath)
		}
		if err := os.RemoveAll(th.testRepoPath); err != nil {
			return fmt.Errorf("failed to remove existing repo: %w", err)
		}
	}

	// Create the new repository
	if err := os.MkdirAll(th.testRepoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repo directory: %w", err)
	}

	// Create the _e2e directory
	e2eDir := filepath.Join(th.testRepoPath, "_e2e")
	if err := os.MkdirAll(e2eDir, 0755); err != nil {
		return fmt.Errorf("failed to create _e2e directory: %w", err)
	}

	// Write the appropriate test script based on test type
	var testScript string
	var scriptName string

	if th.testType == TestTimeout {
		testScript = resources.RunSlowTestScript
		scriptName = "run-slow-test.sh"
	} else {
		testScript = resources.RunE2EScript
		scriptName = "run-e2e.sh"
	}

	scriptPath := filepath.Join(e2eDir, scriptName)
	if err := th.writeFileFromResource(testScript, scriptPath, true); err != nil {
		return fmt.Errorf("failed to write test script: %w", err)
	}

	// Initialize git using the embedded setup script logic
	if err := th.initializeGitRepo(); err != nil {
		return fmt.Errorf("failed to initialize git repo: %w", err)
	}

	log.Printf("✅ Test repository created at %s", th.testRepoPath)
	return nil
}

// initializeGitRepo initializes the git repository (logic from setup-test-repo.sh)
func (th *E2ETestHarness) initializeGitRepo() error {
	// Set GIT_PAGER to avoid interactions
	os.Setenv("GIT_PAGER", "cat")

	// Initialize git
	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "CI Test"},
		{"git", "config", "user.email", "ci-test@example.com"},
		{"git", "config", "advice.detachedHead", "false"},
		{"git", "config", "init.defaultBranch", "main"},
		{"git", "config", "pager.branch", "false"},
		{"git", "config", "pager.log", "false"},
		{"git", "config", "core.pager", "cat"},
	}

	for _, cmd := range commands {
		if err := th.runGitCommand(cmd...); err != nil {
			return fmt.Errorf("failed to run git command %v: %w", cmd, err)
		}
	}

	// Create basic structure and files (from setup-test-repo.sh)
	files := map[string]string{
		"README.md":  "# Test Repository\n",
		".gitignore": "node_modules/\n*.log\n.home-ci/\n",
		"app.py":     "# Main application file\nprint('Hello from test app')\n",
	}

	for filename, content := range files {
		filePath := filepath.Join(th.testRepoPath, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create %s: %w", filename, err)
		}
	}

	// First commit and rename branch to main
	if err := th.runGitCommand("git", "add", "."); err != nil {
		return fmt.Errorf("failed to add files: %w", err)
	}
	if err := th.runGitCommand("git", "commit", "-m", "Initial commit"); err != nil {
		return fmt.Errorf("failed to create initial commit: %w", err)
	}
	if err := th.runGitCommand("git", "branch", "-m", "main"); err != nil {
		return fmt.Errorf("failed to rename branch to main: %w", err)
	}

	// Create test branches with commits (from setup-test-repo.sh logic)
	if th.testType != TestTimeout { // Don't create extra branches for timeout test
		branches := []struct {
			name    string
			files   map[string]string
			commits []string
		}{
			{
				name: "feature/test1",
				files: map[string]string{
					"feature1.txt": "Feature 1 content\n",
				},
				commits: []string{"Add feature 1", "Update feature 1"},
			},
			{
				name: "feature/test2",
				files: map[string]string{
					"feature2.txt": "Feature 2 content\n",
				},
				commits: []string{"Add feature 2"},
			},
			{
				name: "bugfix/critical",
				files: map[string]string{
					"bugfix.txt": "Bug fix content\n",
				},
				commits: []string{"Fix critical bug"},
			},
		}

		for _, branch := range branches {
			if err := th.runGitCommand("git", "checkout", "-b", branch.name); err != nil {
				return fmt.Errorf("failed to create branch %s: %w", branch.name, err)
			}

			for filename, content := range branch.files {
				filePath := filepath.Join(th.testRepoPath, filename)
				if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
					return fmt.Errorf("failed to create %s: %w", filename, err)
				}
				if err := th.runGitCommand("git", "add", filename); err != nil {
					return fmt.Errorf("failed to add %s: %w", filename, err)
				}
			}

			for _, commitMsg := range branch.commits {
				if err := th.runGitCommand("git", "commit", "-m", commitMsg); err != nil {
					return fmt.Errorf("failed to commit %s: %w", commitMsg, err)
				}
				if len(branch.commits) > 1 {
					// Update file for next commit
					for filename := range branch.files {
						filePath := filepath.Join(th.testRepoPath, filename)
						if err := os.WriteFile(filePath, []byte(branch.files[filename]+"Updated\n"), 0644); err != nil {
							return fmt.Errorf("failed to update %s: %w", filename, err)
						}
						if err := th.runGitCommand("git", "add", filename); err != nil {
							return fmt.Errorf("failed to add updated %s: %w", filename, err)
						}
					}
				}
			}
		}

		// Return to main and make some commits
		if err := th.runGitCommand("git", "checkout", "main"); err != nil {
			return fmt.Errorf("failed to checkout main: %w", err)
		}

		mainUpdates := []string{"Main update 1", "Main update 2"}
		for i, update := range mainUpdates {
			filename := "main-update.txt"
			filePath := filepath.Join(th.testRepoPath, filename)
			content := fmt.Sprintf("%s\n", update)
			if i > 0 {
				// Append to existing file
				existingContent, _ := os.ReadFile(filePath)
				content = string(existingContent) + content
			}
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				return fmt.Errorf("failed to create %s: %w", filename, err)
			}
			if err := th.runGitCommand("git", "add", filename); err != nil {
				return fmt.Errorf("failed to add %s: %w", filename, err)
			}
			if err := th.runGitCommand("git", "commit", "-m", update); err != nil {
				return fmt.Errorf("failed to commit %s: %w", update, err)
			}
		}
	}

	// Display final state (like setup-test-repo.sh) - skip for timeout tests to reduce verbosity
	if th.testType != TestTimeout {
		log.Println("Available branches:")
		if output, err := th.runGitCommandWithOutput("git", "branch", "-a"); err == nil {
			log.Println(output)
		}

		log.Println("Recent commits on main:")
		if output, err := th.runGitCommandWithOutput("git", "log", "--oneline", "-5"); err == nil {
			log.Println(output)
		}
	}

	return nil
}

// runGitCommand executes a git command in the test repository
func (th *E2ETestHarness) runGitCommand(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = th.testRepoPath
	cmd.Env = append(os.Environ(), "GIT_PAGER=cat")

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Git command failed: %s\nOutput: %s", strings.Join(args, " "), string(output))
		return err
	}
	return nil
}

// runGitCommandWithOutput executes a git command and returns output
func (th *E2ETestHarness) runGitCommandWithOutput(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = th.testRepoPath
	cmd.Env = append(os.Environ(), "GIT_PAGER=cat")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// createConfigFile creates configuration file from embedded resource
func (th *E2ETestHarness) createConfigFile() (string, error) {
	configPath := fmt.Sprintf("/tmp/home-ci-test-config-%d.yaml", time.Now().Unix())

	var configContent string
	if th.testType == TestTimeout {
		configContent = resources.ConfigTimeout
		// Replace repo path in timeout config
		configContent = strings.ReplaceAll(configContent, "/tmp/test-repo-timeout", th.testRepoPath)
	} else {
		configContent = resources.ConfigNormal
		// Replace repo path in normal config
		configContent = strings.ReplaceAll(configContent, "/tmp/test-repo-home-ci", th.testRepoPath)
	}

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return "", fmt.Errorf("failed to create config file: %w", err)
	}

	if th.testType != TestTimeout {
		log.Printf("✅ Configuration file created at %s", configPath)
	}
	return configPath, nil
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

// createCommit creates a new commit on a branch
func (th *E2ETestHarness) createCommit(branch string) error {
	log.Printf("📝 Creating commit on branch %s", branch)

	// Check if the branch exists, if not create it
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = th.testRepoPath
	if err := cmd.Run(); err != nil {
		// The branch doesn't exist, create it
		if err := th.runGitCommand("git", "checkout", "-b", branch); err != nil {
			return fmt.Errorf("failed to create branch %s: %w", branch, err)
		}
		th.branchesCreated++
		log.Printf("✅ Created new branch: %s", branch)
	} else {
		// The branch exists, switch to it
		if err := th.runGitCommand("git", "checkout", branch); err != nil {
			return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
		}
	}

	// Create or modify a file
	safeBranchName := strings.ReplaceAll(branch, "/", "_")
	filename := fmt.Sprintf("file_%s_%d.txt", safeBranchName, time.Now().Unix())
	filePath := filepath.Join(th.testRepoPath, filename)
	content := fmt.Sprintf("Content for %s at %s\n", branch, time.Now().Format(time.RFC3339))

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to create file %s: %w", filename, err)
	}

	// Add and commit
	if err := th.runGitCommand("git", "add", filename); err != nil {
		return fmt.Errorf("failed to add file: %w", err)
	}

	commitMsg := fmt.Sprintf("Add %s on branch %s", filename, branch)
	if err := th.runGitCommand("git", "commit", "-m", commitMsg); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	th.commitsCreated++
	log.Printf("✅ Created commit on %s: %s", branch, commitMsg)
	return nil
}

// monitorState monitors home-ci state.json for running tests and timeouts
func (th *E2ETestHarness) monitorState() {
	go func() {
		// Wait for the .home-ci directory to be created by home-ci
		homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
		for {
			if _, err := os.Stat(homeCIDir); err == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}

		for {
			select {
			case <-th.homeCIContext.Done():
				return
			case <-time.After(2 * time.Second):
				// Check state.json for running tests
				if err := th.checkStateForActivity(homeCIDir); err != nil {
					log.Printf("Error checking state: %v", err)
				}
			}
		}
	}()
}

// checkStateForActivity checks state.json for test execution and timeouts
func (th *E2ETestHarness) checkStateForActivity(homeCIDir string) error {
	stateFile := filepath.Join(homeCIDir, "state.json")

	// Check if state.json exists
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		return nil // No state file yet
	}

	// Read and parse state.json
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return err
	}

	var state struct {
		RunningTests []RunningTest `json:"running_tests"`
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	// Update our running tests from state
	th.runningTests = state.RunningTests
	th.stateFileRead = true // Mark that we've successfully read the state file

	// Display running tests every 15 checks (approximately every 30 seconds)
	th.logCheckCount++
	if th.logCheckCount%15 == 0 {
		th.displayRunningTests()
	}

	// Check for timeout in running tests if it's a timeout test
	if th.testType == TestTimeout {
		return th.checkStateForTimeout()
	}

	return nil
}

// checkStateForTimeout checks running tests in state for timeouts (used only for timeout tests)
func (th *E2ETestHarness) checkStateForTimeout() error {
	// For timeout tests, we expect a 30-second timeout based on config-timeout.yaml
	timeoutDuration := 30 * time.Second
	// Add some buffer for processing
	checkThreshold := timeoutDuration + (10 * time.Second)

	now := time.Now()
	for _, test := range th.runningTests {
		elapsed := now.Sub(test.StartTime)

		// If a test has been running longer than the expected timeout + buffer
		if elapsed > checkThreshold {
			if !th.timeoutDetected {
				th.timeoutDetected = true
				log.Printf("🕐 Timeout detected: test on branch %s has been running for %v (expected timeout: %v)",
					test.Branch, elapsed.Truncate(time.Second), timeoutDuration)
			}
		}
	}

	return nil
}

// displayRunningTests shows current running tests with their details
func (th *E2ETestHarness) displayRunningTests() {
	if len(th.runningTests) == 0 {
		// Only show "No tests currently running" if we've successfully read the state file
		// Otherwise, tests might be running but we just can't see them yet
		if th.stateFileRead {
			log.Printf("📊 No tests currently running")
		} else {
			log.Printf("📊 Waiting for test state information...")
		}
		return
	}

	log.Printf("📊 Currently running tests (%d):", len(th.runningTests))
	for i, test := range th.runningTests {
		duration := time.Since(test.StartTime).Truncate(time.Second)
		log.Printf("   %d. Branch: %s, Commit: %s", i+1, test.Branch, test.Commit[:min(8, len(test.Commit))])
		log.Printf("      LogFile: %s, Running: %v", test.LogFile, duration)
	}
}

// helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// countTestsFromLogs counts the number of tests by counting log files
func (th *E2ETestHarness) countTestsFromLogs() int {
	logDir := filepath.Join(th.testRepoPath, ".home-ci")
	files, err := os.ReadDir(logDir)
	if err != nil {
		return 0
	}

	count := 0
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".log") {
			count++
		}
	}
	return count
}

// printStatistics displays test statistics
func (th *E2ETestHarness) printStatistics() {
	// Count tests from actual log files
	th.totalTestsDetected = th.countTestsFromLogs()

	log.Println("\n📊 Test Statistics:")
	log.Printf("   Test Type: %s", th.getTestTypeName())
	log.Printf("   Duration: %v", th.duration)
	log.Printf("   Commits created: %d", th.commitsCreated)
	log.Printf("   Branches created: %d", th.branchesCreated)
	log.Printf("   Tests detected: %d", th.totalTestsDetected)

	if th.testType == TestTimeout {
		log.Printf("   Timeout detected: %v", th.timeoutDetected)
		if !th.timeoutDetected {
			log.Println("⚠️  WARNING: Timeout test did not detect timeout!")
		} else {
			log.Println("✅ Timeout detection working correctly!")
		}
	} else {
		if th.commitsCreated > 0 && th.totalTestsDetected == 0 {
			log.Println("⚠️  WARNING: No test executions detected despite commits being created!")
		} else if th.totalTestsDetected > 0 {
			log.Println("✅ Test execution detection working correctly!")
		}
	}
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
	default:
		return "Normal Test"
	}
}

// saveTestData saves test data to persistent storage
func (th *E2ETestHarness) saveTestData() error {
	if th.testType != TestTimeout {
		return nil // Only save data for timeout tests
	}

	dataDir := "/tmp/home-ci-data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Create unique filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("timeout-test-%s.json", timestamp)
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

// cleanup cleans up resources
func (th *E2ETestHarness) cleanup() {
	log.Println("🧹 Cleaning up...")

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

	// Skip repository cleanup if no-cleanup flag is set
	if th.noCleanup {
		log.Printf("🔍 Keeping test repository for debugging: %s", th.testRepoPath)
		log.Println("✅ Cleanup completed (repository preserved)")
	} else {
		log.Println("✅ Cleanup completed")
	}
}

// parseTestType parses test type from string
func parseTestType(s string) TestType {
	switch s {
	case "timeout":
		return TestTimeout
	case "quick":
		return TestQuick
	case "long":
		return TestLong
	default:
		return TestNormal
	}
}

func main() {
	var (
		testTypeFlag = flag.String("type", "normal", "Test type: normal, timeout, quick, long")
		durationFlag = flag.String("duration", "3m", "Test duration (e.g., 30s, 5m, 1h)")
		noCleanupFlag = flag.Bool("no-cleanup", false, "Keep test repositories for debugging")
		helpFlag      = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *helpFlag {
		fmt.Println("Home-CI E2E Test Harness")
		fmt.Println("========================")
		fmt.Println("")
		fmt.Println("Usage: e2e_home_ci [options]")
		fmt.Println("")
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Test Types:")
		fmt.Println("  normal   - Standard integration test (default)")
		fmt.Println("  timeout  - Test timeout handling (~1 minute)")
		fmt.Println("  quick    - Quick test (30 seconds)")
		fmt.Println("  long     - Extended test (specified duration)")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  e2e_home_ci -type=normal -duration=5m")
		fmt.Println("  e2e_home_ci -type=timeout")
		fmt.Println("  e2e_home_ci -type=quick")
		fmt.Println("  e2e_home_ci -type=timeout -no-cleanup  # Keep repos for debugging")
		return
	}

	testType := parseTestType(*testTypeFlag)

	// Parse duration
	duration, err := time.ParseDuration(*durationFlag)
	if err != nil {
		log.Fatalf("❌ Invalid duration format: %v", err)
	}

	// Adjust duration based on test type
	switch testType {
	case TestTimeout:
		duration = 60 * time.Second // Fixed duration for timeout tests
	case TestQuick:
		if duration > 30*time.Second {
			duration = 30 * time.Second
		}
	}

	log.Printf("🚀 Starting e2e test harness (%s, %v)...",
		map[TestType]string{TestNormal: "normal", TestTimeout: "timeout", TestQuick: "quick", TestLong: "long"}[testType],
		duration)

	th := NewE2ETestHarness(testType, duration, *noCleanupFlag)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\n⚠️  Received interrupt signal, shutting down...")
		th.cleanup()
		os.Exit(0)
	}()

	// Test steps
	if err := th.setupTestRepo(); err != nil {
		log.Fatalf("❌ Failed to setup test repository: %v", err)
	}

	configPath, err := th.createConfigFile()
	if err != nil {
		log.Fatalf("❌ Failed to create config file: %v", err)
	}

	if err := th.startHomeCI(configPath); err != nil {
		log.Fatalf("❌ Failed to start home-ci: %v", err)
	}

	// Start log monitoring
	th.monitorState()

	// Simulate development activity
	th.simulateActivity()

	// Wait for tests to complete based on type
	if testType == TestTimeout {
		log.Println("⏳ Waiting for timeout to occur...")
		time.Sleep(60 * time.Second) // Wait for timeout + processing
	} else {
		log.Println("⏳ Waiting for final tests to complete...")
		time.Sleep(30 * time.Second)
	}

	// Display statistics
	th.printStatistics()

	// Clean up
	th.cleanup()

	// Determine success
	success := true
	if testType == TestTimeout {
		success = th.timeoutDetected
	} else {
		success = th.totalTestsDetected > 0
	}

	if success {
		log.Println("🎉 Test harness completed successfully!")
	} else {
		log.Println("💥 Test harness failed!")
		os.Exit(1)
	}
}
