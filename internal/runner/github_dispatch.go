package runner

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	githubAPIVersion  = "2022-11-28"
	githubAcceptType  = "application/vnd.github+json"
	githubContentType = "application/json"
)

// GitHubDispatchPayload represents the payload sent to GitHub Actions
type GitHubDispatchPayload struct {
	EventType     string                 `json:"event_type"`
	ClientPayload map[string]interface{} `json:"client_payload"`
	Inputs        map[string]interface{} `json:"inputs,omitempty"`
}

// SecretFile represents the structure of secret.yaml
type SecretFile struct {
	GitHubToken string `yaml:"github_token"`
}

// Artifact represents a file artifact in the dispatch payload
type Artifact struct {
	Content string `json:"content"`
	Type    string `json:"type"`
}

// GitHubClient encapsulates GitHub API operations
type GitHubClient struct {
	httpClient *http.Client
	token      string
}

// NewGitHubClient creates a new GitHub client with the given token
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
	}
}

// loadGitHubToken loads the GitHub token from the secret file
func loadGitHubToken(secretFile, configDir string) (string, error) {
	var absolutePath string
	var err error

	// If secretFile is relative, resolve it relative to the config directory
	if !filepath.IsAbs(secretFile) && configDir != "" {
		absolutePath = filepath.Join(configDir, secretFile)
	} else {
		absolutePath, err = makeAbsolutePath(secretFile)
		if err != nil {
			return "", fmt.Errorf("failed to resolve secret file path: %w", err)
		}
	}

	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file %s: %w", absolutePath, err)
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

// makeAbsolutePath converts relative paths to absolute paths
func makeAbsolutePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	return filepath.Join(cwd, path), nil
}

// SendDispatch sends a repository dispatch event to GitHub
func (gc *GitHubClient) SendDispatch(repoOwner, repoName, eventType string, clientPayload map[string]interface{}) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/dispatches", repoOwner, repoName)

	payload := GitHubDispatchPayload{
		EventType:     eventType,
		ClientPayload: clientPayload,
		Inputs:        map[string]interface{}{}, // Keep empty as requested
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	gc.setHeaders(req)

	resp, err := gc.httpClient.Do(req)
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

// setHeaders sets the required headers for GitHub API requests
func (gc *GitHubClient) setHeaders(req *http.Request) {
	req.Header.Set("Accept", githubAcceptType)
	req.Header.Set("Authorization", "Bearer "+gc.token)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("Content-Type", githubContentType)
}

// readFileAsBase64 reads a file and returns its content as base64 encoded string
func readFileAsBase64(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// parseRepoString parses "owner/repo" format and returns owner and repo name
func parseRepoString(repoString string) (owner, name string, err error) {
	parts := strings.Split(repoString, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repository format, expected 'owner/repo', got '%s'", repoString)
	}
	return parts[0], parts[1], nil
}

// createArtifactsMap creates the artifacts map for the dispatch payload
func createArtifactsMap(branch, commit string, success bool, logFilePath, resultFilePath string) map[string]interface{} {
	artifacts := make(map[string]interface{})

	// Add log file artifact
	if logFilePath != "" {
		if content, err := readFileAsBase64(logFilePath); err == nil {
			fileName := filepath.Base(logFilePath)
			artifacts[fileName] = Artifact{
				Content: content,
				Type:    "log",
			}
			slog.Debug("Added log file to dispatch payload", "file", fileName, "size", len(content))
		} else {
			slog.Debug("Failed to read log file for dispatch", "file", logFilePath, "error", err)
		}
	}

	// Add result file artifact
	if resultFilePath != "" {
		if content, err := readFileAsBase64(resultFilePath); err == nil {
			fileName := filepath.Base(resultFilePath)
			artifacts[fileName] = Artifact{
				Content: content,
				Type:    "result",
			}
			slog.Debug("Added result file to dispatch payload", "file", fileName, "size", len(content))
		} else {
			slog.Debug("Failed to read result file for dispatch", "file", resultFilePath, "error", err)
		}
	}

	// Add metadata artifact
	artifacts["metadata"] = Artifact{
		Content: "", // Metadata doesn't need base64 content
		Type:    "metadata",
	}

	return artifacts
}

// createClientPayload creates the complete client payload for the dispatch
func createClientPayload(branch, commit string, success bool, logFilePath, resultFilePath string) map[string]interface{} {
	// Create artifact name with cleaned branch name and short commit
	branchClean := strings.ReplaceAll(branch, "/", "_")
	commitShort := commit
	if len(commit) > 8 {
		commitShort = commit[:8]
	}
	artifactName := fmt.Sprintf("log-%s-%s", branchClean, commitShort)

	return map[string]interface{}{
		"branch":        branch,
		"commit":        commit,
		"success":       success,
		"timestamp":     fmt.Sprintf("%d", time.Now().Unix()),
		"source":        "home-ci",
		"artifact_name": artifactName,
		"artifacts":     createArtifactsMap(branch, commit, success, logFilePath, resultFilePath),
		"metadata": map[string]interface{}{
			"branch":  branch,
			"commit":  commit,
			"success": success,
		},
	}
}

// determineEventType determines the event type based on configuration and success status
func determineEventType(configEventType string, success bool) string {
	if configEventType != "" {
		return configEventType
	}

	if success {
		return "test-success"
	}
	return "test-failure"
}

// notifyGitHubActions sends a notification to GitHub Actions via repository dispatch
func (tr *TestRunner) notifyGitHubActions(branch, commit string, success bool, logFilePath, resultFilePath string) error {
	config := tr.config.GitHubActionsDispatch

	// Parse repository owner and name
	repoOwner, repoName, err := parseRepoString(config.GitHubRepo)
	if err != nil {
		return err
	}

	// Get config directory from config path
	configDir := ""
	if tr.configPath != "" {
		configDir = filepath.Dir(tr.configPath)
	}

	// Load GitHub token
	token, err := loadGitHubToken(config.GitHubTokenFile, configDir)
	if err != nil {
		return fmt.Errorf("failed to load GitHub token: %w", err)
	}

	// Create GitHub client
	client := NewGitHubClient(token)

	// Determine event type
	eventType := determineEventType(config.DispatchType, success)

	// Create payload
	clientPayload := createClientPayload(branch, commit, success, logFilePath, resultFilePath)

	// Log dispatch attempt
	slog.Debug("Sending GitHub Actions dispatch",
		"repo", config.GitHubRepo,
		"event_type", eventType,
		"branch", branch,
		"commit", commit[:8],
		"success", success)

	// Send dispatch
	if err := client.SendDispatch(repoOwner, repoName, eventType, clientPayload); err != nil {
		return fmt.Errorf("failed to send GitHub dispatch: %w", err)
	}

	// Log success
	slog.Info("GitHub Actions dispatch sent successfully",
		"repo", config.GitHubRepo,
		"event_type", eventType,
		"branch", branch,
		"commit", commit[:8])

	return nil
}