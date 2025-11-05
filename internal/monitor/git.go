package monitor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type GitRepository struct {
	repo        *git.Repository
	repoPath    string
	isRemoteURL bool
	cacheDir    string // Directory for local cache of remote repos
}

func NewGitRepository(repoPath string, cacheBaseDir string) (*GitRepository, error) {
	isRemoteURL := strings.HasPrefix(repoPath, "http://") || strings.HasPrefix(repoPath, "https://")

	if isRemoteURL {
		// For remote URLs, clean up any existing cache on startup
		gr := &GitRepository{
			repo:        nil,
			repoPath:    repoPath,
			isRemoteURL: true,
			cacheDir:    "", // Will be set in cleanupCache based on cacheBaseDir
		}
		gr.cleanupCache(cacheBaseDir)
		return gr, nil
	}

	// For local paths, also use cache-based approach for consistency
	gr := &GitRepository{
		repo:        nil, // Will use cached repo instead
		repoPath:    repoPath,
		isRemoteURL: false,
		cacheDir:    "", // Will be set in cleanupCache based on cacheBaseDir
	}
	gr.cleanupCache(cacheBaseDir)
	return gr, nil
}

// GetPath returns the local path of the repository
func (gr *GitRepository) GetPath() string {
	return gr.repoPath
}

func (gr *GitRepository) GetBranches(recentCommitsWithin time.Duration) ([]string, error) {
	// Use the unified getRemoteBranchesWithRecentCommits method for all cases
	// This works for both remote URLs and local repositories using git.PlainClone
	return gr.getRemoteBranchesWithRecentCommits(recentCommitsWithin)
}

// getRemoteBranchesWithRecentCommits queries branches with recent commits using unified approach
func (gr *GitRepository) getRemoteBranchesWithRecentCommits(recentCommitsWithin time.Duration) ([]string, error) {
	cutoffTime := time.Now().Add(-recentCommitsWithin)
	slog.Debug("Filtering branches by commit recency", "repository", gr.repoPath, "cutoff_time", cutoffTime.Format("2006-01-02 15:04:05"), "recent_commits_within", recentCommitsWithin)

	// Use cached repository approach for both remote URLs and local repositories
	// git.PlainClone works with both remote URLs and local paths
	repo, err := gr.ensureCachedRepo()
	if err != nil {
		return nil, fmt.Errorf("failed to ensure cached repository: %w", err)
	}

	// Fetch latest updates
	err = gr.fetchRemoteUpdates(repo)
	if err != nil {
		slog.Debug("Failed to fetch remote updates", "error", err)
		// Continue with existing cache if fetch fails
	}

	// Check all remote branches for recent commits
	var branchesWithRecentCommits []string

	refs, err := repo.References()
	if err != nil {
		return nil, fmt.Errorf("failed to list references: %w", err)
	}

	err = refs.ForEach(func(ref *plumbing.Reference) error {
		var branchName string

		// For remote URLs: look for remote-tracking branches (refs/remotes/origin/*)
		// For local repos: look for local branches (refs/heads/*)
		if gr.isRemoteURL {
			// Only process remote branch references
			if !ref.Name().IsRemote() || !strings.HasPrefix(ref.Name().String(), "refs/remotes/origin/") {
				return nil
			}
			// Skip the HEAD reference
			if ref.Name().String() == "refs/remotes/origin/HEAD" {
				return nil
			}
			// Extract branch name
			branchName = strings.TrimPrefix(ref.Name().String(), "refs/remotes/origin/")
		} else {
			// For local repositories, process local branches
			if !ref.Name().IsBranch() {
				return nil
			}
			// Extract branch name
			branchName = ref.Name().Short()
		}

		// Check commit timestamp
		hasRecentCommit, err := gr.checkCachedBranchTimestamp(repo, ref, branchName, cutoffTime)
		if err != nil {
			slog.Debug("Failed to check commit timestamp", "branch", branchName, "error", err)
			return nil
		}

		if hasRecentCommit {
			branchesWithRecentCommits = append(branchesWithRecentCommits, branchName)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to process branches: %w", err)
	}

	slog.Debug("Remote branch filtering completed", "total_recent_branches", len(branchesWithRecentCommits), "recent_commits_within", recentCommitsWithin)
	return branchesWithRecentCommits, nil
}

// cleanupCache removes any existing cache directory for this repository
func (gr *GitRepository) cleanupCache(cacheBaseDir string) {
	if gr.cacheDir == "" {
		// Create cache directory path based on repository URL
		repoName := strings.ReplaceAll(strings.ReplaceAll(gr.repoPath, "/", "_"), ":", "_")
		gr.cacheDir = filepath.Join(cacheBaseDir, repoName)
	}

	if _, err := os.Stat(gr.cacheDir); err == nil {
		slog.Debug("Cleaning up existing cache on startup", "cache_dir", gr.cacheDir)
		if err := os.RemoveAll(gr.cacheDir); err != nil {
			slog.Debug("Failed to remove cache directory", "cache_dir", gr.cacheDir, "error", err)
		}
	}
}

// ensureCachedRepo creates or opens a cached repository for remote URL
func (gr *GitRepository) ensureCachedRepo() (*git.Repository, error) {
	if gr.cacheDir == "" {
		return nil, fmt.Errorf("cache directory not set - this should not happen")
	}

	// Check if cached repository already exists
	if _, err := os.Stat(filepath.Join(gr.cacheDir, ".git")); err == nil {
		// Open existing cached repository
		repo, err := git.PlainOpen(gr.cacheDir)
		if err != nil {
			// If opening fails, remove the corrupted cache and recreate
			slog.Debug("Failed to open cached repository, recreating", "cache_dir", gr.cacheDir, "error", err)
			os.RemoveAll(gr.cacheDir)
		} else {
			return repo, nil
		}
	}

	// Create cache directory
	if err := os.MkdirAll(gr.cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Clone repository with shallow depth for monitoring efficiency
	slog.Debug("Creating cached repository", "repository", gr.repoPath, "cache_dir", gr.cacheDir)
	repo, err := git.PlainClone(gr.cacheDir, false, &git.CloneOptions{
		URL:   gr.repoPath,
		Depth: 1, // Shallow clone for efficient monitoring
	})
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository to cache: %w", err)
	}

	return repo, nil
}

// fetchRemoteUpdates fetches latest updates for the cached repository
func (gr *GitRepository) fetchRemoteUpdates(repo *git.Repository) error {
	remote, err := repo.Remote("origin")
	if err != nil {
		return fmt.Errorf("failed to get origin remote: %w", err)
	}

	// Fetch all branches with shallow depth
	err = remote.Fetch(&git.FetchOptions{
		RefSpecs: []config.RefSpec{"refs/heads/*:refs/remotes/origin/*"},
		Depth:    1,    // Only get the latest commit for each branch
		Force:    true, // Force update in case of shallow history conflicts
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to fetch remote updates: %w", err)
	}

	return nil
}

// checkCachedBranchTimestamp checks if a branch in the cached repository has recent commits
func (gr *GitRepository) checkCachedBranchTimestamp(repo *git.Repository, ref *plumbing.Reference, branchName string, cutoffTime time.Time) (bool, error) {
	// Get the commit object
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return false, fmt.Errorf("failed to get commit object for branch %s: %w", branchName, err)
	}

	// Check if the commit is recent enough
	isRecent := commit.Author.When.After(cutoffTime)
	age := time.Since(commit.Author.When)

	if isRecent {
		slog.Debug("Branch has recent commit", "branch", branchName, "commit", commit.Hash.String()[:8], "age", age.Truncate(time.Hour), "commit_date", commit.Author.When.Format("2006-01-02 15:04:05"))
	} /* else {
		slog.Debug("Branch has old commit, excluding", "branch", branchName, "commit", commit.Hash.String()[:8], "age", age.Truncate(time.Hour), "commit_date", commit.Author.When.Format("2006-01-02 15:04:05"))
	} */

	return isRecent, nil
}

func (gr *GitRepository) GetLatestCommitForBranch(branchName string, recentCommitsWithin time.Duration) (*object.Commit, error) {
	// Use cached repository approach for all cases
	repo, err := gr.ensureCachedRepo()
	if err != nil {
		return nil, fmt.Errorf("failed to ensure cached repository: %w", err)
	}

	var refName string
	if gr.isRemoteURL {
		// For remote URLs, use remote tracking branches
		refName = fmt.Sprintf("refs/remotes/origin/%s", branchName)
	} else {
		// For local repositories, use local branches
		refName = fmt.Sprintf("refs/heads/%s", branchName)
	}

	ref, err := repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get reference for branch %s: %w", branchName, err)
	}

	// Get the commit object directly
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit object for branch %s: %w", branchName, err)
	}

	return commit, nil
}

