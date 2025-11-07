package monitor

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/k8s-school/home-ci/internal/runner"
)

type BranchState struct {
	LatestCommit string `json:"latest_commit"`
}

// RunningTest is now defined in runner package

type State struct {
	BranchStates map[string]*BranchState `json:"branch_states"`
	RunningTests []runner.RunningTest    `json:"running_tests"`
}

type StateManager struct {
	state      *State
	stateMutex sync.RWMutex
	stateFile  string
}

func NewStateManager(stateFile string) *StateManager {
	return &StateManager{
		state: &State{
			BranchStates: make(map[string]*BranchState),
			RunningTests: make([]runner.RunningTest, 0),
		},
		stateFile: stateFile,
	}
}

func (sm *StateManager) LoadState() error {
	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("No previous state file found, starting with clean state", "file", sm.stateFile)
			return nil // No previous state
		}
		slog.Error("Failed to read state file", "file", sm.stateFile, "error", err)
		return err
	}

	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	// Try to unmarshal as new format first
	var newState State
	if err := json.Unmarshal(data, &newState); err == nil {
		sm.state = &newState
		// Ensure RunningTests is never nil
		if sm.state.RunningTests == nil {
			sm.state.RunningTests = make([]runner.RunningTest, 0)
		}
		slog.Debug("Loaded state from file", "file", sm.stateFile, "branches", len(sm.state.BranchStates), "running_tests", len(sm.state.RunningTests))
		return nil
	}

	// Fallback: try to unmarshal as old format (just branch states)
	var oldBranchStates map[string]*BranchState
	if err := json.Unmarshal(data, &oldBranchStates); err == nil {
		sm.state.BranchStates = oldBranchStates
		sm.state.RunningTests = make([]runner.RunningTest, 0)
		slog.Debug("Migrated old state format", "file", sm.stateFile, "branches", len(sm.state.BranchStates))
		return nil
	}

	slog.Error("Failed to parse state file", "file", sm.stateFile, "error", err)
	return err
}

func (sm *StateManager) SaveState() error {
	sm.stateMutex.RLock()
	// Ensure RunningTests is never nil before marshaling
	if sm.state.RunningTests == nil {
		sm.state.RunningTests = make([]runner.RunningTest, 0)
	}

	runningCount := len(sm.state.RunningTests)
	branchCount := len(sm.state.BranchStates)

	data, err := json.MarshalIndent(sm.state, "", "  ")
	sm.stateMutex.RUnlock()

	if err != nil {
		slog.Error("Failed to marshal state to JSON", "error", err)
		return err
	}

	if err := os.WriteFile(sm.stateFile, data, 0644); err != nil {
		slog.Error("Failed to write state file", "file", sm.stateFile, "error", err)
		return err
	}

	slog.Debug("Saved state to file", "file", sm.stateFile, "branches", branchCount, "running_tests", runningCount)
	return nil
}

func (sm *StateManager) GetBranchState(branch string) (*BranchState, bool) {
	sm.stateMutex.RLock()
	defer sm.stateMutex.RUnlock()

	state, exists := sm.state.BranchStates[branch]
	return state, exists
}

func (sm *StateManager) SetBranchState(branch string, state *BranchState) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	sm.state.BranchStates[branch] = state
}

func (sm *StateManager) UpdateBranchState(branch, commit string) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	state := sm.state.BranchStates[branch]
	state.LatestCommit = commit
}

// Running tests management
func (sm *StateManager) AddRunningTest(test runner.RunningTest) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	sm.state.RunningTests = append(sm.state.RunningTests, test)
	slog.Debug("Added running test to state", "branch", test.Branch, "commit", test.Commit[:8], "log_file", test.LogFile)
}

func (sm *StateManager) RemoveRunningTest(branch, commit string) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	initialCount := len(sm.state.RunningTests)
	var filtered []runner.RunningTest
	for _, test := range sm.state.RunningTests {
		if test.Branch != branch || test.Commit != commit {
			filtered = append(filtered, test)
		}
	}
	sm.state.RunningTests = filtered

	if len(sm.state.RunningTests) < initialCount {
		slog.Debug("Removed running test from state", "branch", branch, "commit", commit[:8], "remaining_tests", len(sm.state.RunningTests))
	} else {
		slog.Warn("Attempted to remove non-existent running test", "branch", branch, "commit", commit[:8])
	}
}

func (sm *StateManager) GetRunningTests() []runner.RunningTest {
	sm.stateMutex.RLock()
	defer sm.stateMutex.RUnlock()

	// Return a copy to avoid race conditions
	tests := make([]runner.RunningTest, len(sm.state.RunningTests))
	copy(tests, sm.state.RunningTests)
	return tests
}

// GetRunningTestsTyped is now redundant since GetRunningTests returns concrete type

func (sm *StateManager) CleanupOldRunningTests(maxAge time.Duration) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	cutoff := time.Now().Add(-maxAge)
	var active []runner.RunningTest

	for _, test := range sm.state.RunningTests {
		if test.StartTime.After(cutoff) {
			active = append(active, test)
		}
	}

	sm.state.RunningTests = active
}
