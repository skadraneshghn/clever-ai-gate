package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogHub manages dual-destination log output:
//  1. Daily-rotated log files on disk (logs/gateway-YYYY-MM-DD.log)
//  2. A set of live SSE listener channels for the admin playground UI
//
// It implements zapcore.WriteSyncer so it can be plugged directly into a
// zapcore.NewTee core alongside the stdout encoder — zero extra goroutines,
// zero blocking on the hot-path proxy threads.
type LogHub struct {
	mu          sync.RWMutex      // guards both listeners map and currentFile/currentDay
	listeners   map[chan []byte]struct{}
	logDir      string
	currentFile *os.File
	currentDay  int
}

// NewLogHub creates a LogHub and opens (or appends to) today's log file.
// logDir is created with 0755 permissions if it does not already exist.
func NewLogHub(logDir string) (*LogHub, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("telemetry: failed to create log directory %q: %w", logDir, err)
	}

	lh := &LogHub{
		listeners: make(map[chan []byte]struct{}),
		logDir:    logDir,
	}

	if err := lh.rotateLogFileLocked(); err != nil {
		return nil, err
	}

	return lh, nil
}

// Write implements zapcore.WriteSyncer.
//
// Execution path (called from every zap log call):
//  1. Check for day-boundary rollover — if needed, close the current file
//     and open a new one (guarded by the write-lock, rare path).
//  2. Write the raw JSON bytes to the current daily file.
//  3. Fan the same bytes out to all registered SSE listener channels using
//     non-blocking selects — a saturated or disconnected browser never stalls
//     a proxy goroutine.
func (lh *LogHub) Write(p []byte) (n int, err error) {
	lh.mu.Lock()

	// Daily rotation check (fast path: same day, no lock contention)
	if time.Now().Day() != lh.currentDay {
		if rotErr := lh.rotateLogFileLocked(); rotErr != nil {
			// Log rotation failure is non-fatal — keep writing to old file
			_ = rotErr
		}
	}

	// Persist to disk
	n, err = lh.currentFile.Write(p)

	// Take a snapshot of the listener set while still under the write lock
	// so we don't hold the lock during channel sends below.
	listenerSnapshot := make([]chan []byte, 0, len(lh.listeners))
	for ch := range lh.listeners {
		listenerSnapshot = append(listenerSnapshot, ch)
	}

	lh.mu.Unlock()

	// Fan out to live SSE listeners — non-blocking, so slow or disconnected
	// browsers are silently skipped. We copy bytes once outside the lock.
	if len(listenerSnapshot) > 0 {
		logCopy := make([]byte, len(p))
		copy(logCopy, p)

		for _, ch := range listenerSnapshot {
			select {
			case ch <- logCopy:
			default:
				// Channel buffer full — drop this frame for this listener.
				// The SSE handler will reconnect automatically.
			}
		}
	}

	return n, err
}

// Sync implements zapcore.WriteSyncer.
func (lh *LogHub) Sync() error {
	lh.mu.Lock()
	defer lh.mu.Unlock()
	if lh.currentFile != nil {
		return lh.currentFile.Sync()
	}
	return nil
}

// RegisterListener mounts a buffered channel into the live broadcast set.
// The caller is responsible for providing a suitably buffered channel (>=128)
// and for calling UnregisterListener when the SSE connection closes.
func (lh *LogHub) RegisterListener(ch chan []byte) {
	lh.mu.Lock()
	lh.listeners[ch] = struct{}{}
	lh.mu.Unlock()
}

// UnregisterListener removes a channel from the broadcast set.
// Safe to call after the channel is already closed.
func (lh *LogHub) UnregisterListener(ch chan []byte) {
	lh.mu.Lock()
	delete(lh.listeners, ch)
	lh.mu.Unlock()
}

// GetTodayLogPath returns the absolute filesystem path of today's log file.
// Used by the download endpoint to serve the file as an attachment.
func (lh *LogHub) GetTodayLogPath() string {
	now := time.Now()
	return filepath.Join(lh.logDir, fmt.Sprintf("gateway-%s.log", now.Format("2006-01-02")))
}

// rotateLogFileLocked closes the current file handle (if any) and opens a new
// daily file. MUST be called with lh.mu held (write-lock).
func (lh *LogHub) rotateLogFileLocked() error {
	if lh.currentFile != nil {
		_ = lh.currentFile.Sync()
		_ = lh.currentFile.Close()
		lh.currentFile = nil
	}

	now := time.Now()
	logName := fmt.Sprintf("gateway-%s.log", now.Format("2006-01-02"))
	path := filepath.Join(lh.logDir, logName)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("telemetry: failed to open log file %q: %w", path, err)
	}

	lh.currentFile = f
	lh.currentDay = now.Day()
	return nil
}
