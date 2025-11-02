package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/k8s-school/home-ci/internal/config"
)

// StateManager interface to avoid circular imports
type StateManager interface {
	AddRunningTest(test RunningTest)
	RemoveRunningTest(branch, commit string)
	GetRunningTests() []RunningTest
	CleanupOldRunningTests(maxAge time.Duration)
	SaveState() error
}

// RunningTest represents a test that is currently running
type RunningTest struct {
	Branch    string    `json:"branch"`
	Commit    string    `json:"commit"`
	LogFile   string    `json:"log_file"`
	StartTime time.Time `json:"start_time"`
	PID       int       `json:"pid,omitempty"`
}

// TestResult represents the complete result of a test execution
type TestResult struct {
	Branch                    string        `json:"branch"`
	Commit                    string        `json:"commit"`
	LogFile                   string        `json:"log_file"`
	StartTime                 time.Time     `json:"start_time"`
	EndTime                   time.Time     `json:"end_time"`
	Duration                  time.Duration `json:"duration"`
	Success                   bool          `json:"success"`
	TimedOut                  bool          `json:"timed_out"`
	CleanupExecuted           bool          `json:"cleanup_executed"`
	CleanupSuccess            bool          `json:"cleanup_success"`
	GitHubActionsNotified     bool          `json:"github_actions_notified"`
	GitHubActionsSuccess      bool          `json:"github_actions_success"`
	ErrorMessage              string        `json:"error_message,omitempty"`
	CleanupErrorMessage       string        `json:"cleanup_error_message,omitempty"`
	GitHubActionsErrorMessage string        `json:"github_actions_error_message,omitempty"`
}


// TestRunner manages test execution and coordination
type TestRunner struct {
	config       config.Config
	configPath   string        // Path to the config file for resolving relative paths
	logDir       string
	testQueue    chan TestJob
	ctx          context.Context
	semaphore    chan struct{} // Semaphore to limit concurrency
	stateManager StateManager  // State manager for tracking running tests
}

// TestExecution encapsulates a single test execution context
type TestExecution struct {
	runner         *TestRunner
	branch         string
	commit         string
	startTime      time.Time
	logFilePath    string
	resultFilePath string
	tempDir        string
	projectDir     string
	testResult     *TestResult
	logFile        *os.File
}

// NewTestRunner creates a new test runner instance
func NewTestRunner(cfg config.Config, configPath, logDir string, ctx context.Context, stateManager StateManager) *TestRunner {
	return &TestRunner{
		config:       cfg,
		configPath:   configPath,
		logDir:       logDir,
		testQueue:    make(chan TestJob, 100),
		ctx:          ctx,
		semaphore:    make(chan struct{}, cfg.MaxConcurrentRuns),
		stateManager: stateManager,
	}
}

// Start begins processing test jobs from the queue
func (tr *TestRunner) Start() {
	slog.Debug("Starting test runner", "max_concurrent_runs", tr.config.MaxConcurrentRuns)

	for job := range tr.testQueue {
		// Acquire semaphore BEFORE launching goroutine to respect concurrency limit
		tr.semaphore <- struct{}{}
		go func(j TestJob) {
			defer func() { <-tr.semaphore }() // Release when done
			tr.executeTestJobWithoutSemaphore(j)
		}(job)
	}
}


// executeTestJobWithoutSemaphore handles test execution without semaphore management
// The semaphore is expected to be managed by the caller
func (tr *TestRunner) executeTestJobWithoutSemaphore(job TestJob) {
	slog.Debug("Starting tests", "branch", job.Branch, "commit", job.Commit[:8])

	if err := tr.runTests(job.Branch, job.Commit); err != nil {
		slog.Debug("Tests failed", "branch", job.Branch, "error", err)
	} else {
		slog.Debug("Tests completed successfully", "branch", job.Branch)
	}
}

// QueueTestJob adds a test job to the processing queue
func (tr *TestRunner) QueueTestJob(job TestJob) bool {
	select {
	case tr.testQueue <- job:
		return true
	default:
		return false
	}
}

// Close shuts down the test runner
func (tr *TestRunner) Close() {
	close(tr.testQueue)
}

// runTests orchestrates the execution of a single test
func (tr *TestRunner) runTests(branch, commit string) error {
	slog.Debug("Running tests", "branch", branch, "commit", commit[:8], "timeout", tr.config.TestTimeout)

	// Initialize test execution context
	execution := tr.newTestExecution(branch, commit)
	defer execution.cleanup()

	// Setup logging and state management
	if err := execution.setupLogging(); err != nil {
		return err
	}

	if err := execution.registerRunningTest(); err != nil {
		return err
	}

	// Setup repository
	if err := execution.setupRepository(); err != nil {
		return err
	}

	// Execute the test
	if err := execution.executeTest(); err != nil {
		execution.testResult.ErrorMessage = err.Error()
	}

	// Post-execution tasks
	execution.runCleanupIfNeeded()
	execution.sendGitHubNotificationIfNeeded()

	return nil
}

// newTestExecution creates a new test execution context
func (tr *TestRunner) newTestExecution(branch, commit string) *TestExecution {
	startTime := time.Now()
	timestamp := startTime.Format("20060102-150405")
	branchFile := strings.ReplaceAll(branch, "/", "-")

	logFileName := fmt.Sprintf("%s_%s_%s.log", timestamp, branchFile, commit[:8])
	resultFileName := fmt.Sprintf("%s_%s_%s.json", timestamp, branchFile, commit[:8])

	// Extract project name from repo path
	projectName := filepath.Base(tr.config.RepoPath)
	if projectName == "" || projectName == "." || projectName == "/" {
		projectName = "project"
	}
	// Remove trailing slash and .git suffix if present
	projectName = strings.TrimSuffix(projectName, "/")
	projectName = strings.TrimSuffix(projectName, ".git")

	tempDir := fmt.Sprintf("/tmp/home-ci/repos/%s-%s-%s", branchFile, commit[:8], timestamp)
	projectDir := filepath.Join(tempDir, projectName)

	return &TestExecution{
		runner:         tr,
		branch:         branch,
		commit:         commit,
		startTime:      startTime,
		logFilePath:    filepath.Join(tr.logDir, logFileName),
		resultFilePath: filepath.Join(tr.logDir, resultFileName),
		tempDir:        tempDir,
		projectDir:     projectDir,
		testResult: &TestResult{
			Branch:    branch,
			Commit:    commit,
			LogFile:   logFileName,
			StartTime: startTime,
		},
	}
}

// cleanup handles final cleanup tasks for test execution
func (te *TestExecution) cleanup() {
	// Finalize test result
	te.testResult.EndTime = time.Now()
	te.testResult.Duration = te.testResult.EndTime.Sub(te.testResult.StartTime)
	te.saveTestResult()

	// Close log file if open
	if te.logFile != nil {
		te.logFile.Close()
	}

	// Remove from state if state manager is available
	if te.runner.stateManager != nil {
		te.runner.stateManager.RemoveRunningTest(te.branch, te.commit)
		te.runner.stateManager.SaveState()
	}

	// Clean up temp directory if immediate cleanup is needed
	if te.runner.config.KeepTime == 0 && te.tempDir != "" {
		os.RemoveAll(te.tempDir)
	}
}

// setupLogging creates and configures the log file
func (te *TestExecution) setupLogging() error {
	logFile, err := os.Create(te.logFilePath)
	if err != nil {
		return fmt.Errorf("failed to create log file %s: %w", te.logFilePath, err)
	}
	te.logFile = logFile

	slog.Debug("Test output will be logged", "log_file", te.logFilePath)
	return nil
}

// registerRunningTest adds the test to the running tests state
func (te *TestExecution) registerRunningTest() error {
	if te.runner.stateManager == nil {
		return nil
	}

	runningTest := RunningTest{
		Branch:    te.branch,
		Commit:    te.commit,
		LogFile:   filepath.Base(te.logFilePath),
		StartTime: te.startTime,
	}

	te.runner.stateManager.AddRunningTest(runningTest)
	return te.runner.stateManager.SaveState()
}

// setupRepository clones and prepares the repository for testing
func (te *TestExecution) setupRepository() error {
	if err := os.MkdirAll(te.tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	slog.Debug("Created temporary repository", "temp_dir", te.tempDir)

	// Log repository setup
	fmt.Fprintf(te.logFile, "=== Cloning Repository ===\n")
	fmt.Fprintf(te.logFile, "Source: %s\n", te.runner.config.RepoPath)
	fmt.Fprintf(te.logFile, "Destination: %s\n", te.projectDir)
	fmt.Fprintf(te.logFile, "Branch: %s\n", te.branch)
	fmt.Fprintf(te.logFile, "Commit: %s\n", te.commit)
	fmt.Fprintf(te.logFile, "========================\n\n")

	return te.cloneAndCheckoutRepository()
}

// cloneAndCheckoutRepository performs the git operations
func (te *TestExecution) cloneAndCheckoutRepository() error {
	cleanBranchName := strings.TrimPrefix(te.branch, "origin/")
	branchRefName := plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", cleanBranchName))

	// Clone the repository with single branch
	repo, err := git.PlainClone(te.projectDir, false, &git.CloneOptions{
		URL:           te.runner.config.RepoPath,
		ReferenceName: branchRefName,
		SingleBranch:  true,
	})
	if err != nil {
		fmt.Fprintf(te.logFile, "Failed to clone repository: %v\n", err)
		return fmt.Errorf("failed to clone repository to %s: %w", te.projectDir, err)
	}

	fmt.Fprintf(te.logFile, "Repository cloned successfully (single branch: %s)\n", cleanBranchName)

	// Get worktree and checkout specific commit
	worktree, err := repo.Worktree()
	if err != nil {
		fmt.Fprintf(te.logFile, "Failed to get worktree: %v\n", err)
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Checkout the branch
	if err := worktree.Checkout(&git.CheckoutOptions{Branch: branchRefName}); err != nil {
		fmt.Fprintf(te.logFile, "Failed to checkout branch %s: %v\n", cleanBranchName, err)
		return fmt.Errorf("failed to checkout branch %s: %w", cleanBranchName, err)
	}

	fmt.Fprintf(te.logFile, "Checked out branch %s successfully\n", cleanBranchName)

	// Reset to specific commit
	commitHash := plumbing.NewHash(te.commit)
	if err := worktree.Reset(&git.ResetOptions{Commit: commitHash, Mode: git.HardReset}); err != nil {
		fmt.Fprintf(te.logFile, "Failed to reset to commit %s: %v\n", te.commit, err)
		return fmt.Errorf("failed to reset to commit %s: %w", te.commit, err)
	}

	fmt.Fprintf(te.logFile, "Reset to commit %s successfully\n", te.commit)
	fmt.Fprintf(te.logFile, "========================\n\n")

	return nil
}

// executeTest runs the actual test script
func (te *TestExecution) executeTest() error {
	// Prepare command arguments
	args := te.parseCommandArgs()

	// Create context with timeout
	testCtx, testCancel := context.WithTimeout(context.Background(), te.runner.config.TestTimeout)
	defer testCancel()

	// Setup command
	scriptPath := filepath.Join(te.projectDir, te.runner.config.TestScript)
	cmd := exec.CommandContext(testCtx, scriptPath, args...)
	cmd.Dir = te.projectDir
	cmd.Stdout = io.MultiWriter(os.Stdout, te.logFile)
	cmd.Stderr = io.MultiWriter(os.Stderr, te.logFile)

	// Log test execution
	te.logTestExecution(scriptPath, args)

	// Execute test
	testStartTime := time.Now()
	err := cmd.Run()
	duration := time.Since(testStartTime)

	// Process test result
	te.processTestResult(err, testCtx, duration)

	return err
}

// parseCommandArgs parses the configuration options into command arguments
func (te *TestExecution) parseCommandArgs() []string {
	if te.runner.config.Options == "" {
		return []string{}
	}
	return strings.Fields(te.runner.config.Options)
}

// logTestExecution logs the test command and parameters
func (te *TestExecution) logTestExecution(scriptPath string, args []string) {
	fullCommand := fmt.Sprintf("%s %s", scriptPath, strings.Join(args, " "))
	slog.Debug("Executing test command", "command", fullCommand, "working_dir", te.projectDir)

	fmt.Fprintf(te.logFile, "=== CI Test Run ===\n")
	fmt.Fprintf(te.logFile, "Branch: %s\n", te.branch)
	fmt.Fprintf(te.logFile, "Commit: %s\n", te.commit)
	fmt.Fprintf(te.logFile, "Timestamp: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(te.logFile, "Command: %s\n", fullCommand)
	fmt.Fprintf(te.logFile, "Working Directory: %s\n", te.projectDir)
	fmt.Fprintf(te.logFile, "Timeout: %s\n", te.runner.config.TestTimeout)
	fmt.Fprintf(te.logFile, "==================\n\n")
}

// processTestResult analyzes the test execution result and updates test result
func (te *TestExecution) processTestResult(err error, testCtx context.Context, duration time.Duration) {
	if err != nil {
		if testCtx.Err() == context.DeadlineExceeded {
			te.handleTestTimeout(duration)
		} else {
			te.testResult.ErrorMessage = err.Error()
		}
	} else {
		te.testResult.Success = true
	}

	te.logTestCompletion(duration)
}

// handleTestTimeout processes test timeout scenarios
func (te *TestExecution) handleTestTimeout(duration time.Duration) {
	te.testResult.TimedOut = true
	te.testResult.ErrorMessage = fmt.Sprintf("Test timeout after %s", duration)

	slog.Error("Test timeout",
		"branch", te.branch,
		"commit", te.commit[:8],
		"duration", duration,
		"timeout", te.runner.config.TestTimeout)

	fmt.Fprintf(te.logFile, "\n=== TEST TIMEOUT ===\n")
	fmt.Fprintf(te.logFile, "Test execution timed out after %s\n", duration)
	fmt.Fprintf(te.logFile, "Timeout limit: %s\n", te.runner.config.TestTimeout)
	fmt.Fprintf(te.logFile, "Test was killed due to timeout\n")
	fmt.Fprintf(te.logFile, "===================\n")
}

// logTestCompletion logs the completion of test execution
func (te *TestExecution) logTestCompletion(duration time.Duration) {
	slog.Debug("Test completed", "branch", te.branch, "commit", te.commit[:8], "duration", duration)

	if !te.testResult.TimedOut {
		fmt.Fprintf(te.logFile, "\n=== Test Completed ===\n")
		fmt.Fprintf(te.logFile, "Duration: %s\n", duration)
		fmt.Fprintf(te.logFile, "======================\n")
	}
}

// runCleanupIfNeeded executes cleanup script if configured
func (te *TestExecution) runCleanupIfNeeded() {
	if !te.runner.config.Cleanup.AfterE2E || te.runner.config.Cleanup.Script == "" {
		return
	}

	te.testResult.CleanupExecuted = true
	if err := te.runCleanupScript(); err != nil {
		te.testResult.CleanupSuccess = false
		te.testResult.CleanupErrorMessage = err.Error()
		te.logCleanupFailure(err)
	} else {
		te.testResult.CleanupSuccess = true
	}
}

// runCleanupScript executes the cleanup script
func (te *TestExecution) runCleanupScript() error {
	slog.Debug("Running cleanup script",
		"branch", te.branch,
		"commit", te.commit[:8],
		"script", te.runner.config.Cleanup.Script)

	fmt.Fprintf(te.logFile, "\n=== Running Cleanup Script ===\n")
	fmt.Fprintf(te.logFile, "Script: %s\n", te.runner.config.Cleanup.Script)
	fmt.Fprintf(te.logFile, "Working Directory: %s\n", te.projectDir)
	fmt.Fprintf(te.logFile, "==============================\n\n")

	scriptPath := filepath.Join(te.projectDir, te.runner.config.Cleanup.Script)

	// Create context with timeout
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), te.runner.config.TestTimeout)
	defer cleanupCancel()

	cmd := exec.CommandContext(cleanupCtx, scriptPath)
	cmd.Dir = te.projectDir
	cmd.Stdout = io.MultiWriter(os.Stdout, te.logFile)
	cmd.Stderr = io.MultiWriter(os.Stderr, te.logFile)

	return cmd.Run()
}

// logCleanupFailure logs cleanup script failures
func (te *TestExecution) logCleanupFailure(err error) {
	slog.Debug("Cleanup script failed",
		"branch", te.branch,
		"commit", te.commit[:8],
		"error", err)

	fmt.Fprintf(te.logFile, "\n=== Cleanup Script Failed ===\n")
	fmt.Fprintf(te.logFile, "Error: %v\n", err)
	fmt.Fprintf(te.logFile, "============================\n")
}

// sendGitHubNotificationIfNeeded sends GitHub Actions notification if enabled
func (te *TestExecution) sendGitHubNotificationIfNeeded() {
	if !te.runner.config.GitHubActionsDispatch.Enabled {
		return
	}

	te.testResult.GitHubActionsNotified = true
	if err := te.runner.notifyGitHubActions(te.branch, te.commit, te.testResult.Success, te.logFilePath, te.resultFilePath); err != nil {
		te.testResult.GitHubActionsSuccess = false
		te.testResult.GitHubActionsErrorMessage = err.Error()
		slog.Error("GitHub Actions notification failed",
			"branch", te.branch,
			"commit", te.commit[:8],
			"error", err)
	} else {
		te.testResult.GitHubActionsSuccess = true
	}
}

// saveTestResult saves the test result to JSON file
func (te *TestExecution) saveTestResult() {
	if err := te.runner.saveTestResult(*te.testResult, te.resultFilePath); err != nil {
		slog.Error("Failed to save test result", "error", err, "file", te.resultFilePath)
	}
}

// saveTestResult saves a test result to a JSON file
func (tr *TestRunner) saveTestResult(result TestResult, filePath string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal test result: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write test result to %s: %w", filePath, err)
	}

	return nil
}