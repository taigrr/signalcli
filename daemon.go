package signalcli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
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
}

// Daemon manages the signal-cli daemon subprocess.
type Daemon struct {
	config  DaemonConfig
	baseURL string
	cmd     *exec.Cmd

	mu      sync.RWMutex
	running bool
	err     error
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

	if err := d.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start signal-cli: %w", err)
	}

	d.running = true

	// Wait for daemon to be ready
	if err := d.waitReady(ctx); err != nil {
		d.Stop()
		return fmt.Errorf("signal-cli failed to become ready: %w", err)
	}

	// Monitor process in background
	go d.monitor()

	return nil
}

// Stop stops the signal-cli daemon.
func (d *Daemon) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running || d.cmd == nil || d.cmd.Process == nil {
		d.running = false
		return nil
	}

	if err := d.cmd.Process.Signal(os.Interrupt); err != nil {
		// Force kill if interrupt fails
		d.cmd.Process.Kill()
	}

	d.running = false
	return nil
}

// Wait waits for the daemon to exit.
func (d *Daemon) Wait() error {
	d.mu.RLock()
	cmd := d.cmd
	d.mu.RUnlock()

	if cmd == nil {
		return nil
	}

	return cmd.Wait()
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

func (d *Daemon) monitor() {
	if d.cmd == nil {
		return
	}

	err := d.cmd.Wait()

	d.mu.Lock()
	d.running = false
	d.err = err
	d.mu.Unlock()
}
