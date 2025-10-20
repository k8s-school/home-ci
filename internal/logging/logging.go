package logging

import (
	"log/slog"
	"os"
)

// InitLogging initializes the slog logger with the specified verbose level
// verboseLevel: 0=Error only, 1=Warn+Error, 2=Info+Warn+Error, 3=Debug+Info+Warn+Error
func InitLogging(verboseLevel int) {
	var level slog.Level
	switch verboseLevel {
	case 0:
		level = slog.LevelError
	case 1:
		level = slog.LevelWarn
	case 2:
		level = slog.LevelInfo
	case 3:
		level = slog.LevelDebug
	default:
		level = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
}