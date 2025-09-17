package ble

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestDefaultBridgeOptions(t *testing.T) {
	opts := DefaultBridgeOptions()

	assert.NotNil(t, opts)
	assert.Equal(t, "", opts.PTYName)
	assert.Equal(t, 1024, opts.BufferSize)
}

func TestNewBridge(t *testing.T) {
	tests := []struct {
		name         string
		logger       *logrus.Logger
		expectNotNil bool
	}{
		{
			name:         "creates bridge with provided logger",
			logger:       logrus.New(),
			expectNotNil: true,
		},
		{
			name:         "creates bridge with nil logger",
			logger:       nil,
			expectNotNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridge := NewBridge(tt.logger)

			if tt.expectNotNil {
				assert.NotNil(t, bridge)
				assert.NotNil(t, bridge.logger)
				assert.False(t, bridge.isRunning)
				assert.NotNil(t, bridge.stopChan)
				assert.NotNil(t, bridge.stoppedChan)
			}
		})
	}
}

func TestBridge_IsRunning(t *testing.T) {
	bridge := NewBridge(nil)

	// Initially not running
	assert.False(t, bridge.IsRunning())

	// Simulate running state
	bridge.runMutex.Lock()
	bridge.isRunning = true
	bridge.runMutex.Unlock()

	assert.True(t, bridge.IsRunning())

	// Simulate stopped state
	bridge.runMutex.Lock()
	bridge.isRunning = false
	bridge.runMutex.Unlock()

	assert.False(t, bridge.IsRunning())
}

func TestBridge_GetPTYName(t *testing.T) {
	bridge := NewBridge(nil)

	// Initially no PTY
	assert.Equal(t, "", bridge.GetPTYName())
}

func TestBridge_GetStats(t *testing.T) {
	bridge := NewBridge(nil)

	stats := bridge.GetStats()
	assert.NotNil(t, stats)
	assert.Contains(t, stats, "running")
	assert.False(t, stats["running"].(bool))
}

func TestBridge_WriteToDeviceNotRunning(t *testing.T) {
	bridge := NewBridge(nil)

	err := bridge.WriteToDevice([]byte("test data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bridge is not running")
}

func TestBridge_WriteFromDeviceNotRunning(t *testing.T) {
	bridge := NewBridge(nil)

	err := bridge.WriteFromDevice([]byte("test data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bridge is not running")
}

func TestBridge_StartWithoutWriteFunc(t *testing.T) {
	bridge := NewBridge(nil)
	ctx := context.Background()
	opts := DefaultBridgeOptions()

	err := bridge.Start(ctx, opts, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write function is required")
}

func TestBridge_StopNotRunning(t *testing.T) {
	bridge := NewBridge(nil)

	err := bridge.Stop()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bridge is not running")
}

func TestBridge_StartStopCycle(t *testing.T) {
	bridge := NewBridge(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	opts := DefaultBridgeOptions()
	writeFunc := func(data []byte) error {
		return nil
	}

	// Start the bridge
	err := bridge.Start(ctx, opts, writeFunc)
	assert.NoError(t, err)
	assert.True(t, bridge.IsRunning())
	assert.NotEqual(t, "", bridge.GetPTYName())

	// Wait for context to cancel (which should stop the bridge)
	<-ctx.Done()

	// Give some time for cleanup
	time.Sleep(50 * time.Millisecond)

	// Bridge should have stopped
	assert.False(t, bridge.IsRunning())
}

func TestBridge_StartAlreadyRunning(t *testing.T) {
	bridge := NewBridge(nil)
	ctx := context.Background()
	opts := DefaultBridgeOptions()
	writeFunc := func(data []byte) error {
		return nil
	}

	// Start the bridge
	err := bridge.Start(ctx, opts, writeFunc)
	assert.NoError(t, err)
	defer bridge.Stop()

	// Try to start again
	err = bridge.Start(ctx, opts, writeFunc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bridge is already running")
}

func TestBridge_WriteToDeviceWithRunningBridge(t *testing.T) {
	bridge := NewBridge(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := DefaultBridgeOptions()

	// Track write calls
	var writtenData []byte
	writeFunc := func(data []byte) error {
		writtenData = append(writtenData, data...)
		return nil
	}

	// Start the bridge
	err := bridge.Start(ctx, opts, writeFunc)
	assert.NoError(t, err)
	defer bridge.Stop()

	// Write data
	testData := []byte("hello world")
	err = bridge.WriteToDevice(testData)
	assert.NoError(t, err)

	// Verify write function was called
	assert.Equal(t, testData, writtenData)
}

// Benchmark tests
func BenchmarkBridge_IsRunning(b *testing.B) {
	bridge := NewBridge(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bridge.IsRunning()
	}
}

func BenchmarkBridge_GetStats(b *testing.B) {
	bridge := NewBridge(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bridge.GetStats()
	}
}
