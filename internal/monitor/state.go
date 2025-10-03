package monitor

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type BranchState struct {
	LastCommit  string    `json:"last_commit"`
	LastRunTime time.Time `json:"last_run_time"`
	RunsToday   int       `json:"runs_today"`
	LastRunDate string    `json:"last_run_date"`
}

type RunningTest struct {
	Branch    string    `json:"branch"`
	Commit    string    `json:"commit"`
	LogFile   string    `json:"log_file"`
	StartTime time.Time `json:"start_time"`
	PID       int       `json:"pid,omitempty"` // Process ID if available
}

type State struct {
	BranchStates map[string]*BranchState `json:"branch_states"`
	RunningTests []RunningTest           `json:"running_tests"`
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
			RunningTests: make([]RunningTest, 0),
		},
		stateFile: stateFile,
	}
}

func (sm *StateManager) LoadState() error {
	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No previous state
		}
		return err
	}

	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	// Try to unmarshal as new format first
	var newState State
	if err := json.Unmarshal(data, &newState); err == nil {
		sm.state = &newState
		return nil
	}

	// Fallback: try to unmarshal as old format (just branch states)
	var oldBranchStates map[string]*BranchState
	if err := json.Unmarshal(data, &oldBranchStates); err == nil {
		sm.state.BranchStates = oldBranchStates
		sm.state.RunningTests = make([]RunningTest, 0)
		return nil
	}

	return err
}

func (sm *StateManager) SaveState() error {
	sm.stateMutex.RLock()
	data, err := json.MarshalIndent(sm.state, "", "  ")
	sm.stateMutex.RUnlock()

	if err != nil {
		return err
	}

	return os.WriteFile(sm.stateFile, data, 0644)
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
	now := time.Now()

	state.LastCommit = commit
	state.LastRunTime = now
}

// Running tests management
func (sm *StateManager) AddRunningTest(testInterface interface{}) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	// Convert interface{} to RunningTest
	if test, ok := testInterface.(RunningTest); ok {
		sm.state.RunningTests = append(sm.state.RunningTests, test)
	}
}

func (sm *StateManager) RemoveRunningTest(branch, commit string) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	var filtered []RunningTest
	for _, test := range sm.state.RunningTests {
		if test.Branch != branch || test.Commit != commit {
			filtered = append(filtered, test)
		}
	}
	sm.state.RunningTests = filtered
}

func (sm *StateManager) GetRunningTests() []interface{} {
	sm.stateMutex.RLock()
	defer sm.stateMutex.RUnlock()

	// Convert to []interface{} for the interface
	result := make([]interface{}, len(sm.state.RunningTests))
	for i, test := range sm.state.RunningTests {
		result[i] = test
	}
	return result
}

func (sm *StateManager) GetRunningTestsTyped() []RunningTest {
	sm.stateMutex.RLock()
	defer sm.stateMutex.RUnlock()

	// Return a copy to avoid race conditions
	tests := make([]RunningTest, len(sm.state.RunningTests))
	copy(tests, sm.state.RunningTests)
	return tests
}

func (sm *StateManager) CleanupOldRunningTests(maxAge time.Duration) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	cutoff := time.Now().Add(-maxAge)
	var active []RunningTest

	for _, test := range sm.state.RunningTests {
		if test.StartTime.After(cutoff) {
			active = append(active, test)
		}
	}

	sm.state.RunningTests = active
}