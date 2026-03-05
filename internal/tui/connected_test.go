package tui

import (
	"bytes"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// mockRWC is a mock ReadWriteCloser that captures writes and provides reads.
type mockRWC struct {
	writeBuf bytes.Buffer
	readBuf  *bytes.Buffer
	closed   bool
}

func newMockRWC() *mockRWC {
	return &mockRWC{readBuf: &bytes.Buffer{}}
}

func (m *mockRWC) Read(p []byte) (int, error) {
	if m.closed {
		return 0, io.EOF
	}
	if m.readBuf.Len() == 0 {
		return 0, io.EOF
	}
	return m.readBuf.Read(p)
}

func (m *mockRWC) Write(p []byte) (int, error) {
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	return m.writeBuf.Write(p)
}

func (m *mockRWC) Close() error {
	m.closed = true
	return nil
}

// newTestConnected creates a ConnectedModel wired to a mock for testing.
func newTestConnected(rwc *mockRWC) ConnectedModel {
	return ConnectedModel{
		rwc:        rwc,
		siteName:   "test-site",
		width:      80,
		height:     24,
		historyIdx: -1,
		outputBuf:  &strings.Builder{},
	}
}

func TestHandleKey_LineBuffered(t *testing.T) {
	tests := []struct {
		name      string
		keys      []string
		wantModem string
	}{
		{
			name:      "line sent on enter",
			keys:      []string{"h", "e", "l", "l", "o", "enter"},
			wantModem: "hello\r",
		},
		{
			name:      "empty enter sends CR",
			keys:      []string{"enter"},
			wantModem: "\r",
		},
		{
			name:      "multiple lines",
			keys:      []string{"a", "b", "enter", "c", "d", "enter"},
			wantModem: "ab\rcd\r",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rwc := newMockRWC()
			m := newTestConnected(rwc)

			for _, key := range tt.keys {
				var cmd tea.Cmd
				m, cmd = m.Update(tea.KeyMsg{Type: keyType(key), Runes: keyRunes(key)})
				if cmd != nil {
					msg := cmd()
					if msg != nil {
						m, _ = m.Update(msg)
					}
				}
			}

			if got := rwc.writeBuf.String(); got != tt.wantModem {
				t.Errorf("modem output = %q, want %q", got, tt.wantModem)
			}
		})
	}
}

func TestHandleKey_Backspace(t *testing.T) {
	tests := []struct {
		name      string
		keys      []string
		wantModem string
	}{
		{
			name:      "backspace removes last char",
			keys:      []string{"h", "e", "l", "o", "backspace", "l", "o", "enter"},
			wantModem: "hello\r",
		},
		{
			name:      "backspace on empty buffer is no-op",
			keys:      []string{"backspace", "backspace", "h", "i", "enter"},
			wantModem: "hi\r",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rwc := newMockRWC()
			m := newTestConnected(rwc)

			for _, key := range tt.keys {
				var cmd tea.Cmd
				m, cmd = m.Update(tea.KeyMsg{Type: keyType(key), Runes: keyRunes(key)})
				if cmd != nil {
					msg := cmd()
					if msg != nil {
						m, _ = m.Update(msg)
					}
				}
			}

			if got := rwc.writeBuf.String(); got != tt.wantModem {
				t.Errorf("modem output = %q, want %q", got, tt.wantModem)
			}
		})
	}
}

func TestHandleKey_EscapeSequence(t *testing.T) {
	tests := []struct {
		name      string
		keys      []string
		wantModem string
		wantClean bool
	}{
		{
			name:      "empty enter then tilde dot disconnects",
			keys:      []string{"enter", "~", ".", "enter"},
			wantModem: "\r",
			wantClean: true,
		},
		{
			name:      "tilde dot without prior enter does not disconnect",
			keys:      []string{"a", "~", ".", "b", "enter"},
			wantModem: "a~.b\r",
			wantClean: false,
		},
		{
			name:      "tilde dot after non-empty enter does not disconnect",
			keys:      []string{"x", "enter", "~", ".", "enter"},
			wantModem: "x\r~.\r",
			wantClean: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rwc := newMockRWC()
			m := newTestConnected(rwc)

			for _, key := range tt.keys {
				var cmd tea.Cmd
				m, cmd = m.Update(tea.KeyMsg{Type: keyType(key), Runes: keyRunes(key)})
				if cmd != nil {
					msg := cmd()
					if msg != nil {
						m, _ = m.Update(msg)
					}
				}
			}

			if got := rwc.writeBuf.String(); got != tt.wantModem {
				t.Errorf("modem output = %q, want %q", got, tt.wantModem)
			}
			if m.cleaning != tt.wantClean {
				t.Errorf("cleaning = %v, want %v", m.cleaning, tt.wantClean)
			}
		})
	}
}

func TestHandleKey_CtrlC(t *testing.T) {
	rwc := newMockRWC()
	m := newTestConnected(rwc)

	// Type some text then Ctrl+C — should start cleanup without sending
	for _, key := range []string{"h", "e", "l", "l", "o"} {
		m, _ = m.Update(tea.KeyMsg{Type: keyType(key), Runes: keyRunes(key)})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if got := rwc.writeBuf.String(); got != "" {
		t.Errorf("modem output = %q, want empty (nothing sent before Enter)", got)
	}
	if !m.cleaning {
		t.Error("expected cleaning=true after Ctrl+C")
	}
}

func TestHandleKey_History(t *testing.T) {
	rwc := newMockRWC()
	m := newTestConnected(rwc)

	// Send two commands
	for _, key := range []string{"f", "o", "o", "enter"} {
		var cmd tea.Cmd
		m, cmd = m.Update(tea.KeyMsg{Type: keyType(key), Runes: keyRunes(key)})
		if cmd != nil {
			msg := cmd()
			if msg != nil {
				m, _ = m.Update(msg)
			}
		}
	}
	for _, key := range []string{"b", "a", "r", "enter"} {
		var cmd tea.Cmd
		m, cmd = m.Update(tea.KeyMsg{Type: keyType(key), Runes: keyRunes(key)})
		if cmd != nil {
			msg := cmd()
			if msg != nil {
				m, _ = m.Update(msg)
			}
		}
	}

	// Press up → should show "bar"
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := string(m.input); got != "bar" {
		t.Errorf("after 1st up, input = %q, want %q", got, "bar")
	}

	// Press up again → should show "foo"
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := string(m.input); got != "foo" {
		t.Errorf("after 2nd up, input = %q, want %q", got, "foo")
	}

	// Press down → should show "bar"
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := string(m.input); got != "bar" {
		t.Errorf("after down, input = %q, want %q", got, "bar")
	}
}

func TestModemData_AppendsToOutput(t *testing.T) {
	rwc := newMockRWC()
	m := newTestConnected(rwc)

	m, _ = m.Update(modemDataMsg("Hello from modem\r\n"))

	if !strings.Contains(m.outputBuf.String(), "Hello from modem") {
		t.Errorf("output buffer should contain modem data, got %q", m.outputBuf.String())
	}
	if !m.gotData {
		t.Error("gotData should be true after receiving modem data")
	}
}

func TestModemDisconnect_StartsCleanup(t *testing.T) {
	rwc := newMockRWC()
	m := newTestConnected(rwc)

	m, _ = m.Update(modemDisconnectMsg{err: io.EOF})

	if !m.carrierLost {
		t.Error("carrierLost should be true")
	}
	if !m.cleaning {
		t.Error("cleaning should be true")
	}
	if !strings.Contains(m.outputBuf.String(), "CONNECTION LOST") {
		t.Error("output should contain CONNECTION LOST")
	}
}

func TestBatchMode(t *testing.T) {
	rwc := newMockRWC()
	m := newTestConnected(rwc)

	// Toggle batch mode on
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if !m.batchMode {
		t.Fatal("expected batch mode on")
	}

	// Type and enter two commands — should NOT send to modem yet
	for _, key := range []string{"a", "enter", "b", "enter"} {
		m, _ = m.Update(tea.KeyMsg{Type: keyType(key), Runes: keyRunes(key)})
	}
	if got := rwc.writeBuf.String(); got != "" {
		t.Errorf("batch mode should not send yet, got %q", got)
	}
	if len(m.batchLines) != 2 {
		t.Errorf("expected 2 batch lines, got %d", len(m.batchLines))
	}

	// Ctrl+D sends batch
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("expected send batch command")
	}
	msg := cmd()
	if msg != nil {
		m, _ = m.Update(msg)
	}

	if got := rwc.writeBuf.String(); got != "a\rb\r" {
		t.Errorf("batch output = %q, want %q", got, "a\rb\r")
	}
	if m.batchMode {
		t.Error("batch mode should be off after send")
	}
}

// keyType maps string key names to tea.KeyType.
func keyType(key string) tea.KeyType {
	switch key {
	case "enter":
		return tea.KeyEnter
	case "backspace":
		return tea.KeyBackspace
	case "ctrl+c":
		return tea.KeyCtrlC
	case "ctrl+b":
		return tea.KeyCtrlB
	case "ctrl+d":
		return tea.KeyCtrlD
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "esc":
		return tea.KeyEscape
	default:
		return tea.KeyRunes
	}
}

// keyRunes returns runes for printable key input.
func keyRunes(key string) []rune {
	if len(key) == 1 && key[0] >= 0x20 && key[0] <= 0x7e {
		return []rune(key)
	}
	return nil
}
