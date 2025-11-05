package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/k8s-school/home-ci/internal/config"
	"github.com/k8s-school/home-ci/internal/logging"
	"github.com/k8s-school/home-ci/internal/monitor"
)

var (
	configPath string
	verbose    int
	keepTime   string
)

var RootCmd = &cobra.Command{
	Use:   "home-ci",
	Short: "Git CI Monitor - Monitors git repositories for changes and runs tests",
	Long: `A CI monitoring tool that watches git repositories for new commits
and automatically runs tests when changes are detected.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logging
		logging.InitLogging(verbose)

		slog.Debug("Using configuration file", "config_path", configPath)

		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config from '%s': %w", configPath, err)
		}

		// Parse and set the KeepTime from command line
		if keepTime != "" {
			duration, err := time.ParseDuration(keepTime)
			if err != nil {
				return fmt.Errorf("invalid keep-time duration: %w", err)
			}
			cfg.KeepTime = duration
		}

		monitor, err := monitor.NewMonitor(cfg, configPath)
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
	RootCmd.Flags().StringVarP(&configPath, "config", "c", "/etc/home-ci/config.yaml", "Path to configuration file")
	RootCmd.Flags().IntVarP(&verbose, "verbose", "v", 0, "Verbose level (0=error, 1=warn, 2=info, 3=debug)")
	RootCmd.Flags().StringVar(&keepTime, "keep-time", "", "Keep cloned repositories for specified duration (e.g., '2h', '30m', '1h30m') before cleaning up")
}
