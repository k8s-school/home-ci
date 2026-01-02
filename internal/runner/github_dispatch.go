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

// BuildStep represents an individual step in the CI process
type BuildStep struct {
	Name        string        `json:"name"`
	StartTime   time.Time     `json:"start_time"`
	EndTime     time.Time     `json:"end_time"`
	Duration    time.Duration `json:"duration"`
	Success     bool          `json:"success"`
	Command     string        `json:"command,omitempty"`
	ExitCode    int           `json:"exit_code"`
	Output      string        `json:"output,omitempty"`
	ErrorOutput string        `json:"error_output,omitempty"`
}

// createEnhancedClientPayload creates an enhanced client payload with detailed test result information
func createEnhancedClientPayload(branch, commit string, success bool, logFilePath, resultFilePath string, testResult *TestResult) map[string]interface{} {
	// Create artifact name with cleaned branch name and short commit
	branchClean := strings.ReplaceAll(branch, "/", "_")
	commitShort := commit
	if len(commit) > 8 {
		commitShort = commit[:8]
	}
	artifactName := fmt.Sprintf("log-%s-%s", branchClean, commitShort)

	payload := map[string]interface{}{
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

	// Add enhanced information if testResult is available
	if testResult != nil {
		payload["execution"] = map[string]interface{}{
			"start_time":      testResult.StartTime.Format(time.RFC3339),
			"end_time":        testResult.EndTime.Format(time.RFC3339),
			"duration":        testResult.Duration.String(),
			"duration_seconds": testResult.Duration.Seconds(),
			"timed_out":       testResult.TimedOut,
		}

		payload["status"] = map[string]interface{}{
			"test_success":         testResult.Success,
			"cleanup_executed":     testResult.CleanupExecuted,
			"cleanup_success":      testResult.CleanupSuccess,
			"github_actions_notified": testResult.GitHubActionsNotified,
		}

		// Add error information if present
		if testResult.ErrorMessage != "" {
			payload["error"] = map[string]interface{}{
				"test_error": testResult.ErrorMessage,
			}
		}
		if testResult.CleanupErrorMessage != "" {
			if payload["error"] == nil {
				payload["error"] = map[string]interface{}{}
			}
			payload["error"].(map[string]interface{})["cleanup_error"] = testResult.CleanupErrorMessage
		}

		// Add repository information if available
		payload["repository"] = map[string]interface{}{
			"branch": branch,
			"commit": commit,
			"ref":    fmt.Sprintf("refs/heads/%s", branch),
		}

		// Add test environment information
		payload["environment"] = map[string]interface{}{
			"log_file":    logFilePath,
			"result_file": resultFilePath,
			"source":      "home-ci",
			"version":     "1.0", // Could be populated from build info
		}

		// Parse build steps from log if available
		if logFilePath != "" {
			steps := parseBuildStepsFromLog(logFilePath)
			if len(steps) > 0 {
				payload["steps"] = steps
			}
		}
	}

	return payload
}

// parseBuildStepsFromLog parses the log file to extract individual build steps
func parseBuildStepsFromLog(logFilePath string) []map[string]interface{} {
	var steps []map[string]interface{}

	// Read log file content
	content, err := os.ReadFile(logFilePath)
	if err != nil {
		slog.Debug("Failed to read log file for step parsing", "error", err)
		return steps
	}

	lines := strings.Split(string(content), "\n")

	// Look for step patterns in the log
	// Common patterns: "Step:", "Running:", "=== ", "Installing", "Building", "Testing", "Pushing"
	stepPatterns := []string{
		"Step:",
		"Running:",
		"=== ",
		"Installing",
		"Building",
		"Testing",
		"Pushing",
		"Deploying",
		"kubectl",
		"docker build",
		"docker push",
		"go build",
		"go test",
		"npm install",
		"npm run",
		"make ",
	}

	currentStep := ""
	stepStartTime := ""
	stepOutput := []string{}
	stepNumber := 1

	for _, line := range lines {
		// Check if this line starts a new step
		for _, pattern := range stepPatterns {
			if strings.Contains(line, pattern) {
				// Save previous step if exists
				if currentStep != "" {
					step := createStepFromOutput(stepNumber, currentStep, stepStartTime, stepOutput)
					if step != nil {
						steps = append(steps, step)
						stepNumber++
					}
				}

				// Start new step
				currentStep = extractStepName(line)
				stepStartTime = extractTimestamp(line)
				stepOutput = []string{line}
				break
			}
		}

		// Add line to current step output
		if currentStep != "" {
			stepOutput = append(stepOutput, line)
		}
	}

	// Save last step if exists
	if currentStep != "" {
		step := createStepFromOutput(stepNumber, currentStep, stepStartTime, stepOutput)
		if step != nil {
			steps = append(steps, step)
		}
	}

	return steps
}

// extractStepName extracts a clean step name from a log line
func extractStepName(line string) string {
	// Remove timestamps and formatting
	cleaned := strings.TrimSpace(line)

	// Common patterns to extract step names
	if strings.Contains(cleaned, "Installing") {
		return "Install Dependencies"
	}
	if strings.Contains(cleaned, "Building") || strings.Contains(cleaned, "go build") {
		return "Build"
	}
	if strings.Contains(cleaned, "Testing") || strings.Contains(cleaned, "go test") {
		return "Test"
	}
	if strings.Contains(cleaned, "docker build") {
		return "Docker Build"
	}
	if strings.Contains(cleaned, "docker push") || strings.Contains(cleaned, "Pushing") {
		return "Docker Push"
	}
	if strings.Contains(cleaned, "kubectl") {
		return "Kubernetes Deploy"
	}
	if strings.Contains(cleaned, "npm install") {
		return "NPM Install"
	}
	if strings.Contains(cleaned, "npm run") {
		return "NPM Run"
	}
	if strings.Contains(cleaned, "make ") {
		return "Make"
	}
	if strings.Contains(cleaned, "Step:") {
		// Extract step name after "Step:"
		parts := strings.Split(cleaned, "Step:")
		if len(parts) > 1 {
			return strings.TrimSpace(parts[1])
		}
	}

	// Fallback: return first few words
	words := strings.Fields(cleaned)
	if len(words) > 0 {
		if len(words) > 3 {
			return strings.Join(words[:3], " ")
		}
		return strings.Join(words, " ")
	}

	return "Unknown Step"
}

// extractTimestamp attempts to extract timestamp from log line
func extractTimestamp(line string) string {
	// Look for common timestamp patterns
	// 2024-01-02T15:04:05Z or time=2024-01-02T15:04:05Z
	if strings.Contains(line, "time=") {
		start := strings.Index(line, "time=") + 5
		end := start
		for end < len(line) && (line[end] != ' ' && line[end] != '\t') {
			end++
		}
		if end > start {
			return line[start:end]
		}
	}

	// Default to current time if no timestamp found
	return time.Now().Format(time.RFC3339)
}

// createStepFromOutput creates a step map from collected output
func createStepFromOutput(stepNumber int, name, startTime string, output []string) map[string]interface{} {
	if name == "" || len(output) == 0 {
		return nil
	}

	// Determine success based on output
	success := true
	exitCode := 0
	errorOutput := ""

	// Look for error indicators
	for _, line := range output {
		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "error") ||
		   strings.Contains(lowerLine, "failed") ||
		   strings.Contains(lowerLine, "fatal") ||
		   strings.Contains(lowerLine, "exit code") {
			success = false
			if strings.Contains(lowerLine, "exit code") {
				// Try to extract exit code
				parts := strings.Split(lowerLine, "exit code")
				if len(parts) > 1 {
					codeStr := strings.TrimSpace(parts[1])
					if len(codeStr) > 0 && codeStr[0] >= '0' && codeStr[0] <= '9' {
						if code, err := fmt.Sscanf(codeStr, "%d", &exitCode); err == nil && code > 0 {
							// Successfully parsed exit code
						}
					}
				}
			}
			errorOutput += line + "\n"
		}
	}

	// Calculate duration (simplified - assume 1 second per line for demo)
	duration := time.Duration(len(output)) * time.Second
	endTime := startTime
	if parsedStart, err := time.Parse(time.RFC3339, startTime); err == nil {
		endTime = parsedStart.Add(duration).Format(time.RFC3339)
	}

	return map[string]interface{}{
		"step_number":  stepNumber,
		"name":         name,
		"start_time":   startTime,
		"end_time":     endTime,
		"duration":     duration.String(),
		"duration_seconds": duration.Seconds(),
		"success":      success,
		"exit_code":    exitCode,
		"output_lines": len(output),
		"error_output": strings.TrimSpace(errorOutput),
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
	return tr.notifyGitHubActionsWithResult(branch, commit, success, logFilePath, resultFilePath, nil)
}

// notifyGitHubActionsWithResult sends an enhanced notification to GitHub Actions via repository dispatch
func (tr *TestRunner) notifyGitHubActionsWithResult(branch, commit string, success bool, logFilePath, resultFilePath string, testResult *TestResult) error {
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

	// Create payload - use enhanced version if testResult is available
	var clientPayload map[string]interface{}
	if testResult != nil {
		clientPayload = createEnhancedClientPayload(branch, commit, success, logFilePath, resultFilePath, testResult)
	} else {
		clientPayload = createClientPayload(branch, commit, success, logFilePath, resultFilePath)
	}

	// Log dispatch attempt with enhanced information
	logFields := []interface{}{
		"repo", config.GitHubRepo,
		"event_type", eventType,
		"branch", branch,
		"commit", commit[:8],
		"success", success,
	}
	if testResult != nil {
		logFields = append(logFields,
			"duration", testResult.Duration.String(),
			"timed_out", testResult.TimedOut,
			"enhanced_payload", true)
	}
	slog.Debug("Sending GitHub Actions dispatch", logFields...)

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
