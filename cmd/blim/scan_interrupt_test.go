package main

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/pkg/config"
	"github.com/srg/blim/scanner"
)

func TestScanInterrupt(t *testing.T) {
	// Test single scan interrupt
	t.Run("SingleScanInterrupt", func(t *testing.T) {
		// Create configuration
		cfg := config.DefaultConfig()
		cfg.LogLevel = logrus.DebugLevel
		logger := cfg.NewLogger()

		// Create scanner
		s, err := scanner.NewScanner(logger)
		if err != nil {
			t.Skipf("Failed to create BLE scanner (BLE not available?): %v", err)
		}

		// Create scan options for a longer duration to reproduce the hanging issue
		scanOpts := &scanner.ScanOptions{
			Duration:        20 * time.Second, // Use default CLI duration
			DuplicateFilter: true,
		}

		done := make(chan error, 1)
		go func() {
			done <- runSingleScan(s, scanOpts, cfg, logger)
		}()

		// Wait longer to allow more BLE devices to be discovered (more mutex activity)
		time.Sleep(12 * time.Second)

		// Send interrupt signal to ourselves
		process, _ := os.FindProcess(os.Getpid())
		process.Signal(syscall.SIGINT)

		// Wait for completion with timeout
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Single scan returned error: %v", err)
			}
			t.Logf("Single scan completed successfully")
		case <-time.After(5 * time.Second):
			t.Errorf("Single scan did not complete within timeout")
		}
	})

	// Test watch mode interrupt
	t.Run("WatchModeInterrupt", func(t *testing.T) {
		// Create configuration
		cfg := config.DefaultConfig()
		cfg.LogLevel = logrus.DebugLevel
		logger := cfg.NewLogger()

		// Create scanner
		s, err := scanner.NewScanner(logger)
		if err != nil {
			t.Skipf("Failed to create BLE scanner (BLE not available?): %v", err)
		}

		// Create scan options for indefinite scan
		watchOpts := &scanner.ScanOptions{
			Duration:        0, // Indefinite
			DuplicateFilter: true,
		}

		done := make(chan error, 1)
		go func() {
			done <- runWatchMode(s, watchOpts, cfg, logger)
		}()

		// Wait a bit, then send an interrupt
		time.Sleep(500 * time.Millisecond)

		// Send interrupt signal to ourselves
		process, _ := os.FindProcess(os.Getpid())
		process.Signal(syscall.SIGINT)

		// Wait for completion with timeout
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Watch mode returned error: %v", err)
			}
			t.Logf("Watch mode completed successfully")
		case <-time.After(5 * time.Second):
			t.Errorf("Watch mode did not complete within timeout - this indicates a hang!")
		}
	})

	// Test watch mode runs indefinitely and responds to an interrupt
	t.Run("WatchModeHangAfterScanFinishes", func(t *testing.T) {
		// Create configuration
		cfg := config.DefaultConfig()
		cfg.LogLevel = logrus.DebugLevel
		logger := cfg.NewLogger()

		// Create scanner
		s, err := scanner.NewScanner(logger)
		if err != nil {
			t.Skipf("Failed to create BLE scanner (BLE not available?): %v", err)
		}

		// Create scan options - watch mode runs indefinitely regardless of duration
		shortOpts := &scanner.ScanOptions{
			DuplicateFilter: true,
		}

		done := make(chan error, 1)
		go func() {
			done <- runWatchMode(s, shortOpts, cfg, logger)
		}()

		// Watch mode should run indefinitely until interrupted
		// Wait at least 5 seconds to ensure it's running properly
		time.Sleep(5 * time.Second)

		select {
		case err := <-done:
			t.Errorf("Watch mode unexpectedly exited without interrupt: %v", err)
		default:
			// Good - still running as expected
			t.Logf("Watch mode correctly running indefinitely")
		}

		// Now interrupt it to ensure it responds properly
		process, _ := os.FindProcess(os.Getpid())
		process.Signal(syscall.SIGINT)

		// Watch mode should respond to the interrupt
		select {
		case err := <-done:
			if err != nil {
				t.Logf("Watch mode completed with error (expected for interrupt): %v", err)
			} else {
				t.Logf("Watch mode completed successfully after interrupt")
			}
		case <-time.After(5 * time.Second):
			t.Errorf("Watch mode did not respond to interrupt within timeout!")
		}
	})
}
