package monitor

import (
	"fmt"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type GitRepository struct {
	repo     *git.Repository
	repoPath string
}

func NewGitRepository(repoPath string) (*GitRepository, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("repository does not exist or is not a valid git repository at path '%s': %w", repoPath, err)
	}

	return &GitRepository{
		repo:     repo,
		repoPath: repoPath,
	}, nil
}

// GetPath returns the local path of the repository
func (gr *GitRepository) GetPath() string {
	return gr.repoPath
}

func (gr *GitRepository) FetchRemote() error {
	// Only used when fetch_remote is enabled
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

func (gr *GitRepository) GetBranches(fetchRemote bool, maxCommitAge time.Duration) ([]string, error) {
	// If fetchRemote=false, only return local branches
	if !fetchRemote {
		return gr.GetLocalBranches()
	}

	// If fetchRemote=true, local repo acts as cache - only checkout remote branches with recent commits
	remoteBranches, err := gr.GetRemoteBranches()
	if err != nil {
		return nil, err
	}

	var branchesToCheck []string

	// Check each remote branch for recent commits and checkout as local if needed
	for _, remoteBranch := range remoteBranches {
		// Check if remote branch has recent commits
		latestCommit, err := gr.GetLatestCommitForRemoteBranch(remoteBranch, maxCommitAge)
		if err != nil {
			fmt.Printf("Warning: failed to check commits for remote branch %s: %v\n", remoteBranch, err)
			continue
		}

		// Skip branches with no recent commits
		if latestCommit == nil {
			continue
		}

		// Check if local tracking branch already exists
		localRefName := fmt.Sprintf("refs/heads/%s", remoteBranch)
		_, err = gr.repo.Reference(plumbing.ReferenceName(localRefName), true)
		if err != nil {
			// Local branch doesn't exist, checkout remote as local
			if err := gr.CheckoutRemoteBranchAsLocal(remoteBranch); err != nil {
				fmt.Printf("Warning: failed to checkout remote branch %s as local: %v\n", remoteBranch, err)
				continue
			}
		}

		branchesToCheck = append(branchesToCheck, remoteBranch)
	}

	return branchesToCheck, nil
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

func (gr *GitRepository) GetLatestCommitForRemoteBranch(branchName string, maxCommitAge time.Duration) (*object.Commit, error) {
	// Get the latest commit for this remote branch
	refName := fmt.Sprintf("refs/remotes/origin/%s", branchName)
	ref, err := gr.repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get reference for remote branch %s: %w", branchName, err)
	}

	// Calculate the cutoff date
	cutoffDate := time.Now().Add(-maxCommitAge)

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

func (gr *GitRepository) GetLatestCommitForBranch(branchName string, maxCommitAge time.Duration) (*object.Commit, error) {
	// Get the latest commit for this local branch (remote branches are now checked out as local)
	refName := fmt.Sprintf("refs/heads/%s", branchName)
	ref, err := gr.repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get reference for branch %s: %w", branchName, err)
	}

	// Calculate the cutoff date
	cutoffDate := time.Now().Add(-maxCommitAge)

	// Get commits since the cutoff date for this branch
	logOptions := &git.LogOptions{
		From:  ref.Hash(),
		Since: &cutoffDate,
	}

	commitIter, err := gr.repo.Log(logOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit log for branch %s: %w", branchName, err)
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
		return nil, fmt.Errorf("failed to iterate commits for branch %s: %w", branchName, err)
	}

	return latestCommit, nil
}
