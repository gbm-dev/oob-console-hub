package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gbm-dev/oob-console-hub/internal/modem"
)

// State represents the current TUI state.
type State int

const (
	StatePasswordChange State = iota
	StateMenu
	StateDialing
	StateConnected
)

// Messages passed between TUI components.

// DialRequestMsg is sent when the user selects a site.
type DialRequestMsg struct {
	SiteIndex int
}

// ModemAcquiredMsg is sent when a modem device is acquired from the pool.
type ModemAcquiredMsg struct {
	Device string
}

// ModemResetMsg is sent after ATZ succeeds.
type ModemResetMsg struct{}

// DialResultMsg is sent after the dial attempt completes.
type DialResultMsg struct {
	Result     modem.DialResult
	Transcript string
	Modem      *modem.Modem
	Device     string
}

// PasswordChangedMsg is sent after a successful password change.
type PasswordChangedMsg struct{}

// ErrorMsg wraps an error for display.
type ErrorMsg struct {
	Err     error
	Context string
}

// TerminalDoneMsg is sent when the connected session ends (cleanup complete).
type TerminalDoneMsg struct {
	Err error
}

// modemDataMsg carries raw bytes received from the modem.
type modemDataMsg []byte

// modemDisconnectMsg signals the modem read returned an error (carrier lost, device closed).
type modemDisconnectMsg struct {
	err error
}

// wakeTickMsg triggers periodic CR sends until the remote device responds.
type wakeTickMsg struct{}

// cleanupDoneMsg signals that modem hangup, close, and lock release are complete.
type cleanupDoneMsg struct {
	err error
}

// statusMsg is used internally to update the dialing status text.
type statusMsg string

func updateStatus(msg string) tea.Cmd {
	return func() tea.Msg {
		return statusMsg(msg)
	}
}
