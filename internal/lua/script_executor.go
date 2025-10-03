package lua

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
)

// ExecuteScriptWithOutput executes a Lua script with the given device and arguments,
// writing all output to the provided writers.
// The script is executed synchronously, and all output is drained from the channel.
//
// Parameters:
//   - dev: The BLE device to provide to the Lua script
//   - logger: Logger for Lua engine
//   - script: The Lua script code to execute
//   - args: Map of arguments to pass to the script via the arg[] table
//   - stdout: Writer for standard output (if nil, output is discarded)
//   - stderr: Writer for error output (if nil, errors are discarded)
//   - drainTimeout: How long to wait for output after script completes (e.g., 50ms)
//
// Returns an error if script execution fails.
func ExecuteScriptWithOutput(
	dev device.Device,
	logger *logrus.Logger,
	script string,
	args map[string]string,
	stdout, stderr io.Writer,
	drainTimeout time.Duration,
) error {
	// Create Lua API with the connected device
	luaAPI := NewBLEAPI2(dev, logger)
	defer luaAPI.Close()

	logger.WithField("script_size", len(script)).Debug("Starting Lua script execution")
	defer func() {
		logger.Debug("Lua script execution completed")
	}()

	// Build arg[] table initialization from provided arguments
	argTable := "arg = {}\n"
	for key, value := range args {
		argTable += fmt.Sprintf("arg[%q] = %q\n", key, value)
	}

	// Combine arg initialization with script content
	scriptWithArgs := argTable + "\n-- User script\n" + script

	// Get output channel
	outputChan := luaAPI.OutputChannel()

	// Execute the script (synchronous - blocks until script completes)
	if err := luaAPI.ExecuteScript(scriptWithArgs); err != nil {
		return fmt.Errorf("failed to execute script: %w", err)
	}

	// Drain any buffered output from the channel (script has already completed)
	// The channel is never closed, so we use a timeout to detect when there's no more output
	timer := time.NewTimer(drainTimeout)
	defer timer.Stop()
drainLoop:
	for {
		select {
		case record := <-outputChan:
			// Write output to appropriate writer
			if record.Source == "stderr" && stderr != nil {
				fmt.Fprintln(stderr, record.Content)
			} else if record.Source == "stdout" && stdout != nil {
				fmt.Fprint(stdout, record.Content)
			}
			// Reset timeout after each message
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(drainTimeout)
		case <-timer.C:
			// No more output for the specified duration, we're done
			break drainLoop
		}
	}

	return nil
}

// ExecuteScriptWithOutputAsync executes a Lua script asynchronously with the given device and arguments,
// writing all output to the provided writers.
// The script executes in a background goroutine and can be cancelled via context.
//
// Parameters:
//   - ctx: Context for cancellation
//   - dev: The BLE device to provide to the Lua script
//   - logger: Logger for Lua engine
//   - script: The Lua script code to execute
//   - args: Map of arguments to pass to the script via the arg[] table
//   - stdout: Writer for standard output (if nil, output is discarded)
//   - stderr: Writer for error output (if nil, errors are discarded)
//   - drainTimeout: How long to wait for output after script completes (e.g., 50ms)
//
// Returns a channel that will receive the execution error (or nil) when complete.
// The channel will be closed after the error is sent.
func ExecuteScriptWithOutputAsync(
	ctx context.Context,
	dev device.Device,
	logger *logrus.Logger,
	script string,
	args map[string]string,
	stdout, stderr io.Writer,
	drainTimeout time.Duration,
) <-chan error {
	errChan := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("script execution panicked: %v", r)
			}
			close(errChan)
		}()

		// Run the synchronous version in a goroutine
		done := make(chan error, 1)
		go func() {
			done <- ExecuteScriptWithOutput(dev, logger, script, args, stdout, stderr, drainTimeout)
		}()

		// Wait for either completion or cancellation
		select {
		case err := <-done:
			errChan <- err
		case <-ctx.Done():
			errChan <- ctx.Err()
		}
	}()

	return errChan
}
