package monitor

import (
	"fmt"
	"time"

	"github.com/go-git/go-git/v5"
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

func (gr *GitRepository) GetBranches() ([]string, error) {
	return gr.GetLocalBranches()
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

func (gr *GitRepository) GetLatestCommitForBranch(branchName string, maxCommitAge time.Duration) (*object.Commit, error) {
	// Get the latest commit for this local branch
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
