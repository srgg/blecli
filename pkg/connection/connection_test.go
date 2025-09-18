package connection

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestDefaultConnectOptions(t *testing.T) {
	deviceAddr := "AA:BB:CC:DD:EE:FF"
	opts := DefaultConnectOptions(deviceAddr)

	assert.Equal(t, deviceAddr, opts.DeviceAddress)
	assert.Equal(t, 30*time.Second, opts.ConnectTimeout)
	assert.NotNil(t, opts.ServiceUUID)
	assert.NotNil(t, opts.TxCharUUID)
	assert.NotNil(t, opts.RxCharUUID)

	// Verify the default UUIDs are correct
	assert.Equal(t, SerialServiceUUID, *opts.ServiceUUID)
	assert.Equal(t, SerialTxCharUUID, *opts.TxCharUUID)
	assert.Equal(t, SerialRxCharUUID, *opts.RxCharUUID)
}

func TestNewConnection(t *testing.T) {
	tests := []struct {
		name         string
		opts         *ConnectOptions
		logger       *logrus.Logger
		expectNotNil bool
	}{
		{
			name:         "creates connection with provided logger",
			opts:         DefaultConnectOptions("AA:BB:CC:DD:EE:FF"),
			logger:       logrus.New(),
			expectNotNil: true,
		},
		{
			name:         "creates connection with nil logger",
			opts:         DefaultConnectOptions("AA:BB:CC:DD:EE:FF"),
			logger:       nil,
			expectNotNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewConnection(tt.opts, tt.logger)

			if tt.expectNotNil {
				assert.NotNil(t, conn)
				assert.NotNil(t, conn.logger)
				assert.False(t, conn.isConnected)
			}
		})
	}
}

func TestConnection_IsConnected(t *testing.T) {
	conn := NewConnection(DefaultConnectOptions("AA:BB:CC:DD:EE:FF"), nil)

	// Initially not connected
	assert.False(t, conn.IsConnected())

	// Simulate connection
	conn.connMutex.Lock()
	conn.isConnected = true
	conn.connMutex.Unlock()

	assert.True(t, conn.IsConnected())

	// Simulate disconnection
	conn.connMutex.Lock()
	conn.isConnected = false
	conn.connMutex.Unlock()

	assert.False(t, conn.IsConnected())
}

func TestConnection_GetDeviceInfo(t *testing.T) {
	conn := NewConnection(DefaultConnectOptions("AA:BB:CC:DD:EE:FF"), nil)

	info := conn.GetDeviceInfo()
	assert.NotNil(t, info)
	assert.Contains(t, info, "connected")
	assert.False(t, info["connected"].(bool))
}

func TestConnection_WriteNotConnected(t *testing.T) {
	conn := NewConnection(DefaultConnectOptions("AA:BB:CC:DD:EE:FF"), nil)

	err := conn.Write([]byte("test data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestConnection_SetDataHandler(t *testing.T) {
	conn := NewConnection(DefaultConnectOptions("AA:BB:CC:DD:EE:FF"), nil)

	called := false
	handler := func(data []byte) {
		called = true
	}

	conn.SetDataHandler(handler)
	assert.NotNil(t, conn.onData)

	// Simulate data reception
	conn.handleNotification([]byte("test"))
	assert.True(t, called)
}

func TestSerialUUIDs(t *testing.T) {
	// Verify that the Nordic UART Service UUIDs are correctly defined
	assert.Equal(t, "6e400001b5a3f393e0a9e50e24dcca9e", SerialServiceUUID.String())
	assert.Equal(t, "6e400003b5a3f393e0a9e50e24dcca9e", SerialTxCharUUID.String())
	assert.Equal(t, "6e400002b5a3f393e0a9e50e24dcca9e", SerialRxCharUUID.String())
}

func TestConnection_DisconnectNotConnected(t *testing.T) {
	conn := NewConnection(DefaultConnectOptions("AA:BB:CC:DD:EE:FF"), nil)

	err := conn.Disconnect()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

// Benchmark tests
func BenchmarkConnection_GetDeviceInfo(b *testing.B) {
	conn := NewConnection(DefaultConnectOptions("AA:BB:CC:DD:EE:FF"), nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = conn.GetDeviceInfo()
	}
}

func BenchmarkConnection_IsConnected(b *testing.B) {
	conn := NewConnection(DefaultConnectOptions("AA:BB:CC:DD:EE:FF"), nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = conn.IsConnected()
	}
}
