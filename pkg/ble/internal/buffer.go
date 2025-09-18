package internal

import (
	"sync"

	"github.com/aarzilli/golua/lua"
)

// Buffer represents a data buffer exposed to Lua scripts
// Implements the Buffer API from the design document:
// buffer:read(n)      -- returns up to n bytes, consumes them
// buffer:peek(n)      -- returns up to n bytes, does not consume
// buffer:consume(n)   -- discards n bytes
// buffer:append(data) -- append bytes (Go feeds input)
type Buffer struct {
	data  []byte
	mutex sync.RWMutex
	name  string // for debugging
}

// NewBuffer creates a new buffer
func NewBuffer(name string) *Buffer {
	return &Buffer{
		data: make([]byte, 0),
		name: name,
	}
}

// Read returns up to n bytes and consumes them from the buffer
func (b *Buffer) Read(n int) []byte {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if n <= 0 || len(b.data) == 0 {
		return []byte{}
	}

	if n > len(b.data) {
		n = len(b.data)
	}

	result := make([]byte, n)
	copy(result, b.data[:n])

	// Remove consumed bytes
	b.data = b.data[n:]

	return result
}

// Peek returns up to n bytes without consuming them
func (b *Buffer) Peek(n int) []byte {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if n <= 0 || len(b.data) == 0 {
		return []byte{}
	}

	if n > len(b.data) {
		n = len(b.data)
	}

	result := make([]byte, n)
	copy(result, b.data[:n])

	return result
}

// Consume discards n bytes from the buffer
func (b *Buffer) Consume(n int) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if n <= 0 || len(b.data) == 0 {
		return
	}

	if n >= len(b.data) {
		b.data = b.data[:0] // Clear buffer
	} else {
		b.data = b.data[n:]
	}
}

// Append adds data to the end of the buffer
func (b *Buffer) Append(data []byte) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.data = append(b.data, data...)
}

// Len returns the current buffer length
func (b *Buffer) Len() int {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return len(b.data)
}

// Clear empties the buffer
func (b *Buffer) Clear() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.data = b.data[:0]
}

// RegisterBufferAPI registers the buffer API functions in the Lua state
func RegisterBufferAPI(L *lua.State, buffer *Buffer) {
	// Create buffer table
	L.NewTable()

	// buffer:read(n)
	L.PushString("read")
	L.PushGoFunction(func(L *lua.State) int {
		n := int(L.ToInteger(2))
		data := buffer.Read(n)
		L.PushString(string(data))
		return 1
	})
	L.SetTable(-3)

	// buffer:peek(n)
	L.PushString("peek")
	L.PushGoFunction(func(L *lua.State) int {
		n := int(L.ToInteger(2))
		data := buffer.Peek(n)
		L.PushString(string(data))
		return 1
	})
	L.SetTable(-3)

	// buffer:consume(n)
	L.PushString("consume")
	L.PushGoFunction(func(L *lua.State) int {
		n := int(L.ToInteger(2))
		buffer.Consume(n)
		return 0
	})
	L.SetTable(-3)

	// buffer:append(data)
	L.PushString("append")
	L.PushGoFunction(func(L *lua.State) int {
		data := L.ToString(2)
		buffer.Append([]byte(data))
		return 0
	})
	L.SetTable(-3)

	// Set global 'buffer' variable
	L.SetGlobal("buffer")
}
