package device

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/darwin"
	"github.com/sirupsen/logrus"
)

// BLEDescriptor implements the Descriptor interface
type BLEDescriptor struct {
	uuid string
}

func (d *BLEDescriptor) GetUUID() string {
	return d.uuid
}

// BLECharacteristic implements the Characteristic interface
type BLECharacteristic struct {
	uuid        string
	properties  string
	descriptors []Descriptor
	value       []byte
	mu          sync.RWMutex
	BLEChar     *ble.Characteristic
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

// BLESimpleService implements the Service interface for basic services
type BLESimpleService struct {
	uuid            string
	characteristics []Characteristic
}

func (s *BLESimpleService) GetUUID() string {
	return s.uuid
}

func (s *BLESimpleService) GetCharacteristics() []Characteristic {
	return s.characteristics
}

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

// BLEConnection represents a live BLE connection (notifications, writes)
type BLEConnection struct {
	client      ble.Client
	logger      *logrus.Logger
	writeMutex  sync.Mutex
	connMutex   sync.RWMutex
	isConnected bool
}

// BLEDevice implements the Device interface for BLE devices
type BLEDevice struct {
	// Device data
	id          string
	name        string
	address     string
	rssi        int
	txPower     *int
	connectable bool
	lastSeen    time.Time
	services    []Service
	manufData   []byte
	serviceData map[string][]byte

	// BLE-specific data
	bleServices        map[string]*BLEService        // UUID -> service
	bleCharacteristics map[string]*BLECharacteristic // UUID -> characteristic
	connection         *BLEConnection
	onData             func(uuid string, data []byte)
	logger             *logrus.Logger
	mu                 sync.RWMutex
}

// NewBLEDeviceFromAdvertisement creates a BLEDevice from a BLE advertisement
func NewBLEDeviceFromAdvertisement(adv ble.Advertisement, logger *logrus.Logger) *BLEDevice {
	if logger == nil {
		logger = logrus.New()
	}

	dev := &BLEDevice{
		id:                 adv.Addr().String(),
		name:               adv.LocalName(),
		address:            adv.Addr().String(),
		rssi:               adv.RSSI(),
		connectable:        adv.Connectable(),
		lastSeen:           time.Now(),
		services:           make([]Service, 0),
		manufData:          adv.ManufacturerData(),
		serviceData:        make(map[string][]byte),
		bleServices:        make(map[string]*BLEService),
		bleCharacteristics: make(map[string]*BLECharacteristic),
		logger:             logger,
	}

	// Convert service UUIDs into minimal Service entries (UUID only)
	for _, svc := range adv.Services() {
		dev.services = append(dev.services, &BLESimpleService{uuid: svc.String()})
	}

	// Convert service data
	for _, svcData := range adv.ServiceData() {
		dev.serviceData[svcData.UUID.String()] = svcData.Data
	}

	// Extract TX power if available
	if adv.TxPowerLevel() != 127 { // 127 means TX power not available
		txPower := int(adv.TxPowerLevel())
		dev.txPower = &txPower
	}

	// Try to extract name from manufacturer data if no local name
	if dev.name == "" {
		if extractedName := dev.extractNameFromManufacturerData(adv.ManufacturerData()); extractedName != "" {
			dev.name = extractedName
		}
	}

	return dev
}

// NewBLEDeviceWithAddress creates a BLEDevice with the specified address
func NewBLEDeviceWithAddress(address string, logger *logrus.Logger) *BLEDevice {
	if logger == nil {
		logger = logrus.New()
	}

	return &BLEDevice{
		id:                 address,
		address:            address,
		services:           make([]Service, 0),
		serviceData:        make(map[string][]byte),
		bleServices:        make(map[string]*BLEService),
		bleCharacteristics: make(map[string]*BLECharacteristic),
		lastSeen:           time.Now(),
		logger:             logger,
	}
}

// Device interface implementation

func (d *BLEDevice) GetID() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.id
}

func (d *BLEDevice) GetName() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.name
}

func (d *BLEDevice) GetAddress() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.address
}

func (d *BLEDevice) GetRSSI() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.rssi
}

func (d *BLEDevice) GetTxPower() *int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.txPower
}

func (d *BLEDevice) IsConnectable() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.connectable
}

func (d *BLEDevice) GetLastSeen() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastSeen
}

func (d *BLEDevice) GetServices() []Service {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.services
}

func (d *BLEDevice) GetManufacturerData() []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.manufData
}

func (d *BLEDevice) GetServiceData() map[string][]byte {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.serviceData
}

func (d *BLEDevice) DisplayName() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.name != "" {
		return d.name
	}
	return d.address
}

func (d *BLEDevice) IsExpired(timeout time.Duration) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return time.Since(d.lastSeen) > timeout
}

// Connect establishes a BLE connection and populates live characteristics
func (d *BLEDevice) Connect(ctx context.Context, opts *ConnectOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if strings.TrimSpace(d.address) == "" {
		return fmt.Errorf("failed connect to device: device address is not set")
	}

	if d.connection != nil && d.connection.IsConnected() {
		return fmt.Errorf("already connected")
	}

	d.logger.WithField("address", d.address).Info("Connecting to BLE device...")

	// Create platform BLE device (darwin for macOS)
	dev, err := darwin.NewDevice()
	if err != nil {
		return fmt.Errorf("failed to create BLE device: %w", err)
	}
	ble.SetDefaultDevice(dev)

	// Timeout context
	connCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()

	// Connect to BLE device
	client, err := ble.Dial(connCtx, ble.NewAddr(d.address))
	if err != nil {
		return fmt.Errorf("failed to connect to device: %w", err)
	}

	// Discover services and characteristics
	profile, err := client.DiscoverProfile(true)
	if err != nil {
		client.CancelConnection()
		return fmt.Errorf("failed to discover profile: %w", err)
	}

	// Populate services and characteristics
	for _, svc := range profile.Services {
		svcUUID := svc.UUID.String()
		bleSvc, ok := d.bleServices[svcUUID]
		if !ok {
			bleSvc = &BLEService{
				UUID:            svcUUID,
				Characteristics: make(map[string]*BLECharacteristic),
			}
			d.bleServices[svcUUID] = bleSvc
		}

		for _, char := range svc.Characteristics {
			uuid := char.UUID.String()
			bleChar, ok := d.bleCharacteristics[uuid]
			if !ok {
				// Create BLECharacteristic
				bleChar = &BLECharacteristic{
					uuid:        char.UUID.String(),
					properties:  blePropsToString(char.Property),
					descriptors: []Descriptor{},
					BLEChar:     char,
				}
				d.bleCharacteristics[uuid] = bleChar
				bleSvc.Characteristics[uuid] = bleChar
			} else {
				// Update live handle
				bleChar.BLEChar = char
				bleSvc.Characteristics[uuid] = bleChar
			}
		}
	}

	// Create connection wrapper
	conn := &BLEConnection{
		client:      client,
		logger:      d.logger,
		isConnected: true,
	}
	d.connection = conn

	// Subscribe to notify/indicate characteristics
	for _, bleChar := range d.bleCharacteristics {
		if bleChar.BLEChar == nil {
			continue
		}
		if bleChar.BLEChar.Property&ble.CharNotify != 0 || bleChar.BLEChar.Property&ble.CharIndicate != 0 {
			uuid := bleChar.GetUUID()
			d.logger.WithField("uuid", uuid).Info("Subscribing to notifications")
			err := client.Subscribe(bleChar.BLEChar, false, func(data []byte) {
				bleChar.mu.Lock()
				bleChar.value = data
				bleChar.mu.Unlock()
				if d.onData != nil {
					d.onData(uuid, data)
				}
			})
			if err != nil {
				d.logger.WithFields(logrus.Fields{
					"uuid":  uuid,
					"error": err,
				}).Warn("Failed to subscribe to characteristic")
			}
		}
	}

	d.logger.WithField("services", len(d.bleServices)).Info("BLE device connected")
	return nil
}

// Disconnect closes connection and clears live handles
func (d *BLEDevice) Disconnect() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.connection == nil || !d.connection.IsConnected() {
		d.logger.Info("Already disconnected")
		return nil
	}

	d.logger.Info("Disconnecting BLE device...")

	for _, bleChar := range d.bleCharacteristics {
		if bleChar.BLEChar != nil && d.connection.client != nil {
			_ = d.connection.client.Unsubscribe(bleChar.BLEChar, false)
			_ = d.connection.client.Unsubscribe(bleChar.BLEChar, true)
		}
		bleChar.BLEChar = nil
	}

	err := d.connection.client.CancelConnection()
	d.connection.isConnected = false
	d.connection.client = nil
	d.connection = nil

	d.logger.Info("BLE device disconnected")
	return err
}

// IsConnected returns connection status
func (d *BLEDevice) IsConnected() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.connection != nil && d.connection.IsConnected()
}

// Update refreshes device information from a new advertisement
func (d *BLEDevice) Update(adv ble.Advertisement) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.rssi = adv.RSSI()
	d.lastSeen = time.Now()

	// Update name if it wasn't available before or changed
	if name := adv.LocalName(); name != "" {
		d.name = name
	} else if d.name == "" {
		// Try to extract name from manufacturer data if no local name
		if extractedName := d.extractNameFromManufacturerData(adv.ManufacturerData()); extractedName != "" {
			d.name = extractedName
		}
	}

	// Update manufacturer data
	if manufData := adv.ManufacturerData(); len(manufData) > 0 {
		d.manufData = manufData
	}

	// Merge advertised services (ensure UUID entries exist)
	for _, svc := range adv.Services() {
		u := svc.String()
		if !d.hasServiceUUID(u) {
			d.services = append(d.services, &BLESimpleService{uuid: u})
		}
	}

	// Update service data
	for _, svcData := range adv.ServiceData() {
		d.serviceData[svcData.UUID.String()] = svcData.Data
	}

	// Update TX power
	if adv.TxPowerLevel() != 127 {
		txPower := int(adv.TxPowerLevel())
		d.txPower = &txPower
	}
}

// BLE-specific methods

// IsConnected returns connection status for BLEConnection
func (c *BLEConnection) IsConnected() bool {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()
	return c.isConnected
}

// WriteToCharacteristic writes data to a BLE characteristic
func (b *BLEDevice) WriteToCharacteristic(uuid string, data []byte) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.connection == nil || !b.connection.IsConnected() {
		return fmt.Errorf("device not connected")
	}

	char, ok := b.bleCharacteristics[uuid]
	if !ok {
		return fmt.Errorf("characteristic %s not found", uuid)
	}

	b.connection.writeMutex.Lock()
	defer b.connection.writeMutex.Unlock()

	maxChunk := 20
	for len(data) > 0 {
		n := len(data)
		if n > maxChunk {
			n = maxChunk
		}
		if err := b.connection.client.WriteCharacteristic(char.BLEChar, data[:n], false); err != nil {
			return err
		}
		data = data[n:]
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// GetBLEServices returns services with their characteristics
func (b *BLEDevice) GetBLEServices() []*BLEService {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]*BLEService, 0, len(b.bleServices))
	for _, svc := range b.bleServices {
		result = append(result, svc)
	}
	return result
}

// GetCharacteristics returns all characteristics as device.Characteristic
func (b *BLEDevice) GetCharacteristics() []Characteristic {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]Characteristic, 0, len(b.bleCharacteristics))
	for _, c := range b.bleCharacteristics {
		result = append(result, c)
	}
	return result
}

// SetDataHandler sets callback for received notifications
func (b *BLEDevice) SetDataHandler(f func(uuid string, data []byte)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onData = f
}

// Helper functions

func blePropsToString(p ble.Property) string {
	var s []string
	if p&ble.CharRead != 0 {
		s = append(s, "Read")
	}
	if p&ble.CharWrite != 0 {
		s = append(s, "Write")
	}
	if p&ble.CharNotify != 0 {
		s = append(s, "Notify")
	}
	if p&ble.CharIndicate != 0 {
		s = append(s, "Indicate")
	}
	return strings.Join(s, "|")
}

// extractNameFromManufacturerData attempts to extract a device name from manufacturer data
func (d *BLEDevice) extractNameFromManufacturerData(data []byte) string {
	if len(data) < 4 {
		return ""
	}

	// Common patterns in manufacturer data that may contain device names:

	// Pattern 1: Look for readable ASCII strings longer than 3 characters
	// Many devices embed their name as ASCII text in manufacturer data
	for i := 0; i < len(data)-3; i++ {
		if isReadableASCII(data[i]) {
			// Found start of potential string, extract it
			var nameBytes []byte
			for j := i; j < len(data) && j < i+32; j++ { // Limit to 32 chars
				if isReadableASCII(data[j]) {
					nameBytes = append(nameBytes, data[j])
				} else {
					break
				}
			}

			if len(nameBytes) >= 3 { // Minimum meaningful name length
				name := strings.TrimSpace(string(nameBytes))
				if len(name) >= 3 && isValidDeviceName(name) {
					return name
				}
			}
		}
	}

	// Pattern 2: Apple iBeacon format - check for known manufacturer IDs
	if len(data) >= 2 {
		manufacturerID := uint16(data[0]) | uint16(data[1])<<8

		switch manufacturerID {
		case 0x004C: // Apple
			return d.parseAppleManufacturerData(data[2:])
		case 0x0006: // Microsoft
			return d.parseMicrosoftManufacturerData(data[2:])
		case 0x000F: // Broadcom
			return d.parseBroadcomManufacturerData(data[2:])
		}
	}

	return ""
}

// isReadableASCII checks if a byte represents a readable ASCII character
func isReadableASCII(b byte) bool {
	return b >= 32 && b <= 126 && unicode.IsPrint(rune(b))
}

// isValidDeviceName checks if a string looks like a valid device name
func isValidDeviceName(name string) bool {
	if len(name) < 3 || len(name) > 32 {
		return false
	}

	// Must contain at least one letter
	hasLetter := false
	for _, r := range name {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}

	return hasLetter
}

// parseAppleManufacturerData attempts to extract device names from Apple manufacturer data
func (d *BLEDevice) parseAppleManufacturerData(data []byte) string {
	// Apple devices sometimes include device type information
	// This is a simplified parser - real implementation would be more comprehensive
	return ""
}

// parseMicrosoftManufacturerData attempts to extract device names from Microsoft manufacturer data
func (d *BLEDevice) parseMicrosoftManufacturerData(data []byte) string {
	// Microsoft devices sometimes include device information
	return ""
}

// parseBroadcomManufacturerData attempts to extract device names from Broadcom manufacturer data
func (d *BLEDevice) parseBroadcomManufacturerData(data []byte) string {
	// Broadcom devices sometimes include device information
	return ""
}

// hasServiceUUID checks if Services already contains a service with the given UUID (case-insensitive)
func (d *BLEDevice) hasServiceUUID(uuid string) bool {
	for _, s := range d.services {
		if strings.EqualFold(s.GetUUID(), uuid) {
			return true
		}
	}
	return false
}
