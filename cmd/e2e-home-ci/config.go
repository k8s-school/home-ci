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

// createConfigFile creates configuration file from embedded resource
func (th *E2ETestHarness) createConfigFile() (string, error) {
	// Place config file in the run's temp directory
	configPath := filepath.Join(th.tempRunDir, "home-ci-config.yaml")

	var configContent string
	if th.testType == TestTimeout {
		configContent = resources.ConfigTimeout
		// Replace repo path in timeout config
		configContent = strings.ReplaceAll(configContent, "/tmp/test-repo-timeout", th.testRepoPath)
	} else if th.testType == TestDispatch {
		configContent = resources.ConfigDispatch
		// Replace repo path in dispatch config
		configContent = strings.ReplaceAll(configContent, "/tmp/test-repo-home-ci", th.testRepoPath)
	} else {
		configContent = resources.ConfigNormal
		// Replace repo path in normal config
		configContent = strings.ReplaceAll(configContent, "/tmp/test-repo-home-ci", th.testRepoPath)
	}

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return "", fmt.Errorf("failed to create config file: %w", err)
	}

	if th.testType != TestTimeout {
		log.Printf("✅ Configuration file created at %s", configPath)
	}
	return configPath, nil
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