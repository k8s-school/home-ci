package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	Content     string                 `json:"content"`
	Type        string                 `json:"type"`
	Compressed  bool                   `json:"compressed,omitempty"`
	Truncated   bool                   `json:"truncated,omitempty"`
	OriginalSize int                    `json:"original_size,omitempty"`
	Files       []ArchiveFileMetadata  `json:"files,omitempty"` // For archive type only
}

// ArchiveFileMetadata contains metadata about files in an archive
type ArchiveFileMetadata struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Truncated    bool   `json:"truncated"`
	OriginalSize int    `json:"original_size"`
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

	// Log detailed request information
	slog.Debug("GitHub API request details",
		"method", req.Method,
		"url", req.URL.String(),
		"payload", string(jsonData),
		"headers", map[string]string{
			"Accept": req.Header.Get("Accept"),
			"Authorization": "Bearer ***", // Mask the token
			"X-GitHub-Api-Version": req.Header.Get("X-GitHub-Api-Version"),
			"Content-Type": req.Header.Get("Content-Type"),
		})

	resp, err := gc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Log response details
	slog.Debug("GitHub API response details",
		"status_code", resp.StatusCode,
		"status", resp.Status)

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		slog.Debug("GitHub API error response", "body", string(body))
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


// FileToArchive represents a file to be added to the tar.gz archive
type FileToArchive struct {
	Name         string
	Data         []byte
	Type         string
	Truncated    bool
	OriginalSize int
}

// createCompressedArtifactsArchive creates a single tar.gz archive containing all files
func createCompressedArtifactsArchive(files []FileToArchive) (Artifact, error) {
	if len(files) == 0 {
		return Artifact{}, fmt.Errorf("no files to archive")
	}

	// Calculate total original size
	totalOriginalSize := 0
	for _, file := range files {
		totalOriginalSize += file.OriginalSize
	}

	// Create tar.gz archive
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, file := range files {
		// Create tar header
		hdr := &tar.Header{
			Name: file.Name,
			Mode: 0644,
			Size: int64(len(file.Data)),
		}

		// Write header
		if err := tw.WriteHeader(hdr); err != nil {
			return Artifact{}, fmt.Errorf("failed to write tar header for %s: %w", file.Name, err)
		}

		// Write file data
		if _, err := tw.Write(file.Data); err != nil {
			return Artifact{}, fmt.Errorf("failed to write tar data for %s: %w", file.Name, err)
		}
	}

	// Close tar and gzip writers
	if err := tw.Close(); err != nil {
		return Artifact{}, fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		return Artifact{}, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	// Get compressed data
	compressedData := buf.Bytes()

	// Check if any files were truncated and build metadata
	anyTruncated := false
	fileMetadata := make([]ArchiveFileMetadata, 0, len(files))
	for _, file := range files {
		if file.Truncated {
			anyTruncated = true
		}
		fileMetadata = append(fileMetadata, ArchiveFileMetadata{
			Name:         file.Name,
			Type:         file.Type,
			Truncated:    file.Truncated,
			OriginalSize: file.OriginalSize,
		})
	}

	// Encode as base64
	content := base64.StdEncoding.EncodeToString(compressedData)

	return Artifact{
		Content:     content,
		Type:        "archive",
		Compressed:  true,
		Truncated:   anyTruncated,
		OriginalSize: totalOriginalSize,
		Files:       fileMetadata,
	}, nil
}

// readFileForArchive reads a file with limits and returns data ready for archive
func readFileForArchive(filePath string, maxBytes, maxLines int, fileType string) (FileToArchive, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return FileToArchive{}, err
	}

	originalSize := len(data)
	truncated := false

	// Apply same truncation logic as readFileAsBase64WithLimits
	if len(data) > maxBytes || (maxLines > 0 && strings.Count(string(data), "\n") > maxLines) {
		lines := strings.Split(string(data), "\n")
		if len(lines) > maxLines && maxLines > 0 {
			// Keep last maxLines
			lines = lines[len(lines)-maxLines:]
			truncated = true
		}

		// Reconstruct data
		truncatedContent := strings.Join(lines, "\n")
		data = []byte(truncatedContent)

		// Double-check byte limit
		if len(data) > maxBytes {
			// Truncate to maxBytes, keeping end of file
			if len(data) > maxBytes {
				data = data[len(data)-maxBytes:]
				truncated = true
			}
		}
	}

	fileName := filepath.Base(filePath)
	return FileToArchive{
		Name:         fileName,
		Data:         data,
		Type:         fileType,
		Truncated:    truncated,
		OriginalSize: originalSize,
	}, nil
}


// parseRepoString parses "owner/repo" format and returns owner and repo name
func parseRepoString(repoString string) (owner, name string, err error) {
	parts := strings.Split(repoString, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repository format, expected 'owner/repo', got '%s'", repoString)
	}
	return parts[0], parts[1], nil
}

// findYAMLReportFile looks for the YAML e2e report file in the log directory
func findYAMLReportFile(logDir string) string {
	// Check for the standard e2e-report.yaml file
	standardReportFile := filepath.Join(logDir, "e2e-report.yaml")
	if _, err := os.Stat(standardReportFile); err == nil {
		slog.Debug("Found YAML e2e report file", "file", standardReportFile)
		return standardReportFile
	}

	slog.Debug("No YAML e2e report file found", "dir", logDir)
	return ""
}

// truncateBase64Content creates a truncated version of a payload for logging
func truncateBase64Content(payload map[string]interface{}) map[string]interface{} {
	truncated := make(map[string]interface{})
	for key, value := range payload {
		if key == "artifacts" {
			if artifacts, ok := value.(map[string]interface{}); ok {
				truncatedArtifacts := make(map[string]interface{})
				for artifactKey, artifactValue := range artifacts {
					if artifact, ok := artifactValue.(Artifact); ok {
						truncatedArtifact := Artifact{
							Type:        artifact.Type,
							Compressed:  artifact.Compressed,
							Truncated:   artifact.Truncated,
							OriginalSize: artifact.OriginalSize,
						}
						if len(artifact.Content) > 15 {
							truncatedArtifact.Content = artifact.Content[:15] + "..."
						} else {
							truncatedArtifact.Content = artifact.Content
						}
						truncatedArtifacts[artifactKey] = truncatedArtifact
					} else {
						truncatedArtifacts[artifactKey] = artifactValue
					}
				}
				truncated[key] = truncatedArtifacts
			} else {
				truncated[key] = value
			}
		} else {
			truncated[key] = value
		}
	}
	return truncated
}

// createArtifactsMap creates the artifacts map for the dispatch payload using combined archive
func createArtifactsMap(branch, commit string, success bool, logFilePath, resultFilePath string, hasResultFile bool, maxFileBytes, maxLogLines int) (map[string]interface{}, error) {
	slog.Debug("Creating artifacts map", "branch", branch, "commit", commit, "success", success, "logFile", logFilePath, "resultFile", resultFilePath, "hasResultFile", hasResultFile)
	artifacts := make(map[string]interface{})
	var files []FileToArchive

	// Add log file
	if logFilePath != "" {
		if file, err := readFileForArchive(logFilePath, maxFileBytes, maxLogLines, "log"); err == nil {
			files = append(files, file)
			if file.Truncated {
				slog.Warn("Log file truncated for archive", "file", file.Name, "original_size", file.OriginalSize, "max_bytes", maxFileBytes, "max_lines", maxLogLines)
			}
			slog.Debug("Added log file to archive", "file", file.Name, "size", len(file.Data), "truncated", file.Truncated)
		} else {
			slog.Debug("Failed to read log file for archive", "file", logFilePath, "error", err)
		}
	}

	// Add result file
	if resultFilePath != "" {
		if file, err := readFileForArchive(resultFilePath, maxFileBytes, maxLogLines, "result"); err == nil {
			files = append(files, file)
			if file.Truncated {
				slog.Warn("Result file truncated for archive", "file", file.Name, "original_size", file.OriginalSize, "max_bytes", maxFileBytes, "max_lines", maxLogLines)
			}
			slog.Debug("Added result file to archive", "file", file.Name, "size", len(file.Data), "truncated", file.Truncated)
		} else {
			slog.Debug("Failed to read result file for archive", "file", resultFilePath, "error", err)
		}
	}

	// Look for the YAML report file
	if logFilePath != "" {
		logDir := filepath.Dir(logFilePath)
		yamlReportFile := findYAMLReportFile(logDir)
		if yamlReportFile != "" {
			if file, err := readFileForArchive(yamlReportFile, maxFileBytes, maxLogLines, "e2e-report"); err == nil {
				files = append(files, file)
				if file.Truncated {
					slog.Warn("YAML report file truncated for archive", "file", file.Name, "original_size", file.OriginalSize, "max_bytes", maxFileBytes, "max_lines", maxLogLines)
				}
				slog.Debug("Added YAML report file to archive", "file", file.Name, "size", len(file.Data), "truncated", file.Truncated)
			} else {
				slog.Debug("Failed to read YAML report file for archive", "file", yamlReportFile, "error", err)
			}
		} else if hasResultFile {
			return nil, fmt.Errorf("result file is required (has_result_file=true) but no e2e-report.yaml file found in %s. Make sure your test script creates the file specified by HOME_CI_RESULT_FILE environment variable", logDir)
		}
	}

	// Create archive if we have files
	if len(files) > 0 {
		if archive, err := createCompressedArtifactsArchive(files); err == nil {
			artifacts["combined-archive.tar.gz"] = archive
			slog.Info("Created combined archive", "files_count", len(files), "compressed_size", len(archive.Content), "original_total_size", archive.OriginalSize, "truncated", archive.Truncated)
		} else {
			slog.Error("Failed to create combined archive", "error", err)
			return nil, fmt.Errorf("failed to create combined archive: %w", err)
		}
	}

	// Add metadata artifact
	artifacts["metadata"] = Artifact{
		Content:     "", // Metadata doesn't need base64 content
		Type:        "metadata",
		Compressed:  false,
		Truncated:   false,
		OriginalSize: 0,
	}

	return artifacts, nil
}

// createClientPayload creates the complete client payload for the dispatch
func createClientPayload(branch, commit string, success bool, logFilePath, resultFilePath string, hasResultFile bool, maxFileBytes, maxLogLines int) (map[string]interface{}, error) {
	// Create artifact name with cleaned branch name and short commit
	branchClean := strings.ReplaceAll(branch, "/", "_")
	commitShort := commit
	if len(commit) > 8 {
		commitShort = commit[:8]
	}
	artifactName := fmt.Sprintf("log-%s-%s", branchClean, commitShort)

	artifacts, err := createArtifactsMap(branch, commit, success, logFilePath, resultFilePath, hasResultFile, maxFileBytes, maxLogLines)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"branch":        branch,
		"commit":        commit,
		"success":       success,
		"timestamp":     fmt.Sprintf("%d", time.Now().Unix()),
		"source":        "home-ci",
		"artifact_name": artifactName,
		"artifacts":     artifacts,
		"metadata": map[string]interface{}{
			"branch":  branch,
			"commit":  commit,
			"success": success,
		},
	}, nil
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

	// Create payload with size limits from config
	clientPayload, err := createClientPayload(branch, commit, success, logFilePath, resultFilePath, config.HasResultFile, config.MaxFileBytes, config.MaxLogLines)
	if err != nil {
		return fmt.Errorf("failed to create client payload: %w", err)
	}

	// Log dispatch attempt with request details
	slog.Debug("Sending GitHub Actions dispatch",
		"repo", config.GitHubRepo,
		"event_type", eventType,
		"branch", branch,
		"commit", commit[:8],
		"success", success,
		"payload", truncateBase64Content(clientPayload))

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
