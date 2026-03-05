package modem

import (
	"os"
	"path/filepath"
	"testing"
)

func testDeviceLock(t *testing.T) (*DeviceLock, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ttySL0")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	return NewDeviceLock(path), path
}

func TestDeviceLockAcquireRelease(t *testing.T) {
	dl, devPath := testDeviceLock(t)

	dev, err := dl.Acquire("site-a")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if dev != devPath {
		t.Errorf("expected %q, got %q", devPath, dev)
	}

	// Should be busy now
	_, err = dl.Acquire("site-b")
	if err == nil {
		t.Error("expected error when modem is busy")
	}

	// Release and re-acquire
	dl.Release()
	dev, err = dl.Acquire("site-b")
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	if dev != devPath {
		t.Errorf("expected %q, got %q", devPath, dev)
	}
}

func TestDeviceLockMissingDevice(t *testing.T) {
	dl := NewDeviceLock("/dev/nonexistent-test-device")

	_, err := dl.Acquire("test")
	if err == nil {
		t.Error("expected error for missing device")
	}
}

func TestDeviceLockActiveSite(t *testing.T) {
	dl, _ := testDeviceLock(t)

	if site := dl.ActiveSite(); site != "" {
		t.Errorf("expected empty active site, got %q", site)
	}

	dl.Acquire("site-a")
	if site := dl.ActiveSite(); site != "site-a" {
		t.Errorf("expected %q, got %q", "site-a", site)
	}

	dl.Release()
	if site := dl.ActiveSite(); site != "" {
		t.Errorf("expected empty after release, got %q", site)
	}
}

func TestDeviceLockIsAvailable(t *testing.T) {
	dl, _ := testDeviceLock(t)

	if !dl.IsAvailable() {
		t.Error("expected available when idle")
	}

	dl.Acquire("site-a")
	if dl.IsAvailable() {
		t.Error("expected not available when busy")
	}

	dl.Release()
	if !dl.IsAvailable() {
		t.Error("expected available after release")
	}
}

func TestDeviceLockDevicePath(t *testing.T) {
	dl := NewDeviceLock("/dev/ttySL0")
	if dl.DevicePath() != "/dev/ttySL0" {
		t.Errorf("expected /dev/ttySL0, got %q", dl.DevicePath())
	}
}
