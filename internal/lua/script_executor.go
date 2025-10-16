package lua

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/groutine"
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
	luaAPI *LuaAPI,
	logger *logrus.Logger,
	script string,
	args map[string]string,
	stdout, stderr io.Writer,
	drainTimeout time.Duration,
) error {

	if luaAPI == nil {
		// Create a Lua API with the connected device
		luaAPI = NewBLEAPI2(dev, logger)
		defer luaAPI.Close()
	}

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

	// Only drain output if at least one writer is provided
	var drainer *OutputDrainer

	// If both stdout and stderr are nil, skip consumption and let the caller handle OutputChannel
	if stdout != nil || stderr != nil {
		drainer = NewOutputDrainer(ctx, luaAPI.OutputChannel(), logger, stdout, stderr)

		defer func() {
			// Stop the drainer after a script completes
			drainer.Cancel()

			// Wait for the goroutine to exit with a timeout
			done := make(chan struct{})
			groutine.Go(ctx, fmt.Sprintf("drainer-wait-script-%d", len(script)), func(ctx context.Context) {
				drainer.Wait()
				close(done)
			})

			select {
			case <-done:
				// finished successfully
			case <-time.After(drainTimeout / 2):
				logger.WithField("timeout", drainTimeout/2).Debug("Output drainer did not exit within timeout")
			}
		}()
	}

	// Execute the script (blocking)
	if err := luaAPI.ExecuteScript(ctx, scriptWithArgs); err != nil {
		return fmt.Errorf("failed to execute script: %w", err)
	}

	return nil
}
