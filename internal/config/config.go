package config

import (
	"os"
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
	RepoPath               string                 `yaml:"repo_path"`
	CheckInterval          time.Duration          `yaml:"check_interval"`
	TestScript             string                 `yaml:"test_script"`
	MaxConcurrentRuns      int                    `yaml:"max_concurrent_runs"`
	Options                string                 `yaml:"options"`
	MaxCommitAge           time.Duration          `yaml:"max_commit_age"`
	TestTimeout            time.Duration          `yaml:"test_timeout"`
	FetchRemote            bool                   `yaml:"fetch_remote"`
	Cleanup                Cleanup                `yaml:"cleanup"`
	GitHubActionsDispatch  GitHubActionsDispatch  `yaml:"github_actions_dispatch"`
}

func Load(path string) (Config, error) {
	var config Config

	// Default config
	config = Config{
		RepoPath:          ".",
		CheckInterval:     5 * time.Minute,
		TestScript:        "./e2e/fink-ci.sh",
		MaxConcurrentRuns: 2,
		Options:           "-c -i ztf",
		MaxCommitAge:      240 * time.Hour, // 10 days
		TestTimeout:       30 * time.Minute, // 30 minutes default timeout
		FetchRemote:       true,            // By default, fetch from remote
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
		if os.IsNotExist(err) {
			return config, nil // Use defaults
		}
		return config, err
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return config, err
	}

	return config, nil
}