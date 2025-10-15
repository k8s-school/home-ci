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

type TestRunner struct {
	config       config.Config
	logDir       string
	testQueue    chan TestJob
	ctx          context.Context
	semaphore    chan struct{} // Semaphore to limit concurrency
	stateManager StateManager  // State manager for tracking running tests
}

func NewTestRunner(cfg config.Config, logDir string, ctx context.Context, stateManager StateManager) *TestRunner {
	return &TestRunner{
		config:       cfg,
		logDir:       logDir,
		testQueue:    make(chan TestJob, 100),
		ctx:          ctx,
		semaphore:    make(chan struct{}, cfg.MaxConcurrentRuns),
		stateManager: stateManager,
	}
}

func (tr *TestRunner) Start() {
	slog.Debug("Starting test runner", "max_concurrent_runs", tr.config.MaxConcurrentRuns)

	for job := range tr.testQueue {
		// Launch each job in a separate goroutine
		go func(job TestJob) {
			// Acquire a slot in the semaphore
			tr.semaphore <- struct{}{}
			defer func() { <-tr.semaphore }() // Release the slot at the end

			slog.Debug("Starting tests", "branch", job.Branch, "commit", job.Commit[:8])

			if err := tr.runTests(job.Branch, job.Commit); err != nil {
				slog.Debug("Tests failed", "branch", job.Branch, "error", err)
			} else {
				slog.Debug("Tests completed successfully", "branch", job.Branch)
			}
		}(job)
	}
}

func (tr *TestRunner) QueueTestJob(job TestJob) bool {
	select {
	case tr.testQueue <- job:
		slog.Debug("Queued test job", "branch", job.Branch)
		return true
	default:
		slog.Debug("Test queue full, skipping branch", "branch", job.Branch)
		return false
	}
}

func (tr *TestRunner) Close() {
	close(tr.testQueue)
}

func (tr *TestRunner) runTests(branch, commit string) error {
	slog.Debug("Running tests", "branch", branch, "commit", commit[:8], "timeout", tr.config.TestTimeout)

	startTime := time.Now()

	// Create log file name with timestamp, branch, and commit hash
	timestamp := startTime.Format("20060102-150405")
	// replace /with - in branch names to avoid filesystem issues
	branchFile := strings.ReplaceAll(branch, "/", "-")
	logFileName := fmt.Sprintf("%s_%s_%s.log", timestamp, branchFile, commit[:8])
	logFilePath := filepath.Join(tr.logDir, logFileName)

	// Create JSON result file with same name but .json extension
	resultFileName := fmt.Sprintf("%s_%s_%s.json", timestamp, branchFile, commit[:8])
	resultFilePath := filepath.Join(tr.logDir, resultFileName)

	// Initialize test result
	testResult := TestResult{
		Branch:                branch,
		Commit:                commit,
		LogFile:               logFileName,
		StartTime:             startTime,
		Success:               false,
		TimedOut:              false,
		CleanupExecuted:       false,
		CleanupSuccess:        false,
		GitHubActionsNotified: false,
		GitHubActionsSuccess:  false,
	}

	// Ensure test result is saved even if function exits early
	defer func() {
		testResult.EndTime = time.Now()
		testResult.Duration = testResult.EndTime.Sub(testResult.StartTime)
		tr.saveTestResult(testResult, resultFilePath)
	}()

	// Register test as running in state
	runningTest := RunningTest{
		Branch:    branch,
		Commit:    commit,
		LogFile:   logFileName,
		StartTime: time.Now(),
	}
	if tr.stateManager != nil {
		tr.stateManager.AddRunningTest(runningTest)
		tr.stateManager.SaveState()

		// Ensure test is removed from state even if function exits early
		defer func() {
			tr.stateManager.RemoveRunningTest(branch, commit)
			tr.stateManager.SaveState()
		}()
	}

	// Create log file
	logFile, err := os.Create(logFilePath)
	if err != nil {
		return fmt.Errorf("failed to create log file %s: %w", logFilePath, err)
	}
	defer logFile.Close()

	slog.Debug("Test output will be logged", "log_file", logFilePath)

	// Create temporary directory for cloning
	tempDir := fmt.Sprintf("/tmp/home-ci/repos/%s-%s-%s", branchFile, commit[:8], timestamp)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Schedule cleanup based on KeepTime setting
	if tr.config.KeepTime == 0 {
		defer os.RemoveAll(tempDir) // Clean up immediately if KeepTime is 0
	}
	// For KeepTime > 0, rely on the periodic cleanup routine in monitor
	// This ensures cleanup works even if home-ci is restarted

	slog.Debug("Created temporary repository", "temp_dir", tempDir)

	fmt.Fprintf(logFile, "=== Cloning Repository ===\n")
	fmt.Fprintf(logFile, "Source: %s\n", tr.config.RepoPath)
	fmt.Fprintf(logFile, "Destination: %s\n", tempDir)
	fmt.Fprintf(logFile, "Branch: %s\n", branch)
	fmt.Fprintf(logFile, "Commit: %s\n", commit)
	fmt.Fprintf(logFile, "========================\n\n")

	// Clean branch name (remove origin/ prefix if present)
	cleanBranchName := strings.TrimPrefix(branch, "origin/")
	branchRefName := plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", cleanBranchName))

	// Clone the repository with single branch (equivalent to git clone --single-branch --branch <branchname>)
	repo, err := git.PlainClone(tempDir, false, &git.CloneOptions{
		URL:           tr.config.RepoPath,
		ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", cleanBranchName)),
		SingleBranch:  true,
	})
	if err != nil {
		fmt.Fprintf(logFile, "Failed to clone repository: %v\n", err)
		return fmt.Errorf("failed to clone repository to %s: %w", tempDir, err)
	}

	fmt.Fprintf(logFile, "Repository cloned successfully (single branch: %s)\n", cleanBranchName)

	// Get the worktree
	worktree, err := repo.Worktree()
	if err != nil {
		fmt.Fprintf(logFile, "Failed to get worktree: %v\n", err)
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Checkout the branch (should already be on it, but make sure)
	err = worktree.Checkout(&git.CheckoutOptions{
		Branch: branchRefName,
	})
	if err != nil {
		fmt.Fprintf(logFile, "Failed to checkout branch %s: %v\n", cleanBranchName, err)
		return fmt.Errorf("failed to checkout branch %s: %w", cleanBranchName, err)
	}

	fmt.Fprintf(logFile, "Checked out branch %s successfully\n", cleanBranchName)

	// Reset to the specific commit
	commitHash := plumbing.NewHash(commit)
	err = worktree.Reset(&git.ResetOptions{
		Commit: commitHash,
		Mode:   git.HardReset,
	})
	if err != nil {
		fmt.Fprintf(logFile, "Failed to reset to commit %s: %v\n", commit, err)
		return fmt.Errorf("failed to reset to commit %s: %w", commit, err)
	}

	fmt.Fprintf(logFile, "Reset to commit %s successfully\n", commit)
	fmt.Fprintf(logFile, "========================\n\n")

	// Parse options as command line arguments
	args := []string{}
	if tr.config.Options != "" {
		optionArgs := strings.Fields(tr.config.Options)
		args = append(args, optionArgs...)
	}

	// Create context with timeout
	testCtx, testCancel := context.WithTimeout(context.Background(), tr.config.TestTimeout)
	defer testCancel()

	scriptPath := filepath.Join(tempDir, tr.config.TestScript)
	cmd := exec.CommandContext(testCtx, scriptPath, args...)
	cmd.Dir = tempDir

	// Log the full command that will be executed
	fullCommand := fmt.Sprintf("%s %s", scriptPath, strings.Join(args, " "))
	slog.Debug("Executing test command", "command", fullCommand, "working_dir", tempDir)

	// Create writers that output to both console and log file
	cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
	cmd.Stderr = io.MultiWriter(os.Stderr, logFile)

	// Write header to log file
	fmt.Fprintf(logFile, "=== CI Test Run ===\n")
	fmt.Fprintf(logFile, "Branch: %s\n", branch)
	fmt.Fprintf(logFile, "Commit: %s\n", commit)
	fmt.Fprintf(logFile, "Timestamp: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(logFile, "Command: %s\n", fullCommand)
	fmt.Fprintf(logFile, "Working Directory: %s\n", tempDir)
	fmt.Fprintf(logFile, "Timeout: %s\n", tr.config.TestTimeout)
	fmt.Fprintf(logFile, "==================\n\n")

	testStartTime := time.Now()
	err = cmd.Run()
	duration := time.Since(testStartTime)

	// Check if the error was due to timeout and update test result
	isTimeout := false
	if err != nil {
		if testCtx.Err() == context.DeadlineExceeded {
			isTimeout = true
			testResult.TimedOut = true
			testResult.ErrorMessage = fmt.Sprintf("Test timeout after %s", duration)

			slog.Error("Test timeout", "branch", branch, "commit", commit[:8], "duration", duration, "timeout", tr.config.TestTimeout)
			fmt.Fprintf(logFile, "\n=== TEST TIMEOUT ===\n")
			fmt.Fprintf(logFile, "Test execution timed out after %s\n", duration)
			fmt.Fprintf(logFile, "Timeout limit: %s\n", tr.config.TestTimeout)
			fmt.Fprintf(logFile, "Test was killed due to timeout\n")
			fmt.Fprintf(logFile, "===================\n")
		} else {
			testResult.ErrorMessage = err.Error()
		}
	} else {
		testResult.Success = true
	}

	// Log completion time
	slog.Debug("Test completed", "branch", branch, "commit", commit[:8], "duration", duration)
	if !isTimeout {
		fmt.Fprintf(logFile, "\n=== Test Completed ===\n")
		fmt.Fprintf(logFile, "Duration: %s\n", duration)
		fmt.Fprintf(logFile, "======================\n")
	}

	// Run cleanup script if configured and after_e2e is true (regardless of test result)
	if tr.config.Cleanup.AfterE2E && tr.config.Cleanup.Script != "" {
		testResult.CleanupExecuted = true
		if cleanupErr := tr.runCleanupScript(tempDir, branch, commit, logFile); cleanupErr != nil {
			testResult.CleanupSuccess = false
			testResult.CleanupErrorMessage = cleanupErr.Error()
			slog.Debug("Cleanup script failed", "branch", branch, "commit", commit[:8], "error", cleanupErr)
			fmt.Fprintf(logFile, "\n=== Cleanup Script Failed ===\n")
			fmt.Fprintf(logFile, "Error: %v\n", cleanupErr)
			fmt.Fprintf(logFile, "============================\n")
		} else {
			testResult.CleanupSuccess = true
		}
	}

	// GitHub Actions notification (if enabled)
	if tr.config.GitHubActionsDispatch.Enabled {
		testResult.GitHubActionsNotified = true
		if notifyErr := tr.notifyGitHubActions(branch, commit, testResult.Success); notifyErr != nil {
			testResult.GitHubActionsSuccess = false
			testResult.GitHubActionsErrorMessage = notifyErr.Error()
			slog.Debug("GitHub Actions notification failed", "branch", branch, "commit", commit[:8], "error", notifyErr)
		} else {
			testResult.GitHubActionsSuccess = true
		}
	}

	// Note: test is automatically removed from state by defer
	// Note: test result is automatically saved by defer

	return err
}

// runCleanupScript executes the cleanup script
func (tr *TestRunner) runCleanupScript(tempDir, branch, commit string, logFile *os.File) error {
	slog.Debug("Running cleanup script", "branch", branch, "commit", commit[:8], "script", tr.config.Cleanup.Script)

	fmt.Fprintf(logFile, "\n=== Running Cleanup Script ===\n")
	fmt.Fprintf(logFile, "Script: %s\n", tr.config.Cleanup.Script)
	fmt.Fprintf(logFile, "Working Directory: %s\n", tempDir)
	fmt.Fprintf(logFile, "==============================\n\n")

	scriptPath := filepath.Join(tempDir, tr.config.Cleanup.Script)

	// Create context with timeout (use same timeout as test)
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), tr.config.TestTimeout)
	defer cleanupCancel()

	cmd := exec.CommandContext(cleanupCtx, scriptPath)
	cmd.Dir = tempDir

	// Set environment variables for the cleanup script
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HOME_CI_BRANCH=%s", branch),
		fmt.Sprintf("HOME_CI_COMMIT=%s", commit),
		fmt.Sprintf("HOME_CI_TEMP_DIR=%s", tempDir),
	)

	// If HOME_CI_DATA_DIR is set in environment, pass it through
	if dataDir := os.Getenv("HOME_CI_DATA_DIR"); dataDir != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("HOME_CI_DATA_DIR=%s", dataDir))
	}

	// Create writers that output to both console and log file
	cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
	cmd.Stderr = io.MultiWriter(os.Stderr, logFile)

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	if err != nil {
		if cleanupCtx.Err() == context.DeadlineExceeded {
			slog.Error("Cleanup script timeout", "branch", branch, "commit", commit[:8], "duration", duration)
			fmt.Fprintf(logFile, "\n=== CLEANUP SCRIPT TIMEOUT ===\n")
			fmt.Fprintf(logFile, "Cleanup script timed out after %s\n", duration)
			fmt.Fprintf(logFile, "==============================\n")
			return fmt.Errorf("cleanup script timeout after %s", duration)
		}
		return fmt.Errorf("cleanup script failed: %w", err)
	}

	slog.Debug("Cleanup script completed", "branch", branch, "commit", commit[:8], "duration", duration)
	fmt.Fprintf(logFile, "\n=== Cleanup Script Completed ===\n")
	fmt.Fprintf(logFile, "Duration: %s\n", duration)
	fmt.Fprintf(logFile, "================================\n")

	return nil
}

// saveTestResult saves the test result to a JSON file
func (tr *TestRunner) saveTestResult(result TestResult, filePath string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		slog.Debug("Failed to marshal test result", "error", err)
		return fmt.Errorf("failed to marshal test result: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		slog.Debug("Failed to write test result file", "file", filePath, "error", err)
		return fmt.Errorf("failed to write test result file: %w", err)
	}

	slog.Debug("Saved test result", "file", filePath, "success", result.Success, "timed_out", result.TimedOut)
	return nil
}

// Note: notifyGitHubActions is now implemented in github_dispatch.go
