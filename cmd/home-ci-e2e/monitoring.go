package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// monitorState monitors home-ci state files for running tests and timeouts
func (th *E2ETestHarness) monitorState() {
	go func() {
		// Wait for the state directory to be created by home-ci (new architecture)
		stateDir := filepath.Join(th.tempRunDir, "state")
		for {
			if _, err := os.Stat(stateDir); err == nil {
				break
			}
			// Also check old location as fallback
			homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
			if _, err := os.Stat(homeCIDir); err == nil {
				stateDir = homeCIDir
				break
			}
			time.Sleep(1 * time.Second)
		}

		for {
			select {
			case <-th.homeCIContext.Done():
				return
			case <-time.After(2 * time.Second):
				// Check state files for running tests
				if err := th.checkStateForActivity(stateDir); err != nil {
					log.Printf("Error checking state: %v", err)
				}
			}
		}
	}()
}

// checkStateForActivity checks state files for test execution and timeouts
func (th *E2ETestHarness) checkStateForActivity(stateDir string) error {
	var stateFile string

	// Check if it's the new architecture (state directory with repo-specific files)
	if strings.Contains(stateDir, "/state") && !strings.Contains(stateDir, ".home-ci") {
		// New architecture: state_dir/repo_name.json
		stateFile = filepath.Join(stateDir, th.repoName+".json")
	} else {
		// Old architecture: .home-ci/state.json
		stateFile = filepath.Join(stateDir, "state.json")
	}

	// Check if state file exists
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		return nil // No state file yet
	}

	// Read and parse state.json
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return err
	}

	var state struct {
		RunningTests []RunningTest `json:"running_tests"`
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	// Update our running tests from state
	th.runningTests = state.RunningTests
	th.stateFileRead = true // Mark that we've successfully read the state file

	// Display running tests every 15 checks (approximately every 30 seconds)
	th.logCheckCount++
	if th.logCheckCount%15 == 0 {
		th.displayRunningTests()
	}

	// Check for timeout in running tests if it's a timeout test
	if th.testType == TestTimeout {
		return th.checkStateForTimeout()
	}

	return nil
}

// checkStateForTimeout checks for timeout by examining JSON result files (used only for timeout tests)
func (th *E2ETestHarness) checkStateForTimeout() error {
	if th.timeoutDetected {
		return nil // Already detected
	}

	// Check JSON result files for timeout indication in new architecture location
	resultsDir := filepath.Join(th.tempRunDir, "logs", th.repoName, "results")
	files, err := os.ReadDir(resultsDir)
	if err != nil {
		// Fallback to old location
		homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
		files, err = os.ReadDir(homeCIDir)
		if err != nil {
			return nil // No files yet
		}

		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
				jsonPath := filepath.Join(homeCIDir, file.Name())
				if th.checkJSONForTimeout(jsonPath) {
					th.timeoutDetected = true
					log.Printf("🕐 Timeout detected: found timeout in result file %s", file.Name())
					return nil
				}
			}
		}
		return nil
	}

	// Check new architecture location
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			jsonPath := filepath.Join(resultsDir, file.Name())
			if th.checkJSONForTimeout(jsonPath) {
				th.timeoutDetected = true
				log.Printf("🕐 Timeout detected: found timeout in result file %s", file.Name())
				return nil
			}
		}
	}

	return nil
}

// checkJSONForTimeout checks if a JSON result file indicates a timeout
func (th *E2ETestHarness) checkJSONForTimeout(jsonPath string) bool {
	content, err := os.ReadFile(jsonPath)
	if err != nil {
		return false
	}

	var result TestResult
	if err := json.Unmarshal(content, &result); err != nil {
		return false
	}

	return result.TimedOut
}

// displayRunningTests shows current running tests with their details
func (th *E2ETestHarness) displayRunningTests() {
	if len(th.runningTests) == 0 {
		// Only show "No tests currently running" if we've successfully read the state file
		// Otherwise, tests might be running but we just can't see them yet
		if th.stateFileRead {
			log.Printf("📊 No tests currently running")
		} else {
			log.Printf("📊 Waiting for test state information...")
		}
		return
	}

	log.Printf("📊 Currently running tests (%d):", len(th.runningTests))
	for i, test := range th.runningTests {
		duration := time.Since(test.StartTime).Truncate(time.Second)
		log.Printf("   %d. Branch: %s, Commit: %s", i+1, test.Branch, test.Commit[:min(8, len(test.Commit))])
		log.Printf("      LogFile: %s, Running: %v", test.LogFile, duration)
	}
}
