package goble

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
)

// ----------------------------
// Subscription
// ----------------------------

func newRecord(mode device.StreamMode) *device.Record {
	r := &device.Record{
		TsUs: time.Now().UnixMicro(),
	}
	if mode == device.StreamBatched {
		r.BatchValues = make(map[string][][]byte)
	} else {
		r.Values = make(map[string][]byte)
	}
	return r
}

type Subscription struct {
	Chars    []*BLECharacteristic
	Mode     device.StreamMode
	MaxRate  time.Duration
	Callback func(*device.Record)

	ctx    context.Context
	cancel context.CancelFunc
}

// ----------------------------
// Subscription Manager
// ----------------------------

// SubscriptionManager manages the lifecycle of Lua subscriptions
type SubscriptionManager struct {
	subscriptions []*Subscription
	wg            sync.WaitGroup
	mu            sync.Mutex
	logger        *logrus.Logger
}

// NewSubscriptionManager creates a new subscription manager
func NewSubscriptionManager(logger *logrus.Logger) *SubscriptionManager {
	return &SubscriptionManager{
		subscriptions: make([]*Subscription, 0),
		logger:        logger,
	}
}

// Add adds a subscription to the manager and starts its goroutine
func (m *SubscriptionManager) Add(sub *Subscription, runner func(*Subscription)) {
	m.mu.Lock()
	m.subscriptions = append(m.subscriptions, sub)
	m.mu.Unlock()

	m.wg.Add(1)
	go runner(sub)
}

// CancelAll cancels all active subscriptions and clears the list
func (m *SubscriptionManager) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sub := range m.subscriptions {
		if sub.cancel != nil {
			sub.cancel()
		}
	}
	m.subscriptions = nil
}

// Wait waits for all subscription goroutines to complete
func (m *SubscriptionManager) Wait() {
	if m.logger != nil {
		m.logger.Debug("Waiting for subscription goroutines to complete...")
	}
	m.wg.Wait()
	if m.logger != nil {
		m.logger.Debug("All subscription goroutines completed")
	}
}

// Done decrements the wait group counter (called by subscription goroutines)
func (m *SubscriptionManager) Done() {
	m.wg.Done()
}

// ----------------------------
// Subscription Methods
// ----------------------------

// Subscribe subscribes to notifications from multiple services and characteristics with streaming patterns.
// Supports advanced subscription with streaming patterns and callbacks:
//
//	connection.Subscribe([]*device.SubscribeOptions{
//	  { Service: "0000180d-0000-1000-8000-00805f9b34fb", Characteristics: []string{"00002a37-0000-1000-8000-00805f9b34fb"} },
//	  { Service: "1000180d-0000-1000-8000-00805f9b34fb", Characteristics: []string{"10002a37-0000-1000-8000-00805f9b34fb"} }
//	}, device.StreamEveryUpdate, 0, func(record *device.Record) { ... })
func (c *BLEConnection) Subscribe(opts []*device.SubscribeOptions, mode device.StreamMode, maxRate time.Duration, callback func(*device.Record)) error {
	// Validate parameters before acquiring any locks or allocating resources
	if callback == nil {
		return fmt.Errorf("no callback specified in Lua subscription")
	}

	if len(opts) == 0 {
		return fmt.Errorf("no services specified in Lua subscription")
	}

	c.logger.WithFields(map[string]interface{}{
		"services": len(opts),
		"mode":     mode,
	}).Debug("Subscribe called - about to create goroutine")

	c.connMutex.Lock()

	// Check if connected (we already hold the lock, so use safe version)
	if !c.isConnectedInternal() {
		c.connMutex.Unlock()
		return fmt.Errorf("device disconnected - reconnect before subscribing to Lua notifications")
	}

	// Validate subscription options and get characteristics from all services
	var allCharacteristics []*BLECharacteristic
	for _, opt := range opts {
		characteristicsToSubscribe, err := c.validateSubscribeOptions(opt, true)
		if err != nil {
			c.connMutex.Unlock()
			return fmt.Errorf("lua subscription %w", err)
		}

		// Convert validated BLECharacteristics for Subscription
		for _, bleChar := range characteristicsToSubscribe {
			allCharacteristics = append(allCharacteristics, bleChar)
		}
	}

	// If no characteristics support notifications after validation
	if len(allCharacteristics) == 0 {
		c.connMutex.Unlock()
		return fmt.Errorf("no characteristics available for Lua subscription across all specified services")
	}

	// Release lock before calling BLESubscribe (which acquires its own locks)
	c.connMutex.Unlock()

	// CRITICAL: Enable BLE notifications by calling BLESubscribe for each service
	// This must be done outside the lock to avoid deadlock (BLESubscribe acquires its own locks)
	for _, opt := range opts {
		// BLESubscribe will validate again and call client.Subscribe() to enable CCCD (Client Characteristic Configuration Descriptor)
		err := c.BLESubscribe(opt)
		if err != nil {
			return fmt.Errorf("failed to enable BLE notifications: %w", err)
		}
	}

	// Re-acquire lock for creating subscription
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	sub := &Subscription{
		Chars:    allCharacteristics,
		Mode:     mode,
		MaxRate:  maxRate,
		Callback: callback,
	}
	sub.ctx, sub.cancel = context.WithCancel(c.ctx)

	// Add subscription to manager and start goroutine
	c.subMgr.Add(sub, c.runSubscription)

	return nil
}

func (c *BLEConnection) runSubscription(sub *Subscription) {
	defer c.subMgr.Done()

	// Recover from panics in subscription callback to prevent crash
	defer func() {
		if r := recover(); r != nil {
			if c.logger != nil {
				c.logger.WithFields(map[string]interface{}{
					"panic":          r,
					"connection_ptr": fmt.Sprintf("%p", c),
				}).Error("Subscription callback panicked")
			}
		}
	}()

	if c.logger != nil {
		c.logger.WithField("connection_ptr", fmt.Sprintf("%p", c)).Debug("Subscription goroutine started")
	}
	defer func() {
		if c.logger != nil {
			c.logger.WithField("connection_ptr", fmt.Sprintf("%p", c)).Debug("Subscription goroutine exiting")
		}
	}()

	// Create ticker for all modes with appropriate interval
	var ticker *time.Ticker
	if sub.Mode == device.StreamBatched || sub.Mode == device.StreamAggregated {
		if sub.MaxRate <= 0 {
			// Default to DefaultBatchedInterval for batched/aggregated modes if MaxRate is 0 or negative
			sub.MaxRate = DefaultBatchedInterval
		}
		ticker = time.NewTicker(sub.MaxRate)
	} else {
		// StreamEveryUpdate mode uses DefaultUpdateInterval
		ticker = time.NewTicker(DefaultUpdateInterval)
	}
	defer ticker.Stop()

	for {
		select {
		case <-sub.ctx.Done():
			return
		case <-ticker.C:
			if sub.Mode == device.StreamBatched {
				record := newRecord(device.StreamBatched)
				for _, c := range sub.Chars {
					// Drain all available updates for this characteristic
					for {
						select {
						case val := <-c.updates:
							record.BatchValues[c.UUID()] = append(record.BatchValues[c.UUID()], val.Data)
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
			} else if sub.Mode == device.StreamAggregated {
				record := newRecord(device.StreamAggregated)
				for _, c := range sub.Chars {
					select {
					case val := <-c.updates:
						record.Values[c.UUID()] = val.Data
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
			} else if sub.Mode == device.StreamEveryUpdate {
				for _, char := range sub.Chars {
					select {
					case <-sub.ctx.Done():
						return
					case val := <-char.updates:
						record := newRecord(device.StreamEveryUpdate)
						record.Values[char.UUID()] = val.Data
						record.TsUs = val.TsUs
						if val.Flags != 0 {
							record.Flags |= val.Flags
						}
						sub.Callback(record)
						releaseBLEValue(val)
					default:
						// No data available, continue to next char
					}
				}
			}
		}
	}
}
