package runner

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/k8s-school/home-ci/internal/config"
)

// MockStateManager implémente StateManager pour les tests
type MockStateManager struct {
	runningTests []RunningTest
	mu           sync.Mutex
}

func (m *MockStateManager) AddRunningTest(test RunningTest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runningTests = append(m.runningTests, test)
}

func (m *MockStateManager) RemoveRunningTest(branch, commit string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, test := range m.runningTests {
		if test.Branch == branch && test.Commit == commit {
			m.runningTests = append(m.runningTests[:i], m.runningTests[i+1:]...)
			break
		}
	}
}

func (m *MockStateManager) GetRunningTests() []RunningTest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]RunningTest(nil), m.runningTests...)
}

func (m *MockStateManager) CleanupOldRunningTests(maxAge time.Duration) {
	// No-op for tests
}

func (m *MockStateManager) SaveState() error {
	return nil
}

func (m *MockStateManager) GetRunningTestsCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runningTests)
}

// Compteurs globaux pour mesurer la concurrence dans les tests
var (
	testRunningCount  int64
	testMaxConcurrent int64
	testRunsCalled    int64
)

// MockTestExecution simule l'exécution d'un test pour les tests de concurrence
func mockTestExecution(duration time.Duration) {
	// Incrémenter le compteur de tests en cours
	current := atomic.AddInt64(&testRunningCount, 1)
	atomic.AddInt64(&testRunsCalled, 1)

	// Mettre à jour le maximum concurrent observé
	for {
		max := atomic.LoadInt64(&testMaxConcurrent)
		if current <= max || atomic.CompareAndSwapInt64(&testMaxConcurrent, max, current) {
			break
		}
	}

	// Simuler le temps d'exécution du test
	time.Sleep(duration)

	// Décrémenter le compteur
	atomic.AddInt64(&testRunningCount, -1)
}

func resetTestCounters() {
	atomic.StoreInt64(&testRunningCount, 0)
	atomic.StoreInt64(&testMaxConcurrent, 0)
	atomic.StoreInt64(&testRunsCalled, 0)
}

// Test du mécanisme de semaphore en isolation
func TestSemaphoreMechanism(t *testing.T) {
	tests := []struct {
		name            string
		maxConcurrent   int
		numGoroutines   int
		workDuration    time.Duration
		expectedMaxConc int
	}{
		{
			name:            "Limit 2 with 4 goroutines",
			maxConcurrent:   2,
			numGoroutines:   4,
			workDuration:    100 * time.Millisecond,
			expectedMaxConc: 2,
		},
		{
			name:            "Limit 1 with 3 goroutines should be sequential",
			maxConcurrent:   1,
			numGoroutines:   3,
			workDuration:    50 * time.Millisecond,
			expectedMaxConc: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTestCounters()

			// Créer un semaphore comme dans le code réel
			semaphore := make(chan struct{}, tt.maxConcurrent)

			var wg sync.WaitGroup

			// Lancer plusieurs goroutines comme le fait executeTestJob
			for i := 0; i < tt.numGoroutines; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()

					// Reproduire exactement la logique de executeTestJob
					semaphore <- struct{}{}        // Acquire
					defer func() { <-semaphore }() // Release

					mockTestExecution(tt.workDuration) // Simulate work
				}(i)
			}

			wg.Wait()

			// Vérifications
			maxObserved := atomic.LoadInt64(&testMaxConcurrent)
			totalCalled := atomic.LoadInt64(&testRunsCalled)

			if totalCalled != int64(tt.numGoroutines) {
				t.Errorf("Expected %d executions, got %d", tt.numGoroutines, totalCalled)
			}

			if maxObserved > int64(tt.expectedMaxConc) {
				t.Errorf("CONCURRENCY VIOLATION: limit=%d, observed=%d",
					tt.expectedMaxConc, maxObserved)
			}

			t.Logf("Semaphore test: limit=%d, goroutines=%d, max_observed=%d ✓",
				tt.maxConcurrent, tt.numGoroutines, maxObserved)
		})
	}
}

// Test qui reproduit le pattern exact du TestRunner pour voir s'il y a un bug
func TestRunnerConcurrencyPattern(t *testing.T) {
	resetTestCounters()

	maxConcurrent := 2
	numJobs := 4

	// Reproduire la logique exacte du TestRunner.Start()
	testQueue := make(chan TestJob, 100)
	semaphore := make(chan struct{}, maxConcurrent)

	// Simuler executeTestJob
	executeTestJob := func(job TestJob) {
		// Acquire semaphore (exactement comme dans le code)
		semaphore <- struct{}{}
		defer func() { <-semaphore }()

		mockTestExecution(100 * time.Millisecond)
	}

	// Reproduire Start() - la boucle qui lance des goroutines
	go func() {
		for job := range testQueue {
			go executeTestJob(job) // *** C'EST ICI LE PROBLEME POTENTIEL ***
		}
	}()

	// Ajouter des jobs rapidement comme dans le test concurrent-limit
	for i := 0; i < numJobs; i++ {
		job := TestJob{
			Branch: "concurrent/test" + string(rune('1'+i)),
			Commit: "commit" + string(rune('0'+i)),
		}
		testQueue <- job
	}

	// Attendre que tous les tests se terminent
	time.Sleep(500 * time.Millisecond)
	close(testQueue)

	// Vérifications
	maxObserved := atomic.LoadInt64(&testMaxConcurrent)
	totalCalled := atomic.LoadInt64(&testRunsCalled)

	t.Logf("TestRunner pattern: limit=%d, jobs=%d, max_observed=%d, total_called=%d",
		maxConcurrent, numJobs, maxObserved, totalCalled)

	if totalCalled != int64(numJobs) {
		t.Errorf("Expected %d tests to be called, got %d", numJobs, totalCalled)
	}

	if maxObserved > int64(maxConcurrent) {
		t.Errorf("BUG CONFIRMED: TestRunner pattern violates concurrency! limit=%d, observed=%d",
			maxConcurrent, maxObserved)
	} else {
		t.Logf("✓ Concurrency respected with TestRunner pattern")
	}
}

// Test qui reproduit exactement le scénario du bug concurrent-limit
func TestConcurrentLimitScenario(t *testing.T) {
	resetTestCounters()

	maxConcurrent := 2

	// Reproduire la logique exacte du TestRunner
	testQueue := make(chan TestJob, 100)
	semaphore := make(chan struct{}, maxConcurrent)

	executeTestJob := func(job TestJob) {
		semaphore <- struct{}{}
		defer func() { <-semaphore }()

		// Tests plus longs comme dans le vrai scénario (15 secondes pour concurrent tests)
		mockTestExecution(150 * time.Millisecond)
	}

	// Démarrer le runner
	go func() {
		for job := range testQueue {
			go executeTestJob(job)
		}
	}()

	// Reproduire exactement ce qui se passe dans concurrent-limit test:
	// 4 commits créés sur 4 branches en moins d'une seconde
	jobs := []TestJob{
		{Branch: "concurrent/test1", Commit: "20e8a4f2"},
		{Branch: "concurrent/test2", Commit: "67563de0"},
		{Branch: "concurrent/test3", Commit: "cf367723"},
		{Branch: "concurrent/test4", Commit: "ac99b5ba"},
	}

	// Ajouter tous les jobs très rapidement (< 2 secondes comme dans le vrai test)
	for i, job := range jobs {
		testQueue <- job
		// Petit délai comme dans le vrai scénario
		time.Sleep(time.Duration(i) * time.Millisecond)
	}

	// Échantillonner la concurrence pendant l'exécution
	var maxSampled int64
	samplingDone := make(chan bool)

	go func() {
		for i := 0; i < 20; i++ {
			current := atomic.LoadInt64(&testRunningCount)
			if current > maxSampled {
				maxSampled = current
			}
			time.Sleep(25 * time.Millisecond)
		}
		samplingDone <- true
	}()

	<-samplingDone

	// Attendre que tous les tests se terminent
	time.Sleep(200 * time.Millisecond)
	close(testQueue)

	// Vérifications
	maxObserved := atomic.LoadInt64(&testMaxConcurrent)
	totalCalled := atomic.LoadInt64(&testRunsCalled)

	t.Logf("Concurrent-limit scenario: limit=%d, jobs=%d", maxConcurrent, len(jobs))
	t.Logf("  max_observed_during_execution=%d", maxObserved)
	t.Logf("  max_sampled_during_test=%d", maxSampled)
	t.Logf("  total_tests_executed=%d", totalCalled)

	if totalCalled != int64(len(jobs)) {
		t.Errorf("Expected %d tests, got %d", len(jobs), totalCalled)
	}

	// C'est ici qu'on devrait voir le bug si il existe
	if maxObserved > int64(maxConcurrent) {
		t.Errorf("🐛 BUG REPRODUCED: Concurrency limit violated! limit=%d, observed=%d",
			maxConcurrent, maxObserved)
	}

	if maxSampled > int64(maxConcurrent) {
		t.Errorf("🐛 BUG DETECTED by sampling: limit=%d, sampled=%d",
			maxConcurrent, maxSampled)
	}
}

// Test pour reproduire une situation de 4 tests concurrents avec chevauchement des temps
func TestAnalyzeConcurrencyLogic(t *testing.T) {
	// Simuler 4 tests qui se chevauchent intentionnellement pour violer la limite de 2
	testResults := []TestResult{
		{
			Branch:    "concurrent/test1",
			Commit:    "commit1",
			StartTime: time.Date(2025, 10, 17, 15, 45, 0, 0, time.UTC),
			EndTime:   time.Date(2025, 10, 17, 15, 45, 15, 0, time.UTC), // 15 secondes
			Success:   true,
		},
		{
			Branch:    "concurrent/test2",
			Commit:    "commit2",
			StartTime: time.Date(2025, 10, 17, 15, 45, 7, 0, time.UTC),  // Démarre 7s après test1
			EndTime:   time.Date(2025, 10, 17, 15, 45, 22, 0, time.UTC), // 15 secondes
			Success:   true,
		},
		{
			Branch:    "bugfix/critical",
			Commit:    "commit3",
			StartTime: time.Date(2025, 10, 17, 15, 44, 56, 0, time.UTC), // Démarre avant test1
			EndTime:   time.Date(2025, 10, 17, 15, 45, 26, 0, time.UTC), // 30 secondes
			Success:   false,
		},
		{
			Branch:    "feature/test2",
			Commit:    "commit4",
			StartTime: time.Date(2025, 10, 17, 15, 45, 4, 0, time.UTC), // Démarre pendant test1
			EndTime:   time.Date(2025, 10, 17, 15, 45, 7, 0, time.UTC), // 3 secondes
			Success:   false,
		},
	}

	maxConcurrent, _ := analyzeConcurrencyFromResults(testResults)

	t.Logf("Analyze concurrency test:")
	t.Logf("  Test 1: 15:44:56 -> 15:45:26 (bugfix/critical)")
	t.Logf("  Test 2: 15:45:00 -> 15:45:15 (concurrent/test1)")
	t.Logf("  Test 3: 15:45:04 -> 15:45:07 (feature/test2)")
	t.Logf("  Test 4: 15:45:07 -> 15:45:22 (concurrent/test2)")
	t.Logf("  Maximum concurrent detected: %d", maxConcurrent)

	// À 15:45:05, nous devrions avoir 3 tests en cours: bugfix/critical, concurrent/test1, feature/test2
	if maxConcurrent >= 3 {
		t.Logf("✓ Concurrency analysis correctly detected overlapping tests: %d", maxConcurrent)
	} else {
		t.Errorf("❌ Concurrency analysis missed overlapping tests. Expected ≥3, got %d", maxConcurrent)
	}
}

// Version simplifiée de la fonction analyzeConcurrency pour les tests
func analyzeConcurrencyFromResults(testResults []TestResult) (int, []string) {
	type Event struct {
		Time time.Time
		Type string // "start" or "end"
		Test string
	}

	var events []Event
	for _, result := range testResults {
		testId := result.Branch + "-" + result.Commit
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

	// Trier par temps
	sort.Slice(events, func(i, j int) bool {
		if events[i].Time.Equal(events[j].Time) {
			return events[i].Type == "end" && events[j].Type == "start"
		}
		return events[i].Time.Before(events[j].Time)
	})

	currentConcurrent := 0
	maxConcurrent := 0

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

	return maxConcurrent, nil
}

// Test du fix du bug de concurrence
func TestConcurrencyFixValidation(t *testing.T) {
	resetTestCounters()

	maxConcurrent := 2
	numJobs := 6

	// Reproduire la logique CORRIGÉE du TestRunner.Start()
	testQueue := make(chan TestJob, 100)
	semaphore := make(chan struct{}, maxConcurrent)

	executeTestJob := func(job TestJob) {
		mockTestExecution(100 * time.Millisecond)
	}

	// Nouvelle logique corrigée : acquérir le semaphore AVANT de lancer la goroutine
	go func() {
		for job := range testQueue {
			// AVANT : go executeTestJob(job) puis semaphore à l'intérieur
			// APRÈS : semaphore d'abord, puis go executeTestJob(job)
			semaphore <- struct{}{} // Acquire BEFORE launching goroutine
			go func(j TestJob) {
				defer func() { <-semaphore }() // Release when done
				executeTestJob(j)
			}(job)
		}
	}()

	// Ajouter 6 jobs très rapidement comme dans le bug original
	for i := 0; i < numJobs; i++ {
		job := TestJob{
			Branch: "test/branch" + string(rune('1'+i)),
			Commit: "commit" + string(rune('0'+i)),
		}
		testQueue <- job
	}

	// Attendre que tous les tests se terminent
	time.Sleep(300 * time.Millisecond)
	close(testQueue)

	// Vérifications
	maxObserved := atomic.LoadInt64(&testMaxConcurrent)
	totalCalled := atomic.LoadInt64(&testRunsCalled)

	t.Logf("Concurrency fix validation:")
	t.Logf("  limit=%d, jobs=%d, max_observed=%d, total_called=%d",
		maxConcurrent, numJobs, maxObserved, totalCalled)

	if totalCalled != int64(numJobs) {
		t.Errorf("Expected %d tests, got %d", numJobs, totalCalled)
	}

	if maxObserved > int64(maxConcurrent) {
		t.Errorf("❌ FIX FAILED: Concurrency limit still violated! limit=%d, observed=%d",
			maxConcurrent, maxObserved)
	} else {
		t.Logf("✅ FIX SUCCESSFUL: Concurrency limit respected! limit=%d, max_observed=%d",
			maxConcurrent, maxObserved)
	}
}

// TestExecuteTestHomeCIResultFileHandling verifies that HOME_CI_RESULT_FILE is properly set and accessible to test scripts
func TestExecuteTestHomeCIResultFileHandling(t *testing.T) {
	// Create temporary directories for test
	tempDir, err := os.MkdirTemp("", "home-ci-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logsDir := filepath.Join(tempDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("Failed to create logs directory: %v", err)
	}

	workspaceDir := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("Failed to create workspace directory: %v", err)
	}

	// Create a test script that verifies HOME_CI_RESULT_FILE is set and creates the result file
	testScript := filepath.Join(workspaceDir, "test_script.sh")
	scriptContent := `#!/bin/bash
set -e

# Check that HOME_CI_RESULT_FILE is set
if [ -z "$HOME_CI_RESULT_FILE" ]; then
    echo "ERROR: HOME_CI_RESULT_FILE environment variable is not set"
    exit 1
fi

echo "SUCCESS: HOME_CI_RESULT_FILE is set to: $HOME_CI_RESULT_FILE"

# Create the result file with test data
cat > "$HOME_CI_RESULT_FILE" << 'EOF'
test_name: "sample_e2e_test"
status: "passed"
duration: "45.2s"
results:
  - test_case: "login_functionality"
    status: "passed"
    duration: "12.1s"
  - test_case: "data_validation"
    status: "passed"
    duration: "33.1s"
artifacts:
  - "screenshots/login_test.png"
  - "reports/performance_metrics.json"
EOF

echo "Created result file at: $HOME_CI_RESULT_FILE"
echo "Result file contents:"
cat "$HOME_CI_RESULT_FILE"

exit 0
`

	if err := os.WriteFile(testScript, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	// Create test config
	cfg := config.Config{
		Repository:  tempDir,
		RepoName:    "test-repo",
		TestScript:  testScript,
		TestTimeout: time.Minute,
		Options:     "",
		WorkDir:     tempDir,
	}

	// Create log file
	logFile, err := os.Create(filepath.Join(logsDir, "test.log"))
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	defer logFile.Close()

	// Create TestRunner and TestExecution
	ctx := context.Background()
	testRunner := &TestRunner{
		config: cfg,
		ctx:    ctx,
	}

	testResult := &TestResult{
		Branch:    "test-branch",
		Commit:    "abc123def",
		StartTime: time.Now(),
	}

	// Create the proper logs directory that HOME_CI_RESULT_FILE will point to
	expectedLogsDir := cfg.GetLogsDir("test-branch", "abc123def")
	if err := os.MkdirAll(expectedLogsDir, 0755); err != nil {
		t.Fatalf("Failed to create expected logs directory: %v", err)
	}

	testExecution := &TestExecution{
		runner:       testRunner,
		branch:       "test-branch",
		commit:       "abc123def",
		workspaceDir: workspaceDir,
		projectDir:   workspaceDir,
		logFile:      logFile,
		testResult:   testResult,
	}

	// Execute the test
	err = testExecution.executeTest()

	// Verify execution was successful
	if err != nil {
		t.Errorf("executeTest() failed: %v", err)
	}

	if !testExecution.testResult.Success {
		t.Errorf("Test should have succeeded, but testResult.Success = false")
	}

	// Verify that the result file was created
	// Use the config method to get the correct logs directory path
	expectedResultFile := filepath.Join(cfg.GetLogsDir("test-branch", "abc123def"), "e2e-report.yaml")
	if _, err := os.Stat(expectedResultFile); os.IsNotExist(err) {
		t.Errorf("Expected result file was not created: %s", expectedResultFile)
	} else {
		// Read and verify result file content
		content, err := ioutil.ReadFile(expectedResultFile)
		if err != nil {
			t.Errorf("Failed to read result file: %v", err)
		} else {
			contentStr := string(content)
			if !strings.Contains(contentStr, "sample_e2e_test") {
				t.Errorf("Result file doesn't contain expected test content")
			}
			if !strings.Contains(contentStr, "login_functionality") {
				t.Errorf("Result file doesn't contain expected test case content")
			}
			if !strings.Contains(contentStr, "artifacts:") {
				t.Errorf("Result file doesn't contain expected artifacts section")
			}
			t.Logf("✅ Result file created successfully with expected content")
		}
	}

	// Verify log file contains expected messages
	logFile.Close()
	logContent, err := ioutil.ReadFile(filepath.Join(logsDir, "test.log"))
	if err != nil {
		t.Errorf("Failed to read log file: %v", err)
	} else {
		logStr := string(logContent)
		if !strings.Contains(logStr, "SUCCESS: HOME_CI_RESULT_FILE is set to:") {
			t.Errorf("Log file doesn't contain expected success message about HOME_CI_RESULT_FILE")
		}
		if !strings.Contains(logStr, expectedResultFile) {
			t.Errorf("Log file doesn't contain the expected result file path")
		}
		t.Logf("✅ Log file contains expected messages about HOME_CI_RESULT_FILE")
	}

	t.Logf("✅ Test completed successfully - HOME_CI_RESULT_FILE is properly handled")
}
