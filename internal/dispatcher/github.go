package dispatcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"gopkg.in/yaml.v3"
)

type GitHubDispatcher struct {
	token      string
	repo       string
	httpClient *http.Client
}

type DispatchPayload struct {
	Build   bool   `json:"build"`
	E2E     bool   `json:"e2e"`
	Push    bool   `json:"push"`
	Cluster string `json:"cluster"`
	Image   string `json:"image"`
}

type DispatchRequest struct {
	EventType     string          `json:"event_type"`
	ClientPayload DispatchPayload `json:"client_payload"`
}

func NewGitHubDispatcher(repo, secretPath string) (*GitHubDispatcher, error) {
	token, err := loadGitHubToken(secretPath)
	if err != nil {
		return nil, err
	}

	return &GitHubDispatcher{
		token:      token,
		repo:       repo,
		httpClient: &http.Client{},
	}, nil
}

func loadGitHubToken(secretPath string) (string, error) {
	if secretPath == "" {
		secretPath = "secret.yaml"
	}

	// Try to load from secret.yaml first
	if token, err := loadTokenFromSecretFile(secretPath); err == nil {
		return token, nil
	}

	return "", fmt.Errorf("no GitHub token found - please create %s with github_token", secretPath)
}

func loadTokenFromSecretFile(secretPath string) (string, error) {
	data, err := os.ReadFile(secretPath)
	if err != nil {
		return "", err
	}

	var secret struct {
		GitHubToken string `yaml:"github_token"`
	}

	if err := yaml.Unmarshal(data, &secret); err != nil {
		return "", err
	}

	if secret.GitHubToken == "" {
		return "", fmt.Errorf("github_token is empty in %s", secretPath)
	}

	return secret.GitHubToken, nil
}

func (gd *GitHubDispatcher) Dispatch(eventType, cluster, imageURL string, build, e2e, push bool) error {
	if gd.token == "" {
		slog.Warn("No GitHub token available, skipping dispatch")
		return nil
	}

	payload := DispatchPayload{
		Build:   build,
		E2E:     e2e,
		Push:    push,
		Cluster: cluster,
		Image:   imageURL,
	}

	request := DispatchRequest{
		EventType:     eventType,
		ClientPayload: payload,
	}

	slog.Debug("GitHub dispatch payload", "payload", payload)

	return gd.sendDispatch(request)
}

func (gd *GitHubDispatcher) sendDispatch(request DispatchRequest) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/dispatches", gd.repo)

	jsonData, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal dispatch request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+gd.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	slog.Info("Dispatching event to GitHub", "repo", gd.repo, "event_type", request.EventType)

	resp, err := gd.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send dispatch request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	slog.Info("Successfully dispatched event to GitHub")
	return nil
}