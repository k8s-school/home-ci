package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

type Config struct {
	RepoPath        string        `json:"repo_path"`
	CheckInterval   time.Duration `json:"check_interval"`
	TestScript      string        `json:"test_script"`
	MaxRunsPerDay   int           `json:"max_runs_per_day"`
	InputSurvey     string        `json:"input_survey"`
	EnableScience   bool          `json:"enable_science"`
	EnableCleanup   bool          `json:"enable_cleanup"`
	EnableMonitoring bool         `json:"enable_monitoring"`
}

type BranchState struct {
	LastCommit    string    `json:"last_commit"`
	LastRunTime   time.Time `json:"last_run_time"`
	RunsToday     int       `json:"runs_today"`
	LastRunDate   string    `json:"last_run_date"`
}

type Monitor struct {
	config       Config
	repo         *git.Repository
	branchStates map[string]*BranchState
	stateMutex   sync.RWMutex
	stateFile    string
	testQueue    chan TestJob
	ctx          context.Context
	cancel       context.CancelFunc
}

type TestJob struct {
	Branch string
	Commit string
}

func NewMonitor(configPath string) (*Monitor, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	repo, err := git.PlainOpen(config.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &Monitor{
		config:       config,
		repo:         repo,
		branchStates: make(map[string]*BranchState),
		stateFile:    filepath.Join(config.RepoPath, ".git-ci-monitor-state.json"),
		testQueue:    make(chan TestJob, 100),
		ctx:          ctx,
		cancel:       cancel,
	}

	if err := m.loadState(); err != nil {
		log.Printf("Warning: failed to load previous state: %v", err)
	}

	return m, nil
}

func loadConfig(path string) (Config, error) {
	var config Config

	// Default config
	config = Config{
		RepoPath:        ".",
		CheckInterval:   5 * time.Minute,
		TestScript:      "./e2e/fink-ci.sh",
		MaxRunsPerDay:   1,
		InputSurvey:     "ztf",
		EnableScience:   false,
		EnableCleanup:   true,
		EnableMonitoring: false,
	}

	if path == "" {
		return config, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil // Use defaults
		}
		return config, err
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("failed to parse config: %w", err)
	}

	return config, nil
}

func (m *Monitor) loadState() error {
	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No previous state
		}
		return err
	}

	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()

	return json.Unmarshal(data, &m.branchStates)
}

func (m *Monitor) saveState() error {
	m.stateMutex.RLock()
	data, err := json.MarshalIndent(m.branchStates, "", "  ")
	m.stateMutex.RUnlock()

	if err != nil {
		return err
	}

	return os.WriteFile(m.stateFile, data, 0644)
}

func (m *Monitor) Start() error {
	log.Printf("Starting Git CI Monitor...")
	log.Printf("Repository: %s", m.config.RepoPath)
	log.Printf("Check interval: %v", m.config.CheckInterval)
	log.Printf("Max runs per day: %d", m.config.MaxRunsPerDay)

	// Start test runner goroutine
	go m.testRunner()

	// Start monitoring loop
	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	// Initial check
	if err := m.checkForUpdates(); err != nil {
		log.Printf("Error during initial check: %v", err)
	}

	for {
		select {
		case <-m.ctx.Done():
			log.Println("Shutting down monitor...")
			return nil
		case <-ticker.C:
			if err := m.checkForUpdates(); err != nil {
				log.Printf("Error checking for updates: %v", err)
			}
		}
	}
}

func (m *Monitor) Stop() {
	m.cancel()
	close(m.testQueue)
	if err := m.saveState(); err != nil {
		log.Printf("Error saving state: %v", err)
	}
}

func (m *Monitor) checkForUpdates() error {
	log.Println("Checking for updates...")

	// Fetch latest changes
	if err := m.fetchRemote(); err != nil {
		return fmt.Errorf("failed to fetch remote: %w", err)
	}

	// Get all remote branches
	branches, err := m.getRemoteBranches()
	if err != nil {
		return fmt.Errorf("failed to get remote branches: %w", err)
	}

	for _, branch := range branches {
		if err := m.processBranch(branch); err != nil {
			log.Printf("Error processing branch %s: %v", branch, err)
			continue
		}
	}

	return m.saveState()
}

func (m *Monitor) fetchRemote() error {
	cmd := exec.Command("git", "fetch", "origin")
	cmd.Dir = m.config.RepoPath
	return cmd.Run()
}

func (m *Monitor) getRemoteBranches() ([]string, error) {
	remote, err := m.repo.Remote("origin")
	if err != nil {
		return nil, err
	}

	refs, err := remote.List(&git.ListOptions{})
	if err != nil {
		return nil, err
	}

	var branches []string
	for _, ref := range refs {
		if ref.Name().IsBranch() {
			branchName := ref.Name().Short()
			if branchName != "HEAD" {
				branches = append(branches, branchName)
			}
		}
	}

	return branches, nil
}

func (m *Monitor) processBranch(branchName string) error {
	// Get the latest commit for this branch
	refName := fmt.Sprintf("refs/remotes/origin/%s", branchName)
	ref, err := m.repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return fmt.Errorf("failed to get reference for branch %s: %w", branchName, err)
	}

	commitHash := ref.Hash().String()

	m.stateMutex.Lock()
	state, exists := m.branchStates[branchName]
	if !exists {
		state = &BranchState{
			LastCommit:  "",
			LastRunTime: time.Time{},
			RunsToday:   0,
			LastRunDate: "",
		}
		m.branchStates[branchName] = state
	}
	m.stateMutex.Unlock()

	// Check if this is a new commit
	if state.LastCommit == commitHash {
		return nil // No new commits
	}

	log.Printf("New commit detected on branch %s: %s", branchName, commitHash[:8])

	// Check daily limit
	today := time.Now().Format("2006-01-02")
	if state.LastRunDate == today && state.RunsToday >= m.config.MaxRunsPerDay {
		log.Printf("Daily limit reached for branch %s (%d/%d runs)", branchName, state.RunsToday, m.config.MaxRunsPerDay)

		// Update the last commit hash but don't run tests
		m.stateMutex.Lock()
		state.LastCommit = commitHash
		m.stateMutex.Unlock()

		return nil
	}

	// Queue the test job
	select {
	case m.testQueue <- TestJob{Branch: branchName, Commit: commitHash}:
		log.Printf("Queued test job for branch %s", branchName)
	default:
		log.Printf("Test queue full, skipping branch %s", branchName)
	}

	return nil
}

func (m *Monitor) testRunner() {
	log.Println("Starting test runner...")

	for job := range m.testQueue {
		log.Printf("Starting tests for branch %s, commit %s", job.Branch, job.Commit[:8])

		if err := m.runTests(job.Branch, job.Commit); err != nil {
			log.Printf("Tests failed for branch %s: %v", job.Branch, err)
		} else {
			log.Printf("Tests completed successfully for branch %s", job.Branch)
		}

		// Update state after test completion (whether success or failure)
		m.updateBranchState(job.Branch, job.Commit)
	}
}

func (m *Monitor) runTests(branch, commit string) error {
	log.Printf("Running tests for branch %s (commit %s)", branch, commit[:8])

	args := []string{"-b", branch, "-i", m.config.InputSurvey}

	if m.config.EnableScience {
		args = append(args, "-s")
	}
	if m.config.EnableCleanup {
		args = append(args, "-c")
	}
	if m.config.EnableMonitoring {
		args = append(args, "-m")
	}

	cmd := exec.CommandContext(m.ctx, m.config.TestScript, args...)
	cmd.Dir = m.config.RepoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set environment variables that the script expects
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("TOKEN=%s", os.Getenv("TOKEN")),
		fmt.Sprintf("USER=%s", os.Getenv("USER")),
	)

	return cmd.Run()
}

func (m *Monitor) updateBranchState(branch, commit string) {
	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()

	state := m.branchStates[branch]
	now := time.Now()
	today := now.Format("2006-01-02")

	state.LastCommit = commit
	state.LastRunTime = now

	if state.LastRunDate != today {
		state.RunsToday = 1
		state.LastRunDate = today
	} else {
		state.RunsToday++
	}

	log.Printf("Updated state for branch %s: runs today %d/%d", branch, state.RunsToday, m.config.MaxRunsPerDay)
}

func main() {
	configPath := ""
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	monitor, err := NewMonitor(configPath)
	if err != nil {
		log.Fatal(err)
	}

	// Handle graceful shutdown
	go func() {
		// Wait for interrupt signal
		// In a real implementation, you'd handle SIGINT/SIGTERM
		select {}
	}()

	if err := monitor.Start(); err != nil {
		log.Fatal(err)
	}
}