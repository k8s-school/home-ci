package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	appconfig "github.com/k8s-school/home-ci/internal/config"
)

// TestGitRepository_FetchRemote tests the FetchRemote functionality
func TestGitRepository_FetchRemote(t *testing.T) {
	tests := []struct {
		name           string
		setupRepo      func(t *testing.T) (*GitRepository, error)
		expectError    bool
		errorContains  string
	}{
		{
			name: "valid_git_repo_with_remote",
			setupRepo: func(t *testing.T) (*GitRepository, error) {
				repoPath := setupValidGitRepo(t)
				return NewGitRepository(repoPath)
			},
			expectError: false,
		},
		{
			name: "git_repo_without_remote",
			setupRepo: func(t *testing.T) (*GitRepository, error) {
				repoPath := setupGitRepoWithoutRemote(t)
				return NewGitRepository(repoPath)
			},
			expectError: true,
			errorContains: "remote",
		},
		{
			name: "non_git_directory",
			setupRepo: func(t *testing.T) (*GitRepository, error) {
				tempDir := t.TempDir()
				return NewGitRepository(tempDir)
			},
			expectError: true,
			errorContains: "repository does not exist",
		},
		{
			name: "invalid_remote_url",
			setupRepo: func(t *testing.T) (*GitRepository, error) {
				repoPath := setupGitRepoWithInvalidRemote(t)
				return NewGitRepository(repoPath)
			},
			expectError: true,
			errorContains: "failed to fetch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitRepo, setupErr := tt.setupRepo(t)

			// If we expect an error and the setup itself failed, that might be the expected error
			if tt.expectError && setupErr != nil {
				if tt.errorContains != "" && !strings.Contains(setupErr.Error(), tt.errorContains) {
					t.Errorf("Expected setup error to contain '%s', got: %v", tt.errorContains, setupErr)
				} else {
					t.Logf("Expected setup error occurred: %v", setupErr)
				}
				return
			}

			if setupErr != nil {
				t.Fatalf("Unexpected setup error: %v", setupErr)
			}

			err := gitRepo.FetchRemote()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorContains, err)
				}
				t.Logf("Expected error occurred: %v", err)
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestGitRepository_FetchRemote_NetworkError tests network-related errors with go-git
func TestGitRepository_FetchRemote_NetworkError(t *testing.T) {
	// Create a repo with an unreachable remote to test error handling
	repoPath := setupGitRepoWithInvalidRemote(t)
	gitRepo, err := NewGitRepository(repoPath)
	if err != nil {
		t.Fatalf("Failed to create git repository: %v", err)
	}

	err = gitRepo.FetchRemote()

	if err == nil {
		t.Error("Expected error for invalid remote, but got none")
		return
	}

	t.Logf("Network error (as expected): %v", err)

	// With go-git, we should get more descriptive errors instead of exit status 255
	if strings.Contains(err.Error(), "failed to fetch") {
		t.Logf("✓ Got expected 'failed to fetch' error instead of exit status 255")
	}
}

// TestGitRepository_FetchRemote_Timeout tests fetch with timeout scenarios
func TestGitRepository_FetchRemote_Timeout(t *testing.T) {
	// Create a repo with an unreachable remote that might timeout
	repoPath := setupGitRepoWithInvalidRemote(t)
	gitRepo, err := NewGitRepository(repoPath)
	if err != nil {
		t.Fatalf("Failed to create git repository: %v", err)
	}

	// Test with timeout to ensure we don't hang indefinitely
	done := make(chan error, 1)
	go func() {
		done <- gitRepo.FetchRemote()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Expected error for invalid remote, but got none")
		} else {
			t.Logf("Fetch failed as expected: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("FetchRemote timed out - this indicates a hanging git fetch")
	}
}

// Helper functions for test setup

func setupValidGitRepo(t *testing.T) string {
	tempDir := t.TempDir()

	// Initialize git repo using go-git
	repo, err := git.PlainInit(tempDir, false)
	if err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Create an initial commit
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Add and commit using go-git
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Failed to get worktree: %v", err)
	}

	_, err = worktree.Add("test.txt")
	if err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}

	_, err = worktree.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Add a valid remote
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/k8s-school/home-ci.git"},
	})
	if err != nil {
		t.Fatalf("Failed to add remote: %v", err)
	}

	return tempDir
}

func setupGitRepoWithoutRemote(t *testing.T) string {
	tempDir := t.TempDir()

	// Initialize git repo using go-git
	_, err := git.PlainInit(tempDir, false)
	if err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	return tempDir
}

func setupGitRepoWithInvalidRemote(t *testing.T) string {
	tempDir := t.TempDir()

	// Initialize git repo using go-git
	repo, err := git.PlainInit(tempDir, false)
	if err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Add an invalid remote
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://nonexistent-git-host-12345.com/repo.git"},
	})
	if err != nil {
		t.Fatalf("Failed to add remote: %v", err)
	}

	return tempDir
}

// TestFetchRemoteIfEnabled tests the monitor's fetchRemoteIfEnabled method
func TestMonitor_FetchRemoteIfEnabled(t *testing.T) {
	tests := []struct {
		name         string
		fetchRemote  bool
		expectError  bool
		setupGitRepo func(t *testing.T) (*GitRepository, error)
	}{
		{
			name:        "fetch_disabled",
			fetchRemote: false,
			expectError: false,
			setupGitRepo: func(t *testing.T) (*GitRepository, error) {
				// For fetch disabled, even a non-existent repo should work
				return &GitRepository{repoPath: "/nonexistent"}, nil
			},
		},
		{
			name:        "fetch_enabled_valid_repo",
			fetchRemote: true,
			expectError: false,
			setupGitRepo: func(t *testing.T) (*GitRepository, error) {
				repoPath := setupValidGitRepo(t)
				return NewGitRepository(repoPath)
			},
		},
		{
			name:        "fetch_enabled_invalid_repo",
			fetchRemote: true,
			expectError: true,
			setupGitRepo: func(t *testing.T) (*GitRepository, error) {
				repoPath := setupGitRepoWithInvalidRemote(t)
				return NewGitRepository(repoPath)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitRepo, setupErr := tt.setupGitRepo(t)

			if setupErr != nil && tt.expectError {
				t.Logf("Expected setup error occurred: %v", setupErr)
				return
			}

			if setupErr != nil {
				t.Fatalf("Unexpected setup error: %v", setupErr)
			}

			// Create a config with the desired FetchRemote setting
			cfg := appconfig.Config{
				FetchRemote: tt.fetchRemote,
			}

			monitor := &Monitor{
				config:  cfg,
				gitRepo: gitRepo,
			}

			err := monitor.fetchRemoteIfEnabled()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else {
					t.Logf("Expected error occurred: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

