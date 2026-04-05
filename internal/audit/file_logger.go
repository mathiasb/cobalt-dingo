package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileLogger is an append-only, concurrency-safe Logger that writes
// JSON Lines to a file on disk. The file is never truncated.
type FileLogger struct {
	mu     sync.Mutex
	file   *os.File
	closed bool
}

// NewFileLogger opens (or creates) the log file at path.
// Parent directories are created with mode 0700 if they do not exist.
func NewFileLogger(path string) (*FileLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("audit: create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open log file: %w", err)
	}
	return &FileLogger{file: f}, nil
}

// Log marshals entry as a JSON object and appends it as a single line.
// If entry.Timestamp is zero it is set to time.Now().UTC() before encoding.
// Safe for concurrent use.
func (l *FileLogger) Log(entry Entry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit: marshal entry: %w", err)
	}
	// Append newline in one syscall to keep lines atomic under concurrency.
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("audit: logger is closed")
	}
	_, err = l.file.Write(data)
	return err
}

// Close flushes and closes the underlying file.
// After Close, Log returns an error.
func (l *FileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.file.Close()
}
