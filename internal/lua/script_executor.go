package lua

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
)

// ExecuteDeviceScriptWithOutput executes a Lua script with the given device and arguments,
// writing all output to the provided writers.
// The script is executed synchronously, and all output is drained from the channel.
//
// Parameters:
//   - ctx: Context for cancellation
//   - dev: The BLE device to provide to the Lua script
//   - logger: Logger for the Lua engine
//   - script: The Lua script code to execute
//   - args: Map of arguments to pass to the script via the arg[] table
//   - stdout: Writer for standard output (if nil, output is discarded)
//   - stderr: Writer for error output (if nil, errors are discarded)
//   - drainTimeout: How long to wait for output after script completes (e.g., 50ms)
//
// Returns an error if script execution fails.
func ExecuteDeviceScriptWithOutput(
	ctx context.Context,
	dev device.Device,
	logger *logrus.Logger,
	script string,
	args map[string]string,
	stdout, stderr io.Writer,
	drainTimeout time.Duration,
) error {
	// Create a Lua API with the connected device
	luaAPI := NewBLEAPI2(dev, logger)
	defer luaAPI.Close()

	logger.WithField("script_size", len(script)).Debug("Starting Lua script execution")
	defer func() {
		logger.Debug("Lua script execution completed")
	}()

	// Build arg[] table initialization from provided arguments
	// Using strings.Builder for efficient string concatenation
	var argBuilder strings.Builder
	argBuilder.WriteString("arg = {}\n")
	for key, value := range args {
		// strings.Builder.Write never returns an error, safe to ignore
		_, _ = fmt.Fprintf(&argBuilder, "arg[%q] = %q\n", key, value)
	}

	// Combine arg initialization with script content
	scriptWithArgs := argBuilder.String() + "\n-- User script\n" + script

	// Get output channel
	outputChan := luaAPI.OutputChannel()

	// Start draining output concurrently with script execution to prevent RingChannel overflow
	// This ensures output is consumed in real-time, not after the script completes
	outputDone := make(chan struct{})
	stopDrain := make(chan struct{})

	go func() {
		defer close(outputDone)
		for {
			select {
			case record := <-outputChan:
				// Write output to the appropriate writer
				if record.Source == "stderr" && stderr != nil {
					if _, err := fmt.Fprintln(stderr, record.Content); err != nil {
						logger.WithError(err).Debug("Failed to write to stderr")
					}
				} else if record.Source == "stdout" && stdout != nil {
					if _, err := fmt.Fprint(stdout, record.Content); err != nil {
						logger.WithError(err).Debug("Failed to write to stdout")
					}
				}
			case <-stopDrain:
				// Drain any remaining output before stopping
				for {
					select {
					case record := <-outputChan:
						if record.Source == "stderr" && stderr != nil {
							if _, err := fmt.Fprintln(stderr, record.Content); err != nil {
								logger.WithError(err).Debug("Failed to write to stderr during final drain")
							}
						} else if record.Source == "stdout" && stdout != nil {
							if _, err := fmt.Fprint(stdout, record.Content); err != nil {
								logger.WithError(err).Debug("Failed to write to stdout during final drain")
							}
						}
					default:
						// No more output, exit
						return
					}
				}
			case <-ctx.Done():
				// Context canceled, stop draining
				return
			}
		}
	}()

	// Execute the script (synchronous - blocks until script completes)
	scriptErr := luaAPI.ExecuteScript(ctx, scriptWithArgs)

	// Wait for any remaining buffered output after a script completes
	// Use timeout to detect when there's no more output
	timer := time.NewTimer(drainTimeout)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Drain timeout reached
	case <-ctx.Done():
		// Context canceled
	}

	// Signal output goroutine to stop and drain remaining output
	// Using select to handle a case where the goroutine already exited due to context cancellation
	select {
	case stopDrain <- struct{}{}:
		// Signal sent successfully
	default:
		// Goroutine may have already stopped due to context cancellation
	}
	close(stopDrain)

	// Wait for the output goroutine to finish with a timeout
	// Use half of drainTimeout as a reasonable cleanup duration
	goroutineCleanupTimeout := drainTimeout / 2
	if goroutineCleanupTimeout < 50*time.Millisecond {
		goroutineCleanupTimeout = 50 * time.Millisecond
	}

	select {
	case <-outputDone:
		// Goroutine completed successfully
	case <-time.After(goroutineCleanupTimeout):
		logger.WithField("timeout", goroutineCleanupTimeout).Debug("Output draining goroutine did not complete within timeout")
	}

	if scriptErr != nil {
		return fmt.Errorf("failed to execute script: %w", scriptErr)
	}

	return nil
}
