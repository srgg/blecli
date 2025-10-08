package device

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/darwin"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/bledb"
)

// DeviceFactory creates ble.Device instances (can be overridden in tests)
var DeviceFactory = func() (ble.Device, error) {
	dev, err := darwin.NewDevice()
	if err != nil {
		// Wrap Bluetooth state errors with clearer messages
		if strings.Contains(err.Error(), "central manager has invalid state") {
			if strings.Contains(err.Error(), "have=4") { // StatePoweredOff
				return nil, fmt.Errorf("Bluetooth is turned off - please enable Bluetooth and retry")
			}
			return nil, fmt.Errorf("Bluetooth is not ready - %w", err)
		}
		return nil, err
	}
	return dev, nil
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

// BLEValue represents a BLE notification value.
// IMPORTANT: BLEValue objects are pooled and reused. The Data slice is only valid
// until the value is released back to the pool. Subscribers MUST copy Data immediately
// if they need to retain it beyond the callback invocation.
type BLEValue struct {
	TsUs  int64
	Data  []byte
	Seq   uint64
	Flags uint32
}

var valuePool = sync.Pool{
	New: func() interface{} { return &BLEValue{Data: make([]byte, 0, 256)} },
}

var globalBLESeq uint64

func newBLEValue(data []byte) *BLEValue {
	v := valuePool.Get().(*BLEValue)
	v.TsUs = time.Now().UnixMicro()
	v.Seq = atomic.AddUint64(&globalBLESeq, 1)
	v.Flags = 0
	if cap(v.Data) < len(data) {
		v.Data = make([]byte, len(data))
	}
	v.Data = v.Data[:len(data)]
	copy(v.Data, data)
	return v
}

func releaseBLEValue(v *BLEValue) {
	// Reset fields to zero to avoid keeping stale data
	v.TsUs = 0
	v.Seq = 0
	v.Flags = 0

	// Prevent keeping large buffers in the pool
	const maxPooledBufferSize = 1024 // 1KB max
	if cap(v.Data) > maxPooledBufferSize {
		// Buffer too large, reallocate to default size
		v.Data = make([]byte, 0, 256)
	} else {
		// Normal size, just reset length
		v.Data = v.Data[:0]
	}

	valuePool.Put(v)
}

// ----------------------------
// BLECharacteristic
// ----------------------------

type BLECharacteristic struct {
	uuid        string
	knownName   string
	properties  string
	descriptors []Descriptor
	value       []byte
	BLEChar     *ble.Characteristic
	connection  *BLEConnection // reference to parent connection for reading

	updates chan *BLEValue
	closed  atomic.Bool
	mu      sync.RWMutex
	subs    []func(*BLEValue)
}

func NewCharacteristic(c *ble.Characteristic, buffer int, conn *BLEConnection) *BLECharacteristic {
	rawUUID := c.UUID.String()
	uuid := normalizeUUID(rawUUID)

	// Populate and sort descriptors
	descriptors := make([]Descriptor, 0, len(c.Descriptors))
	for _, d := range c.Descriptors {
		descRawUUID := d.UUID.String()
		descriptors = append(descriptors, &BLEDescriptor{
			uuid:      normalizeUUID(descRawUUID),
			knownName: bledb.LookupDescriptor(descRawUUID),
		})
	}
	// Sort by UUID for consistent ordering
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].GetUUID() < descriptors[j].GetUUID()
	})

	return &BLECharacteristic{
		uuid:        uuid,                                // store normalized
		knownName:   bledb.LookupCharacteristic(rawUUID), // lookup using raw form if DB expects dashed
		BLEChar:     c,
		properties:  blePropsToString(c.Property),
		updates:     make(chan *BLEValue, buffer),
		descriptors: descriptors,
		subs:        nil,
		connection:  conn,
	}
}

func (c *BLECharacteristic) EnqueueValue(v *BLEValue) {
	// Check if channel is closed before attempting to send
	// This prevents panic from send on closed channel if BLE callbacks fire after shutdown
	if c.closed.Load() {
		releaseBLEValue(v)
		return
	}

	select {
	case c.updates <- v:
	default:
		// Channel full, drop the oldest
		old := <-c.updates
		old.Flags |= FlagDropped
		releaseBLEValue(old)
		// Recheck closed before second send (could have closed while we were dropping)
		if !c.closed.Load() {
			c.updates <- v
		} else {
			releaseBLEValue(v)
		}
	}
}

// Subscribe registers a callback for characteristic notifications.
// IMPORTANT: BLEValue objects are pooled and reused. The callback MUST copy
// v.Data immediately if it needs to retain the data beyond the callback invocation.
// The Data slice becomes invalid after the callback returns.
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

func (c *BLECharacteristic) KnownName() string {
	return c.knownName
}

func (c *BLECharacteristic) GetProperties() string {
	return c.properties
}

func (c *BLECharacteristic) GetDescriptors() []Descriptor {
	return c.descriptors
}

// GetValue returns the current cached value of the characteristic.
// IMPORTANT: The returned slice is READ-ONLY. Callers MUST NOT modify it.
// Modifying the returned slice will cause data races and undefined behavior.
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

// Read reads the current value of the characteristic from the device
func (c *BLECharacteristic) Read() ([]byte, error) {
	if c.connection == nil {
		return nil, fmt.Errorf("no connection available for reading")
	}

	if c.BLEChar == nil {
		return nil, fmt.Errorf("characteristic not initialized")
	}

	if c.connection.client == nil {
		return nil, fmt.Errorf("not connected to device")
	}

	data, err := c.connection.client.ReadCharacteristic(c.BLEChar)
	if err != nil {
		return nil, fmt.Errorf("failed to read characteristic: %w", err)
	}

	return data, nil
}

// CloseUpdates safely closes the updates channel (once only, thread-safe)
func (c *BLECharacteristic) CloseUpdates() {
	if c.closed.CompareAndSwap(false, true) {
		close(c.updates)
	}
}

// ResetUpdates recreates the updates channel (for reconnection).
// MUST only be called after CloseUpdates() has been called.
// Returns error if channel is not closed.
func (c *BLECharacteristic) ResetUpdates(buffer int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Verify channel is closed before resetting
	if !c.closed.Load() {
		return fmt.Errorf("cannot reset updates channel: channel is still open")
	}

	c.updates = make(chan *BLEValue, buffer)
	c.closed.Store(false)
	return nil
}

// ----------------------------
// BLE Service
// ----------------------------

// BLEService represents a GATT service and its characteristics
type BLEService struct {
	UUID            string
	knownName       string
	Characteristics map[string]*BLECharacteristic
}

func (s *BLEService) GetUUID() string {
	return s.UUID
}

func (s *BLEService) KnownName() string {
	return s.knownName
}

func (s *BLEService) GetCharacteristics() []Characteristic {
	result := make([]Characteristic, 0, len(s.Characteristics))
	for _, char := range s.Characteristics {
		result = append(result, char)
	}
	// Sort by UUID for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].GetUUID() < result[j].GetUUID()
	})
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

	ctx    context.Context
	cancel context.CancelFunc
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
	subWg         sync.WaitGroup // Tracks active Lua subscription goroutines
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

func (c *BLEConnection) GetServices() []Service {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	result := make([]Service, 0, len(c.services))
	for _, v := range c.services {
		result = append(result, v)
	}
	// Sort by UUID for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].GetUUID() < result[j].GetUUID()
	})
	return result
}

func (c *BLEConnection) GetService(uuid string) (Service, error) {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	// Normalize UUID for lookup
	normalizedUUID := normalizeUUID(uuid)
	svc, ok := c.services[normalizedUUID]
	if !ok {
		return nil, fmt.Errorf("service %s not found", uuid)
	}
	return svc, nil
}

// GetDevice returns the associated Device object.
// LIMITATION: BLEConnection does not hold a reference to the Device object.
// This method always returns nil due to architectural constraints where the
// Device is created during scanning but not retained during connection.
// Callers MUST handle nil returns from this method.
func (c *BLEConnection) GetDevice() Device {
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
		subscriptions: make([]*LuaSubscription, 0),
		ctx:           context.Background(),
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
		svcRawUUID := bleSvc.UUID.String()
		svcUUID := normalizeUUID(svcRawUUID)
		c.logger.WithField("service_uuid", svcRawUUID).Debug("Found service UUID")
		svc, ok := c.services[svcUUID]
		if !ok {
			svc = &BLEService{
				UUID:            svcUUID,                         // store normalized
				knownName:       bledb.LookupService(svcRawUUID), // lookup using raw form if DB expects dashed
				Characteristics: make(map[string]*BLECharacteristic),
			}
			c.services[svcUUID] = svc
		}

		for _, bleCharacteristic := range bleSvc.Characteristics {
			charRawUUID := bleCharacteristic.UUID.String()
			charUUID := normalizeUUID(charRawUUID)
			c.logger.WithFields(logrus.Fields{
				"service_uuid": svcUUID,
				"char_uuid":    charRawUUID,
			}).Debug("Found characteristic UUID")
			characteristic, ok := svc.Characteristics[charUUID]
			if !ok {
				// Create BLECharacteristic with connection reference for reading
				characteristic = NewCharacteristic(bleCharacteristic, 128, c)
				svc.Characteristics[charUUID] = characteristic
			} else {
				// Reconnecting - update live handle and recreate channel if closed on disconnect
				characteristic.BLEChar = bleCharacteristic
				if characteristic.closed.Load() {
					if err := characteristic.ResetUpdates(128); err != nil {
						c.logger.WithFields(logrus.Fields{
							"char_uuid": charUUID,
							"error":     err,
						}).Warn("Failed to reset updates channel during reconnection")
					}
				}
			}
		}
	}

	// Mark as connected and assign client
	c.client = client
	c.isConnected = true

	// Set up context for subscriptions - derive from caller's context to tie lifecycle
	c.ctx, c.cancel = context.WithCancel(ctx)

	c.logger.WithField("services", len(c.services)).Info("BLE device connected")
	return nil
}

func (c *BLEConnection) Disconnect() error {
	// Acquire and snapshot state, cancel subs under lock
	c.connMutex.Lock()
	if c.client == nil || !c.isConnected {
		c.connMutex.Unlock()
		if c.logger != nil {
			c.logger.Info("Already disconnected")
		}
		return nil
	}

	if c.logger != nil {
		c.logger.WithField("connection_ptr", fmt.Sprintf("%p", c)).Info("Disconnecting BLE device...")
	}

	// Cancel per-subscriptions (they will stop via sub.ctx)
	for _, sub := range c.subscriptions {
		if sub != nil && sub.cancel != nil {
			sub.cancel()
		}
	}
	// clear subscription list
	c.subscriptions = nil

	// Grab client and cancel func to release lock before blocking waits
	client := c.client
	cancel := c.cancel

	// Snapshot services to drain channels outside the lock
	servicesCopy := make(map[string]*BLEService)
	for k, v := range c.services {
		servicesCopy[k] = v
	}

	// set fields to nil/false while still holding lock
	c.client = nil
	c.cancel = nil
	c.isConnected = false
	c.connMutex.Unlock()

	// Cancel the connection-level context (if present)
	if cancel != nil {
		cancel()
	}

	// Wait for all subscription goroutines to exit
	if c.logger != nil {
		c.logger.WithField("connection_ptr", fmt.Sprintf("%p", c)).Debug("Waiting for subscription goroutines to complete...")
	}
	c.subWg.Wait()
	if c.logger != nil {
		c.logger.WithField("connection_ptr", fmt.Sprintf("%p", c)).Debug("All subscription goroutines completed")
	}

	// Unsubscribe from remote BLE notifications before cancelling connection
	unsubscribeErrors := c.unsubscribeAllCharacteristics(client, servicesCopy)
	if len(unsubscribeErrors) > 0 && c.logger != nil {
		c.logger.WithField("errors", strings.Join(unsubscribeErrors, "; ")).Warn("Failed to unsubscribe from some characteristics during disconnect")
	}

	// Drain and close per-characteristic update channels
	for _, service := range servicesCopy {
		for _, char := range service.Characteristics {
			drainAndReleaseChannel(char.updates)
			// Close channel to signal EOF - will be recreated on reconnect
			char.CloseUpdates()
		}
	}

	// Finally, disconnect BLE client (network call) outside the lock
	var disconnectErr error
	if client != nil {
		disconnectErr = client.CancelConnection()
	}

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

// drainAndReleaseChannel drains all pending BLEValue objects from a channel and releases them to the pool.
func drainAndReleaseChannel(ch chan *BLEValue) {
	for {
		select {
		case v := <-ch:
			if v == nil {
				return
			}
			releaseBLEValue(v)
		default:
			return
		}
	}
}

// tryUnsubscribe attempts to unsubscribe from a characteristic using both notify and indicate modes.
// Returns error only if both modes fail. Logs success/failure appropriately.
func (c *BLEConnection) tryUnsubscribe(client ble.Client, char *BLECharacteristic, serviceUUID, charUUID string) error {
	if char.BLEChar == nil {
		return nil
	}

	err1 := client.Unsubscribe(char.BLEChar, false) // notify
	err2 := client.Unsubscribe(char.BLEChar, true)  // indicate

	// Only return error if both notify and indicate failed
	if err1 != nil && err2 != nil {
		if c.logger != nil {
			c.logger.WithFields(logrus.Fields{
				"serviceUUID": serviceUUID,
				"charUUID":    charUUID,
				"notifyErr":   err1,
				"indicateErr": err2,
			}).Error("Failed to unsubscribe from characteristic notifications")
		}
		return fmt.Errorf("%s: notify=%v, indicate=%v", charUUID, err1, err2)
	}

	if c.logger != nil {
		c.logger.WithFields(logrus.Fields{
			"serviceUUID": serviceUUID,
			"charUUID":    charUUID,
		}).Debug("Unsubscribed from characteristic notifications")
	}
	return nil
}

// unsubscribeAllCharacteristics unsubscribes from all characteristics in the given services.
// Returns a list of error messages for failed unsubscriptions.
// Should be called without holding locks.
func (c *BLEConnection) unsubscribeAllCharacteristics(client ble.Client, services map[string]*BLEService) []string {
	var unsubscribeErrors []string

	if client == nil {
		return unsubscribeErrors
	}

	for serviceUUID, service := range services {
		for charUUID, char := range service.Characteristics {
			if err := c.tryUnsubscribe(client, char, serviceUUID, charUUID); err != nil {
				unsubscribeErrors = append(unsubscribeErrors, fmt.Sprintf("%s (in service %s): %v", charUUID, serviceUUID, err))
			}
		}
	}

	return unsubscribeErrors
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
	// Acquire lock, validate, copy characteristics, then release lock before network calls
	c.connMutex.RLock()

	// Check if connected
	if !c.isConnectedInternal() {
		c.connMutex.RUnlock()
		if c.client == nil {
			return fmt.Errorf("device not connected - establish connection before subscribing to notifications")
		}
		return fmt.Errorf("device disconnected - reconnect before subscribing to notifications")
	}

	// Validate subscription options and get characteristics
	characteristicsToSubscribe, err := c.validateSubscribeOptions(opts, true)
	if err != nil {
		c.connMutex.RUnlock()
		return fmt.Errorf("subscription %w", err)
	}

	// If no characteristics support notifications after validation
	if len(characteristicsToSubscribe) == 0 {
		c.connMutex.RUnlock()
		return fmt.Errorf("no characteristics available for subscription in service %s", opts.Service)
	}

	// Copy characteristics and get client reference
	characteristicsCopy := make(map[string]*BLECharacteristic)
	for k, v := range characteristicsToSubscribe {
		characteristicsCopy[k] = v
	}
	client := c.client
	c.connMutex.RUnlock()

	// All validation passed - proceed with subscriptions outside the lock
	var subscriptionErrors []string
	for charUUID, char := range characteristicsCopy {
		// create a local variable to capture the current char
		charCapture := char
		err := client.Subscribe(char.BLEChar, false, func(data []byte) {
			c.ProcessCharacteristicNotification(charCapture, data)
		})

		if err != nil {
			subscriptionErrors = append(subscriptionErrors, fmt.Sprintf("%s: %v", charUUID, err))
			if c.logger != nil {
				c.logger.WithFields(logrus.Fields{
					"serviceUUID": opts.Service,
					"charUUID":    charUUID,
					"error":       err,
				}).Error("Failed to subscribe to characteristic notifications")
			}
		} else {
			if c.logger != nil {
				c.logger.WithFields(logrus.Fields{
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
	// Acquire lock, validate, copy characteristics, then release lock before network calls
	c.connMutex.RLock()

	// Validate specific subscription options (don't require notification support for unsubscribe)
	characteristicsToUnsubscribe, err := c.validateSubscribeOptions(opts, false)
	if err != nil {
		c.connMutex.RUnlock()
		return fmt.Errorf("unsubscribe %w", err)
	}

	// If no characteristics found after validation
	if len(characteristicsToUnsubscribe) == 0 {
		c.connMutex.RUnlock()
		return fmt.Errorf("no characteristics available for unsubscribe in service %s", opts.Service)
	}

	// Copy characteristics and get client reference
	characteristicsCopy := make(map[string]*BLECharacteristic)
	for k, v := range characteristicsToUnsubscribe {
		characteristicsCopy[k] = v
	}
	client := c.client
	c.connMutex.RUnlock()

	// All validation passed - proceed with unsubscriptions outside the lock
	var unsubscribeErrors []string
	for charUUID, char := range characteristicsCopy {
		if err := c.tryUnsubscribe(client, char, opts.Service, charUUID); err != nil {
			unsubscribeErrors = append(unsubscribeErrors, err.Error())
		}
	}

	// Return error if any unsubscriptions failed
	if len(unsubscribeErrors) > 0 {
		return fmt.Errorf("unsubscribe failures - %s", strings.Join(unsubscribeErrors, "; "))
	}

	return nil
}

// unsubscribeInternal performs unsubscribe operations
// Acquires and releases locks as needed to avoid deadlocks
func (c *BLEConnection) unsubscribeInternal(opts *SubscribeOptions) error {
	// Handle unsubscribe from all subscriptions when opts is nil
	if opts == nil {
		var unsubscribeErrors []string

		// Acquire lock to cancel subscriptions and snapshot state
		c.connMutex.Lock()

		// First: cancel and remove per-subscriptions so runLuaSubscription exits
		for _, sub := range c.subscriptions {
			if sub != nil && sub.cancel != nil {
				sub.cancel()
			}
		}
		// Clear subscriptions slice (we'll still try to call client.Unsubscribe below)
		c.subscriptions = nil

		// Snapshot client and services for operations outside the lock
		client := c.client
		servicesCopy := make(map[string]*BLEService)
		for k, v := range c.services {
			servicesCopy[k] = v
		}

		c.connMutex.Unlock()

		// Wait for all subscription goroutines to exit (outside lock to avoid deadlock)
		c.subWg.Wait()

		// Unsubscribe from remote BLE notifications
		unsubscribeErrors = c.unsubscribeAllCharacteristics(client, servicesCopy)

		// Drain per-characteristic update channels and release BLEValue objects
		for _, service := range servicesCopy {
			for _, char := range service.Characteristics {
				drainAndReleaseChannel(char.updates)
			}
		}

		if len(unsubscribeErrors) > 0 {
			return fmt.Errorf("unsubscribe failures - %s", strings.Join(unsubscribeErrors, "; "))
		}

		if c.logger != nil {
			c.logger.Info("Successfully unsubscribed from all characteristic notifications (local cleanup done)")
		}
		return nil
	}

	// Acquire lock to validate and snapshot client
	c.connMutex.RLock()

	// Validate specific subscription options (don't require notification support for unsubscribe)
	characteristicsToUnsubscribe, err := c.validateSubscribeOptions(opts, false)
	if err != nil {
		c.connMutex.RUnlock()
		return fmt.Errorf("unsubscribe %w", err)
	}

	// If no characteristics found after validation
	if len(characteristicsToUnsubscribe) == 0 {
		c.connMutex.RUnlock()
		return fmt.Errorf("no characteristics available for unsubscribe in service %s", opts.Service)
	}

	// Snapshot client for network operations outside the lock
	client := c.client
	c.connMutex.RUnlock()

	// All validation passed - proceed with unsubscriptions
	var unsubscribeErrors []string
	for charUUID, char := range characteristicsToUnsubscribe {
		if err := c.tryUnsubscribe(client, char, opts.Service, charUUID); err != nil {
			unsubscribeErrors = append(unsubscribeErrors, err.Error())
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
	c.logger.WithFields(logrus.Fields{
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
	sub.ctx, sub.cancel = context.WithCancel(c.ctx)

	c.subscriptions = append(c.subscriptions, sub)

	c.subWg.Add(1)
	go c.runLuaSubscription(sub)

	return nil
}

func (c *BLEConnection) runLuaSubscription(sub *LuaSubscription) {
	defer c.subWg.Done()

	if c.logger != nil {
		c.logger.WithField("connection_ptr", fmt.Sprintf("%p", c)).Debug("Subscription goroutine started")
	}
	defer func() {
		if c.logger != nil {
			c.logger.WithField("connection_ptr", fmt.Sprintf("%p", c)).Debug("Subscription goroutine exiting")
		}
	}()

	// Only create ticker for modes that need rate limiting
	var ticker *time.Ticker
	var tickerC <-chan time.Time
	if sub.Mode == StreamBatched || sub.Mode == StreamAggregated {
		if sub.MaxRate <= 0 {
			// Default to 100ms for batched/aggregated modes if MaxRate is 0 or negative
			sub.MaxRate = 100 * time.Millisecond
		}
		ticker = time.NewTicker(sub.MaxRate)
		tickerC = ticker.C
		defer ticker.Stop()
	} else {
		// nil channel blocks forever in select - perfect for EveryUpdate mode
		tickerC = nil
	}

	for {
		select {
		case <-sub.ctx.Done():
			return
		case <-tickerC:
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
				hasData := false
				for _, char := range sub.Chars {
					select {
					case <-sub.ctx.Done():
						return
					case val := <-char.updates:
						record := newRecord(StreamEveryUpdate)
						record.Values[char.GetUUID()] = val.Data
						record.TsUs = val.TsUs
						if val.Flags != 0 {
							record.Flags |= val.Flags
						}
						sub.Callback(record)
						releaseBLEValue(val)
						hasData = true
					default:
					}
				}
				// Sleep briefly if no data available to avoid hot loop
				if !hasData {
					time.Sleep(5 * time.Millisecond)
				}
			} else {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
}
