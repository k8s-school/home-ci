package config

import (
	"testing"
)

func TestExtractGitHubRepoFormat(t *testing.T) {
	tests := []struct {
		name     string
		repoPath string
		expected string
	}{
		{
			name:     "HTTPS GitHub URL with .git",
			repoPath: "https://github.com/k8s-school/home-ci.git",
			expected: "k8s-school/home-ci",
		},
		{
			name:     "HTTPS GitHub URL without .git",
			repoPath: "https://github.com/k8s-school/home-ci",
			expected: "k8s-school/home-ci",
		},
		{
			name:     "SSH GitHub URL with .git",
			repoPath: "git@github.com:k8s-school/home-ci.git",
			expected: "k8s-school/home-ci",
		},
		{
			name:     "SSH GitHub URL without .git",
			repoPath: "git@github.com:k8s-school/home-ci",
			expected: "k8s-school/home-ci",
		},
		{
			name:     "Non-GitHub URL",
			repoPath: "https://gitlab.com/user/repo.git",
			expected: "",
		},
		{
			name:     "Local path without git remote",
			repoPath: "/path/to/local/repo",
			expected: "",
		},
		{
			name:     "Empty string",
			repoPath: "",
			expected: "",
		},
		{
			name:     "Current directory with GitHub remote",
			repoPath: ".",
			expected: "k8s-school/home-ci", // This assumes the test is run in the home-ci repo
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGitHubRepoFormat(tt.repoPath)
			if result != tt.expected {
				t.Errorf("extractGitHubRepoFormat(%q) = %q, want %q", tt.repoPath, result, tt.expected)
			}
		})
	}
}

func TestConfigNormalizeGitHubRepoDefault(t *testing.T) {
	tests := []struct {
		name               string
		repository         string
		initialGitHubRepo  string
		expectedGitHubRepo string
	}{
		{
			name:               "GitHub repo defaults from repository",
			repository:         "https://github.com/k8s-school/home-ci.git",
			initialGitHubRepo:  "",
			expectedGitHubRepo: "k8s-school/home-ci",
		},
		{
			name:               "Existing GitHub repo is preserved",
			repository:         "https://github.com/k8s-school/home-ci.git",
			initialGitHubRepo:  "custom/repo",
			expectedGitHubRepo: "custom/repo",
		},
		{
			name:               "Non-GitHub repository leaves github_repo empty",
			repository:         "https://gitlab.com/user/repo.git",
			initialGitHubRepo:  "",
			expectedGitHubRepo: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				Repository: tt.repository,
				RepoName:   "test-repo",
				GitHubActionsDispatch: GitHubActionsDispatch{
					GitHubRepo: tt.initialGitHubRepo,
				},
			}

			err := config.Normalize()
			if err != nil {
				t.Fatalf("Normalize() failed: %v", err)
			}

			if config.GitHubActionsDispatch.GitHubRepo != tt.expectedGitHubRepo {
				t.Errorf("GitHubRepo = %q, want %q", config.GitHubActionsDispatch.GitHubRepo, tt.expectedGitHubRepo)
			}
		})
	}
}