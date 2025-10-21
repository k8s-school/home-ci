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

const (
	// Directory names
	homeCIDirName    = ".home-ci"
	stateFileName    = "state.json"
	tmpHomeCIRepos   = "/tmp/home-ci/repos"

	// Cleanup intervals
	defaultCleanupInterval = time.Hour
	minCleanupInterval     = 10 * time.Minute
	initialCleanupInterval = 5 * time.Minute
	maxInitialCleanupRuns  = 3

	// Directory permissions
	dirPerm = 0755
)

type Monitor struct {
	config       config.Config
	gitRepo      *GitRepository
	stateManager *StateManager
	testRunner   *runner.TestRunner
	cleanupMgr   *CleanupManager
	ctx          context.Context
	cancel       context.CancelFunc
}

// CleanupManager handles repository cleanup operations
type CleanupManager struct {
	keepTime     time.Duration
	ctx          context.Context
}

// NewCleanupManager creates a new cleanup manager
func NewCleanupManager(keepTime time.Duration, ctx context.Context) *CleanupManager {
	return &CleanupManager{
		keepTime: keepTime,
		ctx:      ctx,
	}
}

func NewMonitor(cfg config.Config, configPath string) (*Monitor, error) {
	gitRepo, err := NewGitRepository(cfg.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repository at '%s': %w\n\nPlease check your configuration:\n1. Ensure repo_path in %s points to a valid git repository\n2. If using a placeholder configuration, update repo_path to point to an actual repository\n3. Example: repo_path: \"/path/to/your/repo\" or repo_path: \".\" for current directory", cfg.RepoPath, err, configPath)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create .home-ci directory in repo for logs and state
	homeCIDir := filepath.Join(cfg.RepoPath, homeCIDirName)
	if err := os.MkdirAll(homeCIDir, dirPerm); err != nil {
		return nil, fmt.Errorf("failed to create .home-ci directory: %w", err)
	}

	logDir := homeCIDir
	stateFile := filepath.Join(homeCIDir, stateFileName)
	stateManager := NewStateManager(stateFile)

	testRunner := runner.NewTestRunner(cfg, configPath, logDir, ctx, stateManager)
	cleanupMgr := NewCleanupManager(cfg.KeepTime, ctx)

	m := &Monitor{
		config:       cfg,
		gitRepo:      gitRepo,
		stateManager: stateManager,
		testRunner:   testRunner,
		cleanupMgr:   cleanupMgr,
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
		go m.cleanupMgr.startCleanupRoutine()
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

	if err := m.fetchRemoteIfEnabled(); err != nil {
		return err
	}

	branches, err := m.gitRepo.GetBranches()
	if err != nil {
		return fmt.Errorf("failed to get branches: %w", err)
	}

	m.processBranches(branches)
	return m.stateManager.SaveState()
}

// fetchRemoteIfEnabled fetches remote changes if enabled in config
func (m *Monitor) fetchRemoteIfEnabled() error {
	if !m.config.FetchRemote {
		return nil
	}

	if err := m.gitRepo.FetchRemote(); err != nil {
		return fmt.Errorf("failed to fetch remote: %w", err)
	}
	return nil
}

// processBranches processes all branches for new commits
func (m *Monitor) processBranches(branches []string) {
	for _, branch := range branches {
		if err := m.processBranchWithDateFilter(branch); err != nil {
			slog.Debug("Error processing branch", "branch", branch, "error", err)
			continue
		}
	}
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
func (cm *CleanupManager) startCleanupRoutine() {
	if cm.keepTime <= 0 {
		return
	}

	// Run initial cleanup to handle repositories from previous sessions
	slog.Debug("Running initial cleanup for repositories from previous sessions")
	cm.cleanupOldRepositories()

	cleanupInterval := cm.calculateCleanupInterval()
	slog.Debug("Starting cleanup routine", "interval", cleanupInterval, "keep_time", cm.keepTime)

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	// Also run cleanup more frequently for the first few cycles to catch any missed repositories
	initialTicker := time.NewTicker(initialCleanupInterval)
	initialCount := 0

	for {
		select {
		case <-cm.ctx.Done():
			slog.Debug("Stopping cleanup routine")
			initialTicker.Stop()
			return
		case <-initialTicker.C:
			if initialCount < maxInitialCleanupRuns {
				slog.Debug("Running frequent initial cleanup", "run", initialCount+1, "max", maxInitialCleanupRuns)
				cm.cleanupOldRepositories()
				initialCount++
			} else {
				initialTicker.Stop()
			}
		case <-ticker.C:
			cm.cleanupOldRepositories()
		}
	}
}

// calculateCleanupInterval determines the appropriate cleanup interval
func (cm *CleanupManager) calculateCleanupInterval() time.Duration {
	// Run cleanup every hour or every KeepTime/2, whichever is smaller
	cleanupInterval := defaultCleanupInterval
	if cm.keepTime < 2*time.Hour {
		cleanupInterval = cm.keepTime / 2
	}
	if cleanupInterval < minCleanupInterval {
		cleanupInterval = minCleanupInterval
	}
	return cleanupInterval
}

// cleanupOldRepositories removes repository directories older than KeepTime
func (cm *CleanupManager) cleanupOldRepositories() {
	// Check if the directory exists
	if _, err := os.Stat(tmpHomeCIRepos); os.IsNotExist(err) {
		return // Nothing to clean up
	}

	entries, err := os.ReadDir(tmpHomeCIRepos)
	if err != nil {
		slog.Debug("Failed to read repository directory", "dir", tmpHomeCIRepos, "error", err)
		return
	}

	cutoffTime := time.Now().Add(-cm.keepTime)
	cleaned := cm.cleanupDirectories(entries, cutoffTime)

	if cleaned > 0 {
		slog.Debug("Cleanup completed", "removed_directories", cleaned, "keep_time", cm.keepTime)
	}
}

// cleanupDirectories processes directory entries for cleanup
func (cm *CleanupManager) cleanupDirectories(entries []os.DirEntry, cutoffTime time.Time) int {
	cleaned := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(tmpHomeCIRepos, entry.Name())
		if cm.shouldRemoveDirectory(dirPath, cutoffTime) {
			cleaned++
		}
	}

	return cleaned
}

// shouldRemoveDirectory checks if a directory should be removed and removes it
func (cm *CleanupManager) shouldRemoveDirectory(dirPath string, cutoffTime time.Time) bool {
	dirInfo, err := os.Stat(dirPath)
	if err != nil {
		slog.Debug("Failed to stat directory", "dir", dirPath, "error", err)
		return false
	}

	// Check if directory is older than KeepTime
	if !dirInfo.ModTime().Before(cutoffTime) {
		return false
	}

	age := time.Since(dirInfo.ModTime())
	slog.Debug("Removing old repository directory", "dir", dirPath, "age", age.Truncate(time.Minute))

	if err := os.RemoveAll(dirPath); err != nil {
		slog.Debug("Failed to remove old repository directory", "dir", dirPath, "error", err)
		return false
	}

	return true
}
