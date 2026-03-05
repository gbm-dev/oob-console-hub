package tui

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gbm-dev/oob-console-hub/internal/modem"
	"github.com/gbm-dev/oob-console-hub/internal/session"
)

const (
	maxHistory     = 100
	maxOutputLines = 10000
	wakeInterval   = 2 * time.Second
	batchDelay     = 200 * time.Millisecond
)

// ConnectedModel is the split-pane terminal view with:
//   - Scrollable output viewport (top)
//   - Status bar with latency and keybind hints (middle)
//   - Pinned input line with cursor (bottom)
//
// Replaces the old tea.ExecCommand approach with a proper Bubble Tea model
// that gives us scrollback, command history, batch mode, and latency display.
type ConnectedModel struct {
	viewport viewport.Model

	// Input line
	input      []byte
	history    []string
	historyIdx int
	historyTmp string // stash current input when browsing history

	// Batch mode: compose multiple commands locally, send as a group
	batchMode  bool
	batchLines []string

	// Connection
	siteName string
	rwc      io.ReadWriteCloser
	mdm      *modem.Modem
	device   string
	lock     *modem.DeviceLock
	logger   *session.Logger

	// Latency tracking: timestamp on send, measure on first response
	latency     time.Duration
	lastSendAt  time.Time
	awaitingRTT bool

	// State flags
	prevWasEnter bool
	carrierLost  bool
	cleaning     bool
	gotData      bool

	// Output accumulator (pointer: strings.Builder must not be copied after first use)
	outputBuf *strings.Builder

	// Layout
	width  int
	height int
	theme  Theme
}

// NewConnectedModel creates the split-pane terminal view. The session logger
// is created immediately so we can capture all modem output from the start.
func NewConnectedModel(
	mdm *modem.Modem,
	device, siteName, logDir string,
	lock *modem.DeviceLock,
	width, height int,
	theme Theme,
) (ConnectedModel, error) {
	logger, err := session.NewLogger(logDir, siteName, device)
	if err != nil {
		return ConnectedModel{}, fmt.Errorf("creating session logger: %w", err)
	}

	vpHeight := height - 3 // status bar + input line + divider
	if vpHeight < 1 {
		vpHeight = 1
	}
	vp := viewport.New(width, vpHeight)
	vp.SetContent("")

	m := ConnectedModel{
		viewport:   vp,
		mdm:        mdm,
		rwc:        mdm.ReadWriteCloser(),
		device:     device,
		siteName:   siteName,
		lock:       lock,
		logger:     logger,
		width:      width,
		height:     height,
		theme:      theme,
		historyIdx: -1,
		outputBuf:  &strings.Builder{},
	}

	banner := fmt.Sprintf("*** CONNECTED to %s ***\n", siteName)
	m.outputBuf.WriteString(banner)
	m.viewport.SetContent(m.outputBuf.String())

	return m, nil
}

func (m ConnectedModel) Init() tea.Cmd {
	return tea.Batch(
		waitForModemData(m.rwc),
		sendWake(m.rwc),
		wakeTickCmd(),
	)
}

func (m ConnectedModel) Update(msg tea.Msg) (ConnectedModel, tea.Cmd) {
	if m.cleaning {
		// Only process cleanup completion while cleaning
		if msg, ok := msg.(cleanupDoneMsg); ok {
			return m, func() tea.Msg { return TerminalDoneMsg{Err: msg.err} }
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcViewport()
		return m, nil

	case modemDataMsg:
		m.gotData = true

		// Measure latency from last command send
		if m.awaitingRTT && !m.lastSendAt.IsZero() {
			m.latency = time.Since(m.lastSendAt)
			m.awaitingRTT = false
		}

		// Log to session transcript
		if m.logger != nil {
			m.logger.Writer().Write([]byte(msg))
		}

		// Normalize line endings for viewport display
		cleaned := strings.ReplaceAll(string(msg), "\r\n", "\n")
		cleaned = strings.ReplaceAll(cleaned, "\r", "\n")
		m.outputBuf.WriteString(cleaned)

		// Trim if output buffer grows too large
		m.trimOutput()

		// Auto-scroll only if user was already at the bottom
		atBottom := m.viewport.AtBottom()
		m.viewport.SetContent(m.outputBuf.String())
		if atBottom {
			m.viewport.GotoBottom()
		}

		return m, waitForModemData(m.rwc)

	case modemDisconnectMsg:
		if m.cleaning {
			return m, nil
		}
		m.carrierLost = true
		m.cleaning = true

		reason := "CONNECTION LOST"
		if msg.err != nil && msg.err != io.EOF {
			reason = fmt.Sprintf("CONNECTION ERROR: %v", msg.err)
		}
		m.outputBuf.WriteString(fmt.Sprintf("\n*** %s ***\n", reason))
		m.viewport.SetContent(m.outputBuf.String())
		m.viewport.GotoBottom()

		return m, m.cleanup()

	case wakeTickMsg:
		if !m.gotData {
			return m, tea.Batch(
				sendWake(m.rwc),
				wakeTickCmd(),
			)
		}
		return m, nil

	case cleanupDoneMsg:
		return m, func() tea.Msg { return TerminalDoneMsg{Err: msg.err} }

	case tea.KeyMsg:
		newM, cmd, handled := m.handleKey(msg)
		if handled {
			return newM, cmd
		}
		// Unhandled keys pass to viewport for scrolling (pgup/pgdown/etc)
		var vpCmd tea.Cmd
		newM.viewport, vpCmd = newM.viewport.Update(msg)
		return newM, vpCmd
	}

	// Mouse events, etc → viewport
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m ConnectedModel) View() string {
	var b strings.Builder

	// Output viewport
	b.WriteString(m.viewport.View())
	b.WriteByte('\n')

	// Status bar
	b.WriteString(m.statusBar())
	b.WriteByte('\n')

	// Batch lines (when in batch mode)
	if m.batchMode && len(m.batchLines) > 0 {
		start := 0
		if len(m.batchLines) > 5 {
			start = len(m.batchLines) - 5
		}
		for i := start; i < len(m.batchLines); i++ {
			num := m.theme.LabelStyle.Render(fmt.Sprintf(" %d│", i+1))
			b.WriteString(num)
			b.WriteString(m.batchLines[i])
			b.WriteByte('\n')
		}
	}

	// Pinned input line
	prompt := m.theme.NewStyle().Foreground(m.theme.ColorPrimary).Render("> ")
	inputText := m.theme.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Render(string(m.input))
	cursor := m.theme.NewStyle().Foreground(m.theme.ColorPrimary).Blink(true).Render("█")
	b.WriteString(prompt + inputText + cursor)

	return b.String()
}

func (m ConnectedModel) statusBar() string {
	var parts []string

	// Site name
	parts = append(parts,
		m.theme.NewStyle().Bold(true).Foreground(m.theme.ColorPrimary).Render(m.siteName))

	// Latency indicator with color coding
	if m.latency > 0 {
		latStr := fmt.Sprintf("RTT %dms", m.latency.Milliseconds())
		var style lipgloss.Style
		switch {
		case m.latency < 100*time.Millisecond:
			style = m.theme.SuccessStyle
		case m.latency < 500*time.Millisecond:
			style = m.theme.WarningStyle
		default:
			style = m.theme.ErrorStyle
		}
		parts = append(parts, style.Render(latStr))
	} else {
		parts = append(parts, m.theme.LabelStyle.Render("RTT --"))
	}

	// Scroll position
	if !m.viewport.AtBottom() {
		pct := int(m.viewport.ScrollPercent() * 100)
		parts = append(parts, m.theme.WarningStyle.Render(fmt.Sprintf("scroll %d%%", pct)))
	}

	// Mode-specific hints
	if m.batchMode {
		parts = append(parts, m.theme.WarningStyle.Render(
			fmt.Sprintf("BATCH (%d) · ^D send · Esc cancel", len(m.batchLines))))
	} else {
		parts = append(parts, m.theme.LabelStyle.Render("↑↓ hist · ^B batch · PgUp/Dn scroll · ~. quit"))
	}

	return m.theme.StatusBarStyle.Render(strings.Join(parts, "  │  "))
}

// handleKey processes keyboard input. Returns (model, cmd, handled).
func (m ConnectedModel) handleKey(msg tea.KeyMsg) (ConnectedModel, tea.Cmd, bool) {
	key := msg.String()

	switch key {
	case "ctrl+c":
		m.cleaning = true
		m.outputBuf.WriteString("\n*** DISCONNECTED ***\n")
		m.viewport.SetContent(m.outputBuf.String())
		m.viewport.GotoBottom()
		return m, m.cleanup(), true

	case "ctrl+b":
		m.batchMode = !m.batchMode
		if !m.batchMode {
			m.batchLines = nil
		}
		m.recalcViewport()
		return m, nil, true

	case "ctrl+d":
		if m.batchMode && len(m.batchLines) > 0 {
			cmd := m.sendBatch()
			m.batchMode = false
			m.batchLines = nil
			m.recalcViewport()
			return m, cmd, true
		}
		return m, nil, true

	case "esc":
		if m.batchMode {
			m.batchMode = false
			m.batchLines = nil
			m.recalcViewport()
			return m, nil, true
		}
		return m, nil, false // let viewport handle it

	case "ctrl+u":
		m.input = m.input[:0]
		return m, nil, true

	case "ctrl+l":
		m.outputBuf.Reset()
		m.viewport.SetContent("")
		return m, nil, true

	case "up":
		m.navigateHistory(-1)
		return m, nil, true

	case "down":
		m.navigateHistory(1)
		return m, nil, true

	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil, true

	case "enter":
		return m.handleEnter()

	default:
		// Regular printable characters
		if len(key) == 1 && key[0] >= 0x20 && key[0] <= 0x7e {
			m.input = append(m.input, key[0])
			// Don't clear prevWasEnter here — the ~. escape needs to
			// know that the previous *submitted* line was Enter.
			// prevWasEnter is cleared in handleEnter for non-escape lines.
			return m, nil, true
		}
		return m, nil, false // unhandled → pass to viewport
	}
}

func (m ConnectedModel) handleEnter() (ConnectedModel, tea.Cmd, bool) {
	line := string(m.input)

	// ~. escape sequence: disconnect
	if m.prevWasEnter && line == "~." {
		m.cleaning = true
		m.outputBuf.WriteString("\n*** DISCONNECTED ***\n")
		m.viewport.SetContent(m.outputBuf.String())
		m.viewport.GotoBottom()
		return m, m.cleanup(), true
	}

	// Batch mode: accumulate commands locally
	if m.batchMode {
		m.batchLines = append(m.batchLines, line)
		m.input = m.input[:0]
		m.prevWasEnter = true
		m.recalcViewport()
		return m, nil, true
	}

	// Normal mode: send command to modem
	m.lastSendAt = time.Now()
	m.awaitingRTT = true
	cmd := m.sendLine(line)

	// Add to command history (skip empty, skip duplicates)
	if line != "" {
		if len(m.history) == 0 || m.history[len(m.history)-1] != line {
			m.history = append(m.history, line)
			if len(m.history) > maxHistory {
				m.history = m.history[1:]
			}
		}
	}

	m.input = m.input[:0]
	m.historyIdx = -1
	m.historyTmp = ""
	m.prevWasEnter = (line == "") // only empty Enter arms the ~. escape

	return m, cmd, true
}

func (m *ConnectedModel) navigateHistory(dir int) {
	if len(m.history) == 0 {
		return
	}

	if m.historyIdx == -1 {
		if dir > 0 {
			return // already past end
		}
		m.historyTmp = string(m.input)
		m.historyIdx = len(m.history) - 1
	} else {
		m.historyIdx += dir
	}

	if m.historyIdx < 0 {
		m.historyIdx = 0
		return
	}
	if m.historyIdx >= len(m.history) {
		m.input = []byte(m.historyTmp)
		m.historyIdx = -1
		return
	}

	m.input = []byte(m.history[m.historyIdx])
}

func (m *ConnectedModel) recalcViewport() {
	vpHeight := m.height - 3
	if m.batchMode {
		batchRows := len(m.batchLines) + 1 // queued lines + header space
		if batchRows > 6 {
			batchRows = 6
		}
		vpHeight -= batchRows
	}
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.Height = vpHeight
}

func (m *ConnectedModel) trimOutput() {
	content := m.outputBuf.String()
	lines := strings.Split(content, "\n")
	if len(lines) > maxOutputLines {
		lines = lines[len(lines)-maxOutputLines:]
		m.outputBuf.Reset()
		m.outputBuf.WriteString(strings.Join(lines, "\n"))
	}
}

// sendLine writes a single command (+ CR) to the modem.
func (m ConnectedModel) sendLine(line string) tea.Cmd {
	rwc := m.rwc
	return func() tea.Msg {
		var data []byte
		if line != "" {
			data = append([]byte(line), '\r')
		} else {
			data = []byte{'\r'}
		}
		if _, err := rwc.Write(data); err != nil {
			return modemDisconnectMsg{err: err}
		}
		return nil
	}
}

// sendBatch sends all queued batch commands with a small delay between each.
func (m ConnectedModel) sendBatch() tea.Cmd {
	lines := make([]string, len(m.batchLines))
	copy(lines, m.batchLines)
	rwc := m.rwc
	return func() tea.Msg {
		for _, line := range lines {
			data := append([]byte(line), '\r')
			if _, err := rwc.Write(data); err != nil {
				return modemDisconnectMsg{err: err}
			}
			time.Sleep(batchDelay)
		}
		return nil
	}
}

// cleanup performs modem hangup, closes the device, logger, and releases the lock.
func (m ConnectedModel) cleanup() tea.Cmd {
	logger := m.logger
	mdm := m.mdm
	lock := m.lock
	carrierLost := m.carrierLost
	return func() tea.Msg {
		if logger != nil {
			logger.Close()
		}
		if mdm != nil {
			if carrierLost {
				slog.Info("carrier already lost, skipping hangup")
			} else {
				mdm.Hangup()
			}
			mdm.Close()
		}
		if lock != nil {
			lock.Release()
		}
		return cleanupDoneMsg{}
	}
}

// waitForModemData blocks until data arrives from the modem, then returns it
// as a message. Re-issued after each successful read to create a continuous
// read loop within the Bubble Tea command pattern.
func waitForModemData(rwc io.ReadWriteCloser) tea.Cmd {
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := rwc.Read(buf)
		if err != nil {
			return modemDisconnectMsg{err: err}
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		return modemDataMsg(data)
	}
}

// sendWake writes a CR to the modem to wake the remote device.
func sendWake(rwc io.ReadWriteCloser) tea.Cmd {
	return func() tea.Msg {
		if _, err := rwc.Write([]byte("\r")); err != nil {
			slog.Debug("wake: send failed", "err", err)
		}
		return nil
	}
}

func wakeTickCmd() tea.Cmd {
	return tea.Tick(wakeInterval, func(time.Time) tea.Msg {
		return wakeTickMsg{}
	})
}
