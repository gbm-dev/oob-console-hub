package modem

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestDialResultString(t *testing.T) {
	tests := []struct {
		r    DialResult
		want string
	}{
		{ResultConnect, "CONNECT"},
		{ResultBusy, "BUSY"},
		{ResultNoCarrier, "NO CARRIER"},
		{ResultNoDialtone, "NO DIALTONE"},
		{ResultError, "ERROR"},
		{ResultTimeout, "TIMEOUT"},
	}
	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Errorf("DialResult(%d).String() = %q, want %q", tt.r, got, tt.want)
		}
	}
}

func TestConfigureSendsCommands(t *testing.T) {
	// Create a PTY pair to simulate a modem device
	ptmx, pts, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	defer ptmx.Close()
	defer pts.Close()

	m := &Modem{
		dev:  pts,
		path: pts.Name(),
	}

	// Respond OK to each command from the master side
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			cmd := string(buf[:n])
			if strings.Contains(cmd, "AT") {
				ptmx.Write([]byte("\r\nOK\r\n"))
			}
		}
	}()

	cmds := []string{"AT+MS=132,0,4800,9600", "ATS7=60"}
	if err := m.Configure(cmds, 3*time.Second); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	transcript := m.Transcript()
	for _, cmd := range cmds {
		if !strings.Contains(transcript, cmd) {
			t.Errorf("transcript missing %q:\n%s", cmd, transcript)
		}
	}
}

func TestConfigureReturnsErrorOnFailure(t *testing.T) {
	ptmx, pts, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	defer ptmx.Close()
	defer pts.Close()

	m := &Modem{
		dev:  pts,
		path: pts.Name(),
	}

	// Respond ERROR to commands
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			cmd := string(buf[:n])
			if strings.Contains(cmd, "AT") {
				ptmx.Write([]byte("\r\nERROR\r\n"))
			}
		}
	}()

	err = m.Configure([]string{"AT+MS=132,0,4800,9600"}, 3*time.Second)
	if err == nil {
		t.Fatal("expected error from Configure when modem returns ERROR")
	}
}

func TestConfigureEmptyCommands(t *testing.T) {
	// Configure with no commands should succeed without touching the device
	dir := t.TempDir()
	path := dir + "/fakemodem"
	f, _ := os.Create(path)
	f.Close()

	m := &Modem{
		dev:  f,
		path: path,
	}

	if err := m.Configure(nil, time.Second); err != nil {
		t.Fatalf("Configure with nil: %v", err)
	}
	if err := m.Configure([]string{}, time.Second); err != nil {
		t.Fatalf("Configure with empty: %v", err)
	}
}

func TestReadUntil_HandlesTimeouts(t *testing.T) {
	ptmx, pts, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	defer ptmx.Close()
	defer pts.Close()

	m := &Modem{
		dev:  pts,
		path: pts.Name(),
	}

	// Send response after 1.2 seconds (longer than the 500ms readStep)
	go func() {
		time.Sleep(1200 * time.Millisecond)
		ptmx.Write([]byte("\r\nCONNECT\r\n"))
	}()

	start := time.Now()
	resp, err := m.readUntil(3*time.Second, "CONNECT")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("readUntil failed: %v (elapsed: %v)", err, elapsed)
	}
	if !strings.Contains(resp, "CONNECT") {
		t.Errorf("expected response to contain CONNECT, got %q", resp)
	}
	if elapsed < 1200*time.Millisecond {
		t.Errorf("returned too early: %v", elapsed)
	}
}

