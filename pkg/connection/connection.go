package connection

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/darwin"
	"github.com/sirupsen/logrus"
)

// SerialServiceUUID is the standard Nordic UART Service UUID for BLE serial communication
var SerialServiceUUID = ble.MustParse("6E400001-B5A3-F393-E0A9-E50E24DCCA9E")

// SerialTxCharUUID is the TX characteristic (device -> client)
var SerialTxCharUUID = ble.MustParse("6E400003-B5A3-F393-E0A9-E50E24DCCA9E")

// SerialRxCharUUID is the RX characteristic (client -> device)
var SerialRxCharUUID = ble.MustParse("6E400002-B5A3-F393-E0A9-E50E24DCCA9E")

// Connection represents a BLE serial connection to a device
type Connection struct {
	client      ble.Client
	profile     *ble.Profile
	txChar      *ble.Characteristic
	rxChar      *ble.Characteristic
	logger      *logrus.Logger
	onData      func([]byte)
	writeMutex  sync.Mutex
	isConnected bool
	connMutex   sync.RWMutex
}

// ConnectOptions configures the BLE connection
type ConnectOptions struct {
	DeviceAddress  string
	ConnectTimeout time.Duration
	ServiceUUID    *ble.UUID // Optional: custom service UUID
	TxCharUUID     *ble.UUID // Optional: custom TX characteristic UUID
	RxCharUUID     *ble.UUID // Optional: custom RX characteristic UUID
}

// DefaultConnectOptions returns sensible defaults for BLE serial connection
func DefaultConnectOptions(deviceAddress string) *ConnectOptions {
	return &ConnectOptions{
		DeviceAddress:  deviceAddress,
		ConnectTimeout: 30 * time.Second,
		ServiceUUID:    &SerialServiceUUID,
		TxCharUUID:     &SerialTxCharUUID,
		RxCharUUID:     &SerialRxCharUUID,
	}
}

// NewConnection creates a new BLE serial connection
func NewConnection(opts *ConnectOptions, logger *logrus.Logger) *Connection {
	if logger == nil {
		logger = logrus.New()
	}

	return &Connection{
		logger:      logger,
		isConnected: false,
	}
}

// Connect establishes a connection to the BLE device
func (c *Connection) Connect(ctx context.Context, opts *ConnectOptions) error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	if c.isConnected {
		return fmt.Errorf("already connected")
	}

	// Initialize BLE device for macOS
	d, err := darwin.NewDevice()
	if err != nil {
		return fmt.Errorf("failed to create BLE device: %w", err)
	}
	ble.SetDefaultDevice(d)

	c.logger.WithField("address", opts.DeviceAddress).Info("Connecting to BLE device...")

	// Create connection context with timeout
	connectCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()

	// Connect to device
	client, err := ble.Dial(connectCtx, ble.NewAddr(opts.DeviceAddress))
	if err != nil {
		return fmt.Errorf("failed to connect to device: %w", err)
	}

	c.client = client
	c.logger.Info("Connected to device, discovering services...")

	// Discover services and characteristics
	profile, err := client.DiscoverProfile(true)
	if err != nil {
		client.CancelConnection()
		return fmt.Errorf("failed to discover profile: %w", err)
	}

	c.profile = profile

	// Find the serial service
	var serialService *ble.Service
	for _, service := range profile.Services {
		if service.UUID.Equal(*opts.ServiceUUID) {
			serialService = service
			break
		}
	}

	if serialService == nil {
		client.CancelConnection()
		return fmt.Errorf("serial service %s not found", opts.ServiceUUID.String())
	}

	c.logger.WithField("service", serialService.UUID.String()).Info("Found serial service")

	// Find TX and RX characteristics
	for _, char := range serialService.Characteristics {
		if char.UUID.Equal(*opts.TxCharUUID) {
			c.txChar = char
			c.logger.WithField("characteristic", char.UUID.String()).Info("Found TX characteristic")
		} else if char.UUID.Equal(*opts.RxCharUUID) {
			c.rxChar = char
			c.logger.WithField("characteristic", char.UUID.String()).Info("Found RX characteristic")
		}
	}

	if c.txChar == nil {
		client.CancelConnection()
		return fmt.Errorf("TX characteristic %s not found", opts.TxCharUUID.String())
	}

	if c.rxChar == nil {
		client.CancelConnection()
		return fmt.Errorf("RX characteristic %s not found", opts.RxCharUUID.String())
	}

	// Subscribe to TX characteristic for receiving data
	if err := client.Subscribe(c.txChar, false, c.handleNotification); err != nil {
		client.CancelConnection()
		return fmt.Errorf("failed to subscribe to TX characteristic: %w", err)
	}

	c.isConnected = true
	c.logger.Info("BLE serial connection established successfully")

	return nil
}

// Write sends data to the device via the RX characteristic
func (c *Connection) Write(data []byte) error {
	c.connMutex.RLock()
	connected := c.isConnected
	c.connMutex.RUnlock()

	if !connected {
		return fmt.Errorf("not connected")
	}

	if c.rxChar == nil {
		return fmt.Errorf("RX characteristic not available")
	}

	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()

	// Split large writes into chunks (BLE typically has ~20 byte MTU limit)
	maxChunkSize := 20
	for len(data) > 0 {
		chunkSize := len(data)
		if chunkSize > maxChunkSize {
			chunkSize = maxChunkSize
		}

		chunk := data[:chunkSize]
		data = data[chunkSize:]

		if err := c.client.WriteCharacteristic(c.rxChar, chunk, false); err != nil {
			return fmt.Errorf("failed to write to RX characteristic: %w", err)
		}

		c.logger.WithField("bytes", len(chunk)).Debug("Wrote chunk to device")

		// Small delay between chunks to avoid overwhelming the device
		if len(data) > 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	return nil
}

// SetDataHandler sets the callback function for incoming data
func (c *Connection) SetDataHandler(handler func([]byte)) {
	c.onData = handler
}

// SetDataHandlerWithUUID sets the callback function for incoming data with UUID
func (c *Connection) SetDataHandlerWithUUID(handler func(string, []byte)) {
	c.onData = func(data []byte) {
		// For now, assume TX characteristic for incoming data
		if c.txChar != nil {
			handler(c.txChar.UUID.String(), data)
		}
	}
}

// GetProfile returns the discovered BLE profile
func (c *Connection) GetProfile() *ble.Profile {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()
	return c.profile
}

// WriteToCharacteristic writes data to a specific characteristic by UUID
func (c *Connection) WriteToCharacteristic(uuid string, data []byte) error {
	c.connMutex.RLock()
	connected := c.isConnected
	profile := c.profile
	c.connMutex.RUnlock()

	if !connected {
		return fmt.Errorf("not connected")
	}

	if profile == nil {
		return fmt.Errorf("no profile available")
	}

	// Find the characteristic by UUID
	var targetChar *ble.Characteristic
	for _, service := range profile.Services {
		for _, char := range service.Characteristics {
			if char.UUID.String() == uuid {
				targetChar = char
				break
			}
		}
		if targetChar != nil {
			break
		}
	}

	if targetChar == nil {
		return fmt.Errorf("characteristic %s not found", uuid)
	}

	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()

	// Split large writes into chunks (BLE typically has ~20 byte MTU limit)
	maxChunkSize := 20
	for len(data) > 0 {
		chunkSize := len(data)
		if chunkSize > maxChunkSize {
			chunkSize = maxChunkSize
		}

		chunk := data[:chunkSize]
		data = data[chunkSize:]

		if err := c.client.WriteCharacteristic(targetChar, chunk, false); err != nil {
			return fmt.Errorf("failed to write to characteristic %s: %w", uuid, err)
		}

		c.logger.WithFields(logrus.Fields{
			"uuid":  uuid,
			"bytes": len(chunk),
		}).Debug("Wrote chunk to characteristic")

		// Small delay between chunks to avoid overwhelming the device
		if len(data) > 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	return nil
}

// handleNotification processes incoming data from the TX characteristic
func (c *Connection) handleNotification(data []byte) {
	c.logger.WithField("bytes", len(data)).Debug("Received data from device")

	if c.onData != nil {
		c.onData(data)
	}
}

// IsConnected returns whether the connection is active
func (c *Connection) IsConnected() bool {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()
	return c.isConnected
}

// Disconnect closes the BLE connection
func (c *Connection) Disconnect() error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	if !c.isConnected {
		return fmt.Errorf("not connected")
	}

	if c.client != nil {
		if err := c.client.CancelConnection(); err != nil {
			c.logger.WithError(err).Warn("Error disconnecting from device")
		}
	}

	c.isConnected = false
	c.client = nil
	c.profile = nil
	c.txChar = nil
	c.rxChar = nil

	c.logger.Info("Disconnected from BLE device")
	return nil
}

// GetDeviceInfo returns information about the connected device
func (c *Connection) GetDeviceInfo() map[string]interface{} {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	info := map[string]interface{}{
		"connected": c.isConnected,
	}

	if c.client != nil {
		info["address"] = c.client.Addr().String()
	}

	if c.profile != nil {
		info["service_count"] = len(c.profile.Services)
	}

	return info
}
