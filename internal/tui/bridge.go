package tui

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const (
	// bridgeExitTimeout is the maximum time to wait for the bridge process to
	// exit after a modem hangup. Must be long enough for the bridge to tear
	// down the Asterisk call and WebSocket connections gracefully.
	bridgeExitTimeout = 15 * time.Second

	// bridgePollInterval is how often we check whether the bridge has exited.
	bridgePollInterval = 500 * time.Millisecond

	// bridgeKillGrace is the time to wait after sending SIGTERM before
	// declaring the bridge stuck.
	bridgeKillGrace = 2 * time.Second

	// bridgePgrepTimeout bounds each pgrep/kill subprocess invocation.
	bridgePgrepTimeout = 2 * time.Second

	// bridgePollIterationsMax is the upper bound on poll loop iterations.
	// At 500ms intervals over 15s, this is 30 — but we assert it explicitly
	// to satisfy the "put a limit on everything" rule.
	bridgePollIterationsMax = 40
)

// waitBridgeExit polls until the slmodem-asterisk-bridge child process exits,
// then returns nil. If the bridge does not exit within bridgeExitTimeout, it
// is killed with SIGTERM and we wait bridgeKillGrace for it to die.
//
// Why this exists: slmodemd spawns the bridge as an external helper on each
// ATDT. The bridge creates an Asterisk call and relays media via WebSocket.
// When the modem gets NO CARRIER and we hang up, slmodemd does not
// immediately kill the bridge — the stale bridge keeps the old Asterisk call
// alive. If we retry ATDT before the bridge exits, slmodemd cannot set up a
// clean audio path and the modem gets immediate NO CARRIER.
func waitBridgeExit() error {
	if !bridgeProcessRunning() {
		return nil // Already gone, no wait needed.
	}

	slog.Info("waiting for bridge process to exit before retry")
	deadline := time.Now().Add(bridgeExitTimeout)

	for i := 0; i < bridgePollIterationsMax; i++ {
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(bridgePollInterval)
		if !bridgeProcessRunning() {
			slog.Info("bridge process exited", "waited_iterations", i+1)
			return nil
		}
	}

	// Bridge did not exit in time. Kill it so the next attempt has a clean
	// audio path through slmodemd.
	slog.Warn("bridge process did not exit within timeout, sending SIGTERM",
		"timeout", bridgeExitTimeout)
	killBridgeProcess()

	time.Sleep(bridgeKillGrace)
	if bridgeProcessRunning() {
		return fmt.Errorf(
			"bridge process still running after SIGTERM + %s grace period", bridgeKillGrace)
	}

	slog.Info("bridge process terminated after SIGTERM")
	return nil
}

// bridgeProcessRunning returns true if a slmodem-asterisk-bridge child process
// is currently running. Distinguishes the bridge child from the slmodemd
// parent, which also contains the bridge binary path in its command line.
func bridgeProcessRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), bridgePgrepTimeout)
	defer cancel()

	out, err := exec.CommandContext(
		ctx, "pgrep", "-fa", "slmodem-asterisk-bridge",
	).CombinedOutput()
	if err != nil {
		return false // pgrep returns non-zero when no processes match.
	}

	return parseBridgeRunning(string(out))
}

// parseBridgeRunning checks pgrep -fa output for a running bridge process.
// Returns true if any line matches the bridge binary but not slmodemd.
//
// pgrep -fa output format: "PID COMMAND_LINE"
//
//	slmodemd line: "100 slmodemd -e /usr/local/bin/slmodem-asterisk-bridge"
//	bridge line:   "212 /usr/local/bin/slmodem-asterisk-bridge --arg ..."
//
// The slmodemd parent always has "slmodemd" in its command line; the bridge
// child does not. We use this to distinguish them.
func parseBridgeRunning(pgrepOutput string) bool {
	for _, line := range strings.Split(pgrepOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		hasBridge := strings.Contains(line, "slmodem-asterisk-bridge")
		hasSlmodemd := strings.Contains(line, "slmodemd")
		if hasBridge && !hasSlmodemd {
			return true
		}
	}
	return false
}

// killBridgeProcess sends SIGTERM to bridge child processes, carefully
// avoiding the slmodemd parent that also has the bridge path in its args.
func killBridgeProcess() {
	ctx, cancel := context.WithTimeout(context.Background(), bridgePgrepTimeout)
	defer cancel()

	out, err := exec.CommandContext(
		ctx, "pgrep", "-fa", "slmodem-asterisk-bridge",
	).CombinedOutput()
	if err != nil {
		return // No matching processes.
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip the slmodemd parent — only kill the bridge child.
		if strings.Contains(line, "slmodemd") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		pid := fields[0]
		slog.Warn("killing stale bridge process", "pid", pid)
		killCtx, killCancel := context.WithTimeout(
			context.Background(), bridgePgrepTimeout)
		_ = exec.CommandContext(killCtx, "kill", pid).Run()
		killCancel()
	}
}
