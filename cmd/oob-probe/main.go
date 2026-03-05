package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gbm-dev/oob-console-hub/internal/modem"
	"github.com/gbm-dev/oob-console-hub/internal/session"
)

func main() {
	device := flag.String("device", envOr("DEVICE_PATH", "/dev/ttySL0"), "modem device path")
	dial := flag.String("dial", "", "phone number to dial (omit to test init only)")
	initCmds := flag.String("init", "", "semicolon-separated AT commands to send after modem init (e.g. AT+MS=132,0,4800,9600)")
	logDir := flag.String("logdir", envOr("LOG_DIR", "./logs"), "directory for session transcript logs")
	timeout := flag.Duration("timeout", 60*time.Second, "total timeout after CONNECT (0 = run until Ctrl+C)")
	enterInterval := flag.Duration("enter-interval", 2*time.Second, "how often to send Enter after CONNECT")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	os.MkdirAll(*logDir, 0755)

	if err := run(ctx, *device, *dial, *initCmds, *logDir, *timeout, *enterInterval); err != nil {
		slog.Error("probe failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, device, dialNum, initCmds, logDir string, timeout, enterInterval time.Duration) error {
	const resetTimeout = 5 * time.Second
	const dialTimeout = 125 * time.Second

	// Open modem
	slog.Info("opening modem", "device", device)
	mdm, err := modem.Open(device)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer mdm.Close()

	// Init (ATZ + ATE0)
	slog.Info("initializing modem")
	if err := mdm.Init(resetTimeout); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	fmt.Fprintln(os.Stderr, "--- Modem initialized ---")

	// Configure (optional AT commands)
	if initCmds != "" {
		cmds := splitCmds(initCmds)
		slog.Info("configuring modem", "commands", cmds)
		if err := mdm.Configure(cmds, resetTimeout); err != nil {
			return fmt.Errorf("configure: %w", err)
		}
		fmt.Fprintln(os.Stderr, "--- Modem configured ---")
	}

	// Print transcript so far
	if t := mdm.Transcript(); t != "" {
		fmt.Fprintf(os.Stderr, "--- AT Transcript ---\n%s--- End Transcript ---\n", t)
	}

	// If no dial number, we're done
	if dialNum == "" {
		slog.Info("no dial number specified, exiting after init")
		return nil
	}

	// Dial
	slog.Info("dialing", "number", dialNum)
	resp, err := mdm.Dial(dialNum, dialTimeout)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	fmt.Fprintf(os.Stderr, "--- Dial result: %s ---\n", resp.Result)
	if resp.Result != modem.ResultConnect {
		fmt.Fprintf(os.Stderr, "--- Transcript ---\n%s--- End ---\n", resp.Transcript)
		return fmt.Errorf("dial failed: %s", resp.Result)
	}

	// Connected — start session logger and Enter-sending loop
	return connectedLoop(ctx, mdm, device, dialNum, logDir, timeout, enterInterval)
}

func connectedLoop(ctx context.Context, mdm *modem.Modem, device, dialNum, logDir string, timeout, enterInterval time.Duration) error {
	logger, err := session.NewLogger(logDir, "probe-"+dialNum, device)
	if err != nil {
		return fmt.Errorf("session logger: %w", err)
	}
	defer logger.Close()

	rwc := mdm.ReadWriteCloser()
	loggedReader := logger.TeeReader(rwc)

	fmt.Fprintln(os.Stderr, "--- CONNECTED — sending Enter, watching for output ---")
	fmt.Fprintf(os.Stderr, "--- Log: %s ---\n", logger.Path())

	// Determine deadline
	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}

	// Read modem output → stdout + log
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(os.Stdout, loggedReader)
		readDone <- err
	}()

	// Send Enter periodically
	ticker := time.NewTicker(enterInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\n--- Interrupted, hanging up ---")
			mdm.Hangup()
			return nil
		case <-deadline:
			fmt.Fprintln(os.Stderr, "\n--- Timeout reached, hanging up ---")
			mdm.Hangup()
			return nil
		case err := <-readDone:
			if err != nil {
				slog.Warn("read ended", "err", err)
			}
			fmt.Fprintln(os.Stderr, "\n--- Connection closed ---")
			return nil
		case <-ticker.C:
			if _, err := rwc.Write([]byte("\r")); err != nil {
				slog.Warn("failed to send Enter", "err", err)
				return nil
			}
			slog.Debug("sent Enter")
		}
	}
}

func splitCmds(s string) []string {
	var cmds []string
	for _, cmd := range strings.Split(s, ";") {
		cmd = strings.TrimSpace(cmd)
		if cmd != "" {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
