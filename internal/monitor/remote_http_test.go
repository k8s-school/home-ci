package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGitRepository_HTTPSRemoteAccess tests accessing remote repositories via HTTPS
func TestGitRepository_HTTPSRemoteAccess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "http_remote_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a test repository
	repoDir := filepath.Join(tempDir, "test-repo")
	_ = createBareTestRepository(t, repoDir)

	// Start HTTP server serving the repository
	server := createGitHTTPServer(t, repoDir)
	defer server.Close()

	repoURL := fmt.Sprintf("%s/test-repo.git", server.URL)

	t.Run("RemoteRepositoryDetection", func(t *testing.T) {
		gitRepo, err := NewGitRepository(repoURL, "/tmp")
		require.NoError(t, err)

		assert.True(t, gitRepo.isRemoteURL, "Should detect HTTPS URL as remote")
		assert.Equal(t, repoURL, gitRepo.repoPath)
		assert.Nil(t, gitRepo.repo, "Should not open local repository for remote URLs")
	})

	t.Run("GetBranchesFromRemote", func(t *testing.T) {
		gitRepo, err := NewGitRepository(repoURL, "/tmp")
		require.NoError(t, err)

		recentCommitsWithin := 24 * time.Hour
		branches, err := gitRepo.GetBranches(recentCommitsWithin)
		require.NoError(t, err)

		assert.Greater(t, len(branches), 0, "Should find at least one branch")
		// Check for either main or master branch (depends on git version/config)
		hasBranch := false
		for _, branch := range branches {
			if branch == "main" || branch == "master" {
				hasBranch = true
				break
			}
		}
		assert.True(t, hasBranch, "Should find main or master branch")
	})

	t.Run("GetLatestCommitForRemoteBranch", func(t *testing.T) {
		gitRepo, err := NewGitRepository(repoURL, "/tmp")
		require.NoError(t, err)

		recentCommitsWithin := 24 * time.Hour
		commit, err := gitRepo.GetLatestCommitForBranch("master", recentCommitsWithin)
		require.NoError(t, err)
		require.NotNil(t, commit)

		assert.NotEmpty(t, commit.Hash.String(), "Commit should have a hash")
		assert.Equal(t, "Test User", commit.Author.Name)
	})

	t.Run("NetworkErrorHandling", func(t *testing.T) {
		// Test with unreachable server
		unreachableURL := "http://localhost:99999/nonexistent-repo.git"
		gitRepo, err := NewGitRepository(unreachableURL, "/tmp")
		require.NoError(t, err) // Creation should succeed

		_, err = gitRepo.GetBranches(24 * time.Hour)
		assert.Error(t, err, "Should fail when repository is unreachable")
	})
}

// TestGitRepository_HTTPSNetworkErrors tests network error scenarios
func TestGitRepository_HTTPSNetworkErrors(t *testing.T) {
	t.Run("TimeoutHandling", func(t *testing.T) {
		// Test with an unreachable port on localhost (should fail quickly)
		timeoutURL := "http://localhost:99999/timeout-repo.git"
		gitRepo, err := NewGitRepository(timeoutURL, "/tmp")
		require.NoError(t, err)

		// This should fail due to connection refused
		start := time.Now()
		_, err = gitRepo.GetBranches(24 * time.Hour)
		duration := time.Since(start)

		// Expect failure due to connection error
		assert.Error(t, err, "Should fail due to connection error")
		t.Logf("Operation took: %v, Error: %v", duration, err)
	})

	t.Run("InvalidURLFormat", func(t *testing.T) {
		invalidURL := "http://not-a-valid-url-format"
		gitRepo, err := NewGitRepository(invalidURL, "/tmp")
		require.NoError(t, err) // Creation should succeed

		_, err = gitRepo.GetBranches(24 * time.Hour)
		assert.Error(t, err, "Should fail with invalid URL")
	})
}

// createBareTestRepository creates a bare git repository for HTTP serving
func createBareTestRepository(t *testing.T, repoPath string) *git.Repository {
	// First create a regular repository
	tempRepo, err := os.MkdirTemp("", "temp_repo_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempRepo)

	repo := createTestRepository(t, tempRepo)

	// Create bare repository by cloning
	_, err = git.PlainClone(repoPath, true, &git.CloneOptions{
		URL: tempRepo,
	})
	require.NoError(t, err)

	// Return the original repo for reference
	return repo
}

// createTestRepository creates a test repository with some commits
func createTestRepository(t *testing.T, repoPath string) *git.Repository {
	repo, err := git.PlainInit(repoPath, false)
	require.NoError(t, err)

	worktree, err := repo.Worktree()
	require.NoError(t, err)

	// Create initial commit
	readmePath := filepath.Join(repoPath, "README.md")
	err = os.WriteFile(readmePath, []byte("# Test Repository\n"), 0644)
	require.NoError(t, err)

	_, err = worktree.Add("README.md")
	require.NoError(t, err)

	_, err = worktree.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	return repo
}

// createGitHTTPServer creates a basic HTTP server that serves git repositories
func createGitHTTPServer(t *testing.T, repoPath string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Basic git HTTP protocol implementation
		switch {
		case strings.HasSuffix(r.URL.Path, "/info/refs"):
			if r.URL.Query().Get("service") == "git-upload-pack" {
				// Serve git-upload-pack advertisement
				cmd := exec.Command("git", "upload-pack", "--stateless-rpc", "--advertise-refs", repoPath)
				output, err := cmd.Output()
				if err != nil {
					t.Logf("Git upload-pack error: %v", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
				w.Header().Set("Cache-Control", "no-cache")
				w.WriteHeader(http.StatusOK)

				// Add git protocol prefix
				pktLine := fmt.Sprintf("001e# service=git-upload-pack\n0000")
				w.Write([]byte(pktLine))
				w.Write(output)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}

		case strings.HasSuffix(r.URL.Path, "/git-upload-pack"):
			// Handle git-upload-pack requests
			cmd := exec.Command("git", "upload-pack", "--stateless-rpc", repoPath)
			cmd.Stdin = r.Body

			output, err := cmd.Output()
			if err != nil {
				t.Logf("Git upload-pack RPC error: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			w.Write(output)

		default:
			t.Logf("Unhandled request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}