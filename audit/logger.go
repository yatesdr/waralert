// Package audit provides JSON-lines audit logging with an in-memory ring buffer.
package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Entry represents a single audit log entry.
type Entry struct {
	Timestamp  time.Time `json:"timestamp"`
	EventType  string    `json:"event_type,omitempty"` // "action", "sms_incoming", etc.
	Chain      string    `json:"chain,omitempty"`
	Block      string    `json:"block,omitempty"`
	ActionType string    `json:"action_type,omitempty"`
	Recipients []string  `json:"recipients,omitempty"`
	Message    string    `json:"message,omitempty"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	// SMS incoming fields
	From    string `json:"from,omitempty"`
	Command string `json:"command,omitempty"`
	Reply   string `json:"reply,omitempty"`
}

// Logger writes audit entries to a JSON-lines file and maintains a ring buffer.
type Logger struct {
	file    *os.File
	ring    []Entry
	ringPos int
	ringLen int
	ringCap int
	mu      sync.RWMutex
}

// NewLogger creates a new audit logger.
func NewLogger(path string, ringSize int) (*Logger, error) {
	if ringSize <= 0 {
		ringSize = 1000
	}

	var file *os.File
	if path != "" {
		var err error
		file, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open audit log: %w", err)
		}
	}

	l := &Logger{
		file:    file,
		ring:    make([]Entry, ringSize),
		ringCap: ringSize,
	}

	// Load existing entries from file into the ring buffer.
	if path != "" {
		l.loadFromFile(path)
	}

	return l, nil
}

// loadFromFile reads existing JSON-lines entries into the ring buffer.
func (l *Logger) loadFromFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		l.ring[l.ringPos] = entry
		l.ringPos = (l.ringPos + 1) % l.ringCap
		if l.ringLen < l.ringCap {
			l.ringLen++
		}
	}
}

// Log writes an audit entry to the file and ring buffer.
func (l *Logger) Log(entry Entry) {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	l.mu.Lock()
	// Add to ring buffer
	l.ring[l.ringPos] = entry
	l.ringPos = (l.ringPos + 1) % l.ringCap
	if l.ringLen < l.ringCap {
		l.ringLen++
	}

	// Write to file
	if l.file != nil {
		data, err := json.Marshal(entry)
		if err == nil {
			l.file.Write(data)
			l.file.Write([]byte("\n"))
		}
	}
	l.mu.Unlock()
}

// Recent returns the most recent entries from the ring buffer, newest first.
func (l *Logger) Recent(limit int) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if limit <= 0 || limit > l.ringLen {
		limit = l.ringLen
	}

	result := make([]Entry, limit)
	for i := 0; i < limit; i++ {
		idx := (l.ringPos - 1 - i + l.ringCap) % l.ringCap
		result[i] = l.ring[idx]
	}
	return result
}

// Close closes the audit log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
