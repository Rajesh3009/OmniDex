package logger

import (
	"sync"
	"time"
)

// LogEntry represents an individual parsed log line.
type LogEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// RingBuffer stores a bounded number of recent log entries in memory.
type RingBuffer struct {
	mu       sync.RWMutex
	capacity int
	entries  []LogEntry
	nextID   int64
}

// NewRingBuffer creates a RingBuffer with specified capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 500
	}
	return &RingBuffer{
		capacity: capacity,
		entries:  make([]LogEntry, 0, capacity),
		nextID:   1,
	}
}

// Add appends a new entry, dropping the oldest if capacity is reached.
func (rb *RingBuffer) Add(level, message string) LogEntry {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	entry := LogEntry{
		ID:        rb.nextID,
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}
	rb.nextID++

	if len(rb.entries) >= rb.capacity {
		rb.entries = append(rb.entries[1:], entry)
	} else {
		rb.entries = append(rb.entries, entry)
	}

	return entry
}

// Entries returns a copy of all current entries in chronological order.
func (rb *RingBuffer) Entries() []LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	res := make([]LogEntry, len(rb.entries))
	copy(res, rb.entries)
	return res
}

// Clear removes all stored entries.
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.entries = make([]LogEntry, 0, rb.capacity)
}
