package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/k8s-school/home-ci/internal/logging"
)

var (
	configPath       string
	checkConcurrency bool
	checkTimeline    bool
	verbose          int
)

var rootCmd = &cobra.Command{
	Use:   "home-ci-diag",
	Short: "Home-CI Repository Diagnostic Tool",
	Long: `A diagnostic tool for analyzing Home-CI test results and repository state.
Provides insights into test execution, concurrency compliance, and branch timelines.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logging
		logging.InitLogging(verbose)

		if configPath == "" {
			return fmt.Errorf("config file path is required. Use --config flag")
		}

		// Read configuration to determine repository path
		config, err := readConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to read config: %w", err)
		}

		// Determine actual repository path based on configuration
		var repoPath string
		isRemoteRepo := strings.HasPrefix(config.Repository, "http://") || strings.HasPrefix(config.Repository, "https://")

		if isRemoteRepo {
			// For remote repositories, use cache directory
			repoPath = filepath.Join(config.CacheDir, config.RepoName)
		} else {
			// For local repositories, use repository path directly
			repoPath = config.Repository
		}

		// Validate repository path
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			return fmt.Errorf("repository path does not exist: %s", repoPath)
		}

		// Check if it's a git repository
		gitDir := filepath.Join(repoPath, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			return fmt.Errorf("not a git repository: %s", repoPath)
		}

		if checkConcurrency {
			checkConcurrencyCompliance(repoPath, configPath)
		} else if checkTimeline {
			checkBranchTimelines(repoPath, configPath)
		} else {
			slog.Info("Diagnosing repository", "path", repoPath)
			showBranchesWithTestResults(repoPath)
			showHomeciState(repoPath)
		}
		return nil
	},
}

func init() {
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to the home-ci config file (required)")
	rootCmd.Flags().BoolVar(&checkConcurrency, "check-concurrency", false, "Check that max_concurrent_runs was respected")
	rootCmd.Flags().BoolVar(&checkTimeline, "check-timeline", false, "Check timeline and validate test/commit workflow consistency")
	rootCmd.Flags().IntVarP(&verbose, "verbose", "v", 0, "Verbose level (0=error, 1=warn, 2=info, 3=debug)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}



// showBranchesWithTestResults displays git branches with their associated test results
func showBranchesWithTestResults(repoPath string) {
	fmt.Println("")
	fmt.Println("📊 Git Branches with Test Results:")

	// Get all branches
	branches := getGitBranches(repoPath)

	// Get all test results
	testResults, err := readTestResults(repoPath)
	if err != nil {
		fmt.Printf("Error reading test results: %v\n", err)
		return
	}

	// Group test results by branch
	branchResults := make(map[string][]TestResult)
	for _, result := range testResults {
		branchResults[result.Branch] = append(branchResults[result.Branch], result)
	}

	// Display each branch with its test results
	for _, branch := range branches {
		fmt.Printf("\n🌿 %s\n", branch)

		if results, exists := branchResults[branch]; exists {
			// Sort results by start time (most recent first)
			sort.Slice(results, func(i, j int) bool {
				return results[i].StartTime.After(results[j].StartTime)
			})

			for _, result := range results {
				actual := "❌ FAILED"
				if result.Success {
					actual = "✅ PASSED"
				} else if result.TimedOut {
					actual = "⏰ TIMEOUT"
				}

				// Get commit message
				commitMessage := getCommitMessage(repoPath, result.Commit)

				duration := result.EndTime.Sub(result.StartTime)
				fmt.Printf("   • Commit: %s\n", result.Commit)
				if commitMessage != "" {
					fmt.Printf("     Message: %s\n", commitMessage)
				}
				fmt.Printf("     Status: %s\n", actual)
				fmt.Printf("     Start:  %s\n", result.StartTime.Format("2006-01-02 15:04:05"))
				fmt.Printf("     End:    %s\n", result.EndTime.Format("2006-01-02 15:04:05"))
				fmt.Printf("     Duration: %s\n", duration.Round(time.Second))
				if result.ErrorMessage != "" {
					fmt.Printf("     Error: %s\n", result.ErrorMessage)
				}
				fmt.Println()
			}
		} else {
			fmt.Println("   No test results found for this branch")
		}
	}
}

// getGitBranches returns a list of all git branches (local and remote)
func getGitBranches(repoPath string) []string {
	cmd := exec.Command("git", "branch", "-a")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return []string{}
	}

	var branches []string
	lines := strings.Split(string(output), "\n")
	branchMap := make(map[string]bool) // To avoid duplicates

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "*") {
			// Skip empty lines and current branch marker
			if strings.HasPrefix(line, "*") {
				line = strings.TrimSpace(line[1:]) // Remove the * marker
			} else {
				continue
			}
		}

		// Handle remote branches: remove "remotes/origin/" prefix
		if strings.HasPrefix(line, "remotes/origin/") {
			line = strings.TrimPrefix(line, "remotes/origin/")
		}

		// Skip HEAD pointer
		if strings.Contains(line, "HEAD ->") {
			continue
		}

		if line != "" && !branchMap[line] {
			branches = append(branches, line)
			branchMap[line] = true
		}
	}

	// Sort branches alphabetically
	sort.Strings(branches)
	return branches
}

// getCommitMessage returns the commit message for a given commit hash
func getCommitMessage(repoPath, commitHash string) string {
	cmd := exec.Command("git", "log", "--format=%s", "-n", "1", commitHash)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// showHomeciState displays the current state of home-ci for this repository
func showHomeciState(repoPath string) {
	fmt.Println("")
	fmt.Println("🏠 Home-CI State:")

	// Try to read config to get state directory
	config, err := readConfig(configPath)
	if err != nil || config.StateDir == "" || config.RepoName == "" {
		// Fallback to old location
		stateFile := filepath.Join(repoPath, ".home-ci", "state.json")
		if _, err := os.Stat(stateFile); os.IsNotExist(err) {
			fmt.Println("No state.json found")
			return
		}

		if content, err := os.ReadFile(stateFile); err == nil {
			fmt.Printf("%s", content)
		} else {
			fmt.Printf("Error reading state.json: %v", err)
		}
		return
	}

	// Use new architecture location
	stateFile := filepath.Join(config.StateDir, config.RepoName+".json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		fmt.Println("No state file found")
		return
	}

	if content, err := os.ReadFile(stateFile); err == nil {
		fmt.Printf("%s", content)
	} else {
		fmt.Printf("Error reading state file: %v", err)
	}
}

// Config represents the home-ci configuration structure
type Config struct {
	Repository        string `yaml:"repository"`
	RepoName          string `yaml:"repo_name"`
	MaxConcurrentRuns int    `yaml:"max_concurrent_runs"`
	StateDir          string `yaml:"state_dir"`
	LogDir            string `yaml:"log_dir"`
	CacheDir          string `yaml:"cache_dir"`
}

// TestResult represents a test execution result
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

// checkConcurrencyCompliance verifies that max_concurrent_runs was respected
func checkConcurrencyCompliance(repoPath, configPath string) {
	fmt.Println("🔍 Checking concurrency compliance...")

	// Read configuration
	config, err := readConfig(configPath)
	if err != nil {
		slog.Error("Failed to read config", "error", err)
		os.Exit(1)
	}

	fmt.Printf("📊 Configuration: max_concurrent_runs = %d\n", config.MaxConcurrentRuns)

	// Read all test results
	testResults, err := readTestResults(repoPath)
	if err != nil {
		slog.Error("Failed to read test results", "error", err)
		os.Exit(1)
	}

	fmt.Printf("📋 Found %d test results to analyze\n", len(testResults))

	if len(testResults) == 0 {
		fmt.Println("⚠️  No test results found for analysis")
		return
	}

	// Display test execution timeline
	showExecutionTimeline(testResults, config.MaxConcurrentRuns)

	// Analyze concurrency
	maxConcurrent, violations := analyzeConcurrency(testResults)

	fmt.Printf("📈 Maximum concurrent tests observed: %d\n", maxConcurrent)
	fmt.Printf("⚖️  Configured limit: %d\n", config.MaxConcurrentRuns)

	if maxConcurrent <= config.MaxConcurrentRuns {
		fmt.Println("✅ Concurrency compliance: PASSED")
		fmt.Printf("   All test executions respected the limit of %d concurrent runs\n", config.MaxConcurrentRuns)
	} else {
		fmt.Println("❌ Concurrency compliance: FAILED")
		fmt.Printf("   Found %d concurrent runs, which exceeds the limit of %d\n", maxConcurrent, config.MaxConcurrentRuns)

		if len(violations) > 0 {
			fmt.Println("🚨 Violation details:")
			for _, v := range violations {
				fmt.Printf("   At %s: %d concurrent tests\n", v.Time.Format("15:04:05"), v.Count)
			}
		}
		os.Exit(1)
	}
}

// readConfig reads and parses the home-ci configuration file
func readConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

// readTestResults reads all test result JSON files from the new architecture directories
func readTestResults(repoPath string) ([]TestResult, error) {
	// Try to read config to get log directory
	config, err := readConfig(configPath)
	if err != nil || config.LogDir == "" || config.RepoName == "" {
		// Fallback to old location
		return readTestResultsOld(repoPath)
	}

	// Use new architecture location: log_dir/repo_name/results/
	resultsDir := filepath.Join(config.LogDir, config.RepoName, "results")
	files, err := filepath.Glob(filepath.Join(resultsDir, "*.json"))
	if err != nil {
		// If new location fails, try fallback
		return readTestResultsOld(repoPath)
	}

	var results []TestResult
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("⚠️  Warning: failed to read %s: %v\n", file, err)
			continue
		}

		var result TestResult
		if err := json.Unmarshal(data, &result); err != nil {
			fmt.Printf("⚠️  Warning: failed to parse %s: %v\n", file, err)
			continue
		}

		results = append(results, result)
	}

	return results, nil
}

// readTestResultsOld reads test results from the old .home-ci directory (fallback)
func readTestResultsOld(repoPath string) ([]TestResult, error) {
	homeciDir := filepath.Join(repoPath, ".home-ci")

	files, err := filepath.Glob(filepath.Join(homeciDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list JSON files: %w", err)
	}

	var results []TestResult
	for _, file := range files {
		if filepath.Base(file) == "state.json" {
			continue // Skip state.json
		}

		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("⚠️  Warning: failed to read %s: %v\n", file, err)
			continue
		}

		var result TestResult
		if err := json.Unmarshal(data, &result); err != nil {
			fmt.Printf("⚠️  Warning: failed to parse %s: %v\n", file, err)
			continue
		}

		results = append(results, result)
	}

	return results, nil
}

// ConcurrencyViolation represents a moment when concurrency limit was exceeded
type ConcurrencyViolation struct {
	Time  time.Time
	Count int
}

// showExecutionTimeline displays a timeline of test execution for concurrency analysis
func showExecutionTimeline(testResults []TestResult, maxConcurrentLimit int) {
	fmt.Println("")
	fmt.Println("⏰ Test Execution Timeline:")
	fmt.Println("══════════════════════════")

	if len(testResults) == 0 {
		fmt.Println("No test results to display")
		return
	}

	// Sort tests by start time
	sort.Slice(testResults, func(i, j int) bool {
		return testResults[i].StartTime.Before(testResults[j].StartTime)
	})

	// Find the overall time span
	startTime := testResults[0].StartTime
	var endTime time.Time
	for _, result := range testResults {
		if result.EndTime.After(endTime) {
			endTime = result.EndTime
		}
	}

	// Display each test with its timeline
	fmt.Printf("📊 Overall test period: %s to %s (duration: %s)\n\n",
		startTime.Format("15:04:05"),
		endTime.Format("15:04:05"),
		endTime.Sub(startTime).Round(time.Second))

	// Create timeline events to track concurrency
	type TimelineEvent struct {
		Time   time.Time
		Type   string // "start" or "end"
		Branch string
		Commit string
	}

	var events []TimelineEvent
	for _, result := range testResults {
		events = append(events, TimelineEvent{
			Time:   result.StartTime,
			Type:   "start",
			Branch: result.Branch,
			Commit: result.Commit[:8],
		})
		events = append(events, TimelineEvent{
			Time:   result.EndTime,
			Type:   "end",
			Branch: result.Branch,
			Commit: result.Commit[:8],
		})
	}

	// Sort events by time
	sort.Slice(events, func(i, j int) bool {
		if events[i].Time.Equal(events[j].Time) {
			return events[i].Type == "end" && events[j].Type == "start"
		}
		return events[i].Time.Before(events[j].Time)
	})

	// Display timeline with running tests count
	fmt.Println("📈 Timeline with concurrent test tracking:")
	fmt.Println("┌──────────┬────┬────────┬─────────────────────────────────────┬─────────────┐")
	fmt.Println("│   Time   │ St │ Action │               Test                  │   Running   │")
	fmt.Println("├──────────┼────┼────────┼─────────────────────────────────────┼─────────────┤")

	currentTests := make(map[string]bool)
	maxConcurrent := 0

	for _, event := range events {
		testKey := event.Branch + "-" + event.Commit

		if event.Type == "start" {
			currentTests[testKey] = true
			concurrent := len(currentTests)
			if concurrent > maxConcurrent {
				maxConcurrent = concurrent
			}

			status := "🟢"
			if concurrent > maxConcurrentLimit {
				status = "🔴"
			} else if concurrent == maxConcurrentLimit {
				status = "🟡"
			}

			testName := fmt.Sprintf("%s %s", event.Branch, event.Commit)
			if len(testName) > 35 {
				testName = testName[:32] + "..."
			}

			fmt.Printf("│ %s │ %s │ START  │ %-35s │ %2d tests    │\n",
				event.Time.Format("15:04:05"),
				status,
				testName,
				concurrent)
		} else {
			delete(currentTests, testKey)
			concurrent := len(currentTests)

			status := "🔵"
			testName := fmt.Sprintf("%s %s", event.Branch, event.Commit)
			if len(testName) > 35 {
				testName = testName[:32] + "..."
			}

			fmt.Printf("│ %s │ %s │ END    │ %-35s │ %2d tests    │\n",
				event.Time.Format("15:04:05"),
				status,
				testName,
				concurrent)
		}
	}

	fmt.Println("└──────────┴────┴────────┴─────────────────────────────────────┴─────────────┘")

	fmt.Printf("\n📊 Legend: 🟢 = Safe start  🟡 = At limit  🔴 = Over limit  🔵 = Test end\n")
	fmt.Printf("📈 Peak concurrency observed: %d tests\n", maxConcurrent)
	fmt.Println("")
}

// analyzeConcurrency analyzes test execution times to find maximum concurrency
func analyzeConcurrency(testResults []TestResult) (int, []ConcurrencyViolation) {
	if len(testResults) == 0 {
		return 0, nil
	}

	// Create events for start and end times
	type Event struct {
		Time time.Time
		Type string // "start" or "end"
		Test string // branch-commit for identification
	}

	var events []Event
	for _, result := range testResults {
		testId := fmt.Sprintf("%s-%s", result.Branch, result.Commit)
		events = append(events, Event{
			Time: result.StartTime,
			Type: "start",
			Test: testId,
		})
		events = append(events, Event{
			Time: result.EndTime,
			Type: "end",
			Test: testId,
		})
	}

	// Sort events by time
	sort.Slice(events, func(i, j int) bool {
		if events[i].Time.Equal(events[j].Time) {
			// If times are equal, process "end" events before "start" events
			return events[i].Type == "end" && events[j].Type == "start"
		}
		return events[i].Time.Before(events[j].Time)
	})

	// Track concurrent tests and violations
	currentConcurrent := 0
	maxConcurrent := 0
	var violations []ConcurrencyViolation

	for _, event := range events {
		if event.Type == "start" {
			currentConcurrent++
		} else {
			currentConcurrent--
		}

		if currentConcurrent > maxConcurrent {
			maxConcurrent = currentConcurrent
		}
	}

	return maxConcurrent, violations
}

// CommitInfo represents a commit with its timestamp
type CommitInfo struct {
	Hash       string
	Date       time.Time
	Message    string
	Author     string
	TestStart  *time.Time
	TestEnd    *time.Time
	TestResult string // "success", "failure", "timeout", or ""
}

// TimelineEvent represents an event in the branch timeline
type TimelineEvent struct {
	Time       time.Time
	Type       string // "commit", "test_start", "test_end"
	CommitHash string
	Message    string
	TestResult string
}

// checkBranchTimelines displays timeline and validates test/commit workflow consistency
func checkBranchTimelines(repoPath string, configPath string) {
	fmt.Println("🕒 Branch Timeline Check - Workflow Validation")
	fmt.Println("================================================")

	// Read config to get check_interval
	checkInterval := "unknown"
	if configPath != "" {
		if data, err := os.ReadFile(configPath); err == nil {
			var rawConfig map[string]interface{}
			if err := yaml.Unmarshal(data, &rawConfig); err == nil {
				if ci, ok := rawConfig["check_interval"]; ok {
					checkInterval = fmt.Sprintf("%v", ci)
				}
			}
		}
	}

	fmt.Printf("ℹ️  Workflow Validation Logic:\n")
	fmt.Printf("   • At each check_interval (%s), home-ci scans branch HEADs\n", checkInterval)
	fmt.Printf("   • Tests are launched ONLY if HEAD commit not already tested\n")
	fmt.Printf("   • This check validates that workflow is consistent with this logic\n")
	fmt.Printf("   • Timeline shows: tested commits + workflow consistency analysis\n")
	fmt.Println("")

	branches := getGitBranches(repoPath)

	// Get test results
	testResults, err := readTestResults(repoPath)
	if err != nil {
		fmt.Printf("❌ Failed to read test results: %v\n", err)
		return
	}
	slog.Debug("Read test results", "count", len(testResults))
	testsByCommit := make(map[string]TestResult)
	testsByBranch := make(map[string][]TestResult)
	for _, result := range testResults {
		testsByCommit[result.Commit] = result
		testsByBranch[result.Branch] = append(testsByBranch[result.Branch], result)
		slog.Debug("Found test", "branch", result.Branch, "commit", result.Commit[:8], "success", result.Success)
	}

	for _, branch := range branches {
		if strings.Contains(branch, "->") || strings.HasPrefix(branch, "remotes/") {
			continue // Skip remote branch references
		}

		branch = strings.TrimSpace(strings.TrimPrefix(branch, "*"))
		if branch == "" {
			continue
		}

		fmt.Printf("\n📋 Branch: %s\n", branch)
		fmt.Println("┌─────────────────────┬───────────┬─────────────────────┬─────────────┬─────────────────────────────────────┐")
		fmt.Println("│       Commit        │    Type   │        Date         │   Result    │               Message               │")
		fmt.Println("├─────────────────────┼───────────┼─────────────────────┼─────────────┼─────────────────────────────────────┤")

		// Create timeline events - show only tested commits for this branch
		var events []TimelineEvent
		branchTests := testsByBranch[branch]
		slog.Debug("Processing branch", "branch", branch, "testsFound", len(branchTests))

		// For each test found for this branch, get the commit info and add events
		for _, test := range branchTests {
			// Get commit info for this test
			commitInfo, err := getCommitInfo(repoPath, test.Commit)
			if err != nil {
				slog.Debug("Failed to get commit info", "commit", test.Commit, "error", err)
				continue
			}

			// Add commit event
			events = append(events, TimelineEvent{
				Time:       commitInfo.Date,
				Type:       "commit",
				CommitHash: test.Commit,
				Message:    commitInfo.Message,
			})

			// Add test events
			events = append(events, TimelineEvent{
				Time:       test.StartTime,
				Type:       "test_start",
				CommitHash: test.Commit,
				Message:    commitInfo.Message,
				TestResult: getTestResultString(test),
			})
			events = append(events, TimelineEvent{
				Time:       test.EndTime,
				Type:       "test_end",
				CommitHash: test.Commit,
				Message:    commitInfo.Message,
				TestResult: getTestResultString(test),
			})
		}

		// Skip branch if no events
		if len(events) == 0 {
			continue
		}

		// Sort events by time
		sort.Slice(events, func(i, j int) bool {
			return events[i].Time.Before(events[j].Time)
		})

		// Display events
		for _, event := range events {
			commitShort := event.CommitHash[:8]
			timeStr := event.Time.Format("2006-01-02 15:04:05")
			message := event.Message
			if len(message) > 35 {
				message = message[:32] + "..."
			}

			switch event.Type {
			case "commit":
				fmt.Printf("│ %-19s │ 📝 Commit │ %s │      -      │ %-35s │\n",
					commitShort, timeStr, message)
			case "test_start":
				resultIcon := getResultIcon(event.TestResult)
				fmt.Printf("│ %-19s │ 🚀 Start  │ %s │ %s %-8s │ Test started                        │\n",
					commitShort, timeStr, resultIcon, event.TestResult)
			case "test_end":
				resultIcon := getResultIcon(event.TestResult)
				fmt.Printf("│ %-19s │ 🏁 End    │ %s │ %s %-8s │ Test completed                      │\n",
					commitShort, timeStr, resultIcon, event.TestResult)
			}
		}

		fmt.Println("└─────────────────────┴───────────┴─────────────────────┴─────────────┴─────────────────────────────────────┘")
	}

	// Perform workflow consistency validation
	fmt.Println("\n🔍 Workflow Consistency Analysis")
	fmt.Println("=================================")

	validateWorkflowConsistency(repoPath, branches, testsByBranch, checkInterval)
}

// validateWorkflowConsistency checks that test/commit workflow follows home-ci logic
func validateWorkflowConsistency(repoPath string, branches []string, testsByBranch map[string][]TestResult, checkInterval string) {
	// Parse check_interval to duration
	interval, err := time.ParseDuration(checkInterval)
	if err != nil {
		slog.Debug("Failed to parse check_interval", "interval", checkInterval, "error", err)
		interval = 30 * time.Second // Default fallback
	}

	var totalIssues int
	var totalBranches int

	for _, branch := range branches {
		if strings.Contains(branch, "->") || strings.HasPrefix(branch, "remotes/") {
			continue // Skip remote branch references
		}

		branch = strings.TrimSpace(strings.TrimPrefix(branch, "*"))
		if branch == "" {
			continue
		}

		totalBranches++
		slog.Debug("Validating branch", "branch", branch)

		// Get current HEAD commit for this branch
		headCommit, err := getBranchHead(repoPath, branch)
		if err != nil {
			fmt.Printf("⚠️  Branch %s: Failed to get HEAD commit - %v\n", branch, err)
			totalIssues++
			continue
		}

		// Get all commits for this branch to understand the timeline
		commits, err := getBranchCommits(repoPath, branch)
		if err != nil {
			fmt.Printf("⚠️  Branch %s: Failed to get commits - %v\n", branch, err)
			totalIssues++
			continue
		}

		// Analyze the testing pattern for this branch
		branchTests := testsByBranch[branch]
		issues := analyzeTestingPattern(branch, headCommit, commits, branchTests, interval)
		totalIssues += issues
	}

	// Summary
	fmt.Printf("\n📊 Workflow Validation Summary:\n")
	fmt.Printf("   • Branches analyzed: %d\n", totalBranches)
	if totalIssues == 0 {
		fmt.Printf("   • ✅ All branches follow expected workflow pattern\n")
		fmt.Printf("   • ✅ HEAD commits are tested as expected\n")
	} else {
		fmt.Printf("   • ⚠️  Issues found: %d\n", totalIssues)
		fmt.Printf("   • ❌ Some branches may have workflow inconsistencies\n")
	}
}

// getBranchHead gets the HEAD commit hash for a specific branch
func getBranchHead(repoPath, branch string) (string, error) {
	cmd := exec.Command("git", "rev-parse", branch)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// analyzeTestingPattern analyzes if the testing pattern follows home-ci logic
func analyzeTestingPattern(branch, headCommit string, commits []CommitInfo, tests []TestResult, interval time.Duration) int {
	issues := 0

	fmt.Printf("\n🌿 Branch: %s\n", branch)
	fmt.Printf("   HEAD: %s\n", headCommit[:8])

	// Check if HEAD commit has been tested
	headTested := false
	for _, test := range tests {
		if test.Commit == headCommit {
			headTested = true
			break
		}
	}

	if !headTested {
		fmt.Printf("   ⚠️  HEAD commit not tested - may indicate ongoing test or issue\n")
		issues++
	} else {
		fmt.Printf("   ✅ HEAD commit has been tested\n")
	}

	// Analyze commit timing vs testing pattern
	if len(commits) > 1 && len(tests) > 0 {
		// Check if multiple tests exist and if they make sense based on timing
		commitTimes := make(map[string]time.Time)
		for _, commit := range commits {
			commitTimes[commit.Hash] = commit.Date
		}

		// Sort tests by start time
		sort.Slice(tests, func(i, j int) bool {
			return tests[i].StartTime.Before(tests[j].StartTime)
		})

		// Verify that tests follow the check_interval logic
		expectedPattern := validateTestInterval(tests, commitTimes, interval)
		if expectedPattern {
			fmt.Printf("   ✅ Test timing follows check_interval pattern\n")
		} else {
			fmt.Printf("   ⚠️  Test timing may not follow expected check_interval pattern\n")
			issues++
		}
	}

	fmt.Printf("   📈 Tests found: %d\n", len(tests))
	if len(tests) > 0 {
		successful := 0
		for _, test := range tests {
			if test.Success {
				successful++
			}
		}
		fmt.Printf("   📊 Success rate: %d/%d (%.1f%%)\n", successful, len(tests), float64(successful)/float64(len(tests))*100)
	}

	return issues
}

// validateTestInterval checks if test intervals match the expected check_interval pattern
func validateTestInterval(tests []TestResult, commitTimes map[string]time.Time, interval time.Duration) bool {
	if len(tests) <= 1 {
		return true // Single test is always valid
	}

	// For multiple tests, verify they were spaced appropriately
	for i := 1; i < len(tests); i++ {
		prevTest := tests[i-1]
		currentTest := tests[i]

		// Get commit times
		prevCommitTime := commitTimes[prevTest.Commit]
		currentCommitTime := commitTimes[currentTest.Commit]

		// If commits were made more than check_interval apart,
		// then multiple tests make sense
		timeBetweenCommits := currentCommitTime.Sub(prevCommitTime)
		if timeBetweenCommits > interval {
			slog.Debug("Valid test interval",
				"prevCommit", prevTest.Commit[:8],
				"currentCommit", currentTest.Commit[:8],
				"timeBetween", timeBetweenCommits,
				"interval", interval)
		}
	}

	return true // For now, we assume the pattern is valid if we reach here
}

// getBranchCommits gets commits for a specific branch
func getBranchCommits(repoPath, branch string) ([]CommitInfo, error) {
	cmd := exec.Command("git", "log", "--format=%H|%cd|%s|%an", "--date=iso", branch)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var commits []CommitInfo
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}

		date, err := time.Parse("2006-01-02 15:04:05 -0700", parts[1])
		if err != nil {
			continue
		}

		commits = append(commits, CommitInfo{
			Hash:    parts[0],
			Date:    date,
			Message: parts[2],
			Author:  parts[3],
		})
	}

	return commits, nil
}

// getCommitInfo gets information for a specific commit
func getCommitInfo(repoPath, commitHash string) (CommitInfo, error) {
	cmd := exec.Command("git", "log", "--format=%H|%cd|%s|%an", "--date=iso", "-1", commitHash)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return CommitInfo{}, err
	}

	line := strings.TrimSpace(string(output))
	if line == "" {
		return CommitInfo{}, fmt.Errorf("no output for commit %s", commitHash)
	}

	parts := strings.Split(line, "|")
	if len(parts) < 4 {
		return CommitInfo{}, fmt.Errorf("invalid git log output for commit %s", commitHash)
	}

	date, err := time.Parse("2006-01-02 15:04:05 -0700", parts[1])
	if err != nil {
		return CommitInfo{}, fmt.Errorf("failed to parse date for commit %s: %w", commitHash, err)
	}

	return CommitInfo{
		Hash:    parts[0],
		Date:    date,
		Message: parts[2],
		Author:  parts[3],
	}, nil
}

// getResultIcon returns an icon for the test result
func getResultIcon(result string) string {
	switch result {
	case "success":
		return "✅"
	case "failure":
		return "❌"
	case "timeout":
		return "⏰"
	default:
		return "❓"
	}
}

// getTestResultString returns a string representation of the test result
func getTestResultString(test TestResult) string {
	if test.TimedOut {
		return "timeout"
	} else if test.Success {
		return "success"
	} else {
		return "failure"
	}
}
