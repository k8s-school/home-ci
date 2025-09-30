package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/k8s-school/ciux/log"
	"github.com/spf13/cobra"

	"github.com/k8s-school/home-ci/internal/config"
	"github.com/k8s-school/home-ci/internal/monitor"
)

var (
	configPath string
	verbose    bool
)

var RootCmd = &cobra.Command{
	Use:   "home-ci",
	Short: "Git CI Monitor - Monitors git repositories for changes and runs tests",
	Long: `A CI monitoring tool that watches git repositories for new commits
and automatically runs tests when changes are detected.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logging with ciux/log which configures slog
		verbosity := 0
		if verbose {
			verbosity = -10 // Debug level
		}
		log.Init(verbosity)

		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		monitor, err := monitor.NewMonitor(cfg)
		if err != nil {
			return fmt.Errorf("failed to create monitor: %w", err)
		}

		// Handle graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigCh
			slog.Debug("Received shutdown signal")
			monitor.Stop()
		}()

		return monitor.Start()
	},
}

func init() {
	RootCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to configuration file")
	RootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging (debug level)")
}