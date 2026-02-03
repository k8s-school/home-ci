package state

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/k8s-school/home-ci/internal/runner"
)

// RepositoryState represents the state for a single repository
type RepositoryState struct {
	BranchStates map[string]*runner.BranchState `json:"branch_states"`
	RunningTests []runner.RunningTest           `json:"running_tests"`
	LastUpdated  time.Time                      `json:"last_updated"`
}

// StateManager manages per-repository state files
type StateManager struct {
	stateDir   string
	repoName   string
	stateMutex sync.RWMutex
	state      *RepositoryState
}

// NewStateManager creates a new state manager for a specific repository
func NewStateManager(stateDir, repoName string) *StateManager {
	return &StateManager{
		stateDir: stateDir,
		repoName: repoName,
		state: &RepositoryState{
			BranchStates: make(map[string]*runner.BranchState),
			RunningTests: make([]runner.RunningTest, 0),
			LastUpdated:  time.Now(),
		},
	}
}

// getStateFilePath returns the path to the state file for this repository
func (sm *StateManager) getStateFilePath() string {
	return filepath.Join(sm.stateDir, fmt.Sprintf("%s.json", sm.repoName))
}

// LoadState loads the state from the repository-specific state file
func (sm *StateManager) LoadState() error {
	// Ensure state directory exists
	if err := os.MkdirAll(sm.stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory %s: %w", sm.stateDir, err)
	}

	stateFile := sm.getStateFilePath()
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("No previous state file found, starting with clean state",
				"repo", sm.repoName, "file", stateFile)
			return nil // No previous state
		}
		slog.Error("Failed to read state file", "repo", sm.repoName, "file", stateFile, "error", err)
		return err
	}

	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	// Try to unmarshal as repository state format
	var newState RepositoryState
	if err := json.Unmarshal(data, &newState); err == nil {
		sm.state = &newState
		// Ensure RunningTests is never nil
		if sm.state.RunningTests == nil {
			sm.state.RunningTests = make([]runner.RunningTest, 0)
		}
		slog.Debug("Loaded repository state from file",
			"repo", sm.repoName,
			"file", stateFile,
			"branches", len(sm.state.BranchStates),
			"running_tests", len(sm.state.RunningTests))
		return nil
	}

	// Invalid or old format - start with clean state
	slog.Info("State file has invalid or old format, starting with clean state",
		"repo", sm.repoName, "file", stateFile)
	// State is already initialized in NewStateManager with clean values

	return nil
}

// SaveState saves the current state to the repository-specific state file
func (sm *StateManager) SaveState() error {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	sm.state.LastUpdated = time.Now()

	// Ensure RunningTests is never nil before marshaling
	if sm.state.RunningTests == nil {
		sm.state.RunningTests = make([]runner.RunningTest, 0)
	}

	data, err := json.MarshalIndent(sm.state, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal repository state to JSON", "repo", sm.repoName, "error", err)
		return err
	}

	// Ensure state directory exists before writing file
	if err := os.MkdirAll(sm.stateDir, 0755); err != nil {
		slog.Error("Failed to create state directory", "repo", sm.repoName, "dir", sm.stateDir, "error", err)
		return fmt.Errorf("failed to create state directory %s: %w", sm.stateDir, err)
	}

	stateFile := sm.getStateFilePath()
	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		slog.Error("Failed to write repository state file", "repo", sm.repoName, "file", stateFile, "error", err)
		return err
	}

	slog.Debug("Saved repository state to file",
		"repo", sm.repoName,
		"file", stateFile,
		"branches", len(sm.state.BranchStates),
		"running_tests", len(sm.state.RunningTests))

	return nil
}

// GetBranchState returns the state for a specific branch
func (sm *StateManager) GetBranchState(branch string) *runner.BranchState {
	sm.stateMutex.RLock()
	defer sm.stateMutex.RUnlock()

	return sm.state.BranchStates[branch]
}

// UpdateBranchState updates the state for a specific branch
func (sm *StateManager) UpdateBranchState(branch, commit string) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	if sm.state.BranchStates[branch] == nil {
		sm.state.BranchStates[branch] = &runner.BranchState{}
	}
	sm.state.BranchStates[branch].LatestCommit = commit
}

// AddRunningTest adds a test to the running tests list
func (sm *StateManager) AddRunningTest(test runner.RunningTest) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	sm.state.RunningTests = append(sm.state.RunningTests, test)
}

// RemoveRunningTest removes a test from the running tests list
func (sm *StateManager) RemoveRunningTest(branch, commit string) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	for i, test := range sm.state.RunningTests {
		if test.Branch == branch && test.Commit == commit {
			sm.state.RunningTests = append(sm.state.RunningTests[:i], sm.state.RunningTests[i+1:]...)
			break
		}
	}
}

// GetRunningTests returns a copy of the running tests list
func (sm *StateManager) GetRunningTests() []runner.RunningTest {
	sm.stateMutex.RLock()
	defer sm.stateMutex.RUnlock()

	// Return a copy to avoid race conditions
	tests := make([]runner.RunningTest, len(sm.state.RunningTests))
	copy(tests, sm.state.RunningTests)
	return tests
}

// CleanupOldRunningTests removes tests older than maxAge from the running tests list
func (sm *StateManager) CleanupOldRunningTests(maxAge time.Duration) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	cutoff := time.Now().Add(-maxAge)
	var activeTests []runner.RunningTest

	for _, test := range sm.state.RunningTests {
		if test.StartTime.After(cutoff) {
			activeTests = append(activeTests, test)
		} else {
			slog.Debug("Removing stale running test",
				"repo", sm.repoName,
				"branch", test.Branch,
				"commit", test.Commit[:8],
				"age", time.Since(test.StartTime))
		}
	}

	removedCount := len(sm.state.RunningTests) - len(activeTests)
	if removedCount > 0 {
		sm.state.RunningTests = activeTests
		slog.Info("Cleaned up old running tests",
			"repo", sm.repoName,
			"removed", removedCount,
			"remaining", len(activeTests))
	}
}
