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
	RepoOrigin             string                 `yaml:"repo_origin"` // Git origin URL
	RepoName               string                 `yaml:"repo_name"`   // Repository name for organization
	RepoPath               string                 `yaml:"repo_path"`   // Legacy field, will be derived from RepoOrigin if empty

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
	MaxCommitAge           time.Duration          `yaml:"max_commit_age"`
	TestTimeout            time.Duration          `yaml:"test_timeout"`
	FetchRemote            bool                   `yaml:"fetch_remote"`
	KeepTime               time.Duration          `yaml:"keep_time"`
	Cleanup                Cleanup                `yaml:"cleanup"`
	GitHubActionsDispatch  GitHubActionsDispatch  `yaml:"github_actions_dispatch"`
}

func Load(path string) (Config, error) {
	var config Config

	// Default config
	config = Config{
		// Repository configuration
		RepoOrigin:        "",
		RepoName:          "",
		RepoPath:          ".", // Legacy fallback

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
		MaxCommitAge:      240 * time.Hour, // 10 days
		TestTimeout:       30 * time.Minute, // 30 minutes default timeout
		FetchRemote:       true,            // By default, fetch from remote
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

// Normalize validates and normalizes the configuration, handling backward compatibility
func (c *Config) Normalize() error {
	// Handle backward compatibility: if RepoOrigin is empty but RepoPath is set,
	// treat RepoPath as the origin (for existing configs)
	if c.RepoOrigin == "" && c.RepoPath != "" && c.RepoPath != "." {
		c.RepoOrigin = c.RepoPath
	}

	// Extract repository name from origin if not explicitly set
	if c.RepoName == "" {
		if c.RepoOrigin != "" {
			c.RepoName = extractRepoName(c.RepoOrigin)
		} else {
			return fmt.Errorf("either repo_name or repo_origin must be specified")
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