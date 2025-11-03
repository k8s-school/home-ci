package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/k8s-school/home-ci/resources"
	"gopkg.in/yaml.v3"
)

// writeConfigFile writes a specific config file to the test type directory
func (th *E2ETestHarness) writeConfigFile(configType, fileName, content string) error {
	// Create the test directory if it doesn't exist
	testDir := th.testType.getTestDirectory()
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return fmt.Errorf("failed to create test directory %s: %w", testDir, err)
	}

	configPath := filepath.Join(testDir, fileName)

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to create %s config file: %w", configType, err)
	}

	if th.testType != TestTimeout {
		log.Printf("✅ %s configuration file created at %s", configType, configPath)
	}
	return nil
}

// createConfigFile creates configuration file from embedded resource for current test type
func (th *E2ETestHarness) createConfigFile() (string, error) {
	configFileName, configContent, configType := th.getConfigForTestType()

	if err := th.writeConfigFile(configType, configFileName, configContent); err != nil {
		return "", err
	}

	configPath := filepath.Join(th.testType.getTestDirectory(), configFileName)

	// Initialize the repo name from the config file that was just created
	if err := th.initializeRepoName(configPath); err != nil {
		return "", fmt.Errorf("failed to initialize repo name: %w", err)
	}

	return configPath, nil
}

// initializeRepoName reads and caches the repo name from the config file
func (th *E2ETestHarness) initializeRepoName(configPath string) error {
	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	// Parse YAML to extract repo_name
	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse YAML config file %s: %w", configPath, err)
	}

	if repoName, ok := config["repo_name"].(string); ok && repoName != "" {
		th.repoName = repoName
		return nil
	}

	return fmt.Errorf("repo_name not found or empty in config file %s", configPath)
}


// getConfigForTestType returns config file name, content and type for the current test type
func (th *E2ETestHarness) getConfigForTestType() (string, string, string) {
	switch th.testType {
	case TestSuccess:
		return "config-success.yaml", resources.ConfigSuccess, "Success"
	case TestFail:
		return "config-fail.yaml", resources.ConfigFail, "Fail"
	case TestTimeout:
		return "config-timeout.yaml", resources.ConfigTimeout, "Timeout"
	case TestDispatchOneSuccess:
		return "config-dispatch-one-success.yaml", resources.ConfigDispatchOneSuccess, "Dispatch-One-Success"
	case TestDispatchNoTokenFile:
		return "config-dispatch-no-token-file.yaml", resources.ConfigDispatchNoTokenFile, "Dispatch-No-Token-File"
	case TestDispatchAll:
		return "config-dispatch-all.yaml", resources.ConfigDispatchAll, "Dispatch-All"
	case TestQuick:
		return "config-quick.yaml", resources.ConfigQuick, "Quick"
	case TestLong:
		return "config-long.yaml", resources.ConfigLong, "Long"
	case TestConcurrentLimit:
		return "config-concurrent-limit.yaml", resources.ConfigConcurrentLimit, "Concurrent-Limit"
	case TestContinuousCI:
		return "config-continuous-ci.yaml", resources.ConfigContinuousCI, "Continuous-CI"
	case TestCacheLocal:
		return "config-cache-local.yaml", th.getCacheLocalConfig(), "Cache-Local"
	case TestCacheRemote:
		return "config-cache-remote.yaml", th.getCacheRemoteConfig(), "Cache-Remote"
	default: // TestNormal
		return "config-normal.yaml", resources.ConfigNormal, "Normal"
	}
}

// createAllConfigFiles creates all configuration files for init command
func (th *E2ETestHarness) createAllConfigFiles() error {
	configTypes := []struct {
		name     string
		fileName string
		content  string
	}{
		{"Success", "config-success.yaml", resources.ConfigSuccess},
		{"Fail", "config-fail.yaml", resources.ConfigFail},
		{"Timeout", "config-timeout.yaml", resources.ConfigTimeout},
		{"Dispatch-One-Success", "config-dispatch-one-success.yaml", resources.ConfigDispatchOneSuccess},
		{"Dispatch-No-Token-File", "config-dispatch-no-token-file.yaml", resources.ConfigDispatchNoTokenFile},
		{"Dispatch-All", "config-dispatch-all.yaml", resources.ConfigDispatchAll},
		{"Quick", "config-quick.yaml", resources.ConfigQuick},
		{"Normal", "config-normal.yaml", resources.ConfigNormal},
		{"Long", "config-long.yaml", resources.ConfigLong},
		{"Concurrent-Limit", "config-concurrent-limit.yaml", resources.ConfigConcurrentLimit},
		{"Continuous-CI", "config-continuous-ci.yaml", resources.ConfigContinuousCI},
	}

	for _, config := range configTypes {
		if err := th.writeConfigFile(config.name, config.fileName, config.content); err != nil {
			return err
		}
	}

	return nil
}

// loadTestExpectations loads the test expectations configuration
func (th *E2ETestHarness) loadTestExpectations() (*TestExpectationConfig, error) {
	var config TestExpectationConfig

	if err := yaml.Unmarshal([]byte(resources.TestExpectations), &config); err != nil {
		return nil, fmt.Errorf("failed to parse test expectations: %w", err)
	}

	return &config, nil
}

// getExpectedResult determines what result is expected for a given branch and commit
func (th *E2ETestHarness) getExpectedResult(config *TestExpectationConfig, branch, commit, commitMessage string) string {
	// Check global commit patterns first (highest priority)
	for _, pattern := range config.GlobalScenarios.CommitPatterns {
		if matched, _ := filepath.Match(pattern.Pattern, commitMessage); matched {
			return pattern.ExpectedResult
		}
	}

	// Check branch-specific scenarios
	if branchConfig, exists := config.BranchScenarios[branch]; exists {
		// Check special cases for this branch
		for _, specialCase := range branchConfig.SpecialCases {
			if strings.HasPrefix(commit, specialCase.CommitHashPrefix) {
				return specialCase.ExpectedResult
			}
		}
		return branchConfig.DefaultResult
	}

	// Check wildcard patterns
	for branchPattern, branchConfig := range config.BranchScenarios {
		if strings.Contains(branchPattern, "*") {
			if matched, _ := filepath.Match(branchPattern, branch); matched {
				return branchConfig.DefaultResult
			}
		}
	}

	// Default to success if no pattern matches
	return "success"
}

// getCacheLocalConfig returns config for cache-local test (fetchRemote: false)
func (th *E2ETestHarness) getCacheLocalConfig() string {
	return `repo_path: ` + th.testRepoPath + `
check_interval: 5s
test_script: ./e2e/run-e2e.sh
max_concurrent_runs: 2
options: ""
max_commit_age: 240h
test_timeout: 30s
fetch_remote: false
keep_time: 0
cleanup:
  after_e2e: true
  script: ""
github_actions_dispatch:
  enabled: false
  github_repo: ""
  github_token_file: ""
  dispatch_type: ""
`
}

// getCacheRemoteConfig returns config for cache-remote test (fetchRemote: true)
func (th *E2ETestHarness) getCacheRemoteConfig() string {
	return `repo_path: ` + th.testRepoPath + `
check_interval: 5s
test_script: ./e2e/run-e2e.sh
max_concurrent_runs: 2
options: ""
max_commit_age: 240h
test_timeout: 30s
fetch_remote: true
keep_time: 0
cleanup:
  after_e2e: true
  script: ""
github_actions_dispatch:
  enabled: false
  github_repo: ""
  github_token_file: ""
  dispatch_type: ""
`
}