package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/k8s-school/home-ci/internal/logging"
)


var (
	testType  string
	duration  string
	noCleanup bool
	initFlag  bool
	verbose   int
)

var rootCmd = &cobra.Command{
	Use:   "home-ci-e2e",
	Short: "Home-CI E2E Test Harness",
	Long: `A testing tool for the Home-CI system that simulates various development scenarios
and verifies the CI system's behavior under different conditions.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logging
		logging.InitLogging(verbose)

		if initFlag {
			return runInitialization()
		}

		return runE2ETests()
	},
}

func init() {
	rootCmd.Flags().StringVarP(&testType, "type", "t", "normal", "Test type: success, fail, timeout, dispatch-one-success, dispatch-no-token-file, quick, dispatch-all, normal, long, concurrent-limit, continuous-ci")
	rootCmd.Flags().StringVarP(&duration, "duration", "d", "3m", "Test duration (e.g., 30s, 5m, 1h)")
	rootCmd.Flags().BoolVar(&noCleanup, "no-cleanup", false, "Keep test repositories for debugging")
	rootCmd.Flags().BoolVarP(&initFlag, "init", "i", false, "Initialize e2e environment (create git repository and config files) and exit")
	rootCmd.Flags().IntVarP(&verbose, "verbose", "v", 0, "Verbose level (0=error, 1=warn, 2=info, 3=debug)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runInitialization() error {
	testTypeVal, err := parseTestType(testType)
	if err != nil {
		return err
	}
	th := NewE2ETestHarness(testTypeVal, 0, noCleanup)

	slog.Info("Cleaning /tmp/home-ci/repos directory...")
	if err := th.cleanupReposDirectory(); err != nil {
		slog.Warn("Failed to clean repos directory", "error", err)
	}

	slog.Info("Creating all configuration files...")
	if err := th.createAllConfigFiles(); err != nil {
		return fmt.Errorf("failed to create config files: %w", err)
	}

	slog.Info("E2E environment initialization completed!")
	slog.Info("Repository created", "path", th.testRepoPath)
	slog.Info("Config files created in: /tmp/home-ci/e2e/")
	slog.Info("Cleaned /tmp/home-ci/repos directory")
	slog.Info("To explore the repository:")
	slog.Info("   cd " + th.testRepoPath)
	slog.Info("   git log --oneline --all --graph")
	return nil
}

func runE2ETests() error {
	testTypeVal, err := parseTestType(testType)
	if err != nil {
		return err
	}

	// Parse duration
	durationVal, err := time.ParseDuration(duration)
	if err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}

	// Adjust duration based on test type
	switch testTypeVal {
	case TestSuccess, TestFail:
		durationVal = 30 * time.Second // Short duration for single commit tests
	case TestTimeout:
		durationVal = 60 * time.Second // Fixed duration for timeout tests
	case TestDispatchOneSuccess, TestDispatchNoTokenFile:
		durationVal = 45 * time.Second // Slightly longer for dispatch tests
	case TestQuick:
		if durationVal > 30*time.Second {
			durationVal = 30 * time.Second
		}
	case TestDispatchAll:
		if durationVal > 45*time.Second {
			durationVal = 45 * time.Second // Longer for dispatch-all tests
		}
	case TestConcurrentLimit:
		durationVal = 120 * time.Second // Fixed duration for concurrent limit tests (increased for proper concurrency)
	case TestContinuousCI:
		durationVal = 75 * time.Second // Fixed duration for continuous CI test (optimized for speed)
	// TestNormal and TestLong use user-specified duration
	}

	slog.Info("🚀 Starting e2e test harness", "type", testTypeName[testTypeVal], "duration", durationVal)

	th := NewE2ETestHarness(testTypeVal, durationVal, noCleanup)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Warn("Received interrupt signal, shutting down...")
		th.cleanupE2EResources()
		os.Exit(0)
	}()

	// Test steps
	if err := th.setupTestRepo(); err != nil {
		return fmt.Errorf("failed to setup test repository: %w", err)
	}

	configPath, err := th.createConfigFile()
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	if err := th.startHomeCI(configPath); err != nil {
		return fmt.Errorf("failed to start home-ci: %w", err)
	}

	// Start log monitoring
	th.monitorState()

	// Simulate development activity
	th.simulateActivity()

	// Wait for tests to complete based on type
	if testTypeVal == TestTimeout {
		slog.Info("Waiting for timeout to occur...")
		time.Sleep(60 * time.Second) // Wait for timeout + processing
	} else if testTypeVal.isSingleCommitTest() {
		slog.Info("Waiting for single commit test to complete...")
		time.Sleep(20 * time.Second) // Shorter wait for single commit tests
	} else if testTypeVal == TestConcurrentLimit {
		slog.Info("Waiting for concurrent limit tests to complete...")
		time.Sleep(60 * time.Second) // Longer wait for concurrent limit tests due to proper concurrency control
	} else if testTypeVal == TestContinuousCI {
		slog.Info("Waiting for continuous CI tests to complete...")
		time.Sleep(45 * time.Second) // Wait for continuous integration tests with variable commits
	} else {
		slog.Info("Waiting for final tests to complete...")
		time.Sleep(30 * time.Second)
	}

	// Display statistics
	th.printStatistics()

	// Analyze test results against expectations
	resultsValid := th.analyzeTestResults()

	// Clean up e2e test harness resources
	th.cleanupE2EResources()

	// Determine success based on test type
	success := true
	switch testTypeVal {
	case TestTimeout:
		success = th.timeoutDetected && th.verifyCleanupExecuted() && resultsValid
	case TestSuccess, TestFail, TestDispatchOneSuccess:
		// For single commit tests, success means at least one test was detected and all results valid
		success = th.totalTestsDetected > 0 && resultsValid
	default:
		// For multi-commit tests, success means tests were detected and all results valid
		success = th.totalTestsDetected > 0 && resultsValid
	}

	if success {
		slog.Info("Test harness completed successfully!")
		return nil
	} else {
		return fmt.Errorf("test harness failed")
	}
}

