package config

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Config holds application configuration
type Config struct {
	LogLevel      logrus.Level  `json:"log_level"`
	ScanTimeout   time.Duration `json:"scan_timeout"`
	DeviceTimeout time.Duration `json:"device_timeout"`
	OutputFormat  string        `json:"output_format"`
}

// DefaultConfig returns default configuration values
func DefaultConfig() *Config {
	return &Config{
		LogLevel:      logrus.InfoLevel,
		ScanTimeout:   10 * time.Second,
		DeviceTimeout: 30 * time.Second,
		OutputFormat:  "table", // table, json, csv
	}
}

// NewLogger creates a configured logger instance
func (c *Config) NewLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(c.LogLevel)

	// Use structured logging format
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
	})

	return logger
}
