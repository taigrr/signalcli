package signalcli

import (
	"context"
	"fmt"
	"time"
)

// MemoryUsage returns the resident set size (RSS) of the signal-cli process in
// bytes. It returns an error if the daemon is not running or its memory cannot
// be sampled.
//
// signal-cli runs on the JVM and is prone to gradually ballooning memory; use
// this together with Watch to bound its footprint.
//
// The RSS sampling is platform-specific (see memory_*.go): Linux reads
// /proc/<pid>/statm, macOS shells out to ps, and it is unsupported on Windows.
func (d *Daemon) MemoryUsage(ctx context.Context) (uint64, error) {
	d.mu.RLock()
	running := d.running
	cmd := d.cmd
	d.mu.RUnlock()

	if !running || cmd == nil || cmd.Process == nil {
		return 0, fmt.Errorf("daemon is not running")
	}

	return sampleRSS(ctx, cmd.Process.Pid)
}

// Restart gracefully stops the daemon and starts it again.
//
// Stop waits for signal-cli to exit so it can flush local state, and messages
// that arrive while the daemon is down are queued by the Signal servers and
// redelivered on reconnect — so no messages are dropped. A Listener started
// against this daemon reconnects automatically once the daemon is back up.
func (d *Daemon) Restart(ctx context.Context) error {
	if err := d.Stop(); err != nil {
		return fmt.Errorf("stop for restart: %w", err)
	}
	if err := d.Start(ctx); err != nil {
		return fmt.Errorf("start for restart: %w", err)
	}
	return nil
}

// WatchConfig configures the memory watchdog.
type WatchConfig struct {
	// MemoryLimit is the RSS threshold in bytes. When exceeded for
	// ConsecutiveHits samples the daemon is restarted. Required (> 0).
	MemoryLimit uint64

	// Interval is how often memory is sampled (default 30s).
	Interval time.Duration

	// ConsecutiveHits is the number of back-to-back over-limit samples
	// required before restarting, to ride out transient spikes (default 3).
	ConsecutiveHits int

	// OnRestart, if set, is called just before a restart is triggered with the
	// RSS that tripped the limit.
	OnRestart func(rss uint64)

	// OnError, if set, is called when sampling or restarting fails. The watch
	// loop continues regardless.
	OnError func(err error)
}

// Watch samples the daemon's memory on an interval and restarts it when RSS
// stays above the configured limit for ConsecutiveHits samples. It blocks
// until ctx is cancelled (returning ctx.Err()) and is safe to run in its own
// goroutine alongside a Listener, which reconnects automatically across
// restarts.
func (d *Daemon) Watch(ctx context.Context, cfg WatchConfig) error {
	if cfg.MemoryLimit == 0 {
		return fmt.Errorf("WatchConfig.MemoryLimit must be > 0")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.ConsecutiveHits <= 0 {
		cfg.ConsecutiveHits = 3
	}

	// Cap the JVM heap below the watchdog limit so signal-cli grows
	// predictably and trips the restart before exhausting host memory. Only
	// derive it if the caller didn't set an explicit heap.
	d.mu.Lock()
	if d.config.JavaMaxHeapMB <= 0 {
		heapMB := int(cfg.MemoryLimit * 3 / 4 / (1024 * 1024))
		// Floor to a JVM-viable minimum so tiny limits don't truncate to 0
		// (which buildEnv treats as "no cap", leaving the JVM uncapped).
		if heapMB < 32 {
			heapMB = 32
		}
		d.config.JavaMaxHeapMB = heapMB
	}
	d.mu.Unlock()

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	hits := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		rss, err := d.MemoryUsage(ctx)
		if err != nil {
			hits = 0
			if cfg.OnError != nil {
				cfg.OnError(err)
			}
			continue
		}

		if rss < cfg.MemoryLimit {
			hits = 0
			continue
		}

		hits++
		if hits < cfg.ConsecutiveHits {
			continue
		}
		hits = 0

		if cfg.OnRestart != nil {
			cfg.OnRestart(rss)
		}
		if err := d.Restart(ctx); err != nil {
			if cfg.OnError != nil {
				cfg.OnError(err)
			}
		}
	}
}
