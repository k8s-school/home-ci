package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	RepoPath           string        `yaml:"repo_path"`
	CheckInterval      time.Duration `yaml:"check_interval"`
	TestScript         string        `yaml:"test_script"`
	MaxRunsPerDay      int           `yaml:"max_runs_per_day"`
	MaxConcurrentRuns  int           `yaml:"max_concurrent_runs"`
	Options            string        `yaml:"options"`
	MaxCommitAge       time.Duration `yaml:"max_commit_age"`
}

func Load(path string) (Config, error) {
	var config Config

	// Default config
	config = Config{
		RepoPath:          ".",
		CheckInterval:     5 * time.Minute,
		TestScript:        "./e2e/fink-ci.sh",
		MaxRunsPerDay:     1,
		MaxConcurrentRuns: 2,
		Options:           "-c -i ztf",
		MaxCommitAge:      240 * time.Hour, // 10 days
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