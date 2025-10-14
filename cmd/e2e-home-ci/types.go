package main

import (
	"context"
	"os/exec"
	"time"
)

type TestType int

const (
	TestNormal TestType = iota
	TestTimeout
	TestQuick
	TestLong
	TestDispatch
)

var testTypeName = map[TestType]string{
	TestNormal:   "normal",
	TestTimeout:  "timeout",
	TestQuick:    "quick",
	TestLong:     "long",
	TestDispatch: "dispatch",
}

// RunningTest represents a test currently in progress
type RunningTest struct {
	Branch    string    `json:"branch"`
	Commit    string    `json:"commit"`
	LogFile   string    `json:"log_file"`
	StartTime time.Time `json:"start_time"`
}

type E2ETestHarness struct {
	testType      TestType
	duration      time.Duration
	testRepoPath  string
	tempRunDir    string // Unique temp directory for this run (contains repo and data)
	homeCIProcess *exec.Cmd
	homeCIContext context.Context
	homeCICancel  context.CancelFunc
	noCleanup     bool // Skip cleanup for debugging

	// Statistics
	commitsCreated     int
	branchesCreated    int
	runningTests       []RunningTest
	totalTestsDetected int // Total number of tests detected (for statistics)
	timeoutDetected    bool
	logCheckCount      int  // Counter for periodic display
	stateFileRead      bool // Track if we've successfully read state.json
}

// TestResult represents the test result structure (copy from runner package to avoid import)
type TestResult struct {
	Branch                    string        `json:"branch"`
	Commit                    string        `json:"commit"`
	LogFile                   string        `json:"log_file"`
	StartTime                 time.Time     `json:"start_time"`
	EndTime                   time.Time     `json:"end_time"`
	Duration                  time.Duration `json:"duration"`
	Success                   bool          `json:"success"`
	TimedOut                  bool          `json:"timed_out"`
	CleanupExecuted           bool          `json:"cleanup_executed"`
	CleanupSuccess            bool          `json:"cleanup_success"`
	GitHubActionsNotified     bool          `json:"github_actions_notified"`
	GitHubActionsSuccess      bool          `json:"github_actions_success"`
	ErrorMessage              string        `json:"error_message,omitempty"`
	CleanupErrorMessage       string        `json:"cleanup_error_message,omitempty"`
	GitHubActionsErrorMessage string        `json:"github_actions_error_message,omitempty"`
}

// TestExpectationConfig represents the test expectations configuration
type TestExpectationConfig struct {
	GlobalScenarios struct {
		CommitPatterns []struct {
			Pattern        string `yaml:"pattern"`
			ExpectedResult string `yaml:"expected_result"`
			Description    string `yaml:"description"`
		} `yaml:"commit_patterns"`
	} `yaml:"global_scenarios"`

	BranchScenarios map[string]struct {
		DefaultResult string `yaml:"default_result"`
		Description   string `yaml:"description"`
		SpecialCases  []struct {
			CommitHashPrefix string `yaml:"commit_hash_prefix"`
			ExpectedResult   string `yaml:"expected_result"`
			Description      string `yaml:"description"`
		} `yaml:"special_cases"`
	} `yaml:"branch_scenarios"`
}

// ValidationResult represents the result of validating test expectations
type ValidationResult struct {
	TotalTests         int     `json:"total_tests"`
	ExpectedSuccesses  int     `json:"expected_successes"`
	ExpectedFailures   int     `json:"expected_failures"`
	ExpectedTimeouts   int     `json:"expected_timeouts"`
	ActualSuccesses    int     `json:"actual_successes"`
	ActualFailures     int     `json:"actual_failures"`
	ActualTimeouts     int     `json:"actual_timeouts"`
	CorrectPredictions int     `json:"correct_predictions"`
	ValidationScore    float64 `json:"validation_score"`
}

// parseTestType parses test type from string
func parseTestType(s string) TestType {
	switch s {
	case "timeout":
		return TestTimeout
	case "quick":
		return TestQuick
	case "long":
		return TestLong
	case "dispatch":
		return TestDispatch
	default:
		return TestNormal
	}
}

// helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}