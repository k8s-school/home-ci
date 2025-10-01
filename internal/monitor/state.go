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

type StateManager struct {
	branchStates map[string]*BranchState
	stateMutex   sync.RWMutex
	stateFile    string
}

func NewStateManager(stateFile string) *StateManager {
	return &StateManager{
		branchStates: make(map[string]*BranchState),
		stateFile:    stateFile,
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

	return json.Unmarshal(data, &sm.branchStates)
}

func (sm *StateManager) SaveState() error {
	sm.stateMutex.RLock()
	data, err := json.MarshalIndent(sm.branchStates, "", "  ")
	sm.stateMutex.RUnlock()

	if err != nil {
		return err
	}

	return os.WriteFile(sm.stateFile, data, 0644)
}

func (sm *StateManager) GetBranchState(branch string) (*BranchState, bool) {
	sm.stateMutex.RLock()
	defer sm.stateMutex.RUnlock()

	state, exists := sm.branchStates[branch]
	return state, exists
}

func (sm *StateManager) SetBranchState(branch string, state *BranchState) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	sm.branchStates[branch] = state
}

func (sm *StateManager) UpdateBranchState(branch, commit string) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	state := sm.branchStates[branch]
	now := time.Now()

	state.LastCommit = commit
	state.LastRunTime = now
}