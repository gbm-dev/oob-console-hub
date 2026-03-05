package session

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLogger(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLogger(dir, "testsite", "/dev/ttySL0")
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	defer l.Close()

	// Check file was created
	path := l.Path()
	if !strings.Contains(path, "testsite_") {
		t.Errorf("path %q doesn't contain site name", path)
	}
	if !strings.HasSuffix(path, "_ttySL0.log") {
		t.Errorf("path %q doesn't end with device name", path)
	}
}

func TestLoggerTeeReader(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLogger(dir, "testsite", "/dev/ttySL0")
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	input := "hello from modem\n"
	tee := l.TeeReader(strings.NewReader(input))

	// Read through the tee
	data, err := io.ReadAll(tee)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != input {
		t.Errorf("tee read = %q, want %q", data, input)
	}

	l.Close()

	// Check log file contains the data
	logData, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(logData), "hello from modem") {
		t.Error("log file doesn't contain tee'd data")
	}
	if !strings.Contains(string(logData), "Session ended") {
		t.Error("log file doesn't contain footer")
	}
}

func TestLoggerCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	l, err := NewLogger(dir, "site", "/dev/ttySL0")
	if err != nil {
		t.Fatalf("NewLogger with nested dir: %v", err)
	}
	l.Close()

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected dir to be created: %v", err)
	}
}
