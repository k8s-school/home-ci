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

// initializeGitRepo initializes the git repository (logic from setup-test-repo.sh)
func (th *E2ETestHarness) initializeGitRepo() error {
	// Set GIT_PAGER to avoid interactions
	os.Setenv("GIT_PAGER", "cat")

	// Initialize git
	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "CI Test"},
		{"git", "config", "user.email", "ci-test@example.com"},
		{"git", "config", "advice.detachedHead", "false"},
		{"git", "config", "init.defaultBranch", "main"},
		{"git", "config", "pager.branch", "false"},
		{"git", "config", "pager.log", "false"},
		{"git", "config", "core.pager", "cat"},
	}

	for _, cmd := range commands {
		if err := th.runGitCommand(cmd...); err != nil {
			return fmt.Errorf("failed to run git command %v: %w", cmd, err)
		}
	}

	// Create basic structure and files (from setup-test-repo.sh)
	files := map[string]string{
		"README.md":  "# Test Repository\n",
		".gitignore": "node_modules/\n*.log\n.home-ci/\n",
		"app.py":     "# Main application file\nprint('Hello from test app')\n",
	}

	for filename, content := range files {
		filePath := filepath.Join(th.testRepoPath, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create %s: %w", filename, err)
		}
	}

	// First commit and rename branch to main
	if err := th.runGitCommand("git", "add", "."); err != nil {
		return fmt.Errorf("failed to add files: %w", err)
	}
	if err := th.runGitCommand("git", "commit", "-m", "Initial commit"); err != nil {
		return fmt.Errorf("failed to create initial commit: %w", err)
	}
	if err := th.runGitCommand("git", "branch", "-m", "main"); err != nil {
		return fmt.Errorf("failed to rename branch to main: %w", err)
	}

	// Create test branches with commits (from setup-test-repo.sh logic)
	if th.testType != TestTimeout { // Don't create extra branches for timeout test
		branches := []struct {
			name    string
			files   map[string]string
			commits []string
		}{
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
			if err := th.runGitCommand("git", "checkout", "-b", branch.name); err != nil {
				return fmt.Errorf("failed to create branch %s: %w", branch.name, err)
			}

			for filename, content := range branch.files {
				filePath := filepath.Join(th.testRepoPath, filename)
				if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
					return fmt.Errorf("failed to create %s: %w", filename, err)
				}
				if err := th.runGitCommand("git", "add", filename); err != nil {
					return fmt.Errorf("failed to add %s: %w", filename, err)
				}
			}

			for _, commitMsg := range branch.commits {
				if err := th.runGitCommand("git", "commit", "-m", commitMsg); err != nil {
					return fmt.Errorf("failed to commit %s: %w", commitMsg, err)
				}
				if len(branch.commits) > 1 {
					// Update file for next commit
					for filename := range branch.files {
						filePath := filepath.Join(th.testRepoPath, filename)
						if err := os.WriteFile(filePath, []byte(branch.files[filename]+"Updated\n"), 0644); err != nil {
							return fmt.Errorf("failed to update %s: %w", filename, err)
						}
						if err := th.runGitCommand("git", "add", filename); err != nil {
							return fmt.Errorf("failed to add updated %s: %w", filename, err)
						}
					}
				}
			}
		}

		// Return to main and make some commits
		if err := th.runGitCommand("git", "checkout", "main"); err != nil {
			return fmt.Errorf("failed to checkout main: %w", err)
		}

		mainUpdates := []string{"Main update 1", "Main update 2"}
		for i, update := range mainUpdates {
			filename := "main-update.txt"
			filePath := filepath.Join(th.testRepoPath, filename)
			content := fmt.Sprintf("%s\n", update)
			if i > 0 {
				// Append to existing file
				existingContent, _ := os.ReadFile(filePath)
				content = string(existingContent) + content
			}
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				return fmt.Errorf("failed to create %s: %w", filename, err)
			}
			if err := th.runGitCommand("git", "add", filename); err != nil {
				return fmt.Errorf("failed to add %s: %w", filename, err)
			}
			if err := th.runGitCommand("git", "commit", "-m", update); err != nil {
				return fmt.Errorf("failed to commit %s: %w", update, err)
			}
		}
	}

	// Display final state (like setup-test-repo.sh) - skip for timeout tests to reduce verbosity
	if th.testType != TestTimeout {
		log.Println("Available branches:")
		if output, err := th.runGitCommandWithOutput("git", "branch", "-a"); err == nil {
			log.Println(output)
		}

		log.Println("Recent commits on main:")
		if output, err := th.runGitCommandWithOutput("git", "log", "--oneline", "-5"); err == nil {
			log.Println(output)
		}
	}

	return nil
}

// runGitCommand executes a git command in the test repository
func (th *E2ETestHarness) runGitCommand(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = th.testRepoPath
	cmd.Env = append(os.Environ(), "GIT_PAGER=cat")

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
	cmd.Env = append(os.Environ(), "GIT_PAGER=cat")

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

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
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