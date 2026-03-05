package modem

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode"
)

// DialResult represents the outcome of a dial attempt.
type DialResult int

const (
	ResultConnect DialResult = iota
	ResultBusy
	ResultNoCarrier
	ResultNoDialtone
	ResultError
	ResultTimeout
)

func (r DialResult) String() string {
	switch r {
	case ResultConnect:
		return "CONNECT"
	case ResultBusy:
		return "BUSY"
	case ResultNoCarrier:
		return "NO CARRIER"
	case ResultNoDialtone:
		return "NO DIALTONE"
	case ResultError:
		return "ERROR"
	case ResultTimeout:
		return "TIMEOUT"
	default:
		return "UNKNOWN"
	}
}

// DialResponse holds the result and raw AT transcript from a dial attempt.
type DialResponse struct {
	Result     DialResult
	Transcript string // raw AT command/response exchange
}

// Modem represents an open modem device.
type Modem struct {
	dev  *os.File
	path string
	log  strings.Builder // accumulates the full AT transcript
}

// Open opens a modem device at the given path.
func Open(devicePath string) (*Modem, error) {
	f, err := os.OpenFile(devicePath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening modem device %s: %w", devicePath, err)
	}
	m := &Modem{
		dev:  f,
		path: devicePath,
	}
	slog.Debug("modem opened", "device", devicePath)
	return m, nil
}

// Init sends ATE0 (disable echo) and ATZ (reset). Call after Open.
func (m *Modem) Init(timeout time.Duration) error {
	// Drain any stale data in the buffer
	m.drain()

	// Force command mode: if modem is stuck in online/data mode after a
	// failed dial, the +++ escape sequence returns it to command mode.
	slog.Debug("modem init: sending escape sequence", "device", m.path)
	time.Sleep(1100 * time.Millisecond)
	m.dev.Write([]byte("+++"))
	time.Sleep(1100 * time.Millisecond)
	m.drain()

	// Send ATH to hang up any lingering connection
	m.dev.Write([]byte("ATH\r"))
	m.readUntil(2*time.Second, "OK", "ERROR", "NO CARRIER")
	m.drain()

	// Reset modem first. ATZ can restore default settings.
	resp, err := m.runAT("ATZ", timeout, "OK", "ERROR")
	if err != nil {
		return fmt.Errorf("ATZ: no response (%w)", err)
	}
	if strings.Contains(resp, "ERROR") {
		return fmt.Errorf("ATZ returned ERROR: %s", cleanResponse(resp))
	}
	m.drain()

	// Disable echo after reset so it stays off for dial commands.
	resp, err = m.runAT("ATE0", timeout, "OK", "ERROR")
	if err != nil {
		return fmt.Errorf("ATE0: no response (%w)", err)
	}
	if strings.Contains(resp, "ERROR") {
		return fmt.Errorf("ATE0 returned ERROR: %s", cleanResponse(resp))
	}

	// Enable blind dialing (ignore dial tone)
	resp, err = m.runAT("ATX3", timeout, "OK", "ERROR")
	if err != nil {
		return fmt.Errorf("ATX3: no response (%w)", err)
	}
	if strings.Contains(resp, "ERROR") {
		return fmt.Errorf("ATX3 returned ERROR: %s", cleanResponse(resp))
	}

	// Drain again after reset to clear any echo/noise
	m.drain()
	return nil
}

// Configure sends a sequence of AT commands (e.g. AT+MS=132,0,4800,9600).
// Each command must return OK within the timeout. Called after Init, before Dial.
func (m *Modem) Configure(commands []string, timeout time.Duration) error {
	for _, cmd := range commands {
		m.drain()
		resp, err := m.runAT(cmd, timeout, "OK", "ERROR")
		if err != nil {
			return fmt.Errorf("%s: no response (%w)", cmd, err)
		}
		if strings.Contains(resp, "ERROR") {
			return fmt.Errorf("%s returned ERROR: %s", cmd, cleanResponse(resp))
		}
	}
	return nil
}

// Dial sends ATDT and returns the result with full transcript.
func (m *Modem) Dial(phone string, timeout time.Duration) (DialResponse, error) {
	// Drain before dialing to ensure clean buffer
	m.drain()

	cmd := fmt.Sprintf("ATDT%s", phone)
	m.logCmd(cmd)
	if _, err := m.dev.Write([]byte(cmd + "\r")); err != nil {
		return DialResponse{Result: ResultError, Transcript: m.log.String()},
			fmt.Errorf("sending ATDT: %w", err)
	}

	resp, err := m.readUntil(timeout, "CONNECT", "BUSY", "NO CARRIER", "NO DIALTONE", "ERROR")
	m.logResp(resp)

	transcript := m.log.String()

	if err != nil {
		slog.Warn("modem dial timeout", "device", m.path, "transcript", transcript)
		return DialResponse{Result: ResultTimeout, Transcript: transcript}, nil
	}

	var result DialResult
	switch {
	case strings.Contains(resp, "CONNECT"):
		result = ResultConnect
	case strings.Contains(resp, "BUSY"):
		result = ResultBusy
	case strings.Contains(resp, "NO CARRIER"):
		result = ResultNoCarrier
	case strings.Contains(resp, "NO DIALTONE"):
		result = ResultNoDialtone
	default:
		result = ResultError
	}

	slog.Info("modem dial result", "device", m.path, "result", result.String(), "transcript", transcript)
	return DialResponse{Result: result, Transcript: transcript}, nil
}

// Transcript returns the accumulated AT command log.
func (m *Modem) Transcript() string {
	return m.log.String()
}

// Hangup sends the escape sequence and ATH to hang up.
func (m *Modem) Hangup() error {
	slog.Debug("modem hangup", "device", m.path)
	time.Sleep(1100 * time.Millisecond)
	if _, err := m.dev.Write([]byte("+++")); err != nil {
		return fmt.Errorf("sending escape: %w", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := m.dev.Write([]byte("ATH\r")); err != nil {
		return fmt.Errorf("sending ATH: %w", err)
	}
	m.readUntil(3*time.Second, "OK", "ERROR")
	return nil
}

// ReadWriteCloser returns the underlying device for raw I/O pass-through.
func (m *Modem) ReadWriteCloser() io.ReadWriteCloser {
	return NewBufferedReadWriteCloser(m.dev)
}

// BufferedReadWriteCloser eagerly drains the underlying reader in a background
// goroutine and provides data via a channel. This prevents PTY buffer
// backpressure from affecting modem timing sensitive tasks like V.32 training.
type BufferedReadWriteCloser struct {
	inner  io.ReadWriteCloser
	dataCh chan []byte
	errCh  chan error
	close  chan struct{}
	done   chan struct{}
}

func NewBufferedReadWriteCloser(inner io.ReadWriteCloser) *BufferedReadWriteCloser {
	b := &BufferedReadWriteCloser{
		inner:  inner,
		dataCh: make(chan []byte, 100),
		errCh:  make(chan error, 1),
		close:  make(chan struct{}),
		done:   make(chan struct{}),
	}

	go b.readLoop()
	return b
}

func (b *BufferedReadWriteCloser) readLoop() {
	defer close(b.done)
	buf := make([]byte, 4096)
	for {
		select {
		case <-b.close:
			return
		default:
			n, err := b.inner.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				select {
				case b.dataCh <- data:
				case <-b.close:
					return
				}
			}
			if err != nil {
				select {
				case b.errCh <- err:
				case <-b.close:
				}
				return
			}
		}
	}
}

func (b *BufferedReadWriteCloser) Read(p []byte) (int, error) {
	select {
	case data := <-b.dataCh:
		n := copy(p, data)
		// Note: if len(p) < len(data), the remainder is lost.
		// In our TUI usage, p is always 4096 and data chunks are small.
		return n, nil
	case err := <-b.errCh:
		return 0, err
	case <-b.close:
		return 0, io.EOF
	}
}

func (b *BufferedReadWriteCloser) Write(p []byte) (int, error) {
	return b.inner.Write(p)
}

func (b *BufferedReadWriteCloser) Close() error {
	close(b.close)
	<-b.done
	return b.inner.Close()
}

// Close closes the modem device.
func (m *Modem) Close() error {
	slog.Debug("modem closed", "device", m.path)
	return m.dev.Close()
}

// drain reads and discards any buffered data from the modem.
func (m *Modem) drain() {
	m.dev.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 1024)
	for {
		n, err := m.dev.Read(buf)
		if n > 0 {
			slog.Debug("modem drain", "device", m.path, "bytes", n)
		}
		if err != nil {
			break
		}
	}
	m.dev.SetReadDeadline(time.Time{})
}

func (m *Modem) logCmd(cmd string) {
	line := fmt.Sprintf(">>> %s\n", cmd)
	m.log.WriteString(line)
	slog.Debug("modem send", "device", m.path, "cmd", cmd)
}

func (m *Modem) logResp(resp string) {
	cleaned := cleanResponse(resp)
	if cleaned != "" {
		line := fmt.Sprintf("<<< %s\n", cleaned)
		m.log.WriteString(line)
		slog.Debug("modem recv", "device", m.path, "resp", cleaned)
	}
}

func (m *Modem) runAT(cmd string, timeout time.Duration, matches ...string) (string, error) {
	m.logCmd(cmd)
	if _, err := m.dev.Write([]byte(cmd + "\r")); err != nil {
		return "", fmt.Errorf("sending %s: %w", cmd, err)
	}
	resp, err := m.readUntil(timeout, matches...)
	m.logResp(resp)
	return resp, err
}

// readUntil reads lines until one contains a match string or timeout.
func (m *Modem) readUntil(timeout time.Duration, matches ...string) (string, error) {
	started := time.Now()
	deadline := started.Add(timeout)
	var accumulated strings.Builder
	buf := make([]byte, 1024)

	upperMatches := make([]string, len(matches))
	for i, match := range matches {
		upperMatches[i] = strings.ToUpper(match)
	}

	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		readStep := 500 * time.Millisecond
		if remaining < readStep {
			readStep = remaining
		}
		if readStep <= 0 {
			break
		}
		m.dev.SetReadDeadline(time.Now().Add(readStep))
		n, err := m.dev.Read(buf)
		if n > 0 {
			accumulated.Write(buf[:n])
			upperResp := strings.ToUpper(accumulated.String())
			slog.Debug("readUntil progress", "device", m.path, "read", n, "total", accumulated.Len())

			for _, match := range upperMatches {
				if strings.Contains(upperResp, match) {
					m.dev.SetReadDeadline(time.Time{})
					return accumulated.String(), nil
				}
			}
		}

		if err != nil && !os.IsTimeout(err) {
			slog.Debug("readUntil error", "device", m.path, "err", err, "type", fmt.Sprintf("%T", err))
			return accumulated.String(), err
		}
	}
	m.dev.SetReadDeadline(time.Time{})
	slog.Debug("readUntil timeout", "device", m.path, "elapsed", time.Since(started))
	return accumulated.String(), fmt.Errorf("timeout after %s", timeout)
}

// cleanResponse strips control chars and excess whitespace from modem output.
func cleanResponse(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		if !unicode.IsPrint(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
