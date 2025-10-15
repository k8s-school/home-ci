package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/k8s-school/home-ci/internal/config"
	"github.com/k8s-school/home-ci/internal/runner"
)

type Monitor struct {
	config       config.Config
	gitRepo      *GitRepository
	stateManager *StateManager
	testRunner   *runner.TestRunner
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewMonitor(cfg config.Config) (*Monitor, error) {
	gitRepo, err := NewGitRepository(cfg.RepoPath)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create .home-ci directory in repo for logs and state
	homeCIDir := filepath.Join(cfg.RepoPath, ".home-ci")
	if err := os.MkdirAll(homeCIDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .home-ci directory: %w", err)
	}

	logDir := homeCIDir
	stateFile := filepath.Join(homeCIDir, "state.json")
	stateManager := NewStateManager(stateFile)

	testRunner := runner.NewTestRunner(cfg, logDir, ctx, stateManager)

	m := &Monitor{
		config:       cfg,
		gitRepo:      gitRepo,
		stateManager: stateManager,
		testRunner:   testRunner,
		ctx:          ctx,
		cancel:       cancel,
	}

	if err := m.stateManager.LoadState(); err != nil {
		slog.Debug("Failed to load previous state", "error", err)
	}

	return m, nil
}

func (m *Monitor) Start() error {
	slog.Debug("Starting Git CI Monitor")
	slog.Debug("Configuration", "repository", m.config.RepoPath, "check_interval", m.config.CheckInterval, "max_concurrent_runs", m.config.MaxConcurrentRuns, "max_commit_age", m.config.MaxCommitAge, "options", m.config.Options)

	// Start test runner goroutine
	go m.testRunner.Start()

	// Start cleanup routine if KeepTime is configured
	if m.config.KeepTime > 0 {
		// Run initial cleanup to handle repositories from previous sessions
		go func() {
			slog.Debug("Running initial cleanup for repositories from previous sessions")
			m.cleanupOldRepositories()
			// Start the periodic cleanup routine
			m.startCleanupRoutine()
		}()
	}

	// Start monitoring loop
	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	// Initial check
	if err := m.checkForUpdates(); err != nil {
		slog.Debug("Error during initial check", "error", err)
	}

	for {
		select {
		case <-m.ctx.Done():
			slog.Debug("Shutting down monitor")
			return nil
		case <-ticker.C:
			if err := m.checkForUpdates(); err != nil {
				slog.Debug("Error checking for updates", "error", err)
			}
		}
	}
}

func (m *Monitor) Stop() {
	m.cancel()
	m.testRunner.Close()
	if err := m.stateManager.SaveState(); err != nil {
		slog.Debug("Error saving state", "error", err)
	}
}

func (m *Monitor) checkForUpdates() error {
	slog.Debug("Checking for updates")

	// Fetch latest changes only if enabled
	if m.config.FetchRemote {
		if err := m.gitRepo.FetchRemote(); err != nil {
			return fmt.Errorf("failed to fetch remote: %w", err)
		}
	}

	// Get all branches
	branches, err := m.gitRepo.GetBranches()
	if err != nil {
		return fmt.Errorf("failed to get branches: %w", err)
	}

	for _, branch := range branches {
		// slog.Debug("Processing branch", "branch", branch)
		if err := m.processBranchWithDateFilter(branch); err != nil {
			slog.Debug("Error processing branch", "branch", branch, "error", err)
			continue
		}
	}

	return m.stateManager.SaveState()
}

func (m *Monitor) processBranchWithDateFilter(branchName string) error {
	// Get the latest commit for this branch
	latestCommit, err := m.gitRepo.GetLatestCommitForBranch(branchName, m.config.MaxCommitAge)
	if err != nil {
		return err
	}

	// If no commits found within the time window
	if latestCommit == nil {
		return nil
	}

	commitHash := latestCommit.Hash.String()

	// Check if this is a new commit
	state, exists := m.stateManager.GetBranchState(branchName)
	if exists && state.LatestCommit == commitHash {
		return nil // No new commits
	}

	slog.Debug("New commit detected", "branch", branchName, "commit", commitHash[:8], "age", time.Since(latestCommit.Author.When).Truncate(time.Hour))

	// Initialize or get branch state
	if !exists {
		state = &BranchState{
			LatestCommit: "",
		}
		m.stateManager.SetBranchState(branchName, state)
	}

	// Queue the test job
	job := runner.TestJob{Branch: branchName, Commit: commitHash}
	if m.testRunner.QueueTestJob(job) {
		// Update state after queuing
		m.stateManager.UpdateBranchState(branchName, commitHash)
		slog.Debug("Updated state", "branch", branchName)
	}

	return nil
}

// startCleanupRoutine periodically cleans up old repository directories in /tmp/home-ci
func (m *Monitor) startCleanupRoutine() {
	// Run cleanup every hour or every KeepTime/2, whichever is smaller
	cleanupInterval := time.Hour
	if m.config.KeepTime < 2*time.Hour {
		cleanupInterval = m.config.KeepTime / 2
	}
	if cleanupInterval < 10*time.Minute {
		cleanupInterval = 10 * time.Minute // Minimum cleanup interval
	}

	slog.Debug("Starting cleanup routine", "interval", cleanupInterval, "keep_time", m.config.KeepTime)

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	// Also run cleanup more frequently for the first few cycles to catch any missed repositories
	initialTicker := time.NewTicker(5 * time.Minute)
	initialCount := 0
	const maxInitialRuns = 3

	for {
		select {
		case <-m.ctx.Done():
			slog.Debug("Stopping cleanup routine")
			initialTicker.Stop()
			return
		case <-initialTicker.C:
			if initialCount < maxInitialRuns {
				slog.Debug("Running frequent initial cleanup", "run", initialCount+1, "max", maxInitialRuns)
				m.cleanupOldRepositories()
				initialCount++
			} else {
				initialTicker.Stop()
			}
		case <-ticker.C:
			m.cleanupOldRepositories()
		}
	}
}

// cleanupOldRepositories removes repository directories older than KeepTime
func (m *Monitor) cleanupOldRepositories() {
	homeCiDir := "/tmp/home-ci/repos"

	// Check if the directory exists
	if _, err := os.Stat(homeCiDir); os.IsNotExist(err) {
		return // Nothing to clean up
	}

	entries, err := os.ReadDir(homeCiDir)
	if err != nil {
		slog.Debug("Failed to read /tmp/home-ci/repos directory", "error", err)
		return
	}

	cutoffTime := time.Now().Add(-m.config.KeepTime)
	cleaned := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(homeCiDir, entry.Name())

		// Get directory creation/modification time
		dirInfo, err := os.Stat(dirPath)
		if err != nil {
			slog.Debug("Failed to stat directory", "dir", dirPath, "error", err)
			continue
		}

		// Check if directory is older than KeepTime
		if dirInfo.ModTime().Before(cutoffTime) {
			age := time.Since(dirInfo.ModTime())
			slog.Debug("Removing old repository directory", "dir", dirPath, "age", age.Truncate(time.Minute))
			if err := os.RemoveAll(dirPath); err != nil {
				slog.Debug("Failed to remove old repository directory", "dir", dirPath, "error", err)
			} else {
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		slog.Debug("Cleanup completed", "removed_directories", cleaned, "keep_time", m.config.KeepTime)
	}
}
