package runner

import (
	"context"
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
	AddRunningTest(test interface{})
	RemoveRunningTest(branch, commit string)
	GetRunningTests() []interface{}
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

	// Create log file name with timestamp, branch, and commit hash
	timestamp := time.Now().Format("20060102-150405")
	// replace /with - in branch names to avoid filesystem issues
	branchFile := strings.ReplaceAll(branch, "/", "-")
	logFileName := fmt.Sprintf("%s_%s_%s.log", timestamp, branchFile, commit[:8])
	logFilePath := filepath.Join(tr.logDir, logFileName)

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
	tempDir, err := os.MkdirTemp("/tmp", fmt.Sprintf("ci-test-%s-%s-", branchFile, commit[:8]))
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir) // Clean up temp directory

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

	// Parse options and add branch parameter
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

	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	// Check if the error was due to timeout
	if err != nil {
		if testCtx.Err() == context.DeadlineExceeded {
			timeoutMsg := fmt.Sprintf("Test execution timed out after %s (limit: %s)", duration, tr.config.TestTimeout)
			slog.Error("Test timeout", "branch", branch, "commit", commit[:8], "duration", duration, "timeout", tr.config.TestTimeout)
			fmt.Fprintf(logFile, "\n=== TEST TIMEOUT ===\n")
			fmt.Fprintf(logFile, "Test execution timed out after %s\n", duration)
			fmt.Fprintf(logFile, "Timeout limit: %s\n", tr.config.TestTimeout)
			fmt.Fprintf(logFile, "Test was killed due to timeout\n")
			fmt.Fprintf(logFile, "===================\n")
			return fmt.Errorf("test timeout: %s", timeoutMsg)
		}
	}

	// Log completion time
	slog.Debug("Test completed", "branch", branch, "commit", commit[:8], "duration", duration)
	fmt.Fprintf(logFile, "\n=== Test Completed ===\n")
	fmt.Fprintf(logFile, "Duration: %s\n", duration)
	fmt.Fprintf(logFile, "======================\n")

	// Note: test is automatically removed from state by defer

	return err
}
