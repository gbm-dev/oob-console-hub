package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gbm-dev/oob-console-hub/internal/config"
	"github.com/gbm-dev/oob-console-hub/internal/modem"
)

// siteItem implements list.Item for the site selector.
type siteItem struct {
	site   config.Site
	index  int
	active bool // currently connected by someone
}

func (i siteItem) Title() string       { return i.site.Name }
func (i siteItem) Description() string { return i.site.Description }
func (i siteItem) FilterValue() string { return i.site.Name + " " + i.site.Description }

// siteDelegate renders site items in the list.
type siteDelegate struct {
	theme Theme
}

func (d siteDelegate) Height() int                             { return 1 }
func (d siteDelegate) Spacing() int                            { return 0 }
func (d siteDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d siteDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	si, ok := item.(siteItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	// Status indicator
	status := "  "
	if si.active {
		status = d.theme.NewStyle().Foreground(d.theme.ColorSuccess).Render("● ")
	}

	// Cursor
	cursor := "  "
	if isSelected {
		cursor = d.theme.NewStyle().Foreground(d.theme.ColorPrimary).Render("> ")
	}

	// Name
	var nameStyle lipgloss.Style
	if isSelected {
		nameStyle = d.theme.NewStyle().Foreground(d.theme.ColorPrimary).Bold(true)
	} else {
		nameStyle = d.theme.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
	}

	// Description + baud — dimmed, separated
	detail := d.theme.NewStyle().Foreground(d.theme.ColorMuted).Render(
		fmt.Sprintf(" — %s (%d baud)", si.site.Description, si.site.BaudRate))

	fmt.Fprintf(w, "%s%s%s%s", cursor, status, nameStyle.Render(si.site.Name), detail)
}

// MenuModel is the site selection view.
type MenuModel struct {
	list     list.Model
	sites    []config.Site
	lock     *modem.DeviceLock
	username string
	sipInfo  SIPInfo
	theme    Theme
}

// NewMenuModel creates the site selection menu.
func NewMenuModel(sites []config.Site, username string, lock *modem.DeviceLock, width, height int, theme Theme) MenuModel {
	activeSite := lock.ActiveSite()

	items := make([]list.Item, len(sites))
	for i, s := range sites {
		items[i] = siteItem{site: s, index: i, active: s.Name == activeSite}
	}

	l := list.New(items, siteDelegate{theme: theme}, width, height-4)
	l.Title = "OOB Console Hub"
	l.Styles.Title = theme.TitleStyle
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)

	return MenuModel{
		list:     l,
		sites:    sites,
		lock:     lock,
		username: username,
		theme:    theme,
	}
}

func (m MenuModel) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return checkSIPStatus() },
		sipTick(),
	)
}

func (m MenuModel) Update(msg tea.Msg) (MenuModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't intercept keys while filtering
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "enter":
			if i, ok := m.list.SelectedItem().(siteItem); ok {
				return m, func() tea.Msg { return DialRequestMsg{SiteIndex: i.index} }
			}
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height-4)
	case sipStatusMsg:
		m.sipInfo = SIPInfo(msg)
		return m, nil
	case sipTickMsg:
		return m, tea.Batch(
			func() tea.Msg { return checkSIPStatus() },
			sipTick(),
		)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m MenuModel) View() string {
	// Status bar
	var parts []string

	// 1. slmodemd health
	if m.sipInfo.ModemReady {
		parts = append(parts, m.theme.SuccessStyle.Render("● SLMODEMD"))
	} else {
		parts = append(parts, m.theme.ErrorStyle.Render("● SLMODEMD DOWN"))
	}

	// 2. slmodem-sip-bridge health
	if m.sipInfo.BridgeReady {
		parts = append(parts, m.theme.SuccessStyle.Render("● BRIDGE"))
	} else {
		parts = append(parts, m.theme.ErrorStyle.Render("● BRIDGE DOWN"))
	}

	// 3. SIP configuration health
	if m.sipInfo.Status == SIPConfigured {
		parts = append(parts, m.theme.SuccessStyle.Render("● SIP"))
	} else {
		parts = append(parts, m.theme.ErrorStyle.Render("● SIP DOWN"))
	}

	// User info
	parts = append(parts, m.theme.LabelStyle.Render(m.username))
	parts = append(parts, m.theme.LabelStyle.Render("enter connect · q quit"))

	footer := m.theme.StatusBarStyle.Render("  " + strings.Join(parts, "  │  "))
	return m.list.View() + "\n" + footer
}

// refreshItems updates the list items with current active status.
func (m *MenuModel) refreshItems() {
	activeSite := m.lock.ActiveSite()

	items := make([]list.Item, len(m.sites))
	for i, s := range m.sites {
		items[i] = siteItem{site: s, index: i, active: s.Name == activeSite}
	}
	m.list.SetItems(items)
}
