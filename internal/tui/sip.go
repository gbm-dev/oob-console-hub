package tui

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// SIPStatus represents the SIP configuration/readiness state.
type SIPStatus int

const (
	SIPUnknown SIPStatus = iota
	SIPConfigured
	SIPUnconfigured
)

// SIPInfo holds the SIP configuration status and local infra health.
type SIPInfo struct {
	Status     SIPStatus
	ModemReady bool // /dev/ttySL0 exists
	BridgeReady bool // slmodem-sip-bridge process running
}

// sipStatusMsg carries the result of a SIP status check.
type sipStatusMsg SIPInfo

// checkSIPStatus runs health checks for all components.
func checkSIPStatus() tea.Msg {
	info := SIPInfo{Status: SIPUnconfigured}
	devicePath := os.Getenv("DEVICE_PATH")
	if devicePath == "" {
		devicePath = "/dev/ttySL0"
	}

	// 1. Check modem PTY from slmodemd
	if _, err := os.Stat(devicePath); err == nil {
		info.ModemReady = true
	}

	// 2. Check slmodemd is configured to use the external bridge helper.
	// Use a 2s timeout for pgrep
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "pgrep", "-fa", "slmodemd").CombinedOutput(); err == nil &&
		strings.Contains(string(out), "slmodem-sip-bridge") {
		info.BridgeReady = true
	}

	// 3. Check SIP is configured by verifying required env vars are set.
	// The bridge handles its own SIP REGISTER — we just verify config exists.
	sipUser := os.Getenv("TELNYX_SIP_USER")
	sipDomain := os.Getenv("TELNYX_SIP_DOMAIN")
	if sipDomain == "" {
		sipDomain = "sip.telnyx.com"
	}
	if sipUser != "" && sipDomain != "" {
		info.Status = SIPConfigured
	}

	return sipStatusMsg(info)
}

// sipTickMsg triggers periodic SIP status checks.
type sipTickMsg struct{}

// sipTick returns a command that ticks every 5 seconds.
func sipTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return sipTickMsg{}
	})
}
