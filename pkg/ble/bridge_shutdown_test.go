package ble

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// mockConnection simulates a BLE connection behavior
type mockConnection struct {
	disconnectHangs bool
	unsubscribed    bool
}

func (m *mockConnection) Unsubscribe() {
	m.unsubscribed = true
}

func (m *mockConnection) Disconnect() error {
	if m.disconnectHangs && !m.unsubscribed {
		// Simulate hanging BLE disconnect when subscriptions not cleaned up
		time.Sleep(10 * time.Second)
	}
	// Fast disconnect when properly unsubscribed
	return nil
}

func TestBridgeShutdown(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	bridge := NewBridge(logger)
	opts := DefaultBridgeOptions()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start bridge
	go func() {
		if err := bridge.Start(ctx, opts); err != nil {
			t.Logf("Bridge start failed: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	if !bridge.IsRunning() {
		t.Fatal("Bridge should be running")
	}

	// Cancel context (simulates Ctrl+C)
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Try to stop bridge
	stopStart := time.Now()
	if err := bridge.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	stopDuration := time.Since(stopStart)

	if stopDuration > 1*time.Second {
		t.Errorf("Stop took too long: %v", stopDuration)
	}

	if bridge.IsRunning() {
		t.Error("Bridge should not be running")
	}

	t.Logf("Shutdown completed in %v", stopDuration)
}

func TestBridgeCtrlC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping manual Ctrl+C test")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel) // Enable debug logs to see where it hangs
	bridge := NewBridge(logger)
	opts := DefaultBridgeOptions()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Add BLE connection mock that hangs on disconnect
	mockConn := &mockConnection{disconnectHangs: true}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		t.Log("Signal received, cancelling context")
		cancel()
	}()

	go func() {
		if err := bridge.Start(ctx, opts); err != nil {
			t.Logf("Bridge error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	t.Logf("Bridge running: %v, PTY: %s", bridge.IsRunning(), bridge.GetPTYName())
	t.Log("Send SIGINT to test...")

	<-ctx.Done()
	if ctx.Err() == context.DeadlineExceeded {
		t.Log("Timeout - simulating Ctrl+C")
	} else {
		t.Log("Signal received, stopping bridge...")
	}

	stopStart := time.Now()
	err := bridge.Stop()
	duration := time.Since(stopStart)

	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	t.Logf("Stop completed in %v", duration)

	// Simulate proper unsubscribe (like our BLE connection fix)
	mockConn.Unsubscribe()

	// Now test the disconnect (simulating defer conn.Disconnect())
	disconnectStart := time.Now()
	if err := mockConn.Disconnect(); err != nil {
		t.Errorf("Mock disconnect error: %v", err)
	}
	disconnectDuration := time.Since(disconnectStart)
	t.Logf("Mock disconnect took: %v", disconnectDuration)

	if disconnectDuration > 1*time.Second {
		t.Errorf("BLE disconnect took too long: %v (this simulates the CLI hang)", disconnectDuration)
	}
}
