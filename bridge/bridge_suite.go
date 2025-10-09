package bridge

import (
	"context"
	"fmt"
	"time"

	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/lua"
)

const (
	// scriptOutputTickInterval is the interval for polling script output in tests
	scriptOutputTickInterval = 50 * time.Millisecond

	// bridgeStartupWait is the delay to allow bridge initialization to complete in tests
	bridgeStartupWait = 50 * time.Millisecond
)

// BridgeSuite provides test infrastructure for Bridge tests.
//
// Design: Embeds internal.LuaApiSuite to reuse all test infrastructure (YAML parsing,
// BLE simulation, peripheral mocks, step execution) while adding Bridge-specific features:
//   - Integrates Bridge's LuaAPI into parent suite lifecycle for automatic output validation
//   - Bridge lifecycle management (activeBridge tracking for cleanup)
//
// All validation logic (stdout, stderr) is inherited from parent suite.
//
// Thread Safety: activeBridge field does not require synchronization because testify/suite
// guarantees single-threaded test execution (SetupTest, test method, TearDownTest run sequentially).
type BridgeSuite struct {
	lua.LuaApiSuite
	//activeBridge *bridgeHandle // Track active bridge for cleanup (no sync needed - sequential test execution)

	originalExecutor lua.ScriptExecutor
}

// SetupTest initializes the test environment and sets the executor for polymorphic dispatch
func (suite *BridgeSuite) SetupTest() {
	suite.LuaApiSuite.SetupTest()

	// Set executor to self for polymorphic ExecuteScriptWithCallbacks dispatch
	// This is set once here and persists across the subtests
	suite.originalExecutor = suite.Executor
	suite.Executor = suite
}

// TearDownTest ensures a bridge is stopped before the parent's teardown to prevent race conditions.
// This prevents the Lua state from being closed while BLE callbacks are still active.
func (suite *BridgeSuite) TearDownTest() {
	// Stop active bridge (if any) to cancel context and cleanup subscriptions
	//if suite.activeBridge != nil {
	//	if err := suite.activeBridge.Stop(); err != nil {
	//		suite.T().Errorf("Bridge stop failed: %v", err)
	//	}
	//	suite.activeBridge = nil
	//}

	if suite.originalExecutor != nil {
		suite.Executor = suite.originalExecutor
		suite.originalExecutor = nil
	}

	// Call parent teardown to close Lua state and other resources
	suite.LuaApiSuite.TearDownTest()
}

// RunBridgeTestCasesFromYAML parses YAML and executes Bridge test cases
func (suite *BridgeSuite) RunBridgeTestCasesFromYAML(yamlContent string) {
	suite.RunTestCasesFromYAML(yamlContent)
}

// ExecuteScriptWithCallbacks overrides the parent's template method to use Bridge's LuaAPI.
// Manages full bridge lifecycle: start bridge, execute callbacks, stop bridge.
func (suite *BridgeSuite) ExecuteScriptWithCallbacks(
	script string,
	before func(luaApi *lua.BLEAPI2),
	after func(luaApi *lua.BLEAPI2),
) error {
	bridgeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get address from parent's device
	address := suite.LuaApi.GetDevice().GetAddress()

	// Re-build subscribe options from peripheral configuration
	var subscribeOptions []device.SubscribeOptions
	if suite.PeripheralBuilder != nil {
		for _, svc := range suite.PeripheralBuilder.GetServices() {
			var characteristics []string
			for _, char := range svc.Characteristics {
				characteristics = append(characteristics, char.UUID)
			}
			subscribeOptions = append(subscribeOptions, device.SubscribeOptions{
				Service:         svc.UUID,
				Characteristics: characteristics,
			})
		}
	}

	var scriptErr error

	bridgeCallback := func(b Bridge) (error, error) {
		bridgeLuaApi := b.GetLuaAPI()

		// Set bridge info in Lua API (Bridge interface has GetPTYName/GetSymlinkPath)
		bridgeLuaApi.SetBridge(b)

		suite.Logger.WithField("lua_api_ptr", fmt.Sprintf("%p", bridgeLuaApi)).Debug("BridgeSuite.ExecuteScriptWithCallbacks: SetBridge called")

		// Setup: output collector, connection
		before(bridgeLuaApi)

		// SetTemplateData sets template variables for text assertion
		// Provides bridge-specific data like PTY path and device address
		data := make(map[string]interface{})

		data["PTY"] = b.GetPTYName()
		data["SymlinkPath"] = b.GetSymlinkPath()
		data["DeviceAddress"] = b.GetLuaAPI().GetDevice().GetAddress()

		suite.SetTemplateData(data)

		// Execute script
		err := lua.ExecuteDeviceScriptWithOutput(
			bridgeCtx,
			nil,
			bridgeLuaApi,
			suite.Logger,
			script,
			nil, // no args
			nil, // stdout - collector handles
			nil, // stderr - collector handles
			scriptOutputTickInterval,
		)
		scriptErr = err

		// Execute test steps and validate (blocks until complete)
		after(bridgeLuaApi)

		return nil, nil
	}

	// Run bridge synchronously - blocks until bridgeCallback returns
	_, bridgeErr := RunDeviceBridge(
		bridgeCtx,
		&BridgeOptions{
			Address:        address,
			ConnectTimeout: 5 * time.Second,
			Services:       subscribeOptions,
			Logger:         suite.Logger,
		},
		nil,
		bridgeCallback,
	)

	// Return script error if present, otherwise bridge error
	if scriptErr != nil {
		return scriptErr
	}
	return bridgeErr
}

// bridgeHandle wraps RunDeviceBridge for test lifecycle management
type bridgeHandle struct {
	ctx    context.Context
	cancel context.CancelFunc
	errCh  chan error
}

func (h *bridgeHandle) Stop() error {
	h.cancel()

	select {
	case err, ok := <-h.errCh:
		if !ok {
			return nil
		}
		return err
	case <-time.After(5 * time.Second):
		// defensive: something's stuck; return an error instead of blocking forever
		return fmt.Errorf("bridge stop timeout (blocked waiting for run goroutine)")
	}
}

//// createAndStartBridge creates and starts a bridge using RunDeviceBridge with the given script.
//// Uses the same BLE device instance as the test suite for proper mocking.
//func (suite *BridgeSuite) createAndStartBridge(script string, ctx context.Context) (*bridgeHandle, error) {
//	// Create context if not provided
//	bridgeCtx := ctx
//	var cancel context.CancelFunc
//	if bridgeCtx == nil {
//		bridgeCtx, cancel = context.WithCancel(context.Background())
//	} else {
//		bridgeCtx, cancel = context.WithCancel(bridgeCtx)
//	}
//
//	// Build subscribe options from peripheral configuration
//	var subscribeOptions []device.SubscribeOptions
//	if suite.PeripheralBuilder != nil {
//		for _, svc := range suite.PeripheralBuilder.GetServices() {
//			var characteristics []string
//			for _, char := range svc.Characteristics {
//				characteristics = append(characteristics, char.UUID)
//			}
//			subscribeOptions = append(subscribeOptions, device.SubscribeOptions{
//				Service:         svc.UUID,
//				Characteristics: characteristics,
//			})
//		}
//	}
//
//	// Bridge callback - executes Lua script using ExecuteDeviceScriptWithOutput
//	bridgeCallback := func(b Bridge) (error, error) {
//		// Set bridge info in Lua API (Bridge interface has GetPTYName/GetSymlinkPath)
//		b.GetLuaAPI().SetBridge(b)
//
//		// Execute script with output streaming
//		// Pass nil for both stdout and stderr to skip consumption in ExecuteDeviceScriptWithOutput
//		// This allows parent's luaOutputCapture to collect all output from OutputChannel
//		// Stderr errors are captured via customErrorHandler during Lua execution
//		err := lua.ExecuteDeviceScriptWithOutput(
//			bridgeCtx,
//			nil,
//			b.GetLuaAPI(),
//			suite.Logger,
//			script,
//			nil, // no args
//			nil, // stdout - parent's luaOutputCapture collects from OutputChannel
//			nil, // stderr - also collected by luaOutputCapture; errors via customErrorHandler
//			scriptOutputTickInterval,
//		)
//		if err != nil {
//			return nil, err
//		}
//
//		// Keep bridge running until the test completes (prevents premature Lua state cleanup)
//		// This matches production behavior where the bridge waits for Ctrl+C
//		<-bridgeCtx.Done()
//
//		return nil, nil
//	}
//
//	// Run bridge asynchronously for tests
//	// Buffer size = 1 allows goroutine to send error and exit without blocking,
//	// even if Stop() hasn't been called yet. Single send guarantees no overflow.
//	errCh := make(chan error, 1)
//	go func() {
//		err := func() error {
//			_, err := RunDeviceBridge(
//				bridgeCtx,
//				&BridgeOptions{
//					Address:        "00:00:00:00:01",
//					ConnectTimeout: 5 * time.Second,
//					Services:       subscribeOptions,
//					Logger:         suite.Logger,
//				},
//				nil, // no progress callback for tests
//				bridgeCallback,
//			)
//			return err
//		}()
//
//		// Try to send error, but don't block forever; then always close so Stop() unblocks.
//		select {
//		case errCh <- err:
//		default:
//		}
//		close(errCh)
//	}()
//
//	// Wait a bit for the bridge to start
//	time.Sleep(bridgeStartupWait)
//
//	handle := &bridgeHandle{
//		ctx:    bridgeCtx,
//		cancel: cancel,
//		errCh:  errCh,
//	}
//
//	// Track active bridge for cleanup in TearDownTest
//	suite.activeBridge = handle
//
//	return handle, nil
//}
