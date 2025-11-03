package cache

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
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-billy/v5/osfs"
)

// RepositoryCache manages a cached copy of a Git repository
type RepositoryCache struct {
	CacheDir   string // Base cache directory
	RepoName   string // Repository name
	RepoOrigin string // Repository origin URL
	cachePath  string // Full path to cached repository
}

// NewRepositoryCache creates a new repository cache manager
func NewRepositoryCache(cacheDir, repoName, repoOrigin string) *RepositoryCache {
	return &RepositoryCache{
		CacheDir:   cacheDir,
		RepoName:   repoName,
		RepoOrigin: repoOrigin,
		cachePath:  filepath.Join(cacheDir, repoName),
	}
}

// EnsureCache ensures the repository cache exists and is up to date
func (rc *RepositoryCache) EnsureCache() error {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(rc.CacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory %s: %w", rc.CacheDir, err)
	}

	// Check if cache already exists
	if _, err := os.Stat(rc.cachePath); os.IsNotExist(err) {
		slog.Debug("Creating new repository cache", "repo", rc.RepoName, "path", rc.cachePath)
		return rc.createCache()
	}

	slog.Debug("Updating existing repository cache", "repo", rc.RepoName, "path", rc.cachePath)
	return rc.updateCache()
}

// createCache creates a new bare clone of the repository
func (rc *RepositoryCache) createCache() error {
	// Create bare clone
	_, err := git.PlainClone(rc.cachePath, true, &git.CloneOptions{
		URL:      rc.RepoOrigin,
		Progress: os.Stdout,
	})
	if err != nil {
		return fmt.Errorf("failed to clone repository %s to cache %s: %w", rc.RepoOrigin, rc.cachePath, err)
	}

	slog.Info("Repository cache created", "repo", rc.RepoName, "origin", rc.RepoOrigin, "cache", rc.cachePath)
	return nil
}

// updateCache updates an existing repository cache
func (rc *RepositoryCache) updateCache() error {
	// Open the cached repository
	fs := osfs.New(rc.cachePath)
	storer := filesystem.NewStorage(fs, nil)

	repo, err := git.Open(storer, fs)
	if err != nil {
		return fmt.Errorf("failed to open cached repository %s: %w", rc.cachePath, err)
	}

	// Check if origin remote exists before fetching
	remotes, err := repo.Remotes()
	if err != nil {
		return fmt.Errorf("failed to get remotes for cached repository %s: %w", rc.cachePath, err)
	}

	hasOrigin := false
	for _, remote := range remotes {
		if remote.Config().Name == "origin" {
			hasOrigin = true
			break
		}
	}

	// Only fetch if origin remote exists
	if hasOrigin {
		err = repo.Fetch(&git.FetchOptions{
			RemoteName: "origin",
			RefSpecs: []config.RefSpec{
				config.RefSpec("+refs/heads/*:refs/heads/*"),
				config.RefSpec("+refs/tags/*:refs/tags/*"),
			},
			Progress: os.Stdout,
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return fmt.Errorf("failed to fetch updates for cached repository %s: %w", rc.cachePath, err)
		}
	}

	if err == git.NoErrAlreadyUpToDate {
		slog.Debug("Repository cache is already up to date", "repo", rc.RepoName)
	} else {
		slog.Info("Repository cache updated", "repo", rc.RepoName)
	}

	return nil
}

// CloneToWorkspace clones from cache to a workspace directory for a specific branch and commit
func (rc *RepositoryCache) CloneToWorkspace(workspaceDir, branch, commit string) error {
	// Ensure workspace directory exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory %s: %w", workspaceDir, err)
	}

	// Clone from cache to workspace
	repo, err := git.PlainClone(workspaceDir, false, &git.CloneOptions{
		URL: rc.cachePath,
	})
	if err != nil {
		return fmt.Errorf("failed to clone from cache %s to workspace %s: %w", rc.cachePath, workspaceDir, err)
	}

	// Checkout specific branch and commit
	if err := rc.checkoutBranchCommit(repo, branch, commit); err != nil {
		return fmt.Errorf("failed to checkout branch %s commit %s: %w", branch, commit, err)
	}

	slog.Debug("Repository cloned to workspace",
		"repo", rc.RepoName,
		"workspace", workspaceDir,
		"branch", branch,
		"commit", commit[:8])

	return nil
}

// checkoutBranchCommit checks out a specific branch and commit
func (rc *RepositoryCache) checkoutBranchCommit(repo *git.Repository, branch, commit string) error {
	// Get the worktree
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Parse commit hash
	commitHash := plumbing.NewHash(commit)

	// Checkout the specific commit
	err = worktree.Checkout(&git.CheckoutOptions{
		Hash: commitHash,
	})
	if err != nil {
		// If direct commit checkout fails, try to checkout branch first then commit
		cleanBranchName := strings.TrimPrefix(branch, "origin/")

		branchRef := plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", cleanBranchName))

		// Checkout branch first
		err = worktree.Checkout(&git.CheckoutOptions{
			Branch: branchRef,
			Create: true,
		})
		if err != nil {
			return fmt.Errorf("failed to checkout branch %s: %w", cleanBranchName, err)
		}

		// Then checkout the specific commit
		err = worktree.Checkout(&git.CheckoutOptions{
			Hash: commitHash,
		})
		if err != nil {
			return fmt.Errorf("failed to checkout commit %s: %w", commit, err)
		}
	}

	return nil
}

// GetCachePath returns the path to the cached repository
func (rc *RepositoryCache) GetCachePath() string {
	return rc.cachePath
}

// GetLastUpdateTime returns the last modification time of the cache
func (rc *RepositoryCache) GetLastUpdateTime() (time.Time, error) {
	info, err := os.Stat(rc.cachePath)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}