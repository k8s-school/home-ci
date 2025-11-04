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
	"github.com/go-git/go-git/v5/storage/memory"
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

func (gr *GitRepository) FetchRemote() error {
	// For remote URLs, this is not applicable
	if gr.isRemoteURL {
		return nil
	}

	// For local repositories with remotes
	remote, err := gr.repo.Remote("origin")
	if err != nil {
		return fmt.Errorf("failed to get remote 'origin': %w", err)
	}

	err = remote.Fetch(&git.FetchOptions{})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to fetch from remote: %w", err)
	}

	return nil
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

	// Clone repository with shallow depth for efficiency
	slog.Debug("Creating cached repository", "repository", gr.repoPath, "cache_dir", gr.cacheDir)
	repo, err := git.PlainClone(gr.cacheDir, false, &git.CloneOptions{
		URL:   gr.repoPath,
		Depth: 1, // Shallow clone for performance
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

// getLocalRemoteBranchesWithRecentCommits gets branches from local repository with remote tracking
func (gr *GitRepository) getLocalRemoteBranchesWithRecentCommits(recentCommitsWithin time.Duration) ([]string, error) {
	// Get remote branches from local repository
	remoteBranches, err := gr.GetRemoteBranches()
	if err != nil {
		return nil, err
	}

	var branchesWithRecentCommits []string

	for _, remoteBranch := range remoteBranches {
		// Check if remote branch has recent commits
		latestCommit, err := gr.GetLatestCommitForRemoteBranch(remoteBranch, recentCommitsWithin)
		if err != nil {
			slog.Debug("Failed to check commits for remote branch", "branch", remoteBranch, "error", err)
			continue
		}

		// Skip branches with no recent commits
		if latestCommit == nil {
			continue
		}

		branchesWithRecentCommits = append(branchesWithRecentCommits, remoteBranch)
	}

	return branchesWithRecentCommits, nil
}

func (gr *GitRepository) GetLocalBranches() ([]string, error) {
	refs, err := gr.repo.References()
	if err != nil {
		return nil, err
	}

	var branches []string
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() {
			branchName := ref.Name().Short()
			branches = append(branches, branchName)
		}
		return nil
	})

	return branches, err
}

func (gr *GitRepository) GetRemoteBranches() ([]string, error) {
	// Check if origin remote exists
	_, err := gr.repo.Remote("origin")
	if err != nil {
		return []string{}, nil // No origin remote, return empty list
	}

	refs, err := gr.repo.References()
	if err != nil {
		return nil, err
	}

	var branches []string
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsRemote() && ref.Name().String() != "refs/remotes/origin/HEAD" {
			// Extract branch name from refs/remotes/origin/branch-name
			fullName := ref.Name().Short() // gets "origin/branch-name"
			if len(fullName) > 7 && fullName[:7] == "origin/" {
				branchName := fullName[7:] // Just the branch name without origin/ prefix
				branches = append(branches, branchName)
			}
		}
		return nil
	})

	return branches, err
}

func (gr *GitRepository) CheckoutRemoteBranchAsLocal(branchName string) error {
	// Get the remote reference
	remoteRefName := plumbing.ReferenceName(fmt.Sprintf("refs/remotes/origin/%s", branchName))
	remoteRef, err := gr.repo.Reference(remoteRefName, true)
	if err != nil {
		return fmt.Errorf("failed to get remote reference for branch %s: %w", branchName, err)
	}

	// Create local branch reference
	localRefName := plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branchName))
	localRef := plumbing.NewHashReference(localRefName, remoteRef.Hash())

	// Create the local branch
	err = gr.repo.Storer.SetReference(localRef)
	if err != nil {
		return fmt.Errorf("failed to create local branch %s: %w", branchName, err)
	}

	// Set up tracking configuration
	cfg, err := gr.repo.Config()
	if err != nil {
		return fmt.Errorf("failed to get repository config: %w", err)
	}

	// Add branch configuration for tracking
	cfg.Branches[branchName] = &config.Branch{
		Name:   branchName,
		Remote: "origin",
		Merge:  plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branchName)),
	}

	// Save the updated configuration
	err = gr.repo.Storer.SetConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to save tracking configuration for branch %s: %w", branchName, err)
	}

	return nil
}

func (gr *GitRepository) GetLatestCommitForRemoteBranch(branchName string, recentCommitsWithin time.Duration) (*object.Commit, error) {
	// Get the latest commit for this remote branch
	refName := fmt.Sprintf("refs/remotes/origin/%s", branchName)
	ref, err := gr.repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get reference for remote branch %s: %w", branchName, err)
	}

	// Calculate the cutoff date
	cutoffDate := time.Now().Add(-recentCommitsWithin)

	// Get commits since the cutoff date for this branch
	logOptions := &git.LogOptions{
		From:  ref.Hash(),
		Since: &cutoffDate,
	}

	commitIter, err := gr.repo.Log(logOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit log for remote branch %s: %w", branchName, err)
	}
	defer commitIter.Close()

	// Find the most recent commit
	var latestCommit *object.Commit
	err = commitIter.ForEach(func(commit *object.Commit) error {
		if latestCommit == nil || commit.Author.When.After(latestCommit.Author.When) {
			latestCommit = commit
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to iterate commits for remote branch %s: %w", branchName, err)
	}

	return latestCommit, nil
}

func (gr *GitRepository) GetLatestCommitForBranch(branchName string, recentCommitsWithin time.Duration) (*object.Commit, error) {
	if gr.isRemoteURL {
		// For remote URLs, we need to get the HEAD commit information
		// Since we already filtered branches by recency in getRemoteBranchesWithRecentCommits,
		// we can return a synthetic commit object with the info we have
		return gr.getRemoteCommitInfo(branchName, recentCommitsWithin)
	}

	// Use cached repository approach for local repositories too
	repo, err := gr.ensureCachedRepo()
	if err != nil {
		return nil, fmt.Errorf("failed to ensure cached repository: %w", err)
	}

	// Get the latest commit for this local branch
	refName := fmt.Sprintf("refs/heads/%s", branchName)
	ref, err := repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get reference for branch %s: %w", branchName, err)
	}

	// Get the commit object directly (no need for complex log iteration)
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit object for branch %s: %w", branchName, err)
	}

	return commit, nil
}

// getRemoteCommitInfo gets commit information for a remote branch
func (gr *GitRepository) getRemoteCommitInfo(branchName string, recentCommitsWithin time.Duration) (*object.Commit, error) {
	// Create a remote to list references
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{gr.repoPath},
	})

	// List remote references to find the specific branch
	refs, err := rem.List(&git.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list remote references: %w", err)
	}

	// Find the specific branch
	var targetRef *plumbing.Reference
	branchRefName := plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branchName))

	for _, ref := range refs {
		if ref.Name() == branchRefName {
			targetRef = ref
			break
		}
	}

	if targetRef == nil {
		return nil, fmt.Errorf("branch %s not found in remote repository", branchName)
	}

	// Try to fetch the actual commit to get real commit info
	repo, err := git.Init(memory.NewStorage(), nil)
	if err != nil {
		// Fallback to synthetic commit if memory repo creation fails
		return gr.createSyntheticCommit(targetRef.Hash(), branchName), nil
	}

	// Create remote and fetch the specific commit
	remote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{gr.repoPath},
	})
	if err != nil {
		return gr.createSyntheticCommit(targetRef.Hash(), branchName), nil
	}

	// Fetch only the commit we need
	err = remote.Fetch(&git.FetchOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("%s:%s", targetRef.Name(), targetRef.Name())),
		},
		Depth: 1, // Shallow fetch for performance
	})
	if err != nil {
		slog.Debug("Failed to fetch commit, using synthetic commit", "branch", branchName, "error", err)
		return gr.createSyntheticCommit(targetRef.Hash(), branchName), nil
	}

	// Get the actual commit object
	commit, err := repo.CommitObject(targetRef.Hash())
	if err != nil {
		slog.Debug("Failed to get commit object, using synthetic commit", "branch", branchName, "error", err)
		return gr.createSyntheticCommit(targetRef.Hash(), branchName), nil
	}

	return commit, nil
}

// createSyntheticCommit creates a minimal commit object when real commit data isn't available
func (gr *GitRepository) createSyntheticCommit(hash plumbing.Hash, branchName string) *object.Commit {
	return &object.Commit{
		Hash: hash,
		Author: object.Signature{
			Name:  "Remote Commit",
			Email: "remote@example.com",
			When:  time.Now(), // We assume it's recent since it passed our filter
		},
		Committer: object.Signature{
			Name:  "Remote Commit",
			Email: "remote@example.com",
			When:  time.Now(),
		},
		Message: fmt.Sprintf("Remote commit %s on branch %s", hash.String()[:8], branchName),
	}
}
