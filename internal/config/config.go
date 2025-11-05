package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type GitHubActionsDispatch struct {
	Enabled          bool   `yaml:"enabled"`
	GitHubRepo       string `yaml:"github_repo"`
	GitHubTokenFile  string `yaml:"github_token_file"`
	DispatchType     string `yaml:"dispatch_type"`
}

type Cleanup struct {
	AfterE2E bool   `yaml:"after_e2e"`
	Script   string `yaml:"script"`
}

type Config struct {
	// Repository configuration
	Repository             string                 `yaml:"repository"`  // Git repository URL or path
	RepoName               string                 `yaml:"repo_name"`   // Repository name for organization

	// Directory structure
	CacheDir               string                 `yaml:"cache_dir"`
	StateDir               string                 `yaml:"state_dir"`
	WorkspaceDir           string                 `yaml:"workspace_dir"`
	LogDir                 string                 `yaml:"log_dir"`

	// Test configuration
	CheckInterval          time.Duration          `yaml:"check_interval"`
	TestScript             string                 `yaml:"test_script"`
	MaxConcurrentRuns      int                    `yaml:"max_concurrent_runs"`
	Options                string                 `yaml:"options"`
	RecentCommitsWithin    time.Duration          `yaml:"recent_commits_within"`
	TestTimeout            time.Duration          `yaml:"test_timeout"`
	KeepTime               time.Duration          `yaml:"keep_time"`
	Cleanup                Cleanup                `yaml:"cleanup"`
	GitHubActionsDispatch  GitHubActionsDispatch  `yaml:"github_actions_dispatch"`
}

func Load(path string) (Config, error) {
	var config Config

	// Default config
	config = Config{
		// Repository configuration
		Repository:        "",
		RepoName:          "",

		// Directory structure with Linux FHS standard defaults
		CacheDir:          "/var/cache/home-ci",
		StateDir:          "/var/lib/home-ci/state",
		WorkspaceDir:      "/var/lib/home-ci/workspaces",
		LogDir:            "/var/log/home-ci",

		// Test configuration
		CheckInterval:     5 * time.Minute,
		TestScript:        "e2e/run.sh",
		MaxConcurrentRuns: 2,
		Options:           "-c -i ztf",
		RecentCommitsWithin: 240 * time.Hour, // 10 days
		TestTimeout:       30 * time.Minute, // 30 minutes default timeout
		KeepTime:          0,               // By default, delete repositories immediately after tests
		Cleanup: Cleanup{
			AfterE2E: true,
			Script:   "",
		},
		GitHubActionsDispatch: GitHubActionsDispatch{
			Enabled:         false,
			GitHubRepo:      "",
			GitHubTokenFile: "",
			DispatchType:    "",
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

	// For development/testing, fall back to local directories if standard paths don't exist
	if !isDirWritable(c.CacheDir) {
		c.CacheDir = filepath.Join(os.TempDir(), "home-ci", "cache")
	}
	if !isDirWritable(c.StateDir) {
		c.StateDir = filepath.Join(os.TempDir(), "home-ci", "state")
	}
	if !isDirWritable(c.WorkspaceDir) {
		c.WorkspaceDir = filepath.Join(os.TempDir(), "home-ci", "workspaces")
	}
	if !isDirWritable(c.LogDir) {
		c.LogDir = filepath.Join(os.TempDir(), "home-ci", "logs")
	}

	return nil
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