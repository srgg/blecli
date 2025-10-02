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

// DeviceFactory creates ble.Device instances (can be overridden in tests)
var DeviceFactory = func() (ble.Device, error) {
	return darwin.NewDevice()
}

// ----------------------------
// UUID Normalization Utilities
// ----------------------------

// normalizeUUID converts a UUID string to the internal BLE library format (lowercase, no dashes)
// Handles both standard UUID format (with dashes) and already normalized format (without dashes)
func normalizeUUID(uuid string) string {
	return strings.ToLower(strings.ReplaceAll(uuid, "-", ""))
}

// normalizeUUIDs normalizes a slice of UUID strings to internal format
func normalizeUUIDs(uuids []string) []string {
	normalized := make([]string, len(uuids))
	for i, uuid := range uuids {
		normalized[i] = normalizeUUID(uuid)
	}
	return normalized
}

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

type StreamMode int

const (
	StreamEveryUpdate StreamMode = iota
	StreamBatched
	StreamAggregated
)

type Record struct {
	TsUs        int64
	Seq         uint64
	Values      map[string][]byte   // Single value per characteristic (EveryUpdate/Aggregated modes)
	BatchValues map[string][][]byte // Multiple values per characteristic (Batched mode)
	Flags       uint32
}

func newRecord(mode StreamMode) *Record {
	r := &Record{
		TsUs: time.Now().UnixMicro(),
	}
	if mode == StreamBatched {
		r.BatchValues = make(map[string][][]byte)
	} else {
		r.Values = make(map[string][]byte)
	}
	return r
}

type LuaSubscription struct {
	Chars    []*BLECharacteristic
	Mode     StreamMode
	MaxRate  time.Duration
	Callback func(*Record)
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

func (c *BLEConnection) GetCharacteristic(service, uuid string) (*BLECharacteristic, error) {
	// Normalize UUIDs for consistent lookup
	normalizedServiceUUID := normalizeUUID(service)
	normalizedCharUUID := normalizeUUID(uuid)

	svc, ok := c.services[normalizedServiceUUID]
	if !ok {
		return nil, fmt.Errorf("service \"%s\" not found", service)
	}

	char, ok := svc.Characteristics[normalizedCharUUID]
	if !ok {
		return nil, fmt.Errorf("characteristic \"%s\" not found in service \"%s\"", uuid, service)
	}

	return char, nil
}

func (c *BLEConnection) GetServices() map[string]Service {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	// TODO: Consider improve interfaces to avoid temporary map
	result := make(map[string]Service, len(c.services))
	for k, v := range c.services {
		result[k] = v // automatic interface conversion
	}
	return result
}

func (c *BLEConnection) GetDevice() Device {
	// BLEConnection doesn't directly hold a device reference
	// This is a limitation of the current architecture
	// For now, return nil to satisfy the interface
	return nil
}

// ProcessCharacteristicNotification processes incoming characteristic notification data
// This method is extracted to allow reuse in both production subscriptions and tests
func (c *BLEConnection) ProcessCharacteristicNotification(char *BLECharacteristic, data []byte) {
	// Create a new BLE value from the received data
	val := newBLEValue(data)

	// Update the characteristic's value
	char.SetValue(data)

	// Enqueue the value for any waiting consumers
	char.EnqueueValue(val)

	// Notify all subscribers
	char.notifySubscribers(val)
}

// SimulateNotification provides a proxy method for testing/simulation capabilities
// This method calls ProcessCharacteristicNotification internally
func (c *BLEConnection) SimulateNotification(char *BLECharacteristic, data []byte) {
	c.ProcessCharacteristicNotification(char, data)
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

// Connect establishes a BLE connection and populates live characteristics
func (c *BLEConnection) Connect(ctx context.Context, address string, opts *ConnectOptions) error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	if strings.TrimSpace(address) == "" {
		return fmt.Errorf("failed connect to device: device address is not set")
	}

	if c.isConnectedInternal() {
		return fmt.Errorf("already connected")
	}

	c.logger.WithField("address", address).Info("Connecting to BLE device...")

	// Create a BLE device using the factory (allows for mocking in tests)
	dev, err := DeviceFactory()
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
		return fmt.Errorf("failed to connect to device with address \"%s\": %w", address, err)
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
		c.logger.WithField("service_uuid", svcUUID).Debug("Found service UUID")
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
			c.logger.WithFields(map[string]interface{}{
				"service_uuid": svcUUID,
				"char_uuid":    uuid,
			}).Debug("Found characteristic UUID")
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

	// Set up context for subscriptions
	c.ctx, c.cancel = context.WithCancel(context.Background())

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
	if err := c.unsubscribeInternal(nil); err != nil {
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

// isConnectedInternal checks the connection status without acquiring locks.
// Should only be called when the caller already holds connMutex.RLock() or connMutex.Lock().
func (c *BLEConnection) isConnectedInternal() bool {
	return c.client != nil && c.isConnected
}

func (c *BLEConnection) IsConnected() bool {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()
	return c.isConnectedInternal()
}

// validateSubscribeOptions validates service and characteristics existence and notification support
func (c *BLEConnection) validateSubscribeOptions(opts *SubscribeOptions, requireNotificationSupport bool) (map[string]*BLECharacteristic, error) {
	// Comprehensive validation - collect ALL issues before failing
	var missingServices []string
	var missingChars []string
	var unsupportedChars []string
	characteristicsToProcess := make(map[string]*BLECharacteristic)

	// Normalize UUIDs for consistent lookup (BLE library uses lowercase, no dashes)
	normalizedServiceUUID := normalizeUUID(opts.Service)
	normalizedCharUUIDs := normalizeUUIDs(opts.Characteristics)

	// Validate service exists using normalized UUID
	service, serviceExists := c.services[normalizedServiceUUID]
	if !serviceExists {
		missingServices = append(missingServices, opts.Service)
	} else {
		// Service exists, now validate characteristics
		if len(opts.Characteristics) == 0 {
			// Validate all characteristics in service
			for charUUID, char := range service.Characteristics {
				if char.BLEChar == nil {
					missingChars = append(missingChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.Service))
				} else if requireNotificationSupport && char.BLEChar.Property&ble.CharNotify == 0 && char.BLEChar.Property&ble.CharIndicate == 0 {
					unsupportedChars = append(unsupportedChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.Service))
				} else {
					characteristicsToProcess[charUUID] = char
				}
			}
		} else {
			// Validate specific requested characteristics using normalized UUIDs
			for i, charUUID := range opts.Characteristics {
				normalizedCharUUID := normalizedCharUUIDs[i]
				char, charExists := service.Characteristics[normalizedCharUUID]
				if !charExists {
					missingChars = append(missingChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.Service))
				} else if char.BLEChar == nil {
					missingChars = append(missingChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.Service))
				} else if requireNotificationSupport && char.BLEChar.Property&ble.CharNotify == 0 && char.BLEChar.Property&ble.CharIndicate == 0 {
					unsupportedChars = append(unsupportedChars, fmt.Sprintf("%s (in service %s)", charUUID, opts.Service))
				} else {
					characteristicsToProcess[normalizedCharUUID] = char
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

func (c *BLEConnection) BLESubscribe(opts *SubscribeOptions) error {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	// Check if connected
	if !c.isConnectedInternal() {
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
		return fmt.Errorf("no characteristics available for subscription in service %s", opts.Service)
	}

	// All validation passed - proceed with subscriptions
	var subscriptionErrors []string
	for charUUID, char := range characteristicsToSubscribe {
		// create a local variable to capture the current char
		charCapture := char
		err := c.client.Subscribe(char.BLEChar, false, func(data []byte) {
			c.ProcessCharacteristicNotification(charCapture, data)
		})

		if err != nil {
			subscriptionErrors = append(subscriptionErrors, fmt.Sprintf("%s: %v", charUUID, err))
			if c.logger != nil {
				c.logger.WithFields(map[string]interface{}{
					"serviceUUID": opts.Service,
					"charUUID":    charUUID,
					"error":       err,
				}).Error("Failed to subscribe to characteristic notifications")
			}
		} else {
			if c.logger != nil {
				c.logger.WithFields(map[string]interface{}{
					"serviceUUID": opts.Service,
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

	return c.unsubscribeInternal(opts)
}

// unsubscribeInternal performs unsubscribe operations without acquiring locks
// Should only be called when the caller already holds the appropriate lock
func (c *BLEConnection) unsubscribeInternal(opts *SubscribeOptions) error {
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
		return fmt.Errorf("no characteristics available for unsubscribe in service %s", opts.Service)
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
					"serviceUUID": opts.Service,
					"charUUID":    charUUID,
					"notifyErr":   err1,
					"indicateErr": err2,
				}).Error("Failed to unsubscribe from characteristic notifications")
			}
		} else {
			if c.logger != nil {
				c.logger.WithFields(map[string]interface{}{
					"serviceUUID": opts.Service,
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

// Subscribe subscribes to notifications from multiple services and characteristics with streaming patterns.
// Supports advanced subscription with streaming patterns and callbacks:
//
//	connection.Subscribe([]*SubscribeOptions{
//	  { ServiceUUID: "0000180d-0000-1000-8000-00805f9b34fb", Characteristics: []string{"00002a37-0000-1000-8000-00805f9b34fb"} },
//	  { ServiceUUID: "1000180d-0000-1000-8000-00805f9b34fb", Characteristics: []string{"10002a37-0000-1000-8000-00805f9b34fb"} }
//	}, StreamEveryUpdate, 0, func(record *Record) { ... })
func (c *BLEConnection) Subscribe(opts []*SubscribeOptions, mode StreamMode, maxRate time.Duration, callback func(*Record)) error {
	c.logger.WithFields(map[string]interface{}{
		"services": len(opts),
		"mode":     mode,
	}).Debug("Subscribe called - about to create goroutine")

	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	// Check if connected (we already hold the lock, so use safe version)
	if !c.isConnectedInternal() {
		return fmt.Errorf("device disconnected - reconnect before subscribing to Lua notifications")
	}

	// Check if any services are specified
	if len(opts) == 0 {
		return fmt.Errorf("no services specified in Lua subscription")
	}

	// Check if callback is provided
	if callback == nil {
		return fmt.Errorf("no callback specified in Lua subscription")
	}

	// Validate subscription options and get characteristics from all services
	var allCharacteristics []*BLECharacteristic
	for _, opt := range opts {
		characteristicsToSubscribe, err := c.validateSubscribeOptions(opt, true)
		if err != nil {
			return fmt.Errorf("Lua subscription %w", err)
		}

		// Convert validated BLECharacteristics for LuaSubscription
		for _, bleChar := range characteristicsToSubscribe {
			allCharacteristics = append(allCharacteristics, bleChar)
		}
	}

	// If no characteristics support notifications after validation
	if len(allCharacteristics) == 0 {
		return fmt.Errorf("no characteristics available for Lua subscription across all specified services")
	}

	sub := &LuaSubscription{
		Chars:    allCharacteristics,
		Mode:     mode,
		MaxRate:  maxRate,
		Callback: callback,
	}

	c.subscriptions = append(c.subscriptions, sub)

	go c.runLuaSubscription(sub)

	return nil
}

func (c *BLEConnection) runLuaSubscription(sub *LuaSubscription) {
	// Only create ticker for modes that need rate limiting
	var ticker *time.Ticker
	if sub.Mode == StreamBatched || sub.Mode == StreamAggregated {
		if sub.MaxRate <= 0 {
			// Default to 100ms for batched/aggregated modes if MaxRate is 0 or negative
			sub.MaxRate = 100 * time.Millisecond
		}
		ticker = time.NewTicker(sub.MaxRate)
		defer ticker.Stop()
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-func() <-chan time.Time {
			if ticker != nil {
				return ticker.C
			}
			// Return a channel that never fires for EveryUpdate mode
			return make(chan time.Time)
		}():
			if sub.Mode == StreamBatched {
				record := newRecord(StreamBatched)
				for _, c := range sub.Chars {
					// Drain all available updates for this characteristic
					for {
						select {
						case val := <-c.updates:
							record.BatchValues[c.GetUUID()] = append(record.BatchValues[c.GetUUID()], val.Data)
							if val.Flags != 0 {
								record.Flags |= val.Flags
							}
							record.TsUs = val.TsUs
							releaseBLEValue(val)
						default:
							goto nextChar
						}
					}
				nextChar:
				}
				// Only invoke callback when there's actual data to report
				if len(record.BatchValues) > 0 {
					sub.Callback(record)
				}
			}
			if sub.Mode == StreamAggregated {
				record := newRecord(StreamAggregated)
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
				// Only invoke callback when there's actual data to report
				// Skip empty aggregation ticks to avoid JSON serialization issues with empty Values
				if len(record.Values) > 0 {
					sub.Callback(record)
				}
			}
		default:
			if sub.Mode == StreamEveryUpdate {
				for _, c := range sub.Chars {
					select {
					case val := <-c.updates:
						record := newRecord(StreamEveryUpdate)
						record.Values[c.GetUUID()] = val.Data
						record.TsUs = val.TsUs
						if val.Flags != 0 {
							record.Flags |= val.Flags
						}
						sub.Callback(record)
						releaseBLEValue(val)
					default:
					}
				}
			} else {
				time.Sleep(time.Millisecond)
			}
		}
	}
}
