package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

type TestType int

const (
	// Single commit tests (unit tests)
	TestSuccess TestType = iota
	TestFail
	TestTimeout
	TestDispatchOneSuccess
	TestDispatchNoTokenFile
	// Multi commit tests
	TestQuick
	TestDispatchAll
	TestNormal
	TestLong
	TestConcurrentLimit
	TestContinuousCI
	TestCacheLocal
	TestCacheRemote
)

var testTypeName = map[TestType]string{
	TestSuccess:             "success",
	TestFail:                "fail",
	TestTimeout:             "timeout",
	TestDispatchOneSuccess:  "dispatch-one-success",
	TestDispatchNoTokenFile: "dispatch-no-token-file",
	TestQuick:               "quick",
	TestDispatchAll:         "dispatch-all",
	TestNormal:              "normal",
	TestLong:                "long",
	TestConcurrentLimit:     "concurrent-limit",
	TestContinuousCI:        "continuous-ci",
	TestCacheLocal:          "cache-local",
	TestCacheRemote:         "cache-remote",
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

// TestExecutionError represents an error during test execution (not configuration)
// This type of error should not display usage information
type TestExecutionError struct {
	message string
}

func (e *TestExecutionError) Error() string {
	return e.message
}

// NewTestExecutionError creates a new test execution error
func NewTestExecutionError(message string) *TestExecutionError {
	return &TestExecutionError{message: message}
}

// parseTestType parses test type from string
func parseTestType(s string) (TestType, error) {
	switch s {
	case "success":
		return TestSuccess, nil
	case "fail":
		return TestFail, nil
	case "timeout":
		return TestTimeout, nil
	case "dispatch-one-success":
		return TestDispatchOneSuccess, nil
	case "dispatch-no-token-file":
		return TestDispatchNoTokenFile, nil
	case "dispatch-all":
		return TestDispatchAll, nil
	case "quick":
		return TestQuick, nil
	case "normal":
		return TestNormal, nil
	case "long":
		return TestLong, nil
	case "concurrent-limit":
		return TestConcurrentLimit, nil
	case "continuous-ci":
		return TestContinuousCI, nil
	default:
		return TestNormal, fmt.Errorf("unsupported test type '%s'. Valid types are: success, fail, timeout, dispatch-one-success, dispatch-no-token-file, dispatch-all, quick, normal, long, concurrent-limit, continuous-ci", s)
	}
}

// isSingleCommitTest returns true for tests that need only one commit
func (tt TestType) isSingleCommitTest() bool {
	return tt == TestSuccess || tt == TestFail || tt == TestTimeout || tt == TestDispatchOneSuccess || tt == TestDispatchNoTokenFile
}

// isMultiCommitTest returns true for tests that need multiple commits
func (tt TestType) isMultiCommitTest() bool {
	return tt == TestQuick || tt == TestDispatchAll || tt == TestNormal || tt == TestLong || tt == TestConcurrentLimit || tt == TestContinuousCI
}

// getTestDirectory returns the base directory for this test type
func (tt TestType) getTestDirectory() string {
	return fmt.Sprintf("/tmp/home-ci/e2e/%s", testTypeName[tt])
}

// getRepoPath returns the repository path for this test type
func (tt TestType) getRepoPath() string {
	return filepath.Join(tt.getTestDirectory(), "repo")
}

// getDataPath returns the data directory path for this test type
func (tt TestType) getDataPath() string {
	return filepath.Join(tt.getTestDirectory(), "data")
}

// isDispatchTest returns true for tests that use GitHub Actions dispatch
func (tt TestType) isDispatchTest() bool {
	return tt == TestDispatchOneSuccess || tt == TestDispatchAll || tt == TestDispatchNoTokenFile
}

// helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}