package device

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/darwin"
	"github.com/sirupsen/logrus"
)

// ----------------------------
// Flags
// ----------------------------
const (
	FlagDropped uint32 = 1 << iota
	FlagMissing
)

// ----------------------------
// BLEValue with Pooling
// ----------------------------

type BLEValue struct {
	TsUs  int64
	Data  []byte
	Seq   uint64
	Flags uint32
}

var valuePool = sync.Pool{
	New: func() interface{} { return &BLEValue{Data: make([]byte, 256)} },
}

func newBLEValue(data []byte) *BLEValue {
	v := valuePool.Get().(*BLEValue)
	v.TsUs = time.Now().UnixMicro()
	v.Seq++
	v.Flags = 0
	if cap(v.Data) < len(data) {
		v.Data = make([]byte, len(data))
	}
	v.Data = v.Data[:len(data)]
	copy(v.Data, data)
	return v
}

func releaseBLEValue(v *BLEValue) {
	valuePool.Put(v)
}

// ----------------------------
// BLECharacteristic
// ----------------------------

type BLECharacteristic struct {
	uuid        string
	properties  string
	descriptors []Descriptor
	value       []byte
	BLEChar     *ble.Characteristic

	updates chan *BLEValue
	mu      sync.RWMutex
	subs    []func(*BLEValue)
}

func NewCharacteristic(c *ble.Characteristic, buffer int) *BLECharacteristic {
	return &BLECharacteristic{
		uuid:        c.UUID.String(),
		BLEChar:     c,
		properties:  blePropsToString(c.Property),
		updates:     make(chan *BLEValue, buffer),
		descriptors: []Descriptor{},
		subs:        nil,
	}
}

func (c *BLECharacteristic) EnqueueValue(v *BLEValue) {
	select {
	case c.updates <- v:
	default:
		// Channel full, drop the oldest
		old := <-c.updates
		old.Flags |= FlagDropped
		releaseBLEValue(old)
		c.updates <- v
	}
}

func (c *BLECharacteristic) Subscribe(fn func(*BLEValue)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = append(c.subs, fn)
}

func (c *BLECharacteristic) notifySubscribers(v *BLEValue) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, fn := range c.subs {
		fn(v)
	}
}

func (c *BLECharacteristic) GetUUID() string {
	return c.uuid
}

func (c *BLECharacteristic) GetProperties() string {
	return c.properties
}

func (c *BLECharacteristic) GetDescriptors() []Descriptor {
	return c.descriptors
}

func (c *BLECharacteristic) GetValue() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}

func (c *BLECharacteristic) SetValue(value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = value
}

// ----------------------------
// BLE Service
// ----------------------------

// BLEService represents a GATT service and its characteristics
type BLEService struct {
	UUID            string
	Characteristics map[string]*BLECharacteristic
}

func (s *BLEService) GetUUID() string {
	return s.UUID
}

func (s *BLEService) GetCharacteristics() []Characteristic {
	result := make([]Characteristic, 0, len(s.Characteristics))
	for _, char := range s.Characteristics {
		result = append(result, char)
	}
	return result
}

// ----------------------------
// Lua Subscription
// ----------------------------

type StreamPattern int

const (
	StreamEveryUpdate StreamPattern = iota
	StreamBatched
	StreamAggregated
)

type Record struct {
	TsUs   int64
	Seq    uint64
	Values map[string][]byte
	Flags  uint32
}

var recordPool = sync.Pool{
	New: func() interface{} {
		return &Record{Values: make(map[string][]byte)}
	},
}

func newRecord() *Record {
	r := recordPool.Get().(*Record)
	r.TsUs = time.Now().UnixMicro()
	r.Seq++
	r.Flags = 0
	for k := range r.Values {
		delete(r.Values, k)
	}
	return r
}

func releaseRecord(r *Record) {
	for k := range r.Values {
		delete(r.Values, k)
	}
	recordPool.Put(r)
}

type LuaSubscription struct {
	Chars    []*BLECharacteristic
	Pattern  StreamPattern
	MaxRate  time.Duration
	Callback func(*Record)
	buffer   []*BLEValue
}

// ----------------------------
// BLE Connection
// ----------------------------

// BLEConnection represents a live BLE connection (notifications, writes)
type BLEConnection struct {
	client      ble.Client
	logger      *logrus.Logger
	writeMutex  sync.Mutex
	connMutex   sync.RWMutex
	isConnected bool

	services map[string]*BLEService

	subscriptions []*LuaSubscription
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewBLEConnection(logger *logrus.Logger) *BLEConnection {
	return &BLEConnection{
		client:        nil,
		services:      make(map[string]*BLEService),
		subscriptions: nil,
		ctx:           nil,
		cancel:        nil,
		logger:        logger,
	}
}

//func (m *BLEConnection) Connect(addr string, charUUIDs []string) error {
//	ctx := ble.WithSigHandler(context.WithTimeout(m.ctx, 30*time.Second))
//	cln, err := ble.Dial(ctx, ble.NewAddr(addr))
//	if err != nil {
//		return err
//	}
//	m.client = cln
//
//	for _, uuid := range charUUIDs {
//		c := NewCharacteristic(uuid, 128)
//		m.characteristics[uuid] = c
//
//		char, err := cln.DiscoverCharacteristic(ble.MustParse(uuid))
//		if err != nil {
//			return err
//		}
//
//		err = cln.Subscribe(char, false, func(req []byte) {
//			val := newBLEValue(req)
//			c.EnqueueValue(val)
//			c.notifySubscribers(val)
//		})
//		if err != nil {
//			return err
//		}
//	}
//
//	return nil
//}

// Connect establishes a BLE connection and populates live characteristics
func (c *BLEConnection) Connect(ctx context.Context, address string, opts *ConnectOptions) error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	if strings.TrimSpace(address) == "" {
		return fmt.Errorf("failed connect to device: device address is not set")
	}

	if c.IsConnected() {
		return fmt.Errorf("already connected")
	}

	c.logger.WithField("address", address).Info("Connecting to BLE device...")

	// Create a platform BLE device (darwin for macOS)
	dev, err := darwin.NewDevice()
	if err != nil {
		return fmt.Errorf("failed to create BLE device: %w", err)
	}
	ble.SetDefaultDevice(dev)

	// Timeout context
	connCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()

	// Connect to BLE device
	client, err := ble.Dial(connCtx, ble.NewAddr(address))
	if err != nil {
		return fmt.Errorf("failed to connect to device: %w", err)
	}

	// Discover services and characteristics
	bleProfile, err := client.DiscoverProfile(true)
	if err != nil {
		client.CancelConnection()
		return fmt.Errorf("failed to discover profile: %w", err)
	}

	// Populate services and characteristics from BLE Profile
	for _, bleSvc := range bleProfile.Services {
		svcUUID := bleSvc.UUID.String()
		svc, ok := c.services[svcUUID]
		if !ok {
			svc = &BLEService{
				UUID:            svcUUID,
				Characteristics: make(map[string]*BLECharacteristic),
			}
			c.services[svcUUID] = svc
		}

		for _, bleCharacteristic := range bleSvc.Characteristics {
			uuid := bleCharacteristic.UUID.String()
			characteristic, ok := svc.Characteristics[uuid]
			if !ok {
				// Create BLECharacteristic
				characteristic = NewCharacteristic(bleCharacteristic, 128)
				svc.Characteristics[uuid] = characteristic
			} else {
				// Update live handle
				characteristic.BLEChar = bleCharacteristic
			}
		}
	}

	//// Subscribe to notify/indicate characteristics
	//for _, bleChar := range d.bleCharacteristics {
	//	if bleChar.BLEChar == nil {
	//		continue
	//	}
	//	if bleChar.BLEChar.Property&ble.CharNotify != 0 || bleChar.BLEChar.Property&ble.CharIndicate != 0 {
	//		uuid := bleChar.GetUUID()
	//		d.logger.WithField("uuid", uuid).Info("Subscribing to notifications")
	//		err := client.Subscribe(bleChar.BLEChar, false, func(data []byte) {
	//			bleChar.mu.Lock()
	//			bleChar.value = data
	//			bleChar.mu.Unlock()
	//			if d.onData != nil {
	//				d.onData(uuid, data)
	//			}
	//		})
	//		if err != nil {
	//			d.logger.WithFields(logrus.Fields{
	//				"uuid":  uuid,
	//				"error": err,
	//			}).Warn("Failed to subscribe to characteristic")
	//		}
	//	}
	//}

	// Mark as connected and assign client
	c.client = client
	c.isConnected = true

	c.logger.WithField("services", len(c.services)).Info("BLE device connected")
	return nil
}

func (c *BLEConnection) Disconnect() error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	// Check if already disconnected
	if c.client == nil || !c.isConnected {
		if c.logger != nil {
			c.logger.Info("Already disconnected")
		}
		return nil
	}

	if c.logger != nil {
		c.logger.Info("Disconnecting BLE device...")
	}

	// First, unsubscribe from all subscriptions to clean up properly
	if err := c.Unsubscribe(nil); err != nil {
		if c.logger != nil {
			c.logger.WithField("error", err).Warn("Failed to unsubscribe from all characteristics during disconnect")
		}
		// Don't return error here - continue with disconnect even if unsubscribe fails
	}

	// Cancel context to stop any ongoing operations
	c.cancel()

	// Close the BLE connection
	var disconnectErr error
	if c.client != nil {
		disconnectErr = c.client.CancelConnection()
		c.client = nil
	}

	// Mark as disconnected
	c.isConnected = false

	if c.logger != nil {
		if disconnectErr != nil {
			c.logger.WithField("error", disconnectErr).Warn("BLE device disconnected with errors")
		} else {
			c.logger.Info("BLE device disconnected successfully")
		}
	}

	return disconnectErr
}

func (c *BLEConnection) IsConnected() bool {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()
	return c.client != nil && c.isConnected
}

// validateSubscribeOptions validates service and characteristics existence and notification support
func (c *BLEConnection) validateSubscribeOptions(opts *SubscribeOptions, requireNotificationSupport bool) (map[string]*BLECharacteristic, error) {
	// Comprehensive validation - collect ALL issues before failing
	var missingServices []string
	var missingChars []string
	var unsupportedChars []string
	characteristicsToProcess := make(map[string]*BLECharacteristic)

	// Validate service exists
	service, serviceExists := c.services[opts.ServiceUUID]
	if !serviceExists {
		missingServices = append(missingServices, opts.ServiceUUID)
	} else {
		// Service exists, now validate characteristics
		if len(opts.Characteristics) == 0 {
			// Validate all characteristics in service
			for charUUID, char := range service.Characteristics {
				if char.BLEChar == nil {
					missingChars = append(missingChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.ServiceUUID))
				} else if requireNotificationSupport && char.BLEChar.Property&ble.CharNotify == 0 && char.BLEChar.Property&ble.CharIndicate == 0 {
					unsupportedChars = append(unsupportedChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.ServiceUUID))
				} else {
					characteristicsToProcess[charUUID] = char
				}
			}
		} else {
			// Validate specific requested characteristics
			for _, charUUID := range opts.Characteristics {
				char, charExists := service.Characteristics[charUUID]
				if !charExists {
					missingChars = append(missingChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.ServiceUUID))
				} else if char.BLEChar == nil {
					missingChars = append(missingChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.ServiceUUID))
				} else if requireNotificationSupport && char.BLEChar.Property&ble.CharNotify == 0 && char.BLEChar.Property&ble.CharIndicate == 0 {
					unsupportedChars = append(unsupportedChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.ServiceUUID))
				} else {
					characteristicsToProcess[charUUID] = char
				}
			}
		}
	}

	// Report comprehensive validation results
	if len(missingServices) > 0 || len(missingChars) > 0 || len(unsupportedChars) > 0 {
		var errorParts []string

		if len(missingServices) > 0 {
			errorParts = append(errorParts, fmt.Sprintf("missing services: %s", strings.Join(missingServices, ", ")))
		}
		if len(missingChars) > 0 {
			errorParts = append(errorParts, fmt.Sprintf("missing characteristics: %s", strings.Join(missingChars, ", ")))
		}
		if len(unsupportedChars) > 0 {
			errorParts = append(errorParts, fmt.Sprintf("characteristics without notification support: %s", strings.Join(unsupportedChars, ", ")))
		}

		return nil, fmt.Errorf("validation failed - %s", strings.Join(errorParts, "; "))
	}

	return characteristicsToProcess, nil
}

func (c *BLEConnection) Subscribe(opts *SubscribeOptions) error {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	// Check if connected
	if !c.IsConnected() {
		if c.client == nil {
			return fmt.Errorf("device not connected - establish connection before subscribing to notifications")
		}
		return fmt.Errorf("device disconnected - reconnect before subscribing to notifications")
	}

	// Validate subscription options and get characteristics
	characteristicsToSubscribe, err := c.validateSubscribeOptions(opts, true)
	if err != nil {
		return fmt.Errorf("subscription %w", err)
	}

	// If no characteristics support notifications after validation
	if len(characteristicsToSubscribe) == 0 {
		return fmt.Errorf("no characteristics available for subscription in service %s", opts.ServiceUUID)
	}

	// All validation passed - proceed with subscriptions
	var subscriptionErrors []string
	for charUUID, char := range characteristicsToSubscribe {
		err := c.client.Subscribe(char.BLEChar, false, func(data []byte) {
			// Create a new BLE value from the received data
			val := newBLEValue(data)

			// Update the characteristic's value
			char.SetValue(data)

			// Enqueue the value for any waiting consumers
			char.EnqueueValue(val)

			// Notify all subscribers
			char.notifySubscribers(val)
		})

		if err != nil {
			subscriptionErrors = append(subscriptionErrors, fmt.Sprintf("%s: %v", charUUID, err))
			if c.logger != nil {
				c.logger.WithFields(map[string]interface{}{
					"serviceUUID": opts.ServiceUUID,
					"charUUID":    charUUID,
					"error":       err,
				}).Error("Failed to subscribe to characteristic notifications")
			}
		} else {
			if c.logger != nil {
				c.logger.WithFields(map[string]interface{}{
					"serviceUUID": opts.ServiceUUID,
					"charUUID":    charUUID,
				}).Info("Successfully subscribed to characteristic notifications")
			}
		}
	}

	// Return error if any subscriptions failed
	if len(subscriptionErrors) > 0 {
		return fmt.Errorf("subscription failures - %s", strings.Join(subscriptionErrors, "; "))
	}

	return nil
}

func (c *BLEConnection) Unsubscribe(opts *SubscribeOptions) error {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	// Allow unsubscribe if client exists (for cleanup during disconnect)
	if c.client == nil {
		return fmt.Errorf("unsubscribe unavailable - no active connection")
	}

	// Handle unsubscribe from all subscriptions when opts is nil
	if opts == nil {
		var unsubscribeErrors []string

		// Unsubscribe from all characteristics in all services
		for serviceUUID, service := range c.services {
			for charUUID, char := range service.Characteristics {
				if char.BLEChar != nil {
					// Try both notify and indicate unsubscribe
					err1 := c.client.Unsubscribe(char.BLEChar, false) // notify
					err2 := c.client.Unsubscribe(char.BLEChar, true)  // indicate

					if err1 != nil && err2 != nil {
						unsubscribeErrors = append(unsubscribeErrors, fmt.Sprintf("%s (in service %s): notify=%v, indicate=%v", charUUID, serviceUUID, err1, err2))
					} else {
						if c.logger != nil {
							c.logger.WithFields(map[string]interface{}{
								"serviceUUID": serviceUUID,
								"charUUID":    charUUID,
							}).Info("Successfully unsubscribed from characteristic notifications")
						}
					}
				}
			}
		}

		if len(unsubscribeErrors) > 0 {
			return fmt.Errorf("unsubscribe failures - %s", strings.Join(unsubscribeErrors, "; "))
		}

		if c.logger != nil {
			c.logger.Info("Successfully unsubscribed from all characteristic notifications")
		}
		return nil
	}

	// Validate specific subscription options (don't require notification support for unsubscribe)
	characteristicsToUnsubscribe, err := c.validateSubscribeOptions(opts, false)
	if err != nil {
		return fmt.Errorf("unsubscribe %w", err)
	}

	// If no characteristics found after validation
	if len(characteristicsToUnsubscribe) == 0 {
		return fmt.Errorf("no characteristics available for unsubscribe in service %s", opts.ServiceUUID)
	}

	// All validation passed - proceed with unsubscriptions
	var unsubscribeErrors []string
	for charUUID, char := range characteristicsToUnsubscribe {
		// Try both notify and indicate unsubscribe
		err1 := c.client.Unsubscribe(char.BLEChar, false) // notify
		err2 := c.client.Unsubscribe(char.BLEChar, true)  // indicate

		// Only report error if both notify and indicate failed
		if err1 != nil && err2 != nil {
			unsubscribeErrors = append(unsubscribeErrors, fmt.Sprintf("%s: notify=%v, indicate=%v", charUUID, err1, err2))
			if c.logger != nil {
				c.logger.WithFields(map[string]interface{}{
					"serviceUUID": opts.ServiceUUID,
					"charUUID":    charUUID,
					"notifyErr":   err1,
					"indicateErr": err2,
				}).Error("Failed to unsubscribe from characteristic notifications")
			}
		} else {
			if c.logger != nil {
				c.logger.WithFields(map[string]interface{}{
					"serviceUUID": opts.ServiceUUID,
					"charUUID":    charUUID,
				}).Info("Successfully unsubscribed from characteristic notifications")
			}
		}
	}

	// Return error if any unsubscriptions failed
	if len(unsubscribeErrors) > 0 {
		return fmt.Errorf("unsubscribe failures - %s", strings.Join(unsubscribeErrors, "; "))
	}

	return nil
}

func (c *BLEConnection) SubscribeLua(opts *SubscribeOptions, pattern StreamPattern, maxRate time.Duration, callback func(*Record)) error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	// Check if connected
	if !c.IsConnected() {
		return fmt.Errorf("device disconnected - reconnect before subscribing to Lua notifications")
	}

	// Validate subscription options and get characteristics
	characteristicsToSubscribe, err := c.validateSubscribeOptions(opts, true)
	if err != nil {
		return fmt.Errorf("Lua subscription %w", err)
	}

	// If no characteristics support notifications after validation
	if len(characteristicsToSubscribe) == 0 {
		return fmt.Errorf("no characteristics available for Lua subscription in service %s", opts.ServiceUUID)
	}

	// Convert validated BLECharacteristics for LuaSubscription
	var charObjs []*BLECharacteristic
	for _, bleChar := range characteristicsToSubscribe {
		charObjs = append(charObjs, bleChar)
	}

	sub := &LuaSubscription{
		Chars:    charObjs,
		Pattern:  pattern,
		MaxRate:  maxRate,
		Callback: callback,
		buffer:   make([]*BLEValue, 0, 128),
	}

	c.subscriptions = append(c.subscriptions, sub)

	go c.runLuaSubscription(sub)

	return nil
}

func (c *BLEConnection) runLuaSubscription(sub *LuaSubscription) {
	ticker := time.NewTicker(sub.MaxRate)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if sub.Pattern == StreamBatched && len(sub.buffer) > 0 {
				record := newRecord()
				for _, val := range sub.buffer {
					record.Values[valUUID(val)] = val.Data
					if val.Flags != 0 {
						record.Flags |= val.Flags
					}
					record.TsUs = val.TsUs
					releaseBLEValue(val)
				}
				sub.buffer = sub.buffer[:0]
				sub.Callback(record)
				releaseRecord(record)
			}
			if sub.Pattern == StreamAggregated {
				record := newRecord()
				for _, c := range sub.Chars {
					select {
					case val := <-c.updates:
						record.Values[c.GetUUID()] = val.Data
						if val.Flags != 0 {
							record.Flags |= val.Flags
						}
						record.TsUs = val.TsUs
						releaseBLEValue(val)
					default:
						record.Flags |= FlagMissing
					}
				}
				sub.Callback(record)
				releaseRecord(record)
			}
		default:
			if sub.Pattern == StreamEveryUpdate {
				for _, c := range sub.Chars {
					select {
					case val := <-c.updates:
						record := newRecord()
						record.Values[c.GetUUID()] = val.Data
						record.TsUs = val.TsUs
						if val.Flags != 0 {
							record.Flags |= val.Flags
						}
						sub.Callback(record)
						releaseBLEValue(val)
						releaseRecord(record)
					default:
					}
				}
			} else {
				time.Sleep(time.Millisecond)
			}
		}
	}
}

func valUUID(v *BLEValue) string {
	// Replace with real UUID mapping if needed
	return "uuid-placeholder"
}
