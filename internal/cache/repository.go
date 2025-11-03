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
	// Check if the origin is a local path (not a remote URL)
	if isLocalPath(rc.RepoOrigin) {
		// For local repositories, create bare clone directly
		_, err := git.PlainClone(rc.cachePath, true, &git.CloneOptions{
			URL:      rc.RepoOrigin,
			Progress: os.Stdout,
		})
		if err != nil {
			return fmt.Errorf("failed to clone local repository %s to cache %s: %w", rc.RepoOrigin, rc.cachePath, err)
		}
	} else {
		// For remote repositories, create bare clone with proper remotes
		_, err := git.PlainClone(rc.cachePath, true, &git.CloneOptions{
			URL:      rc.RepoOrigin,
			Progress: os.Stdout,
		})
		if err != nil {
			return fmt.Errorf("failed to clone repository %s to cache %s: %w", rc.RepoOrigin, rc.cachePath, err)
		}
	}

	slog.Info("Repository cache created", "repo", rc.RepoName, "origin", rc.RepoOrigin, "cache", rc.cachePath)

	// For remote repositories, ensure local branches exist for all remote branches
	if !isLocalPath(rc.RepoOrigin) {
		// Open the newly created repository to create local branches
		fs := osfs.New(rc.cachePath)
		storer := filesystem.NewStorage(fs, nil)
		repo, err := git.Open(storer, fs)
		if err != nil {
			return fmt.Errorf("failed to open newly created cache repository %s: %w", rc.cachePath, err)
		}

		if err := rc.createLocalBranches(repo); err != nil {
			return fmt.Errorf("failed to create local branches: %w", err)
		}
	}

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

	// Only fetch if origin remote exists and this is not a local repository
	if hasOrigin && !isLocalPath(rc.RepoOrigin) {
		err = repo.Fetch(&git.FetchOptions{
			RemoteName: "origin",
			RefSpecs: []config.RefSpec{
				config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
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
	} else if isLocalPath(rc.RepoOrigin) {
		slog.Debug("Skipping remote fetch for local repository", "repo", rc.RepoName)
	} else {
		slog.Info("Repository cache updated", "repo", rc.RepoName)
	}

	// Ensure local branches exist for all remote branches
	if err := rc.createLocalBranches(repo); err != nil {
		return fmt.Errorf("failed to create local branches: %w", err)
	}

	return nil
}

// createLocalBranches creates local branches for all remote branches in the cache
func (rc *RepositoryCache) createLocalBranches(repo *git.Repository) error {
	// Get all remote references
	refs, err := repo.References()
	if err != nil {
		return fmt.Errorf("failed to get repository references: %w", err)
	}

	defer refs.Close()

	err = refs.ForEach(func(ref *plumbing.Reference) error {
		refName := ref.Name()

		// Process only remote branch references (refs/remotes/origin/*)
		if refName.IsRemote() && strings.HasPrefix(refName.String(), "refs/remotes/origin/") {
			branchName := strings.TrimPrefix(refName.String(), "refs/remotes/origin/")

			// Skip HEAD reference
			if branchName == "HEAD" {
				return nil
			}

			localBranchRef := plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branchName))

			// Check if local branch already exists
			_, err := repo.Reference(localBranchRef, true)
			if err == nil {
				// Local branch already exists, skip
				return nil
			}

			// Create local branch reference pointing to the same commit as remote branch
			newRef := plumbing.NewHashReference(localBranchRef, ref.Hash())
			err = repo.Storer.SetReference(newRef)
			if err != nil {
				slog.Debug("Failed to create local branch", "branch", branchName, "error", err)
				return nil // Continue with other branches
			}

			slog.Debug("Created local branch", "branch", branchName, "commit", ref.Hash().String()[:8])
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to iterate references: %w", err)
	}

	return nil
}

// CloneToWorkspace clones directly from origin to a workspace directory for a specific branch and commit
func (rc *RepositoryCache) CloneToWorkspace(workspaceDir, branch, commit string) error {
	// Ensure workspace directory exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory %s: %w", workspaceDir, err)
	}

	// Clone directly from origin to workspace
	repo, err := git.PlainClone(workspaceDir, false, &git.CloneOptions{
		URL: rc.RepoOrigin,
	})
	if err != nil {
		return fmt.Errorf("failed to clone from origin %s to workspace %s: %w", rc.RepoOrigin, workspaceDir, err)
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

	// Checkout branch first to avoid detached HEAD state
	cleanBranchName := strings.TrimPrefix(branch, "origin/")
	localBranchRef := plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", cleanBranchName))
	remoteBranchRef := plumbing.ReferenceName(fmt.Sprintf("refs/remotes/origin/%s", cleanBranchName))

	slog.Debug("Preparing to checkout branch", "branch", cleanBranchName, "commit", commit)

	// Check if local branch already exists
	_, err = repo.Reference(localBranchRef, true)
	localBranchExists := err == nil

	if localBranchExists {
		// Local branch exists, just checkout to it
		slog.Debug("Local branch exists, checking out", "branch", cleanBranchName)
		err = worktree.Checkout(&git.CheckoutOptions{
			Branch: localBranchRef,
		})
	} else {
		// Local branch doesn't exist, check if remote branch exists
		slog.Debug("Local branch doesn't exist, checking remote", "branch", cleanBranchName)
		_, err = repo.Reference(remoteBranchRef, true)
		if err == nil {
			// Remote branch exists, create local branch tracking it
			slog.Debug("Creating local branch from remote", "branch", cleanBranchName)

			// Get the remote branch reference to set up tracking
			remoteRef, err := repo.Reference(remoteBranchRef, true)
			if err != nil {
				return fmt.Errorf("failed to get remote branch reference: %w", err)
			}

			slog.Debug("Remote branch details",
				"remoteBranch", remoteBranchRef.String(),
				"remoteHash", remoteRef.Hash().String(),
				"targetCommit", commit)

			err = worktree.Checkout(&git.CheckoutOptions{
				Branch: localBranchRef,
				Create: true,
				Hash:   remoteRef.Hash(),
			})
			if err != nil {
				slog.Debug("Failed to create and checkout local branch with hash, trying without hash", "error", err)
				// Try without specifying hash, let git figure it out
				err = worktree.Checkout(&git.CheckoutOptions{
					Branch: localBranchRef,
					Create: true,
				})
				if err != nil {
					slog.Debug("Failed to create and checkout local branch without hash", "error", err)
				}
			}
		} else {
			// Neither local nor remote branch exists, fallback to commit checkout
			slog.Debug("No branch found, checking out commit directly", "commit", commit)
			err = worktree.Checkout(&git.CheckoutOptions{
				Hash: commitHash,
			})
		}
	}

	if err != nil {
		slog.Debug("Branch checkout failed, falling back to commit checkout", "branch", cleanBranchName, "error", err)
		// If branch checkout fails, fallback to direct commit checkout
		err = worktree.Checkout(&git.CheckoutOptions{
			Hash: commitHash,
		})
		if err != nil {
			return fmt.Errorf("failed to checkout commit %s: %w", commit, err)
		}
		slog.Debug("Fallback commit checkout succeeded", "commit", commit)
	} else {
		slog.Debug("Checkout succeeded", "branch", cleanBranchName)
	}

	// Verify that we ended up on the correct commit
	head, err := repo.Head()
	if err == nil {
		actualCommit := head.Hash().String()
		if actualCommit != commit {
			slog.Debug("Commit mismatch, attempting to checkout specific commit",
				"expectedCommit", commit,
				"actualCommit", actualCommit,
				"branch", cleanBranchName)

			// If we're not on the right commit, try to checkout the specific commit
			err = worktree.Checkout(&git.CheckoutOptions{
				Hash: commitHash,
			})
			if err != nil {
				slog.Warn("Failed to checkout specific commit", "commit", commit, "error", err)
			}
		}
	}

	// Final verification of repository state
	head, err = repo.Head()
	if err == nil {
		slog.Debug("Final repository state",
			"repoName", rc.RepoName,
			"targetBranch", cleanBranchName,
			"targetCommit", commit,
			"actualHead", head.Hash().String()[:8],
			"isOnBranch", head.Name().IsBranch())
	}

	return nil
}

// isLocalPath checks if a repository origin is a local file path rather than a remote URL
func isLocalPath(origin string) bool {
	// Check if it's an absolute path
	if filepath.IsAbs(origin) {
		return true
	}

	// Check if it's a relative path (starts with ./ or ../)
	if strings.HasPrefix(origin, "./") || strings.HasPrefix(origin, "../") {
		return true
	}

	// Check if it doesn't contain a protocol scheme (simple heuristic)
	if !strings.Contains(origin, "://") && !strings.Contains(origin, "@") {
		return true
	}

	return false
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