package signalcli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DaemonConfig holds configuration for the signal-cli daemon.
type DaemonConfig struct {
	// CLIPath is the path to the signal-cli binary.
	CLIPath string

	// Account is the phone number to use (e.g., "+1234567890").
	Account string

	// HTTPHost is the host to bind to (default "127.0.0.1").
	HTTPHost string

	// HTTPPort is the port to bind to (default 8080).
	HTTPPort int

	// ReceiveMode sets the receive mode (default "on-connection").
	ReceiveMode string

	// IgnoreAttachments skips downloading attachments.
	IgnoreAttachments bool

	// IgnoreStories skips story messages.
	IgnoreStories bool

	// SendReadReceipts sends read receipts automatically.
	SendReadReceipts bool

	// JavaMaxHeapMB caps the signal-cli JVM max heap via -Xmx (passed as
	// JAVA_OPTS to the subprocess). 0 means don't set it (JVM default, usually
	// ~1/4 of host RAM). When using Watch, this is auto-derived from the
	// watchdog limit if left 0. Set explicitly to cap the initial process too.
	JavaMaxHeapMB int
}

// Daemon manages the signal-cli daemon subprocess.
type Daemon struct {
	config  DaemonConfig
	baseURL string
	cmd     *exec.Cmd

	mu       sync.RWMutex
	running  bool
	err      error
	stopping bool          // set when the current process is being stopped intentionally
	done     chan struct{} // closed when the current process exits
}

// NewDaemon creates a new daemon manager.
func NewDaemon(cfg DaemonConfig) *Daemon {
	if cfg.HTTPHost == "" {
		cfg.HTTPHost = "127.0.0.1"
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8080
	}

	return &Daemon{
		config:  cfg,
		baseURL: fmt.Sprintf("http://%s:%d", cfg.HTTPHost, cfg.HTTPPort),
	}
}

// BaseURL returns the HTTP base URL for the daemon.
func (d *Daemon) BaseURL() string {
	return d.baseURL
}

// IsRunning checks if the daemon is currently running.
func (d *Daemon) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// IsReachable checks if signal-cli daemon is responding at the configured URL.
func (d *Daemon) IsReachable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", d.baseURL+"/api/v1/rpc", nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()

	// Any response (even 400) means daemon is running
	return true
}

// Start starts the signal-cli daemon if not already running.
// It blocks until the daemon is ready to accept connections.
func (d *Daemon) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return nil
	}

	// Check if already running externally
	if d.IsReachable(ctx) {
		d.running = true
		return nil
	}

	// Build command arguments
	args := d.buildArgs()

	d.cmd = exec.CommandContext(ctx, d.config.CLIPath, args...)
	d.cmd.Stdout = os.Stdout
	d.cmd.Stderr = os.Stderr
	d.cmd.Env = d.buildEnv(d.config.JavaMaxHeapMB)

	if err := d.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start signal-cli: %w", err)
	}

	d.running = true
	d.stopping = false
	d.done = make(chan struct{})

	// Wait for daemon to be ready
	if err := d.waitReady(ctx); err != nil {
		d.stopping = true // this kill is intentional; don't record its exit error
		d.cmd.Process.Kill()
		d.running = false
		go d.monitor(d.cmd, d.done) // reap the killed process
		return fmt.Errorf("signal-cli failed to become ready: %w", err)
	}

	// Monitor process in background
	go d.monitor(d.cmd, d.done)

	return nil
}

// Stop stops the signal-cli daemon.
func (d *Daemon) Stop() error {
	return d.stop(10 * time.Second)
}

// stop signals the daemon to shut down and waits up to grace for it to exit
// cleanly, so signal-cli can flush its local state (no message loss). If the
// process does not exit within grace, it is force-killed.
func (d *Daemon) stop(grace time.Duration) error {
	d.mu.Lock()

	if !d.running || d.cmd == nil || d.cmd.Process == nil {
		d.running = false
		d.mu.Unlock()
		return nil
	}

	proc := d.cmd.Process
	done := d.done
	d.running = false
	d.stopping = true
	d.mu.Unlock()

	if err := proc.Signal(os.Interrupt); err != nil {
		proc.Kill()
		if done != nil {
			<-done
		}
		return nil
	}

	if done == nil {
		return nil
	}

	select {
	case <-done:
	case <-time.After(grace):
		proc.Kill()
		<-done
	}
	return nil
}

// Wait waits for the daemon to exit.
func (d *Daemon) Wait() error {
	d.mu.RLock()
	done := d.done
	d.mu.RUnlock()

	if done == nil {
		return nil
	}

	<-done
	return d.Error()
}

// Error returns the last error from the daemon.
func (d *Daemon) Error() error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.err
}

func (d *Daemon) buildArgs() []string {
	args := []string{}

	if d.config.Account != "" {
		args = append(args, "-a", d.config.Account)
	}

	args = append(args, "daemon")
	args = append(args, "--http", fmt.Sprintf("%s:%d", d.config.HTTPHost, d.config.HTTPPort))
	args = append(args, "--no-receive-stdout")

	if d.config.ReceiveMode != "" {
		args = append(args, "--receive-mode", d.config.ReceiveMode)
	}

	if d.config.IgnoreAttachments {
		args = append(args, "--ignore-attachments")
	}

	if d.config.IgnoreStories {
		args = append(args, "--ignore-stories")
	}

	if d.config.SendReadReceipts {
		args = append(args, "--send-read-receipts")
	}

	return args
}

// buildEnv returns the environment for the signal-cli subprocess, appending an
// -Xmx cap to JAVA_OPTS when JavaMaxHeapMB is set. It preserves any existing
// JAVA_OPTS (e.g. from the parent env) so user-supplied JVM flags are kept.
func (d *Daemon) buildEnv(heapMB int) []string {
	if heapMB <= 0 {
		return nil // inherit parent environment unchanged
	}

	env := os.Environ()
	xmx := fmt.Sprintf("-Xmx%dm", heapMB)

	for i, kv := range env {
		opts, ok := strings.CutPrefix(kv, "JAVA_OPTS=")
		if !ok {
			continue
		}
		// Drop any existing -Xmx so ours is authoritative (avoids duplicate,
		// order-dependent flags).
		fields := strings.Fields(opts)
		kept := fields[:0]
		for _, f := range fields {
			if !strings.HasPrefix(f, "-Xmx") {
				kept = append(kept, f)
			}
		}
		kept = append(kept, xmx)
		env[i] = "JAVA_OPTS=" + strings.Join(kept, " ")
		return env
	}

	return append(env, "JAVA_OPTS="+xmx)
}

func (d *Daemon) waitReady(ctx context.Context) error {
	timeout := 30 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsReachable(ctx) {
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for signal-cli daemon after %v", timeout)
}

func (d *Daemon) monitor(cmd *exec.Cmd, done chan struct{}) {
	if cmd == nil {
		close(done)
		return
	}

	err := cmd.Wait()

	d.mu.Lock()
	// Only mutate shared state if this monitor still owns the current
	// generation; a later Start may have replaced d.done/d.cmd.
	if d.done == done {
		d.running = false
		if d.stopping {
			d.err = nil // intentional shutdown; not a failure
		} else {
			d.err = err
		}
		d.stopping = false
	}
	d.mu.Unlock()

	close(done)
}
