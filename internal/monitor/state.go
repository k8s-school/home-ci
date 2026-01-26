package monitor

import (
	"sync"

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










// GetRunningTestsTyped is now redundant since GetRunningTests returns concrete type

