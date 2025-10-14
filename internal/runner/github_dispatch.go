package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// GitHubDispatchPayload represents the payload sent to GitHub Actions
type GitHubDispatchPayload struct {
	EventType     string                 `json:"event_type"`
	ClientPayload map[string]interface{} `json:"client_payload"`
}

// SecretFile represents the structure of secret.yaml
type SecretFile struct {
	GitHubToken string `yaml:"github_token"`
}

// loadGitHubToken loads the GitHub token from the secret file
func loadGitHubToken(secretFile string) (string, error) {
	// If path is relative, make it absolute from current working directory
	if !filepath.IsAbs(secretFile) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
		secretFile = filepath.Join(cwd, secretFile)
	}

	data, err := os.ReadFile(secretFile)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file %s: %w", secretFile, err)
	}

	var secret SecretFile
	if err := yaml.Unmarshal(data, &secret); err != nil {
		return "", fmt.Errorf("failed to parse secret file: %w", err)
	}

	if secret.GitHubToken == "" {
		return "", fmt.Errorf("github_token not found in secret file")
	}

	return secret.GitHubToken, nil
}

// sendGitHubDispatch sends a repository dispatch event to GitHub
func sendGitHubDispatch(repoOwner, repoName, token, eventType string, clientPayload map[string]interface{}) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/dispatches", repoOwner, repoName)

	payload := GitHubDispatchPayload{
		EventType:     eventType,
		ClientPayload: clientPayload,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// notifyGitHubActions sends a notification to GitHub Actions via repository dispatch
func (tr *TestRunner) notifyGitHubActions(branch, commit string, success bool) error {
	config := tr.config.GitHubActionsDispatch

	// Parse repository owner and name from github_repo config first
	repoParts := strings.Split(config.GitHubRepo, "/")
	if len(repoParts) != 2 {
		return fmt.Errorf("invalid github_repo format, expected 'owner/repo', got '%s'", config.GitHubRepo)
	}

	// Load GitHub token from secret file
	token, err := loadGitHubToken(config.GitHubTokenFile)
	if err != nil {
		return fmt.Errorf("failed to load GitHub token: %w", err)
	}
	repoOwner := repoParts[0]
	repoName := repoParts[1]

	// Determine event type based on success and config
	eventType := config.DispatchType
	if eventType == "" {
		if success {
			eventType = "test-success"
		} else {
			eventType = "test-failure"
		}
	}

	// Create client payload similar to the shell script
	clientPayload := map[string]interface{}{
		"branch":    branch,
		"commit":    commit,
		"success":   success,
		"timestamp": fmt.Sprintf("%d", os.Getuid()), // Simple timestamp-like value
		"source":    "home-ci",
	}

	slog.Debug("Sending GitHub Actions dispatch",
		"repo", config.GitHubRepo,
		"event_type", eventType,
		"branch", branch,
		"commit", commit[:8],
		"success", success)

	err = sendGitHubDispatch(repoOwner, repoName, token, eventType, clientPayload)
	if err != nil {
		return fmt.Errorf("failed to send GitHub dispatch: %w", err)
	}

	slog.Info("GitHub Actions dispatch sent successfully",
		"repo", config.GitHubRepo,
		"event_type", eventType,
		"branch", branch,
		"commit", commit[:8])

	return nil
}