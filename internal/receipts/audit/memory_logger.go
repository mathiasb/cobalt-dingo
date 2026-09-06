package audit

import (
	"sync"
	"time"
)

// MemoryLogger is an in-memory Logger for use in tests.
// It is safe for concurrent use.
type MemoryLogger struct {
	mu      sync.Mutex
	entries []Entry
}

// Log appends entry to the in-memory slice.
// If entry.Timestamp is zero it is set to time.Now().UTC().
func (m *MemoryLogger) Log(entry Entry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

// Entries returns a copy of all logged entries.
// The caller may modify the returned slice without affecting the logger.
func (m *MemoryLogger) Entries() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Entry, len(m.entries))
	copy(cp, m.entries)
	return cp
}

// Reset discards all logged entries.
func (m *MemoryLogger) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = nil
}
