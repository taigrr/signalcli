package signalcli

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// startSleepDaemon starts a Daemon whose "signal-cli" process is really a
// `sleep`, and marks it running with a monitor + done channel, mirroring what
// Start does. Reachability checks are bypassed because we set the fields
// directly. It returns the daemon and a cleanup func.
func startSleepDaemon(t *testing.T) *Daemon {
	t.Helper()

	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	d := NewDaemon(DaemonConfig{CLIPath: "sleep"})
	cmd := exec.Command("sleep", "60")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}

	d.mu.Lock()
	d.cmd = cmd
	d.running = true
	d.done = make(chan struct{})
	d.mu.Unlock()
	go d.monitor(cmd, d.done)

	t.Cleanup(func() { d.Stop() })
	return d
}

func TestMemoryUsageNotRunning(t *testing.T) {
	d := NewDaemon(DaemonConfig{CLIPath: "signal-cli"})
	if _, err := d.MemoryUsage(context.Background()); err == nil {
		t.Error("expected error when daemon is not running")
	}
}

func TestMemoryUsageRunning(t *testing.T) {
	d := startSleepDaemon(t)

	rss, err := d.MemoryUsage(context.Background())
	if err != nil {
		t.Fatalf("MemoryUsage: %v", err)
	}
	if rss == 0 {
		t.Error("expected non-zero RSS for running process")
	}
}

func TestStopWaitsForExit(t *testing.T) {
	d := startSleepDaemon(t)

	start := time.Now()
	if err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if time.Since(start) > 9*time.Second {
		t.Error("Stop should return promptly after graceful exit")
	}
	if d.IsRunning() {
		t.Error("daemon should not be running after Stop")
	}
}

func TestWatchRequiresLimit(t *testing.T) {
	d := NewDaemon(DaemonConfig{CLIPath: "signal-cli"})
	if err := d.Watch(context.Background(), WatchConfig{}); err == nil {
		t.Error("Watch should reject a zero MemoryLimit")
	}
}

func TestWatchDerivesHeapFromLimit(t *testing.T) {
	d := startSleepDaemon(t)

	ctx, cancel := context.WithCancel(t.Context())
	// 256 MiB limit -> 3/4 = 192 MiB heap.
	go d.Watch(ctx, WatchConfig{
		MemoryLimit: 256 * 1024 * 1024,
		Interval:    time.Hour, // don't actually sample/restart
	})

	// Give Watch a moment to apply the derived heap, then stop it.
	time.Sleep(50 * time.Millisecond)
	cancel()

	d.mu.RLock()
	heap := d.config.JavaMaxHeapMB
	d.mu.RUnlock()

	if heap != 192 {
		t.Errorf("expected derived heap 192 MB, got %d", heap)
	}
}

func TestWatchTriggersRestart(t *testing.T) {
	d := startSleepDaemon(t)

	restarts := make(chan uint64, 1)
	cfg := WatchConfig{
		MemoryLimit:     1, // 1 byte: always exceeded
		Interval:        10 * time.Millisecond,
		ConsecutiveHits: 2,
		OnRestart: func(rss uint64) {
			select {
			case restarts <- rss:
			default:
			}
		},
	}

	// Restart will call Start, which tries to launch "sleep" as the daemon and
	// wait for reachability (which never succeeds on a fake daemon). Run Watch
	// in the background and just assert the restart callback fires.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go d.Watch(ctx, cfg)

	select {
	case rss := <-restarts:
		if rss < cfg.MemoryLimit {
			t.Errorf("restart rss %d below limit %d", rss, cfg.MemoryLimit)
		}
	case <-time.After(3 * time.Second):
		t.Error("watchdog did not trigger a restart")
	}
}
