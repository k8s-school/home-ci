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
	} else {
		log.Printf("🔍 Diagnosing repository: %s", repoPath)
		showGitBranches(repoPath)
		showProcessedCommits(repoPath)
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
	Branch    string    `json:"branch"`
	Commit    string    `json:"commit"`
	LogFile   string    `json:"log_file"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Success   bool      `json:"success"`
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