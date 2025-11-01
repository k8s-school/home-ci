package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// TestGitRepository_GetBranches_CacheBehavior tests the cache behavior for fetchRemote
func TestGitRepository_GetBranches_CacheBehavior(t *testing.T) {
	tests := []struct {
		name         string
		fetchRemote  bool
		setupRepo    func(t *testing.T) (*GitRepository, error)
		expectedLen  int
		expectError  bool
	}{
		{
			name:        "fetchRemote_false_returns_local_only",
			fetchRemote: false,
			setupRepo: func(t *testing.T) (*GitRepository, error) {
				repoPath := setupRepoWithLocalAndRemoteBranches(t)
				return NewGitRepository(repoPath)
			},
			expectedLen: 2, // main + feature-branch (local branches only)
			expectError: false,
		},
		{
			name:        "fetchRemote_true_returns_remote_with_recent_commits",
			fetchRemote: true,
			setupRepo: func(t *testing.T) (*GitRepository, error) {
				repoPath := setupRepoWithLocalAndRemoteBranches(t)
				return NewGitRepository(repoPath)
			},
			expectedLen: 2, // Remote branches with recent commits (main + remote-feature)
			expectError: false,
		},
		{
			name:        "fetchRemote_true_no_origin_returns_empty",
			fetchRemote: true,
			setupRepo: func(t *testing.T) (*GitRepository, error) {
				repoPath := setupGitRepoWithoutRemote(t)
				return NewGitRepository(repoPath)
			},
			expectedLen: 0, // No origin remote = no branches
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitRepo, err := tt.setupRepo(t)
			if err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			maxCommitAge := 240 * time.Hour // 10 days
			branches, err := gitRepo.GetBranches(tt.fetchRemote, maxCommitAge)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(branches) != tt.expectedLen {
				t.Errorf("Expected %d branches, got %d: %v", tt.expectedLen, len(branches), branches)
			}

			t.Logf("Branches returned: %v", branches)
		})
	}
}

// TestGitRepository_GetLatestCommitForRemoteBranch tests remote branch commit checking
func TestGitRepository_GetLatestCommitForRemoteBranch(t *testing.T) {
	repoPath := setupRepoWithLocalAndRemoteBranches(t)
	gitRepo, err := NewGitRepository(repoPath)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	tests := []struct {
		name         string
		branchName   string
		maxCommitAge time.Duration
		expectCommit bool
		expectError  bool
	}{
		{
			name:         "recent_commit_within_age_limit",
			branchName:   "main",
			maxCommitAge: 240 * time.Hour, // 10 days
			expectCommit: true,
			expectError:  false,
		},
		{
			name:         "recent_commit_outside_age_limit",
			branchName:   "main",
			maxCommitAge: 1 * time.Hour, // 1 hour (commit is older)
			expectCommit: false,
			expectError:  false,
		},
		{
			name:         "nonexistent_branch",
			branchName:   "nonexistent",
			maxCommitAge: 240 * time.Hour,
			expectCommit: false,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commit, err := gitRepo.GetLatestCommitForRemoteBranch(tt.branchName, tt.maxCommitAge)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.expectCommit && commit == nil {
				t.Error("Expected commit but got nil")
			}

			if !tt.expectCommit && commit != nil {
				t.Errorf("Expected no commit but got: %s", commit.Hash.String()[:8])
			}

			if commit != nil {
				t.Logf("Found commit: %s (age: %v)", commit.Hash.String()[:8], time.Since(commit.Author.When).Truncate(time.Hour))
			}
		})
	}
}

// TestGitRepository_CheckoutRemoteBranchAsLocal tests remote branch checkout functionality
func TestGitRepository_CheckoutRemoteBranchAsLocal(t *testing.T) {
	repoPath := setupRepoWithLocalAndRemoteBranches(t)
	gitRepo, err := NewGitRepository(repoPath)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	tests := []struct {
		name        string
		branchName  string
		expectError bool
	}{
		{
			name:        "checkout_existing_remote_branch",
			branchName:  "remote-feature",
			expectError: false,
		},
		{
			name:        "checkout_nonexistent_remote_branch",
			branchName:  "nonexistent",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gitRepo.CheckoutRemoteBranchAsLocal(tt.branchName)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Verify the local branch was created
			localBranches, err := gitRepo.GetLocalBranches()
			if err != nil {
				t.Fatalf("Failed to get local branches: %v", err)
			}

			found := false
			for _, branch := range localBranches {
				if branch == tt.branchName {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Local branch %s was not created", tt.branchName)
			}

			t.Logf("Successfully created local tracking branch: %s", tt.branchName)
		})
	}
}

// Helper function to setup a repo with both local and remote branches
func setupRepoWithLocalAndRemoteBranches(t *testing.T) string {
	tempDir := t.TempDir()

	// Initialize git repo
	repo, err := git.PlainInit(tempDir, false)
	if err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Failed to get worktree: %v", err)
	}

	// Create initial commit on main
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err = worktree.Add("test.txt")
	if err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}

	mainCommit, err := worktree.Commit("Initial commit on main", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Now().Add(-1 * time.Hour), // 1 hour ago
		},
	})
	if err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Create a local feature branch
	featureBranchRef := "refs/heads/feature-branch"
	err = repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(featureBranchRef), mainCommit))
	if err != nil {
		t.Fatalf("Failed to create feature branch: %v", err)
	}

	// Add origin remote
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/k8s-school/home-ci.git"},
	})
	if err != nil {
		t.Fatalf("Failed to add remote: %v", err)
	}

	// Create remote branches (simulate fetched branches)
	remoteMainRef := "refs/remotes/origin/main"
	err = repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(remoteMainRef), mainCommit))
	if err != nil {
		t.Fatalf("Failed to create remote main branch: %v", err)
	}

	// Create another commit for remote feature branch
	if err := os.WriteFile(testFile, []byte("remote feature content"), 0644); err != nil {
		t.Fatalf("Failed to update test file: %v", err)
	}

	_, err = worktree.Add("test.txt")
	if err != nil {
		t.Fatalf("Failed to add updated file: %v", err)
	}

	remoteFeatureCommit, err := worktree.Commit("Remote feature commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Now().Add(-30 * time.Minute), // 30 minutes ago
		},
	})
	if err != nil {
		t.Fatalf("Failed to commit remote feature: %v", err)
	}

	remoteFeatureRef := "refs/remotes/origin/remote-feature"
	err = repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(remoteFeatureRef), remoteFeatureCommit))
	if err != nil {
		t.Fatalf("Failed to create remote feature branch: %v", err)
	}

	return tempDir
}

// setupGitRepoWithoutRemote is defined in git_test.go