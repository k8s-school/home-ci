package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// validateTestResults validates actual test results against expectations
func (th *E2ETestHarness) validateTestResults() ValidationResult {
	result := ValidationResult{}

	// Load test expectations once
	config, err := th.loadTestExpectations()
	if err != nil {
		log.Printf("⚠️ Failed to load test expectations: %v", err)
		return result
	}

	// Get all test result files from new architecture location
	resultsDir := filepath.Join(th.tempRunDir, "logs", th.repoName, "results")
	files, err := os.ReadDir(resultsDir)
	if err != nil {
		// Fallback to old location
		homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
		files, err = os.ReadDir(homeCIDir)
		if err != nil {
			log.Printf("⚠️ Failed to read test results directory: %v", err)
			return result
		}

		// Process files from old location
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") && file.Name() != "state.json" {
				jsonPath := filepath.Join(homeCIDir, file.Name())
				th.processTestResultFile(jsonPath, config, &result)
			}
		}
	} else {
		// Process files from new location
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
				jsonPath := filepath.Join(resultsDir, file.Name())
				th.processTestResultFile(jsonPath, config, &result)
			}
		}
	}

	// Calculate validation score
	if result.TotalTests > 0 {
		result.ValidationScore = float64(result.CorrectPredictions) / float64(result.TotalTests) * 100.0
	}

	return result
}

// processTestResultFile processes a single test result file and updates the validation result
func (th *E2ETestHarness) processTestResultFile(jsonPath string, config *TestExpectationConfig, result *ValidationResult) {

	content, err := os.ReadFile(jsonPath)
	if err != nil {
		return
	}

	var testResult TestResult
	if err := json.Unmarshal(content, &testResult); err != nil {
		return
	}

	result.TotalTests++

	// Get commit message for this test result
	commitMessage := th.getCommitMessage(testResult.Commit)

	// Determine expected outcome using simplified logic (commit message only)
	expectedResult := th.getExpectedResult(commitMessage)

	// Count expected outcomes
	switch expectedResult {
	case "success":
		result.ExpectedSuccesses++
	case "failure":
		result.ExpectedFailures++
	case "timeout":
		result.ExpectedTimeouts++
	}

	// Count actual outcomes
	if testResult.Success {
		result.ActualSuccesses++
	} else if testResult.TimedOut {
		result.ActualTimeouts++
	} else {
		result.ActualFailures++
	}

	// Check if prediction was correct
	actualResult := "failure" // default
	if testResult.Success {
		actualResult = "success"
	} else if testResult.TimedOut {
		actualResult = "timeout"
	}

	if expectedResult == actualResult {
		result.CorrectPredictions++
	}
}

// verifyCleanupExecuted checks if cleanup was executed for timeout tests
func (th *E2ETestHarness) verifyCleanupExecuted() bool {
	if th.testType != TestTimeout {
		return true // Not relevant for non-timeout tests
	}

	// Check if any test result JSON files indicate cleanup was executed in new architecture location
	resultsDir := filepath.Join(th.tempRunDir, "logs", th.repoName, "results")
	files, err := os.ReadDir(resultsDir)
	if err != nil {
		// Fallback to old location
		homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
		files, err = os.ReadDir(homeCIDir)
		if err != nil {
			log.Printf("⚠️ Could not read test results directory: %v", err)
			return false
		}

		// Check old location
		return th.checkCleanupInFiles(files, homeCIDir)
	}

	// Check new location
	return th.checkCleanupInFiles(files, resultsDir)
}

// checkCleanupInFiles checks for cleanup execution in a list of files
func (th *E2ETestHarness) checkCleanupInFiles(files []os.DirEntry, dirPath string) bool {
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") && file.Name() != "state.json" {
			jsonPath := filepath.Join(dirPath, file.Name())

			content, err := os.ReadFile(jsonPath)
			if err != nil {
				continue
			}

			var result TestResult
			if err := json.Unmarshal(content, &result); err != nil {
				continue
			}

			if result.TimedOut && result.CleanupExecuted {
				log.Printf("✅ Cleanup executed for timeout test: branch=%s, commit=%s, success=%v",
					result.Branch, result.Commit[:8], result.CleanupSuccess)
				return true
			}
		}
	}

	log.Printf("❌ No cleanup execution found for timeout test")
	return false
}

// printStatistics displays test statistics
func (th *E2ETestHarness) printStatistics() {
	// Count tests from actual JSON result files
	th.totalTestsDetected = th.countTestsFromResults()

	log.Println("\n📊 Test Statistics:")
	log.Printf("   Test Type: %s", th.getTestTypeName())
	log.Printf("   Duration: %v", th.duration)
	log.Printf("   Commits created: %d", th.commitsCreated)
	log.Printf("   Branches created: %d", th.branchesCreated)
	log.Printf("   Tests detected: %d", th.totalTestsDetected)

	if th.testType == TestTimeout {
		log.Printf("   Timeout detected: %v", th.timeoutDetected)
		cleanupExecuted := th.verifyCleanupExecuted()
		log.Printf("   Cleanup executed: %v", cleanupExecuted)
		if !th.timeoutDetected {
			log.Println("⚠️  WARNING: Timeout test did not detect timeout!")
		} else if !cleanupExecuted {
			log.Println("⚠️  WARNING: Cleanup was not executed for timeout test!")
		} else {
			log.Println("✅ Timeout detection and cleanup working correctly!")
		}
	} else {
		if th.commitsCreated > 0 && th.totalTestsDetected == 0 {
			log.Println("⚠️  WARNING: No test executions detected despite commits being created!")
		} else if th.totalTestsDetected > 0 {
			log.Println("✅ Test execution detection working correctly!")

			// Validate test results against expectations
			validation := th.validateTestResults()
			if validation.TotalTests > 0 {
				log.Println("\n🎯 Test Expectations Validation:")
				log.Printf("   Total tests validated: %d", validation.TotalTests)
				log.Printf("   Expected: Success=%d, Failure=%d, Timeout=%d",
					validation.ExpectedSuccesses, validation.ExpectedFailures, validation.ExpectedTimeouts)
				log.Printf("   Actual: Success=%d, Failure=%d, Timeout=%d",
					validation.ActualSuccesses, validation.ActualFailures, validation.ActualTimeouts)
				log.Printf("   Correct predictions: %d/%d (%.1f%%)",
					validation.CorrectPredictions, validation.TotalTests, validation.ValidationScore)

				if validation.ValidationScore >= 75.0 {
					log.Println("✅ Test expectations validation passed!")
				} else {
					log.Println("⚠️  Test expectations validation needs improvement")
				}
			}
		}
	}
}

// getExpectedResult determines expected result based on commit message only
func (th *E2ETestHarness) getExpectedResult(commitMessage string) string {
	// Check commit message patterns only (same logic as home-ci-diag)
	if matched, _ := regexp.MatchString(".*FAIL.*", commitMessage); matched {
		return "failure"
	}
	if matched, _ := regexp.MatchString(".*TIMEOUT.*", commitMessage); matched {
		return "timeout"
	}
	if matched, _ := regexp.MatchString(".*SUCCESS.*", commitMessage); matched {
		return "success"
	}

	// No pattern found in commit message - default to success
	return "success"
}
