package tui

import (
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// SIPStatus represents the Telnyx trunk registration state.
type SIPStatus int

const (
	SIPUnknown SIPStatus = iota
	SIPRegistered
	SIPUnregistered
)

// SIPInfo holds the parsed SIP registration details and local infra health.
type SIPInfo struct {
	Status      SIPStatus
	Trunk       string // e.g. "telnyx-out"
	Server      string // e.g. "sip.telnyx.com"
	Expiry      string // e.g. "3434s"
	ModemReady  bool   // /dev/ttySL0 exists
	BridgeReady bool   // slmodem-asterisk-bridge process running
}

// sipStatusMsg carries the result of a SIP registration check.
type sipStatusMsg SIPInfo

// checkSIPStatus runs health checks for all components.
func checkSIPStatus() tea.Msg {
	info := SIPInfo{Status: SIPUnregistered}
	devicePath := os.Getenv("DEVICE_PATH")
	if devicePath == "" {
		devicePath = "/dev/ttySL0"
	}

	// 1. Check modem PTY from slmodemd
	if _, err := os.Stat(devicePath); err == nil {
		info.ModemReady = true
	}

	// 2. Check slmodemd is configured to use the external bridge helper.
	if out, err := exec.Command("pgrep", "-fa", "slmodemd").CombinedOutput(); err == nil &&
		strings.Contains(string(out), "slmodem-asterisk-bridge") {
		info.BridgeReady = true
	}

	// 3. Check Asterisk SIP Registration
	out, err := exec.Command("asterisk", "-rx", "pjsip show registrations").CombinedOutput()
	if err == nil {
		parsed := parseSIPRegistrations(string(out))
		info.Status = parsed.Status
		info.Trunk = parsed.Trunk
		info.Server = parsed.Server
	}

	return sipStatusMsg(info)
}

// parseSIPRegistrations extracts registration info from pjsip show registrations output.
func parseSIPRegistrations(output string) SIPInfo {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip headers, separators, empty lines, "Objects found" line
		if line == "" || strings.HasPrefix(line, "<") || strings.HasPrefix(line, "=") || strings.HasPrefix(line, "Objects") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		// fields[0] = "telnyx-out-reg-0/sip:sip.telnyx.com:5060"
		// fields[1] = "telnyx-out-oauth"
		// fields[2] = "Registered" or "Unregistered"
		trunk := fields[0]
		status := fields[2]

		// Parse trunk name (before -reg-)
		if idx := strings.Index(trunk, "-reg-"); idx > 0 {
			trunk = trunk[:idx]
		}
		// Parse server from URI
		server := ""
		if idx := strings.Index(fields[0], "sip:"); idx >= 0 {
			server = fields[0][idx+4:]
			if colonIdx := strings.LastIndex(server, ":"); colonIdx > 0 {
				server = server[:colonIdx]
			}
		}

		if status == "Registered" {
			return SIPInfo{
				Status: SIPRegistered,
				Trunk:  trunk,
				Server: server,
			}
		}
		return SIPInfo{
			Status: SIPUnregistered,
			Trunk:  trunk,
			Server: server,
		}
	}

	return SIPInfo{Status: SIPUnregistered}
}

// sipTickMsg triggers periodic SIP status checks.
type sipTickMsg struct{}

// sipTick returns a command that ticks every 30 seconds.
func sipTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg {
		return sipTickMsg{}
	})
}
