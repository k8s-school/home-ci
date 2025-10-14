package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// validateTestResults validates actual test results against expectations
func (th *E2ETestHarness) validateTestResults() ValidationResult {
	result := ValidationResult{}

	config, err := th.loadTestExpectations()
	if err != nil {
		log.Printf("⚠️ Failed to load test expectations: %v", err)
		return result
	}

	// Get all test result files
	homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
	files, err := os.ReadDir(homeCIDir)
	if err != nil {
		log.Printf("⚠️ Failed to read test results directory: %v", err)
		return result
	}

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") && file.Name() != "state.json" {
			jsonPath := filepath.Join(homeCIDir, file.Name())

			content, err := os.ReadFile(jsonPath)
			if err != nil {
				continue
			}

			var testResult TestResult
			if err := json.Unmarshal(content, &testResult); err != nil {
				continue
			}

			result.TotalTests++

			// Determine expected outcome
			expectedResult := th.getExpectedResult(config, testResult.Branch, testResult.Commit, "")

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
	}

	// Calculate validation score
	if result.TotalTests > 0 {
		result.ValidationScore = float64(result.CorrectPredictions) / float64(result.TotalTests) * 100.0
	}

	return result
}

// verifyCleanupExecuted checks if cleanup was executed for timeout tests
func (th *E2ETestHarness) verifyCleanupExecuted() bool {
	if th.testType != TestTimeout {
		return true // Not relevant for non-timeout tests
	}

	// Check if any test result JSON files indicate cleanup was executed
	homeCIDir := filepath.Join(th.testRepoPath, ".home-ci")
	files, err := os.ReadDir(homeCIDir)
	if err != nil {
		log.Printf("⚠️ Could not read test results directory: %v", err)
		return false
	}

	cleanupExecuted := false
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") && file.Name() != "state.json" {
			jsonPath := filepath.Join(homeCIDir, file.Name())

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
				cleanupExecuted = true
				break
			}
		}
	}

	if !cleanupExecuted {
		log.Printf("❌ No cleanup execution found for timeout test")
		return false
	}

	return true
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