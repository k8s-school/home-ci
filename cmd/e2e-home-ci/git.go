package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Git configuration
	gitPager        = "cat"
	gitUserName     = "CI Test"
	gitUserEmail    = "ci-test@example.com"
	defaultBranch   = "main"

	// File permissions
	filePerm = 0644

	// Git log display
	logDisplayCount = 5
)

// initializeGitRepo initializes the git repository based on test type
func (th *E2ETestHarness) initializeGitRepo() error {
	if err := th.setupGitEnvironment(); err != nil {
		return err
	}

	if err := th.configureGit(); err != nil {
		return err
	}

	// Create repository content based on test type
	switch {
	case th.testType.isSingleCommitTest():
		return th.createSingleCommitRepository()
	case th.testType == TestQuick || th.testType == TestDispatchAll:
		return th.createMultiTypeTestRepository()
	default: // TestNormal, TestLong
		return th.createMultiBranchRepository()
	}
}

// setupGitEnvironment sets up the git environment
func (th *E2ETestHarness) setupGitEnvironment() error {
	os.Setenv("GIT_PAGER", gitPager)
	return nil
}

// configureGit configures git with necessary settings
func (th *E2ETestHarness) configureGit() error {
	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", gitUserName},
		{"git", "config", "user.email", gitUserEmail},
		{"git", "config", "advice.detachedHead", "false"},
		{"git", "config", "init.defaultBranch", defaultBranch},
		{"git", "config", "pager.branch", "false"},
		{"git", "config", "pager.log", "false"},
		{"git", "config", "core.pager", gitPager},
	}

	for _, cmd := range commands {
		if err := th.runGitCommand(cmd...); err != nil {
			return fmt.Errorf("failed to run git command %v: %w", cmd, err)
		}
	}
	return nil
}

// createInitialFiles creates the basic repository structure
func (th *E2ETestHarness) createInitialFiles() error {
	files := map[string]string{
		"README.md":  "# Test Repository\n",
		".gitignore": "node_modules/\n*.log\n.home-ci/\n",
		"app.py":     "# Main application file\nprint('Hello from test app')\n",
	}

	for filename, content := range files {
		filePath := filepath.Join(th.testRepoPath, filename)
		if err := os.WriteFile(filePath, []byte(content), filePerm); err != nil {
			return fmt.Errorf("failed to create %s: %w", filename, err)
		}
	}
	return nil
}

// createInitialCommit creates the first commit and sets up main branch
func (th *E2ETestHarness) createInitialCommit() error {
	if err := th.runGitCommand("git", "add", "."); err != nil {
		return fmt.Errorf("failed to add files: %w", err)
	}
	if err := th.runGitCommand("git", "commit", "-m", "Initial commit"); err != nil {
		return fmt.Errorf("failed to create initial commit: %w", err)
	}
	if err := th.runGitCommand("git", "branch", "-m", defaultBranch); err != nil {
		return fmt.Errorf("failed to rename branch to %s: %w", defaultBranch, err)
	}
	return nil
}

// BranchConfig represents a branch configuration for testing
type BranchConfig struct {
	name    string
	files   map[string]string
	commits []string
}

// createTestBranches creates test branches with commits
func (th *E2ETestHarness) createTestBranches() error {
	branches := []BranchConfig{
		{
			name: "feature/test1",
			files: map[string]string{
				"feature1.txt": "Feature 1 content\n",
			},
			commits: []string{"Add feature 1", "Update feature 1"},
		},
		{
			name: "feature/test2",
			files: map[string]string{
				"feature2.txt": "Feature 2 content\n",
			},
			commits: []string{"Add feature 2"},
		},
		{
			name: "bugfix/critical",
			files: map[string]string{
				"bugfix.txt": "Bug fix content\n",
			},
			commits: []string{"Fix critical bug"},
		},
	}

	for _, branch := range branches {
		if err := th.createBranchWithCommits(branch); err != nil {
			return err
		}
	}
	return nil
}

// createBranchWithCommits creates a single branch with its commits
func (th *E2ETestHarness) createBranchWithCommits(branch BranchConfig) error {
	if err := th.runGitCommand("git", "checkout", "-b", branch.name); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", branch.name, err)
	}

	if err := th.createBranchFiles(branch.files); err != nil {
		return err
	}

	return th.createBranchCommits(branch)
}

// createBranchFiles creates files for a branch
func (th *E2ETestHarness) createBranchFiles(files map[string]string) error {
	for filename, content := range files {
		filePath := filepath.Join(th.testRepoPath, filename)
		if err := os.WriteFile(filePath, []byte(content), filePerm); err != nil {
			return fmt.Errorf("failed to create %s: %w", filename, err)
		}
		if err := th.runGitCommand("git", "add", filename); err != nil {
			return fmt.Errorf("failed to add %s: %w", filename, err)
		}
	}
	return nil
}

// createBranchCommits creates commits for a branch
func (th *E2ETestHarness) createBranchCommits(branch BranchConfig) error {
	for _, commitMsg := range branch.commits {
		if err := th.runGitCommand("git", "commit", "-m", commitMsg); err != nil {
			return fmt.Errorf("failed to commit %s: %w", commitMsg, err)
		}
		if len(branch.commits) > 1 {
			if err := th.updateBranchFiles(branch.files); err != nil {
				return err
			}
		}
	}
	return nil
}

// updateBranchFiles updates files for next commit
func (th *E2ETestHarness) updateBranchFiles(files map[string]string) error {
	for filename := range files {
		filePath := filepath.Join(th.testRepoPath, filename)
		if err := os.WriteFile(filePath, []byte(files[filename]+"Updated\n"), filePerm); err != nil {
			return fmt.Errorf("failed to update %s: %w", filename, err)
		}
		if err := th.runGitCommand("git", "add", filename); err != nil {
			return fmt.Errorf("failed to add updated %s: %w", filename, err)
		}
	}
	return nil
}

// createMainUpdates creates commits on the main branch
func (th *E2ETestHarness) createMainUpdates() error {
	if err := th.runGitCommand("git", "checkout", defaultBranch); err != nil {
		return fmt.Errorf("failed to checkout %s: %w", defaultBranch, err)
	}

	mainUpdates := []string{"Main update 1", "Main update 2"}
	for i, update := range mainUpdates {
		if err := th.createMainUpdate(update, i); err != nil {
			return err
		}
	}
	return nil
}

// createMainUpdate creates a single update on main branch
func (th *E2ETestHarness) createMainUpdate(update string, index int) error {
	filename := "main-update.txt"
	filePath := filepath.Join(th.testRepoPath, filename)
	content := fmt.Sprintf("%s\n", update)

	if index > 0 {
		// Append to existing file
		existingContent, _ := os.ReadFile(filePath)
		content = string(existingContent) + content
	}

	if err := os.WriteFile(filePath, []byte(content), filePerm); err != nil {
		return fmt.Errorf("failed to create %s: %w", filename, err)
	}
	if err := th.runGitCommand("git", "add", filename); err != nil {
		return fmt.Errorf("failed to add %s: %w", filename, err)
	}
	if err := th.runGitCommand("git", "commit", "-m", update); err != nil {
		return fmt.Errorf("failed to commit %s: %w", update, err)
	}
	return nil
}

// createSingleCommitRepository creates a repository with a single commit based on test type
func (th *E2ETestHarness) createSingleCommitRepository() error {
	if err := th.createInitialFiles(); err != nil {
		return err
	}

	if err := th.createInitialCommit(); err != nil {
		return err
	}

	// Create specific commit based on test type
	var commitMessage, fileName, content string
	switch th.testType {
	case TestSuccess:
		commitMessage = "SUCCESS: This commit should pass"
		fileName = "success.txt"
		content = "This file should make the test succeed"
	case TestFail:
		commitMessage = "FAIL: This commit should fail"
		fileName = "fail.txt"
		content = "This file should make the test fail"
	case TestTimeout:
		commitMessage = "TIMEOUT: This commit should timeout"
		fileName = "timeout.txt"
		content = "This file should make the test timeout"
	case TestDispatchOneSuccess:
		commitMessage = "Single dispatch test commit"
		fileName = "dispatch.txt"
		content = "This commit should trigger GitHub Actions dispatch"
	}

	filePath := filepath.Join(th.testRepoPath, fileName)
	if err := os.WriteFile(filePath, []byte(content), filePerm); err != nil {
		return fmt.Errorf("failed to create %s: %w", fileName, err)
	}

	if err := th.runGitCommand("git", "add", fileName); err != nil {
		return fmt.Errorf("failed to add %s: %w", fileName, err)
	}

	if err := th.runGitCommand("git", "commit", "-m", commitMessage); err != nil {
		return fmt.Errorf("failed to commit %s: %w", commitMessage, err)
	}

	if th.testType != TestTimeout {
		th.displayRepositoryState()
	}
	return nil
}


// createMultiTypeTestRepository creates a repository with test commits on different branches to test all behaviors
func (th *E2ETestHarness) createMultiTypeTestRepository() error {
	if err := th.createInitialFiles(); err != nil {
		return err
	}

	if err := th.createInitialCommit(); err != nil {
		return err
	}

	// Determine test prefix based on test type
	var testPrefix string
	switch th.testType {
	case TestDispatchAll:
		testPrefix = "Dispatch-all"
	case TestQuick:
		testPrefix = "Quick"
	default:
		testPrefix = "Multi-type"
	}

	// Create test commits on different branches to trigger different behaviors
	testCases := []struct {
		branch   string
		message  string
		fileName string
		content  string
	}{
		{"main", fmt.Sprintf("SUCCESS: %s test success case", testPrefix), fmt.Sprintf("%s-success.txt", strings.ToLower(testPrefix)), fmt.Sprintf("This should succeed with %s", strings.ToLower(testPrefix))},
		{"feature/test-fail", fmt.Sprintf("FAIL: %s test failure case", testPrefix), fmt.Sprintf("%s-fail.txt", strings.ToLower(testPrefix)), fmt.Sprintf("This should fail with %s", strings.ToLower(testPrefix))},
		{"bugfix/timeout", fmt.Sprintf("TIMEOUT: %s test timeout case", testPrefix), fmt.Sprintf("%s-timeout.txt", strings.ToLower(testPrefix)), fmt.Sprintf("This should timeout with %s", strings.ToLower(testPrefix))},
	}

	for _, testCase := range testCases {
		// Switch to target branch (create if it doesn't exist)
		if testCase.branch != "main" {
			if err := th.runGitCommand("git", "checkout", "-b", testCase.branch); err != nil {
				return fmt.Errorf("failed to create branch %s: %w", testCase.branch, err)
			}
		} else {
			if err := th.runGitCommand("git", "checkout", "main"); err != nil {
				return fmt.Errorf("failed to switch to main: %w", err)
			}
		}

		// Create file and commit
		filePath := filepath.Join(th.testRepoPath, testCase.fileName)
		if err := os.WriteFile(filePath, []byte(testCase.content), filePerm); err != nil {
			return fmt.Errorf("failed to create %s: %w", testCase.fileName, err)
		}

		if err := th.runGitCommand("git", "add", testCase.fileName); err != nil {
			return fmt.Errorf("failed to add %s: %w", testCase.fileName, err)
		}

		if err := th.runGitCommand("git", "commit", "-m", testCase.message); err != nil {
			return fmt.Errorf("failed to commit %s: %w", testCase.message, err)
		}
	}

	// Switch back to main for display
	if err := th.runGitCommand("git", "checkout", "main"); err != nil {
		return fmt.Errorf("failed to switch back to main: %w", err)
	}

	th.displayRepositoryState()
	return nil
}

// createMultiBranchRepository creates a repository with multiple branches (original logic)
func (th *E2ETestHarness) createMultiBranchRepository() error {
	if err := th.createInitialFiles(); err != nil {
		return err
	}

	if err := th.createInitialCommit(); err != nil {
		return err
	}

	if err := th.createTestBranches(); err != nil {
		return err
	}

	if err := th.createMainUpdates(); err != nil {
		return err
	}

	th.displayRepositoryState()
	return nil
}

// displayRepositoryState shows the current repository state
func (th *E2ETestHarness) displayRepositoryState() {
	log.Println("Available branches:")
	if output, err := th.runGitCommandWithOutput("git", "branch", "-a"); err == nil {
		log.Println(output)
	}

	// Show recent commits for each branch
	if th.testType == TestDispatchAll || th.testType == TestQuick {
		branches := []string{"main", "feature/test-fail", "bugfix/timeout"}
		for _, branch := range branches {
			log.Printf("Recent commits on %s:", branch)
			logArgs := []string{"git", "log", "--oneline", fmt.Sprintf("-%d", logDisplayCount), branch}
			if output, err := th.runGitCommandWithOutput(logArgs...); err == nil {
				log.Println(output)
			}
		}
	} else {
		log.Println("Recent commits on main:")
		logArgs := []string{"git", "log", "--oneline", fmt.Sprintf("-%d", logDisplayCount)}
		if output, err := th.runGitCommandWithOutput(logArgs...); err == nil {
			log.Println(output)
		}
	}
}

// runGitCommand executes a git command in the test repository
func (th *E2ETestHarness) runGitCommand(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = th.testRepoPath
	cmd.Env = append(os.Environ(), fmt.Sprintf("GIT_PAGER=%s", gitPager))

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Git command failed: %s\nOutput: %s", strings.Join(args, " "), string(output))
		return err
	}
	return nil
}

// runGitCommandWithOutput executes a git command and returns output
func (th *E2ETestHarness) runGitCommandWithOutput(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = th.testRepoPath
	cmd.Env = append(os.Environ(), fmt.Sprintf("GIT_PAGER=%s", gitPager))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// createCommit creates a new commit on a branch
func (th *E2ETestHarness) createCommit(branch string) error {
	log.Printf("📝 Creating commit on branch %s", branch)

	// Check if the branch exists, if not create it
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = th.testRepoPath
	if err := cmd.Run(); err != nil {
		// The branch doesn't exist, create it
		if err := th.runGitCommand("git", "checkout", "-b", branch); err != nil {
			return fmt.Errorf("failed to create branch %s: %w", branch, err)
		}
		th.branchesCreated++
		log.Printf("✅ Created new branch: %s", branch)
	} else {
		// The branch exists, switch to it
		if err := th.runGitCommand("git", "checkout", branch); err != nil {
			return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
		}
	}

	// Create or modify a file
	safeBranchName := strings.ReplaceAll(branch, "/", "_")
	filename := fmt.Sprintf("file_%s_%d.txt", safeBranchName, time.Now().Unix())
	filePath := filepath.Join(th.testRepoPath, filename)
	content := fmt.Sprintf("Content for %s at %s\n", branch, time.Now().Format(time.RFC3339))

	if err := os.WriteFile(filePath, []byte(content), filePerm); err != nil {
		return fmt.Errorf("failed to create file %s: %w", filename, err)
	}

	// Add and commit
	if err := th.runGitCommand("git", "add", filename); err != nil {
		return fmt.Errorf("failed to add file: %w", err)
	}

	commitMsg := fmt.Sprintf("Add %s on branch %s", filename, branch)
	if err := th.runGitCommand("git", "commit", "-m", commitMsg); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	th.commitsCreated++
	log.Printf("✅ Created commit on %s: %s", branch, commitMsg)
	return nil
}