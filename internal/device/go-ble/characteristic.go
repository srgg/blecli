package goble

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-ble/ble"
	"github.com/srg/blim/internal/bledb"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/groutine"
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

const (
	// DefaultBLEValueCapacity is the default buffer capacity for pooled BLEValue objects
	DefaultBLEValueCapacity = 256

	// MaxPooledBufferSize is the maximum buffer size to keep in the pool.
	// Buffers larger than this are replaced with default-sized buffers to prevent
	// memory bloat in the pool.
	MaxPooledBufferSize = 1024

	// DefaultReadTimeout is the default timeout for characteristic read operations.
	// This prevents indefinite blocking if a device becomes unresponsive during a read.
	DefaultReadTimeout = 5 * time.Second
)

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
	New: func() interface{} { return &BLEValue{Data: make([]byte, 0, DefaultBLEValueCapacity)} },
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
	if cap(v.Data) > MaxPooledBufferSize {
		// Buffer too large, reallocate to default size
		v.Data = make([]byte, 0, DefaultBLEValueCapacity)
	} else {
		// Normal size, just reset length
		v.Data = v.Data[:0]
	}

	valuePool.Put(v)
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

// ----------------------------
// BLECharacteristic
// ----------------------------

type BLECharacteristic struct {
	uuid        string
	knownName   string
	properties  device.Properties
	descriptors []device.Descriptor
	value       []byte
	BLEChar     *ble.Characteristic
	connection  *BLEConnection // reference to parent connection for reading

	updates chan *BLEValue
	closed  atomic.Bool
	mu      sync.RWMutex
	subs    []func(*BLEValue)
}

func NewCharacteristic(c *ble.Characteristic, buffer int, conn *BLEConnection, descriptors []device.Descriptor) *BLECharacteristic {
	rawUUID := c.UUID.String()
	uuid := device.NormalizeUUID(rawUUID)

	return &BLECharacteristic{
		uuid:        uuid,                                // store normalized
		knownName:   bledb.LookupCharacteristic(rawUUID), // lookup using raw form if DB expects dashed
		BLEChar:     c,
		properties:  NewProperties(c.Property),
		updates:     make(chan *BLEValue, buffer),
		descriptors: descriptors,
		subs:        nil,
		connection:  conn,
	}
}

func (c *BLECharacteristic) EnqueueValue(v *BLEValue) {
	// Check if the channel is closed before attempting to send
	// This prevents panic from sending on a closed channel if BLE callbacks fire after shutdown
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

// Subscribe registers a callback function to be invoked when this characteristic receives notifications.
//
// IMPORTANT: BLEValue objects are pooled and reused for performance. The callback MUST copy
// v.Data immediately if it needs to retain the data beyond the callback invocation, as the
// Data slice becomes invalid after the callback returns and the BLEValue is released back to the pool.
//
// Example:
//
//	char.Subscribe(func(v *BLEValue) {
//	    // Copy data if you need to retain it
//	    dataCopy := make([]byte, len(v.Data))
//	    copy(dataCopy, v.Data)
//	    // Use dataCopy safely after callback returns
//	})
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

func (c *BLECharacteristic) UUID() string {
	return c.uuid
}

func (c *BLECharacteristic) KnownName() string {
	return c.knownName
}

func (c *BLECharacteristic) GetProperties() device.Properties {
	return c.properties
}

func (c *BLECharacteristic) GetDescriptors() []device.Descriptor {
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

// Read reads the current value of the characteristic from the device with the specified timeout.
// This implements the device.CharacteristicReader interface.
func (c *BLECharacteristic) Read(timeout time.Duration) ([]byte, error) {
	return c.ReadWithTimeout(timeout)
}

// ReadWithTimeout reads the current value of the characteristic from the device with the specified timeout.
// This prevents indefinite blocking if the device becomes unresponsive during a read operation.
func (c *BLECharacteristic) ReadWithTimeout(timeout time.Duration) ([]byte, error) {
	if c.connection == nil {
		return nil, fmt.Errorf("no connection available for reading characteristic %s", c.uuid)
	}

	if c.BLEChar == nil {
		return nil, fmt.Errorf("characteristic %s not initialized", c.uuid)
	}

	// Check read property before attempting read
	readProps := c.properties.Read()
	canRead := readProps != nil && readProps.Value() != 0

	if !canRead {
		return nil, fmt.Errorf("characteristic %s does not support read operations: %w", c.uuid, device.ErrUnsupported)
	}

	// Add connection mutex locking to prevent race condition
	c.connection.connMutex.RLock()
	if c.connection.client == nil {
		c.connection.connMutex.RUnlock()
		return nil, fmt.Errorf("read characteristic %s: %w", c.uuid, device.ErrNotConnected)
	}
	client := c.connection.client
	c.connection.connMutex.RUnlock()

	// Perform read with timeout to prevent indefinite blocking
	type readResult struct {
		data []byte
		err  error
	}
	resultCh := make(chan readResult, 1)

	groutine.Go(context.Background(), fmt.Sprintf("ble-characteristic-read-%s", c.uuid), func(ctx context.Context) {
		data, err := client.ReadCharacteristic(c.BLEChar)
		resultCh <- readResult{data: data, err: err}
	})

	select {
	case result := <-resultCh:
		if result.err != nil {
			return nil, fmt.Errorf("failed to read characteristic %s: %w", c.uuid, result.err)
		}
		return result.data, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("read characteristic %s after %v: %w", c.uuid, timeout, device.ErrTimeout)
	}
}

// Write writes data to the characteristic with the specified parameters.
// This implements the device.CharacteristicWriter interface.
// The withResponse parameter determines if write-with-response (true) or write-without-response (false) is used.
func (c *BLECharacteristic) Write(data []byte, withResponse bool, timeout time.Duration) error {
	if c.connection == nil {
		return fmt.Errorf("no connection available for writing characteristic %s", c.uuid)
	}

	if c.BLEChar == nil {
		return fmt.Errorf("characteristic %s not initialized", c.uuid)
	}

	// Check write properties before attempting write
	writeProps := c.properties.Write()
	writeNoRespProps := c.properties.WriteWithoutResponse()
	canWrite := writeProps != nil && writeProps.Value() != 0
	canWriteNoResponse := writeNoRespProps != nil && writeNoRespProps.Value() != 0

	if !canWrite && !canWriteNoResponse {
		return fmt.Errorf("characteristic %s does not support write operations: %w", c.uuid, device.ErrUnsupported)
	}

	// Add connection mutex locking to prevent race conditions
	c.connection.connMutex.RLock()
	if c.connection.client == nil {
		c.connection.connMutex.RUnlock()
		return fmt.Errorf("write characteristic %s: %w", c.uuid, device.ErrNotConnected)
	}
	client := c.connection.client
	c.connection.connMutex.RUnlock()

	// Acquire write mutex to serialize writes
	c.connection.writeMutex.Lock()
	defer c.connection.writeMutex.Unlock()

	// Perform write with timeout to prevent indefinite blocking
	type writeResult struct {
		err error
	}
	resultCh := make(chan writeResult, 1)

	groutine.Go(context.Background(), fmt.Sprintf("ble-characteristic-write-%s", c.uuid), func(ctx context.Context) {
		// BLE client WriteCharacteristic: noResponse parameter is opposite of withResponse
		err := client.WriteCharacteristic(c.BLEChar, data, !withResponse)
		resultCh <- writeResult{err: err}
	})

	select {
	case result := <-resultCh:
		if result.err != nil {
			return fmt.Errorf("failed to write characteristic %s: %w", c.uuid, result.err)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("write characteristic %s after %v: %w", c.uuid, timeout, device.ErrTimeout)
	}
}

// CloseUpdates safely closes the updates channel (once only, thread-safe)
func (c *BLECharacteristic) CloseUpdates() {
	if c.closed.CompareAndSwap(false, true) {
		close(c.updates)
	}
}

// ResetUpdates recreates the update channel (for reconnection).
// MUST only be called after CloseUpdates() has been called.
// Returns an error if the channel is not closed.
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
