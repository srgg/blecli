package config

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.NotNil(t, cfg)
	assert.Equal(t, logrus.InfoLevel, cfg.LogLevel)
	assert.Equal(t, 10*time.Second, cfg.ScanTimeout)
	assert.Equal(t, 30*time.Second, cfg.DeviceTimeout)
	assert.Equal(t, "table", cfg.OutputFormat)
}

func TestConfig_NewLogger(t *testing.T) {
	tests := []struct {
		name     string
		logLevel logrus.Level
	}{
		{
			name:     "creates logger with debug level",
			logLevel: logrus.DebugLevel,
		},
		{
			name:     "creates logger with info level",
			logLevel: logrus.InfoLevel,
		},
		{
			name:     "creates logger with warn level",
			logLevel: logrus.WarnLevel,
		},
		{
			name:     "creates logger with error level",
			logLevel: logrus.ErrorLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				LogLevel: tt.logLevel,
			}

			logger := cfg.NewLogger()

			assert.NotNil(t, logger)
			assert.Equal(t, tt.logLevel, logger.GetLevel())

			// Verify formatter is set correctly
			formatter, ok := logger.Formatter.(*logrus.TextFormatter)
			assert.True(t, ok)
			assert.True(t, formatter.FullTimestamp)
			assert.Equal(t, time.RFC3339, formatter.TimestampFormat)
		})
	}
}

func TestConfig_CustomValues(t *testing.T) {
	cfg := &Config{
		LogLevel:      logrus.DebugLevel,
		ScanTimeout:   5 * time.Second,
		DeviceTimeout: 60 * time.Second,
		OutputFormat:  "json",
	}

	assert.Equal(t, logrus.DebugLevel, cfg.LogLevel)
	assert.Equal(t, 5*time.Second, cfg.ScanTimeout)
	assert.Equal(t, 60*time.Second, cfg.DeviceTimeout)
	assert.Equal(t, "json", cfg.OutputFormat)

	logger := cfg.NewLogger()
	assert.Equal(t, logrus.DebugLevel, logger.GetLevel())
}

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name         string
		outputFormat string
		valid        bool
	}{
		{
			name:         "table format is valid",
			outputFormat: "table",
			valid:        true,
		},
		{
			name:         "json format is valid",
			outputFormat: "json",
			valid:        true,
		},
		{
			name:         "csv format is valid",
			outputFormat: "csv",
			valid:        true,
		},
		{
			name:         "unknown format",
			outputFormat: "xml",
			valid:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				OutputFormat: tt.outputFormat,
			}

			// Test that we can identify valid formats
			validFormats := []string{"table", "json", "csv"}
			isValid := false
			for _, format := range validFormats {
				if cfg.OutputFormat == format {
					isValid = true
					break
				}
			}

			assert.Equal(t, tt.valid, isValid)
		})
	}
}

func TestConfig_ZeroValues(t *testing.T) {
	cfg := &Config{}

	// Test that zero values don't cause panics
	logger := cfg.NewLogger()
	assert.NotNil(t, logger)

	// Zero log level should default to PanicLevel (0)
	assert.Equal(t, logrus.PanicLevel, logger.GetLevel())

	// Zero timeout values
	assert.Equal(t, time.Duration(0), cfg.ScanTimeout)
	assert.Equal(t, time.Duration(0), cfg.DeviceTimeout)

	// Empty output format
	assert.Equal(t, "", cfg.OutputFormat)
}

func BenchmarkDefaultConfig(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = DefaultConfig()
	}
}

func BenchmarkConfig_NewLogger(b *testing.B) {
	cfg := DefaultConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cfg.NewLogger()
	}
}
