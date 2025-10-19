package main

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// configureLogger creates a logger with the appropriate log level based on flags.
// It respects both --log-level and --verbose flags, with --log-level taking precedence.
// Returns a configured logger or error if the log-level is invalid.
func configureLogger(cmd *cobra.Command, verboseFlagName string) (*logrus.Logger, error) {
	// Default to panic level (essentially silent for normal operations)
	logLevel := logrus.PanicLevel

	// Check --log-level first (takes precedence)
	logLevelStr, _ := cmd.Flags().GetString("log-level")
	if logLevelStr != "" {
		switch logLevelStr {
		case "debug":
			logLevel = logrus.DebugLevel
		case "info":
			logLevel = logrus.InfoLevel
		case "warn":
			logLevel = logrus.WarnLevel
		case "error":
			logLevel = logrus.ErrorLevel
		default:
			return nil, fmt.Errorf("invalid log level: %s (must be debug, info, warn, or error)", logLevelStr)
		}
	} else {
		// Fall back to --verbose flag if no --log-level specified
		verbose, _ := cmd.Flags().GetBool(verboseFlagName)
		if verbose {
			logLevel = logrus.DebugLevel
		}
	}

	// Create logger with configured level
	logger := logrus.New()
	logger.SetLevel(logLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
	})

	return logger, nil
}
