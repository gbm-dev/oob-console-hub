package tui

import (
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gbm-dev/oob-console-hub/internal/auth"
	"github.com/gbm-dev/oob-console-hub/internal/config"
	"github.com/gbm-dev/oob-console-hub/internal/modem"
)

// Model is the root Bubble Tea model that manages the TUI state machine.
type Model struct {
	state    State
	width    int
	height   int
	username string
	logDir   string
	theme    Theme

	// Dependencies
	lock  *modem.DeviceLock
	store auth.UserStore
	sites []config.Site

	// Sub-models
	menu      MenuModel
	dialing   DialingModel
	password  PasswordModel
	connected ConnectedModel

	// Active dial state
	activeModem  *modem.Modem
	activeDevice string
	activeSite   config.Site
}

// New creates the root TUI model.
func New(username string, sites []config.Site, lock *modem.DeviceLock, store auth.UserStore, logDir string, forcePassword bool, renderer *lipgloss.Renderer) Model {
	state := StateMenu
	if forcePassword {
		state = StatePasswordChange
	}

	m := Model{
		state:    state,
		username: username,
		logDir:   logDir,
		lock:     lock,
		store:    store,
		sites:    sites,
		width:    80,
		height:   24,
		theme:    NewTheme(renderer),
	}

	if forcePassword {
		m.password = NewPasswordModel(username, store, m.theme)
	} else {
		m.menu = NewMenuModel(sites, username, lock, m.width, m.height, m.theme)
	}

	return m
}

func (m Model) Init() tea.Cmd {
	switch m.state {
	case StatePasswordChange:
		return m.password.Init()
	case StateMenu:
		return m.menu.Init()
	default:
		return nil
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	switch m.state {
	case StatePasswordChange:
		return m.updatePasswordChange(msg)
	case StateMenu:
		return m.updateMenu(msg)
	case StateDialing:
		return m.updateDialing(msg)
	case StateConnected:
		return m.updateConnected(msg)
	}
	return m, nil
}

func (m Model) View() string {
	switch m.state {
	case StatePasswordChange:
		return m.password.View()
	case StateMenu:
		return m.menu.View()
	case StateDialing:
		return m.dialing.View()
	case StateConnected:
		return m.connected.View()
	default:
		return ""
	}
}

func (m Model) updatePasswordChange(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case PasswordChangedMsg:
		m.menu = NewMenuModel(m.sites, m.username, m.lock, m.width, m.height, m.theme)
		m.state = StateMenu
		return m, m.menu.Init()
	case ErrorMsg:
		errMsg := msg.(ErrorMsg)
		m.password.err = errMsg.Err.Error()
		return m, nil
	}

	var cmd tea.Cmd
	m.password, cmd = m.password.Update(msg)
	return m, cmd
}

func (m Model) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DialRequestMsg:
		if msg.SiteIndex >= 0 && msg.SiteIndex < len(m.sites) {
			m.activeSite = m.sites[msg.SiteIndex]
			m.dialing = NewDialingModel(m.activeSite, m.lock, m.theme)
			m.state = StateDialing
			return m, m.dialing.Init()
		}
	}

	var cmd tea.Cmd
	m.menu, cmd = m.menu.Update(msg)
	return m, cmd
}

func (m Model) updateDialing(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case DialResultMsg:
		if msg.Result == modem.ResultConnect {
			m.activeModem = msg.Modem
			m.activeDevice = msg.Device
			m.state = StateConnected

			connected, err := NewConnectedModel(
				msg.Modem, msg.Device, m.activeSite.Name,
				m.logDir, m.lock, m.width, m.height, m.theme,
			)
			if err != nil {
				slog.Error("failed to create connected session", "err", err)
				msg.Modem.Hangup()
				msg.Modem.Close()
				m.lock.Release()
				return m.returnToMenu()
			}
			m.connected = connected
			return m, m.connected.Init()
		}
	case tea.KeyMsg:
		// Keep this transition in the root model to avoid async command hops
		// when leaving the failed-dial screen.
		if msg.String() == "ctrl+c" || (m.dialing.done && msg.String() == "enter") {
			return m.returnToMenu()
		}
	case ErrorMsg:
		// Let dialing model handle it for display
	}

	var cmd tea.Cmd
	m.dialing, cmd = m.dialing.Update(msg)
	return m, cmd
}

func (m Model) updateConnected(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case TerminalDoneMsg:
		termMsg := msg.(TerminalDoneMsg)
		if termMsg.Err != nil {
			slog.Info("terminal session ended", "err", termMsg.Err)
		}
		return m.returnToMenu()
	}

	var cmd tea.Cmd
	m.connected, cmd = m.connected.Update(msg)
	return m, cmd
}

func (m Model) returnToMenu() (tea.Model, tea.Cmd) {
	m.menu = NewMenuModel(m.sites, m.username, m.lock, m.width, m.height, m.theme)
	m.state = StateMenu
	m.activeModem = nil
	m.activeDevice = ""

	return m, tea.Batch(
		tea.ClearScreen,
		m.menu.Init(),
	)
}
