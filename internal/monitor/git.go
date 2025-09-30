package monitor

import (
	"fmt"
	"os/exec"
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
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	return &GitRepository{
		repo:     repo,
		repoPath: repoPath,
	}, nil
}

func (gr *GitRepository) FetchRemote() error {
	cmd := exec.Command("git", "fetch", "origin")
	cmd.Dir = gr.repoPath
	return cmd.Run()
}

func (gr *GitRepository) GetRemoteBranches() ([]string, error) {
	remote, err := gr.repo.Remote("origin")
	if err != nil {
		return nil, err
	}

	refs, err := remote.List(&git.ListOptions{})
	if err != nil {
		return nil, err
	}

	var branches []string
	for _, ref := range refs {
		if ref.Name().IsBranch() {
			branchName := ref.Name().Short()
			if branchName != "HEAD" {
				branches = append(branches, branchName)
			}
		}
	}

	return branches, nil
}

func (gr *GitRepository) GetLatestCommitForBranch(branchName string, maxCommitAge time.Duration) (*object.Commit, error) {
	// Get the latest commit for this branch
	refName := fmt.Sprintf("refs/remotes/origin/%s", branchName)
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