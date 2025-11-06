package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/spf13/cobra"

	"github.com/k8s-school/home-ci/internal/config"
	"github.com/k8s-school/home-ci/internal/logging"
	"github.com/k8s-school/home-ci/internal/runner"
)

var (
	runBranch string
	runCommit string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run tests for a specific branch",
	Long: `Manually trigger test execution for a specific git branch.
If no commit is specified, tests will run against the latest commit of the branch.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logging
		logging.InitLogging(verbose)

		slog.Info("Starting manual test run", "branch", runBranch, "commit", runCommit)

		if runBranch == "" {
			return fmt.Errorf("branch must be specified using --branch flag")
		}

		// Load configuration
		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config from '%s': %w", configPath, err)
		}

		// If no commit specified, get the latest commit from the branch
		if runCommit == "" {
			commit, err := getLatestCommitFromBranch(cfg.Repository, runBranch)
			if err != nil {
				return fmt.Errorf("failed to get latest commit for branch %s: %w", runBranch, err)
			}
			runCommit = commit
			fmt.Printf("Using latest commit from branch %s: %s\n", runBranch, runCommit[:8])
		}

		// Create test runner without state manager for manual execution
		ctx := context.Background()
		testRunner := runner.NewTestRunner(cfg, configPath, cfg.LogDir, ctx, nil)

		// Execute test directly
		fmt.Printf("Running tests for branch '%s' at commit '%s'\n", runBranch, runCommit[:8])

		if err := testRunner.RunTestsManually(runBranch, runCommit); err != nil {
			fmt.Printf("Test execution failed: %v\n", err)
			return err
		}

		fmt.Printf("Test execution completed successfully for branch '%s' at commit '%s'\n", runBranch, runCommit[:8])
		fmt.Printf("Logs are available in: %s/%s/tests/\n", cfg.LogDir, cfg.RepoName)

		return nil
	},
}

// getLatestCommitFromBranch retrieves the latest commit hash from a specific branch
func getLatestCommitFromBranch(repoURL, branch string) (string, error) {
	slog.Debug("Fetching latest commit from branch", "repo", repoURL, "branch", branch)

	// Create a temporary directory for the repository
	tempDir, err := os.MkdirTemp("", "home-ci-run-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Clone the repository with only the specific branch
	repo, err := git.PlainClone(tempDir, false, &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
		Depth:         1, // Only get the latest commit
	})
	if err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	// Get the HEAD reference
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD reference: %w", err)
	}

	return head.Hash().String(), nil
}

func init() {
	RootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runBranch, "branch", "b", "", "Branch name to run tests against (required)")
	runCmd.Flags().StringVarP(&runCommit, "commit", "", "", "Specific commit hash (optional, defaults to latest commit on branch)")
	runCmd.MarkFlagRequired("branch")
}