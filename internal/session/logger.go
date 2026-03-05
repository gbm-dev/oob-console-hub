package session

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Logger writes session transcripts to disk.
type Logger struct {
	file *os.File
	path string
}

// NewLogger creates a session log file in logDir with the pattern:
// {siteName}_{YYYYmmdd-HHMMSS}_{device}.log
func NewLogger(logDir, siteName, device string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("creating log dir: %w", err)
	}

	ts := time.Now().Format("20060102-150405")
	devBase := filepath.Base(device)
	filename := fmt.Sprintf("%s_%s_%s.log", siteName, ts, devBase)
	path := filepath.Join(logDir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("creating log file: %w", err)
	}

	// Write header
	header := fmt.Sprintf("=== Session: %s | Device: %s | Started: %s ===\n",
		siteName, device, time.Now().Format(time.RFC3339))
	f.WriteString(header)

	return &Logger{file: f, path: path}, nil
}

// Writer returns an io.Writer that writes to the log file.
// Use with io.TeeReader to capture modem→user traffic.
func (l *Logger) Writer() io.Writer {
	return l.file
}

// TeeReader wraps r so that reads are teed to the log file.
func (l *Logger) TeeReader(r io.Reader) io.Reader {
	return io.TeeReader(r, l.file)
}

// Path returns the log file path.
func (l *Logger) Path() string {
	return l.path
}

// Close writes a footer and closes the log file.
func (l *Logger) Close() error {
	footer := fmt.Sprintf("\n=== Session ended: %s ===\n", time.Now().Format(time.RFC3339))
	l.file.WriteString(footer)
	return l.file.Close()
}
