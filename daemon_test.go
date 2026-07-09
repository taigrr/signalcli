package signalcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewDaemon(t *testing.T) {
	d := NewDaemon(DaemonConfig{
		CLIPath: "/usr/bin/signal-cli",
		Account: "+1234567890",
	})

	if d.BaseURL() != "http://127.0.0.1:8080" {
		t.Errorf("expected default base URL, got %q", d.BaseURL())
	}

	if d.IsRunning() {
		t.Error("new daemon should not be running")
	}

	if d.Error() != nil {
		t.Error("new daemon should have no error")
	}
}

func TestNewDaemonCustomHostPort(t *testing.T) {
	d := NewDaemon(DaemonConfig{
		CLIPath:  "/usr/bin/signal-cli",
		Account:  "+1234567890",
		HTTPHost: "0.0.0.0",
		HTTPPort: 9090,
	})

	if d.BaseURL() != "http://0.0.0.0:9090" {
		t.Errorf("expected 'http://0.0.0.0:9090', got %q", d.BaseURL())
	}
}

func TestDaemonIsReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/rpc" {
			t.Fatalf("expected /api/v1/rpc path, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	d := &Daemon{baseURL: server.URL}

	if !d.IsReachable(context.Background()) {
		t.Error("daemon should be reachable (any HTTP response means it's running)")
	}
}

func TestDaemonIsNotReachable(t *testing.T) {
	d := &Daemon{baseURL: "http://127.0.0.1:59999"}

	if d.IsReachable(context.Background()) {
		t.Error("daemon should not be reachable on unused port")
	}
}

func TestDaemonBuildArgs(t *testing.T) {
	d := NewDaemon(DaemonConfig{
		CLIPath:           "/usr/bin/signal-cli",
		Account:           "+1234567890",
		HTTPHost:          "127.0.0.1",
		HTTPPort:          8080,
		ReceiveMode:       "on-connection",
		IgnoreAttachments: true,
		IgnoreStories:     true,
		SendReadReceipts:  true,
	})

	args := d.buildArgs()

	expected := map[string]bool{
		"-a":                   true,
		"+1234567890":          true,
		"daemon":               true,
		"--http":               true,
		"127.0.0.1:8080":       true,
		"--no-receive-stdout":  true,
		"--receive-mode":       true,
		"on-connection":        true,
		"--ignore-attachments": true,
		"--ignore-stories":     true,
		"--send-read-receipts": true,
	}

	for _, arg := range args {
		delete(expected, arg)
	}

	if len(expected) > 0 {
		t.Errorf("missing expected args: %v", expected)
	}
}

func TestDaemonStopNotRunning(t *testing.T) {
	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})

	if err := d.Stop(); err != nil {
		t.Errorf("stopping non-running daemon should not error: %v", err)
	}
}

func TestDaemonWaitNilCmd(t *testing.T) {
	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})

	if err := d.Wait(); err != nil {
		t.Errorf("waiting on nil cmd should not error: %v", err)
	}
}

func TestDaemonStartDetectsExternal(t *testing.T) {
	// Simulate an externally running daemon
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d := &Daemon{
		config:  DaemonConfig{CLIPath: "/nonexistent"},
		baseURL: server.URL,
	}

	if err := d.Start(context.Background()); err != nil {
		t.Errorf("Start should detect external daemon: %v", err)
	}

	if !d.IsRunning() {
		t.Error("should be marked as running after detecting external daemon")
	}
}

func TestDaemonStartReturnsStartError(t *testing.T) {
	d := NewDaemon(DaemonConfig{
		CLIPath:  "/definitely/missing/signal-cli",
		HTTPHost: "127.0.0.1",
		HTTPPort: 59999,
	})

	err := d.Start(context.Background())
	if err == nil {
		t.Fatal("expected start to fail for missing binary")
	}
	if !strings.Contains(err.Error(), "failed to start signal-cli") {
		t.Fatalf("expected wrapped start error, got %v", err)
	}
	if d.IsRunning() {
		t.Error("daemon should not be marked running after start failure")
	}
}

func TestDaemonStartCleansUpWhenReadinessFails(t *testing.T) {
	cliPath := filepath.Join(t.TempDir(), "signal-cli")
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\nsleep 10\n"), 0o700); err != nil {
		t.Fatalf("failed to write helper command: %v", err)
	}

	d := NewDaemon(DaemonConfig{
		CLIPath:  cliPath,
		HTTPHost: "127.0.0.1",
		HTTPPort: 59999,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- d.Start(ctx)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected readiness failure")
		}
		if !strings.Contains(err.Error(), "signal-cli failed to become ready") {
			t.Fatalf("expected wrapped readiness error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after readiness failure")
	}

	if d.IsRunning() {
		t.Error("daemon should not be marked running after readiness failure")
	}
}

func TestDaemonWaitReadyHonorsContextCancellation(t *testing.T) {
	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := d.waitReady(ctx); err != context.Canceled {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
}

func TestDaemonMonitorCapturesExitError(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper command: %v", err)
	}

	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})
	d.cmd = cmd
	d.running = true

	d.monitor()

	if d.IsRunning() {
		t.Error("daemon should not be running after monitor observes exit")
	}
	if d.Error() == nil {
		t.Fatal("expected monitor to capture process exit error")
	}
}

func TestDaemonMonitorHandlesNilCommand(t *testing.T) {
	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})
	d.running = true

	d.monitor()

	if !d.IsRunning() {
		t.Error("monitor should leave running state unchanged when command is nil")
	}
}

func TestDaemonStopInterruptsRunningProcess(t *testing.T) {
	cmd := exec.Command("sh", "-c", "trap 'exit 0' INT; while :; do sleep 1; done")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper command: %v", err)
	}

	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})
	d.cmd = cmd
	d.running = true

	if err := d.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if d.IsRunning() {
		t.Error("daemon should be marked stopped immediately")
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- d.Wait()
	}()

	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for helper process to stop")
	}
}
