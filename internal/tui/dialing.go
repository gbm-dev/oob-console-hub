package tui

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gbm-dev/oob-console-hub/internal/config"
	"github.com/gbm-dev/oob-console-hub/internal/modem"
)

// Keep this slightly above Asterisk's Dial() timeout (120s in extensions.conf)
// so we can receive final modem result codes like NO CARRIER instead of a
// premature local TIMEOUT.
const dialTimeout = 125 * time.Second
const resetTimeout = 5 * time.Second
const maxRetries = 3
const retryDelay = 2 * time.Second

// DialingModel shows connection progress with a spinner.
type DialingModel struct {
	spinner    spinner.Model
	site       config.Site
	status     string
	device     string
	transcript string
	showDebug  bool
	err        error
	done       bool
	lock       *modem.DeviceLock
	theme      Theme
}

// NewDialingModel creates a dialing view for the given site.
func NewDialingModel(site config.Site, lock *modem.DeviceLock, theme Theme) DialingModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.WarningStyle
	return DialingModel{
		spinner: s,
		site:    site,
		status:  "Acquiring modem...",
		lock:    lock,
		theme:   theme,
	}
}

func (m DialingModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.acquireAndDial())
}

func (m DialingModel) Update(msg tea.Msg) (DialingModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case statusMsg:
		m.status = string(msg)
		return m, nil

	case DialResultMsg:
		if msg.Result == modem.ResultConnect {
			m.status = m.theme.SuccessStyle.Render("CONNECTED")
			m.device = msg.Device
			return m, nil
		}
		m.done = true
		m.device = msg.Device
		m.transcript = msg.Transcript
		m.err = fmt.Errorf("%s", msg.Result)
		return m, nil

	case ErrorMsg:
		m.done = true
		m.err = msg.Err
		return m, nil

	case tea.KeyMsg:
		if m.done && (msg.String() == "d" || msg.String() == "D") {
			m.showDebug = !m.showDebug
			return m, nil
		}
	}

	return m, nil
}

func (m DialingModel) View() string {
	header := m.theme.TitleStyle.Render(fmt.Sprintf("Connecting to %s", m.site.Name))

	details := fmt.Sprintf(
		"  Phone:  %s\n  Baud:   %d\n  Device: %s",
		m.site.Phone, m.site.BaudRate, m.deviceDisplay())

	if m.err != nil {
		view := header + "\n\n" + details + "\n\n" +
			m.theme.ErrorStyle.Render(fmt.Sprintf("  Error: %s", m.err))
		if m.transcript != "" && m.showDebug {
			view += "\n\n" + m.theme.LabelStyle.Render("  AT log:") + "\n" +
				m.theme.NewStyle().Foreground(m.theme.ColorMuted).PaddingLeft(4).Render(m.transcript)
		}
		if m.transcript != "" {
			if m.showDebug {
				view += "\n\n" + m.theme.LabelStyle.Render("  Press D to hide debug log")
			} else {
				view += "\n\n" + m.theme.LabelStyle.Render("  Press D to show debug log")
			}
		}
		view += "\n" + m.theme.LabelStyle.Render("  Press Enter to return to menu")
		return m.theme.BoxStyle.Render(view)
	}

	return m.theme.BoxStyle.Render(
		header + "\n\n" + details + "\n\n" +
			fmt.Sprintf("  %s %s", m.spinner.View(), m.status),
	)
}

func (m DialingModel) deviceDisplay() string {
	if m.device == "" {
		return "—"
	}
	return m.device
}

// retryable returns true for dial results that may succeed on retry.
func retryable(r modem.DialResult) bool {
	return r == modem.ResultNoCarrier || r == modem.ResultTimeout
}

// acquireAndDial runs the modem acquire → reset → configure → dial sequence
// with automatic retries on transient failures (NO CARRIER, TIMEOUT).
func (m DialingModel) acquireAndDial() tea.Cmd {
	return func() tea.Msg {
		// Step 1: Acquire device
		dev, err := m.lock.Acquire(m.site.Name)
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("modem busy: %w", err), Context: "acquire"}
		}

		var lastResp modem.DialResponse
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if attempt > 1 {
				time.Sleep(retryDelay)
			}

			// Open device
			mdm, err := modem.Open(dev)
			if err != nil {
				m.lock.Release()
				return ErrorMsg{Err: fmt.Errorf("failed to open %s: %w", dev, err), Context: "open"}
			}

			// Initialize modem (ATE0 + ATZ)
			if err := mdm.Init(resetTimeout); err != nil {
				mdm.Close()
				m.lock.Release()
				return ErrorMsg{Err: fmt.Errorf("modem init failed: %w", err), Context: "init"}
			}

			// Send pre-dial configuration commands if any
			if len(m.site.ModemInit) > 0 {
				if err := mdm.Configure(m.site.ModemInit, resetTimeout); err != nil {
					mdm.Close()
					m.lock.Release()
					return ErrorMsg{Err: fmt.Errorf("modem configure failed: %w", err), Context: "configure"}
				}
			}

			// Dial
			resp, err := mdm.Dial(m.site.Phone, dialTimeout)
			if err != nil {
				mdm.Hangup()
				mdm.Close()
				m.lock.Release()
				return ErrorMsg{Err: fmt.Errorf("dial error: %w", err), Context: "dial"}
			}

			if resp.Result == modem.ResultConnect {
				return DialResultMsg{Result: resp.Result, Transcript: resp.Transcript, Modem: mdm, Device: dev}
			}

			lastResp = resp
			slog.Info("dial failed, checking retry", "result", resp.Result, "attempt", attempt, "max", maxRetries)

			// Clean up before potential retry
			mdm.Hangup()
			mdm.Close()

			// Non-retryable results: fail immediately
			if !retryable(resp.Result) {
				m.lock.Release()
				return DialResultMsg{Result: resp.Result, Transcript: resp.Transcript, Device: dev}
			}

			if attempt < maxRetries {
				slog.Info("retrying dial", "attempt", attempt+1, "max", maxRetries)
			}
		}

		// All retries exhausted
		m.lock.Release()
		return DialResultMsg{Result: lastResp.Result, Transcript: lastResp.Transcript, Device: dev}
	}
}
