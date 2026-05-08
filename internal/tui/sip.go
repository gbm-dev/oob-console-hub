package tui

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// SIPStatus represents the Telnyx outbound configuration state.
// slmodem-sip-bridge authenticates per-call directly with Telnyx, so there
// is no persistent registration to query. We surface "configured" vs
// "not configured" based on whether the required credentials are present.
type SIPStatus int

const (
	SIPUnknown SIPStatus = iota
	SIPRegistered
	SIPUnregistered
)

// SIPInfo holds local infra health and Telnyx configuration state.
type SIPInfo struct {
	Status      SIPStatus
	ModemReady  bool // /dev/ttySL0 exists
	BridgeReady bool // slmodemd is configured to launch slmodem-sip-bridge
}

// sipStatusMsg carries the result of a SIP status check.
type sipStatusMsg SIPInfo

// checkSIPStatus runs health checks for all components.
func checkSIPStatus() tea.Msg {
	info := SIPInfo{Status: SIPUnregistered}

	devicePath := os.Getenv("DEVICE_PATH")
	if devicePath == "" {
		devicePath = "/dev/ttySL0"
	}
	if _, err := os.Stat(devicePath); err == nil {
		info.ModemReady = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "pgrep", "-fa", "slmodemd").CombinedOutput(); err == nil &&
		strings.Contains(string(out), "slmodem-sip-bridge") {
		info.BridgeReady = true
	}

	if os.Getenv("TELNYX_SIP_USER") != "" && os.Getenv("TELNYX_SIP_PASS") != "" {
		info.Status = SIPRegistered
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
