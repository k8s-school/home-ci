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

// writeConfigFile writes a specific config file to /tmp/home-ci/e2e/
func (th *E2ETestHarness) writeConfigFile(configType, fileName, content string) error {
	configPath := filepath.Join("/tmp/home-ci/e2e", fileName)

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
	var configFileName string
	var configContent string
	var configType string

	if th.testType == TestTimeout {
		configFileName = "config-timeout.yaml"
		configContent = resources.ConfigTimeout
		configType = "Timeout"
	} else if th.testType == TestDispatch {
		configFileName = "config-dispatch.yaml"
		configContent = resources.ConfigDispatch
		configType = "Dispatch"
	} else {
		configFileName = "config-normal.yaml"
		configContent = resources.ConfigNormal
		configType = "Normal"
	}

	if err := th.writeConfigFile(configType, configFileName, configContent); err != nil {
		return "", err
	}

	configPath := filepath.Join("/tmp/home-ci/e2e", configFileName)
	return configPath, nil
}

// createAllConfigFiles creates all configuration files for init command
func (th *E2ETestHarness) createAllConfigFiles() error {
	configTypes := []struct {
		name     string
		fileName string
		content  string
	}{
		{"Normal", "config-normal.yaml", resources.ConfigNormal},
		{"Timeout", "config-timeout.yaml", resources.ConfigTimeout},
		{"Dispatch", "config-dispatch.yaml", resources.ConfigDispatch},
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