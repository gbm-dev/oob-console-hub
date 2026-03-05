package modem

import (
	"fmt"
	"os"
	"sync"
)

// DeviceLock manages a single modem device (e.g. /dev/ttySL0).
// Only one session can hold the device at a time.
type DeviceLock struct {
	mu         sync.Mutex
	devicePath string
	activeSite string // "" = idle
}

// NewDeviceLock creates a lock for the given modem device path.
func NewDeviceLock(devicePath string) *DeviceLock {
	return &DeviceLock{devicePath: devicePath}
}

// Acquire claims the modem device for the given site.
// Returns the device path if available, or an error if busy or missing.
func (d *DeviceLock) Acquire(siteName string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.activeSite != "" {
		return "", fmt.Errorf("modem busy: connected to %s", d.activeSite)
	}

	if _, err := os.Stat(d.devicePath); err != nil {
		return "", fmt.Errorf("modem device %s not found: %w", d.devicePath, err)
	}

	d.activeSite = siteName
	return d.devicePath, nil
}

// Release marks the modem device as idle.
func (d *DeviceLock) Release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activeSite = ""
}

// ActiveSite returns the name of the currently connected site, or "" if idle.
func (d *DeviceLock) ActiveSite() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.activeSite
}

// IsAvailable returns true if the modem device is not in use.
func (d *DeviceLock) IsAvailable() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.activeSite == ""
}

// DevicePath returns the configured device path.
func (d *DeviceLock) DevicePath() string {
	return d.devicePath
}
