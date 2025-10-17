package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func main() {
	var (
		configPathFlag      = flag.String("config", "", "Path to the home-ci config file (required)")
		checkConcurrencyFlag = flag.Bool("check-concurrency", false, "Check that max_concurrent_runs was respected")
		showTimelineFlag    = flag.Bool("show-timeline", false, "Show timeline of commits and tests per branch")
		helpFlag            = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *helpFlag {
		fmt.Println("Home-CI Repository Diagnostic Tool")
		fmt.Println("==================================")
		fmt.Println("")
		fmt.Println("Usage: home-ci-diag [options]")
		fmt.Println("")
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  home-ci-diag -config=/path/to/config.yaml")
		fmt.Println("  home-ci-diag -config=/tmp/home-ci/e2e/concurrent-limit/config-concurrent-limit.yaml -check-concurrency")
		fmt.Println("  home-ci-diag -config=/path/to/config.yaml -show-timeline")
		return
	}

	if *configPathFlag == "" {
		log.Fatal("❌ Config file path is required. Use -config flag or -help for usage.")
	}

	// Read configuration to get repository path
	config, err := readConfig(*configPathFlag)
	if err != nil {
		log.Fatalf("❌ Failed to read config: %v", err)
	}
	repoPath := config.RepoPath

	// Validate repository path
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		log.Fatalf("❌ Repository path does not exist: %s", repoPath)
	}

	// Check if it's a git repository
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		log.Fatalf("❌ Not a git repository: %s", repoPath)
	}

	if *checkConcurrencyFlag {
		checkConcurrencyCompliance(repoPath, *configPathFlag)
	} else if *showTimelineFlag {
		showBranchTimelines(repoPath, *configPathFlag)
	} else {
		log.Printf("🔍 Diagnosing repository: %s", repoPath)
		showBranchesWithTestResults(repoPath)
		showHomeciState(repoPath)
	}
}

// showGitBranches displays git branches from the repository
func showGitBranches(repoPath string) {
	fmt.Println("")
	fmt.Println("📊 Git branches:")

	cmd := exec.Command("git", "branch", "-a")
	cmd.Dir = repoPath
	if output, err := cmd.Output(); err == nil {
		fmt.Printf("%s", output)
	} else {
		fmt.Println("No branches found or git command failed")
	}
}

// showProcessedCommits displays commits that have been processed by home-ci
func showProcessedCommits(repoPath string) {
	fmt.Println("")
	fmt.Println("📋 Processed commits (JSON results):")

	homeciDir := filepath.Join(repoPath, ".home-ci")
	if _, err := os.Stat(homeciDir); os.IsNotExist(err) {
		fmt.Println("No .home-ci directory found")
		return
	}

	if files, err := filepath.Glob(filepath.Join(homeciDir, "*.json")); err == nil {
		var commits []string
		for _, file := range files {
			if filepath.Base(file) != "state.json" {
				// Extract branch and commit from filename like "20251016-192533_bugfix-timeout_a24b54c3.json"
				basename := filepath.Base(file)
				basename = strings.TrimSuffix(basename, ".json")
				parts := strings.Split(basename, "_")
				if len(parts) >= 3 {
					branch := parts[1]
					commit := parts[2]
					commits = append(commits, fmt.Sprintf("%s-%s", branch, commit))
				}
			}
		}
		if len(commits) > 0 {
			for _, commit := range commits {
				fmt.Println(commit)
			}
		} else {
			fmt.Println("No processed commits found")
		}
	} else {
		fmt.Println("No processed commits found")
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
				status := "❌ FAILED"
				if result.Success {
					status = "✅ PASSED"
				} else if result.TimedOut {
					status = "⏰ TIMEOUT"
				}

				// Get commit message
				commitMessage := getCommitMessage(repoPath, result.Commit)

				duration := result.EndTime.Sub(result.StartTime)
				fmt.Printf("   • Commit: %s\n", result.Commit)
				if commitMessage != "" {
					fmt.Printf("     Message: %s\n", commitMessage)
				}
				fmt.Printf("     Status: %s\n", status)
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
}

// Config represents the home-ci configuration structure
type Config struct {
	RepoPath          string `yaml:"repo_path"`
	MaxConcurrentRuns int    `yaml:"max_concurrent_runs"`
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

// TimeInterval represents a time interval for concurrency analysis
type TimeInterval struct {
	Branch    string
	Commit    string
	StartTime time.Time
	EndTime   time.Time
}

// checkConcurrencyCompliance verifies that max_concurrent_runs was respected
func checkConcurrencyCompliance(repoPath, configPath string) {
	fmt.Println("🔍 Checking concurrency compliance...")

	// Read configuration
	config, err := readConfig(configPath)
	if err != nil {
		log.Fatalf("❌ Failed to read config: %v", err)
	}

	fmt.Printf("📊 Configuration: max_concurrent_runs = %d\n", config.MaxConcurrentRuns)

	// Read all test results
	testResults, err := readTestResults(repoPath)
	if err != nil {
		log.Fatalf("❌ Failed to read test results: %v", err)
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

// readTestResults reads all test result JSON files from the .home-ci directory
func readTestResults(repoPath string) ([]TestResult, error) {
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
		Time  time.Time
		Type  string // "start" or "end"
		Test  string // branch-commit for identification
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
	Hash      string
	Date      time.Time
	Message   string
	Author    string
	TestStart *time.Time
	TestEnd   *time.Time
	TestResult string // "success", "failure", "timeout", or ""
}

// TimelineEvent represents an event in the branch timeline
type TimelineEvent struct {
	Time        time.Time
	Type        string // "commit", "test_start", "test_end"
	CommitHash  string
	Message     string
	TestResult  string
}

// showBranchTimelines displays timeline of commits and tests for each branch
func showBranchTimelines(repoPath string, configPath string) {
	fmt.Println("🕒 Branch Timelines - Commits and Tests")
	fmt.Println("======================================")

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

	fmt.Printf("ℹ️  Home-CI behavior explanation:\n")
	fmt.Printf("   • Normal operation: Tests only the latest commit per branch\n")
	fmt.Printf("   • Check interval: %s (how often home-ci scans for new commits)\n", checkInterval)
	fmt.Printf("   • E2E testing: Multiple commits created rapidly may all be tested\n")
	fmt.Printf("     before home-ci can update its state between check intervals\n")
	fmt.Println("")

	branches := getGitBranches(repoPath)

	// Get test results
	testResults, err := readTestResults(repoPath)
	if err != nil {
		fmt.Printf("❌ Failed to read test results: %v\n", err)
		return
	}
	testsByCommit := make(map[string]TestResult)
	for _, result := range testResults {
		testsByCommit[result.Commit] = result
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

		commits, err := getBranchCommits(repoPath, branch)
		if err != nil {
			fmt.Printf("❌ Failed to get commits for branch %s: %v\n", branch, err)
			continue
		}

		// Create timeline events
		var events []TimelineEvent

		for _, commit := range commits {
			// Add commit event
			events = append(events, TimelineEvent{
				Time:       commit.Date,
				Type:       "commit",
				CommitHash: commit.Hash,
				Message:    commit.Message,
			})

			// Add test events if they exist
			if test, exists := testsByCommit[commit.Hash]; exists {
				events = append(events, TimelineEvent{
					Time:       test.StartTime,
					Type:       "test_start",
					CommitHash: commit.Hash,
					Message:    commit.Message,
					TestResult: getTestResultString(test),
				})
				events = append(events, TimelineEvent{
					Time:       test.EndTime,
					Type:       "test_end",
					CommitHash: commit.Hash,
					Message:    commit.Message,
					TestResult: getTestResultString(test),
				})
			}
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