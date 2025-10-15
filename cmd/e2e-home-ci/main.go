package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var (
		testTypeFlag  = flag.String("type", "normal", "Test type: normal, timeout, quick, long, dispatch")
		durationFlag  = flag.String("duration", "3m", "Test duration (e.g., 30s, 5m, 1h)")
		noCleanupFlag = flag.Bool("no-cleanup", false, "Keep test repositories for debugging")
		initFlag      = flag.Bool("init", false, "Initialize e2e environment (create git repository and config files) and exit")
		helpFlag      = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *helpFlag {
		fmt.Println("Home-CI E2E Test Harness")
		fmt.Println("========================")
		fmt.Println("")
		fmt.Println("Usage: e2e-home-ci [options]")
		fmt.Println("")
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Test Types:")
		fmt.Println("  normal   - Standard integration test (default)")
		fmt.Println("  timeout  - Test timeout handling (~1 minute)")
		fmt.Println("  quick    - Quick test (30 seconds)")
		fmt.Println("  long     - Extended test (specified duration)")
		fmt.Println("  dispatch - Test GitHub Actions dispatch")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  e2e-home-ci -type=normal -duration=5m")
		fmt.Println("  e2e-home-ci -type=timeout")
		fmt.Println("  e2e-home-ci -type=quick")
		fmt.Println("  e2e-home-ci -init                      # Initialize e2e environment")
		fmt.Println("  e2e-home-ci -type=timeout -no-cleanup  # Keep repos for debugging")
		return
	}

	testType := parseTestType(*testTypeFlag)

	// Parse duration
	duration, err := time.ParseDuration(*durationFlag)
	if err != nil {
		log.Fatalf("❌ Invalid duration format: %v", err)
	}

	// Adjust duration based on test type
	switch testType {
	case TestTimeout:
		duration = 60 * time.Second // Fixed duration for timeout tests
	case TestQuick:
		if duration > 30*time.Second {
			duration = 30 * time.Second
		}
	}

	log.Printf("🚀 Starting e2e test harness (%s, %v)...",
		testTypeName[testType],
		duration)

	th := NewE2ETestHarness(testType, duration, *noCleanupFlag)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\n⚠️  Received interrupt signal, shutting down...")
		th.cleanupE2EResources()
		os.Exit(0)
	}()

	// Test steps
	if err := th.setupTestRepo(); err != nil {
		log.Fatalf("❌ Failed to setup test repository: %v", err)
	}

	// If init flag is set, create repository and config files, then exit
	if *initFlag {
		log.Println("🧹 Cleaning /tmp/home-ci/repos directory...")
		if err := th.cleanupReposDirectory(); err != nil {
			log.Printf("⚠️  Warning: Failed to clean repos directory: %v", err)
		}

		log.Println("✅ Creating all configuration files...")
		if err := th.createAllConfigFiles(); err != nil {
			log.Fatalf("❌ Failed to create config files: %v", err)
		}
		log.Println("✅ E2E environment initialization completed!")
		log.Printf("📁 Repository created at: %s", th.testRepoPath)
		log.Printf("⚙️  Config files created in: /tmp/home-ci/e2e/")
		log.Printf("🧹 Cleaned /tmp/home-ci/repos directory")
		log.Printf("🔍 To explore the repository:")
		log.Printf("   cd %s", th.testRepoPath)
		log.Printf("   git log --oneline --all --graph")
		return
	}

	configPath, err := th.createConfigFile()
	if err != nil {
		log.Fatalf("❌ Failed to create config file: %v", err)
	}

	if err := th.startHomeCI(configPath); err != nil {
		log.Fatalf("❌ Failed to start home-ci: %v", err)
	}

	// Start log monitoring
	th.monitorState()

	// Simulate development activity
	th.simulateActivity()

	// Wait for tests to complete based on type
	if testType == TestTimeout {
		log.Println("⏳ Waiting for timeout to occur...")
		time.Sleep(60 * time.Second) // Wait for timeout + processing
	} else {
		log.Println("⏳ Waiting for final tests to complete...")
		time.Sleep(30 * time.Second)
	}

	// Display statistics
	th.printStatistics()

	// Analyze test results against expectations
	th.analyzeTestResults()

	// Clean up e2e test harness resources
	th.cleanupE2EResources()

	// Determine success
	success := true
	if testType == TestTimeout {
		success = th.timeoutDetected && th.verifyCleanupExecuted()
	} else {
		success = th.totalTestsDetected > 0
	}

	if success {
		log.Println("🎉 Test harness completed successfully!")
	} else {
		log.Println("💥 Test harness failed!")
		os.Exit(1)
	}
}
