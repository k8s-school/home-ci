package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type GitHubActionsDispatch struct {
	Enabled         bool   `yaml:"enabled"`
	GitHubRepo      string `yaml:"github_repo"`
	GitHubTokenFile string `yaml:"github_token_file"`
	DispatchType    string `yaml:"dispatch_type"`
	HasResultFile   bool   `yaml:"has_result_file"`
}

type Cleanup struct {
	AfterE2E bool   `yaml:"after_e2e"`
	Script   string `yaml:"script"`
}

type Config struct {
	// Repository configuration
	Repository string `yaml:"repository"` // Git repository URL or path
	RepoName   string `yaml:"repo_name"`  // Repository name for organization

	// Directory structure
	WorkDir string `yaml:"work_dir"` // Base working directory - all paths calculated from this

	// Test configuration
	CheckInterval         time.Duration         `yaml:"check_interval"`
	TestScript            string                `yaml:"test_script"`
	MaxConcurrentRuns     int                   `yaml:"max_concurrent_runs"`
	Options               string                `yaml:"options"`
	RecentCommitsWithin   time.Duration         `yaml:"recent_commits_within"`
	TestTimeout           time.Duration         `yaml:"test_timeout"`
	KeepTime              time.Duration         `yaml:"keep_time"`
	Cleanup               Cleanup               `yaml:"cleanup"`
	GitHubActionsDispatch GitHubActionsDispatch `yaml:"github_actions_dispatch"`
}

func Load(path string) (Config, error) {
	// Default config
	config := Config{
		// Repository configuration
		Repository: "",
		RepoName:   "",

		// Directory structure
		WorkDir: "/tmp/home-ci",

		// Test configuration
		CheckInterval:       5 * time.Minute,
		TestScript:          "e2e/run.sh",
		MaxConcurrentRuns:   2,
		Options:             "-c -i ztf",
		RecentCommitsWithin: 240 * time.Hour,  // 10 days
		TestTimeout:         30 * time.Minute, // 30 minutes default timeout
		KeepTime:            0,                // By default, delete repositories immediately after tests
		Cleanup: Cleanup{
			AfterE2E: true,
			Script:   "",
		},
		GitHubActionsDispatch: GitHubActionsDispatch{
			Enabled:         false,
			GitHubRepo:      "",
			GitHubTokenFile: "",
			DispatchType:    "",
			HasResultFile:   false,
		},
	}

	if path == "" {
		return config, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("cannot read configuration file '%s': %w", path, err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("cannot parse configuration file '%s': %w", path, err)
	}

	// Normalize and validate configuration
	if err := config.Normalize(); err != nil {
		return config, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// Normalize validates and normalizes the configuration
func (c *Config) Normalize() error {
	// Extract repository name from repository if not explicitly set
	if c.RepoName == "" {
		if c.Repository != "" {
			c.RepoName = extractRepoName(c.Repository)
		} else {
			return fmt.Errorf("repository must be specified")
		}
	}

	// Set default github_repo from repository if not explicitly set
	if c.GitHubActionsDispatch.GitHubRepo == "" && c.Repository != "" {
		c.GitHubActionsDispatch.GitHubRepo = extractGitHubRepoFormat(c.Repository)
	}

	// Validate work directory
	if !isDirWritable(c.WorkDir) {
		c.WorkDir = filepath.Join(os.TempDir(), "home-ci")
	}

	return nil
}

// GetCacheDir returns the cache directory path
func (c *Config) GetCacheDir() string {
	return filepath.Join(c.WorkDir, "cache")
}

// GetStateDir returns the state directory path
func (c *Config) GetStateDir() string {
	return filepath.Join(c.WorkDir, "state")
}

// GetWorkspaceDir returns the workspace directory for a specific run
func (c *Config) GetWorkspaceDir(branch, commit string) string {
	runID := c.createRunID(branch, commit)
	return filepath.Join(c.WorkDir, c.RepoName, runID)
}

// GetLogsDir returns the logs directory for a specific run
func (c *Config) GetLogsDir(branch, commit string) string {
	runID := c.createRunID(branch, commit)
	return filepath.Join(c.WorkDir, c.RepoName, runID, "logs")
}

// GetProjectDir returns the project source directory for a specific run
func (c *Config) GetProjectDir(branch, commit string) string {
	runID := c.createRunID(branch, commit)
	return filepath.Join(c.WorkDir, c.RepoName, runID, "src", c.RepoName)
}

// createRunID creates a run identifier from branch and commit
func (c *Config) createRunID(branch, commit string) string {
	// Clean branch name (remove slashes, etc.)
	branchClean := strings.ReplaceAll(branch, "/", "_")
	branchClean = strings.ReplaceAll(branchClean, "\\", "_")

	// Use short commit hash (first 8 chars)
	commitShort := commit
	if len(commit) > 8 {
		commitShort = commit[:8]
	}

	return fmt.Sprintf("%s_%s", branchClean, commitShort)
}

// extractRepoName extracts the repository name from a Git URL or local path
func extractRepoName(repoPath string) string {
	// Handle various Git URL formats:
	// https://github.com/user/repo.git -> repo
	// git@github.com:user/repo.git -> repo
	// /path/to/repo -> repo
	name := filepath.Base(repoPath)
	name = strings.TrimSuffix(name, ".git")
	name = strings.TrimSuffix(name, "/")

	if name == "" || name == "." {
		return "project"
	}

	return name
}

// extractGitHubRepoFormat extracts the GitHub owner/repo format from a Git URL or local path
func extractGitHubRepoFormat(repoPath string) string {
	// Handle various Git URL formats:
	// https://github.com/owner/repo.git -> owner/repo
	// git@github.com:owner/repo.git -> owner/repo
	// For local paths (like "."), try to get GitHub URL from git remote
	// For non-GitHub URLs, return empty string

	// If it's empty, return empty
	if repoPath == "" {
		return ""
	}

	// If it's a local path (like "." or relative/absolute paths without protocol),
	// try to get the remote URL from git
	if !strings.Contains(repoPath, "://") && !strings.Contains(repoPath, "@") {
		if gitRemoteURL := getGitRemoteURL(repoPath); gitRemoteURL != "" {
			return extractGitHubRepoFormat(gitRemoteURL)
		}
		return ""
	}

	// Normalize the path
	path := strings.TrimSuffix(repoPath, ".git")
	path = strings.TrimSuffix(path, "/")

	// Handle SSH format: git@github.com:owner/repo
	if strings.Contains(path, "git@github.com:") {
		parts := strings.Split(path, ":")
		if len(parts) >= 2 {
			return parts[1]
		}
	}

	// Handle HTTPS format: https://github.com/owner/repo
	if strings.Contains(path, "github.com/") {
		parts := strings.Split(path, "github.com/")
		if len(parts) >= 2 {
			return parts[1]
		}
	}

	// Return empty string for non-GitHub repositories
	return ""
}

// getGitRemoteURL tries to get the origin remote URL for a local git repository
func getGitRemoteURL(repoPath string) string {
	// Try to get the remote URL using git command
	cmd := exec.Command("git", "remote", "get-url", "origin")
	if repoPath != "" && repoPath != "." {
		cmd.Dir = repoPath
	}

	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

// isDirWritable checks if a directory exists and is writable, or if parent exists and is writable
func isDirWritable(path string) bool {
	// Check if directory exists
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		// Test write permission
		testFile := filepath.Join(path, ".home-ci-write-test")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err == nil {
			os.Remove(testFile)
			return true
		}
	}

	// Check if parent directory exists and is writable
	parent := filepath.Dir(path)
	if parent != path { // Avoid infinite recursion
		return isDirWritable(parent)
	}

	return false
}
