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

	"github.com/k8s-school/home-ci/internal/config"
)

type TestRunner struct {
	config    config.Config
	logDir    string
	testQueue chan TestJob
	ctx       context.Context
	semaphore chan struct{} // Semaphore pour limiter la concurrence
}

func NewTestRunner(cfg config.Config, logDir string, ctx context.Context) *TestRunner {
	return &TestRunner{
		config:    cfg,
		logDir:    logDir,
		testQueue: make(chan TestJob, 100),
		ctx:       ctx,
		semaphore: make(chan struct{}, cfg.MaxConcurrentRuns),
	}
}

func (tr *TestRunner) Start() {
	slog.Debug("Starting test runner", "max_concurrent_runs", tr.config.MaxConcurrentRuns)

	for job := range tr.testQueue {
		// Lancer chaque job dans une goroutine séparée
		go func(job TestJob) {
			// Acquérir un slot dans le semaphore
			tr.semaphore <- struct{}{}
			defer func() { <-tr.semaphore }() // Libérer le slot à la fin

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
	slog.Debug("Running tests", "branch", branch, "commit", commit[:8])

	// Create log file name with timestamp, branch, and commit hash
	timestamp := time.Now().Format("20060102-150405")
	// replace /with - in branch names to avoid filesystem issues
	branchFile := strings.ReplaceAll(branch, "/", "-")
	logFileName := fmt.Sprintf("%s_%s_%s.log", timestamp, branchFile, commit[:8])
	logFilePath := filepath.Join(tr.logDir, logFileName)

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

	// Clone the repository to the temporary directory
	cloneCmd := exec.CommandContext(tr.ctx, "git", "clone", tr.config.RepoPath, tempDir)
	cloneCmd.Stdout = io.MultiWriter(os.Stdout, logFile)
	cloneCmd.Stderr = io.MultiWriter(os.Stderr, logFile)

	fmt.Fprintf(logFile, "=== Cloning Repository ===\n")
	fmt.Fprintf(logFile, "Source: %s\n", tr.config.RepoPath)
	fmt.Fprintf(logFile, "Destination: %s\n", tempDir)
	fmt.Fprintf(logFile, "========================\n\n")

	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("failed to clone repository to %s: %w", tempDir, err)
	}

	// Checkout the specific branch and commit
	checkoutCmd := exec.CommandContext(tr.ctx, "git", "checkout", branch)
	checkoutCmd.Dir = tempDir
	checkoutCmd.Stdout = io.MultiWriter(os.Stdout, logFile)
	checkoutCmd.Stderr = io.MultiWriter(os.Stderr, logFile)

	fmt.Fprintf(logFile, "=== Checking out branch ===\n")
	fmt.Fprintf(logFile, "Branch: %s\n", branch)
	fmt.Fprintf(logFile, "========================\n\n")

	if err := checkoutCmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
	}

	// Reset to the specific commit
	resetCmd := exec.CommandContext(tr.ctx, "git", "reset", "--hard", commit)
	resetCmd.Dir = tempDir
	resetCmd.Stdout = io.MultiWriter(os.Stdout, logFile)
	resetCmd.Stderr = io.MultiWriter(os.Stderr, logFile)

	fmt.Fprintf(logFile, "=== Resetting to commit ===\n")
	fmt.Fprintf(logFile, "Commit: %s\n", commit)
	fmt.Fprintf(logFile, "========================\n\n")

	if err := resetCmd.Run(); err != nil {
		return fmt.Errorf("failed to reset to commit %s: %w", commit, err)
	}

	// Parse options and add branch parameter
	args := []string{}
	if tr.config.Options != "" {
		optionArgs := strings.Fields(tr.config.Options)
		args = append(args, optionArgs...)
	}

	scriptPath := filepath.Join(tempDir, tr.config.TestScript)
	cmd := exec.CommandContext(tr.ctx, scriptPath, args...)
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
	fmt.Fprintf(logFile, "==================\n\n")

	return cmd.Run()
}
