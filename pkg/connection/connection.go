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

// Connection represents a BLE connection to a device
type Connection struct {
	client          ble.Client
	profile         *ble.Profile
	service         *ble.Service
	characteristics map[string]*ble.Characteristic // UUID -> characteristic
	subscribed      []*ble.Characteristic          // Track subscribed characteristics
	logger          *logrus.Logger
	onData          func(string, []byte) // UUID, data
	writeMutex      sync.Mutex
	isConnected     bool
	connMutex       sync.RWMutex
}

// ConnectOptions configures the BLE connection
type ConnectOptions struct {
	DeviceAddress  string
	ConnectTimeout time.Duration
	ServiceUUID    *ble.UUID // Required: service UUID to work with
}

// DefaultConnectOptions returns sensible defaults for BLE connection
func DefaultConnectOptions(deviceAddress string) *ConnectOptions {
	return &ConnectOptions{
		DeviceAddress:  deviceAddress,
		ConnectTimeout: 30 * time.Second,
		ServiceUUID:    &SerialServiceUUID,
	}
}

// NewConnection creates a new BLE serial connection
func NewConnection(opts *ConnectOptions, logger *logrus.Logger) *Connection {
	if logger == nil {
		logger = logrus.New()
	}

	return &Connection{
		logger:          logger,
		isConnected:     false,
		characteristics: make(map[string]*ble.Characteristic),
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

	// Monitor parent context cancellation during connection
	go func() {
		select {
		case <-ctx.Done():
			c.logger.Debug("Parent context cancelled during connection, cancelling dial")
			cancel() // Cancel the connection attempt
		case <-connectCtx.Done():
			// Connection completed or timed out normally
		}
	}()

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

	// Find the target service
	var targetService *ble.Service
	for _, service := range profile.Services {
		if service.UUID.Equal(*opts.ServiceUUID) {
			targetService = service
			break
		}
	}

	if targetService == nil {
		client.CancelConnection()
		return fmt.Errorf("service %s not found", opts.ServiceUUID.String())
	}

	c.service = targetService
	c.logger.WithField("service", targetService.UUID.String()).Info("Found service")

	// Collect all characteristics and subscribe to notification-capable ones
	for _, char := range targetService.Characteristics {
		c.characteristics[char.UUID.String()] = char
		c.logger.WithField("characteristic", char.UUID.String()).Info("Found characteristic")

		// Subscribe to characteristics that support notifications or indications
		if char.Property&ble.CharNotify != 0 || char.Property&ble.CharIndicate != 0 {
			c.logger.WithField("characteristic", char.UUID.String()).Info("Subscribing to notifications")
			if err := client.Subscribe(char, false, func(data []byte) {
				c.handleNotification(char.UUID.String(), data)
			}); err != nil {
				c.logger.WithFields(logrus.Fields{
					"characteristic": char.UUID.String(),
					"error":          err,
				}).Warn("Failed to subscribe to characteristic, continuing...")
			} else {
				c.subscribed = append(c.subscribed, char)
			}
		}
	}

	c.isConnected = true
	c.logger.WithFields(logrus.Fields{
		"service":         targetService.UUID.String(),
		"characteristics": len(c.characteristics),
	}).Info("BLE connection established successfully")

	return nil
}

// SetDataHandler sets the callback function for incoming data (legacy, no UUID)
func (c *Connection) SetDataHandler(handler func([]byte)) {
	c.onData = func(uuid string, data []byte) {
		handler(data) // Ignore UUID for legacy compatibility
	}
}

// SetDataHandlerWithUUID sets the callback function for incoming data with UUID
func (c *Connection) SetDataHandlerWithUUID(handler func(string, []byte)) {
	c.onData = handler
}

// GetCharacteristics returns all available characteristics for the connected service
func (c *Connection) GetCharacteristics() map[string]*ble.Characteristic {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()
	return c.characteristics
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

// handleNotification processes incoming data from any characteristic
func (c *Connection) handleNotification(uuid string, data []byte) {
	// Use trace level to avoid spam in debug logs
	if c.logger.Level >= logrus.TraceLevel {
		c.logger.WithFields(logrus.Fields{
			"uuid":  uuid,
			"bytes": len(data),
		}).Trace("Received data from characteristic")
	}

	if c.onData != nil {
		c.onData(uuid, data)
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
	c.logger.Debug("=== DISCONNECT START ===")
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	if !c.isConnected {
		c.logger.Debug("Already disconnected, returning")
		return nil
	}

	if c.client != nil {
		// First unsubscribe from all notifications and indications
		for _, char := range c.subscribed {
			// Try unsubscribing from notifications
			if err := c.client.Unsubscribe(char, false); err != nil {
				c.logger.WithFields(logrus.Fields{
					"characteristic": char.UUID.String(),
					"type":           "notification",
					"error":          err,
				}).Debug("Failed to unsubscribe from notifications")
			}
			// Try unsubscribing from indications
			if err := c.client.Unsubscribe(char, true); err != nil {
				c.logger.WithFields(logrus.Fields{
					"characteristic": char.UUID.String(),
					"type":           "indication",
					"error":          err,
				}).Debug("Failed to unsubscribe from indications")
			}
		}
		c.subscribed = nil

		c.logger.Debug("Starting CancelConnection...")
		if err := c.client.CancelConnection(); err != nil {
			c.logger.WithError(err).Error("CancelConnection failed")
		} else {
			c.logger.Debug("CancelConnection completed")
		}
	}

	c.isConnected = false
	c.client = nil
	c.profile = nil
	c.service = nil
	c.characteristics = make(map[string]*ble.Characteristic)
	c.subscribed = nil

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
