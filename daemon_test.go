package signalcli

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// freePort returns a TCP port that was free at call time. There is an inherent
// TOCTOU window, but it avoids the false results of a hardcoded port that some
// other process (or a stale daemon) might already be listening on.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

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
			t.Errorf("expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/rpc" {
			t.Errorf("expected /api/v1/rpc path, got %s", r.URL.Path)
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

func TestDaemonBuildEnvNoHeap(t *testing.T) {
	d := NewDaemon(DaemonConfig{CLIPath: "signal-cli"})
	if env := d.buildEnv(d.config.JavaMaxHeapMB); env != nil {
		t.Errorf("expected nil env when heap unset, got %v", env)
	}
}

func TestDaemonBuildEnvSetsXmx(t *testing.T) {
	t.Setenv("JAVA_OPTS", "")
	d := NewDaemon(DaemonConfig{CLIPath: "signal-cli", JavaMaxHeapMB: 256})

	var got string
	for _, kv := range d.buildEnv(d.config.JavaMaxHeapMB) {
		if strings.HasPrefix(kv, "JAVA_OPTS=") {
			got = kv
		}
	}
	if !strings.Contains(got, "-Xmx256m") {
		t.Errorf("expected JAVA_OPTS with -Xmx256m, got %q", got)
	}
}

func TestDaemonBuildEnvPreservesJavaOpts(t *testing.T) {
	t.Setenv("JAVA_OPTS", "-Dfoo=bar")
	d := NewDaemon(DaemonConfig{CLIPath: "signal-cli", JavaMaxHeapMB: 512})

	var got string
	for _, kv := range d.buildEnv(d.config.JavaMaxHeapMB) {
		if strings.HasPrefix(kv, "JAVA_OPTS=") {
			got = kv
		}
	}
	if !strings.Contains(got, "-Dfoo=bar") || !strings.Contains(got, "-Xmx512m") {
		t.Errorf("expected preserved opts and -Xmx512m, got %q", got)
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
		HTTPPort: freePort(t),
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
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	cliPath := filepath.Join(t.TempDir(), "signal-cli")
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\nsleep 10\n"), 0o700); err != nil {
		t.Fatalf("failed to write helper command: %v", err)
	}

	d := NewDaemon(DaemonConfig{
		CLIPath:  cliPath,
		HTTPHost: "127.0.0.1",
		HTTPPort: freePort(t),
	})

	// Generous enough that fork/exec of the helper reliably succeeds (so we
	// exit via the readiness path, not a context error during Start), but
	// short enough that waitReady's ctx-cancel fires quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
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

	// The intentional kill on the readiness-failure path is marked stopping, so
	// its exit is not recorded as a daemon error.
	if err := d.Wait(); err != nil {
		t.Errorf("Wait after intentional readiness kill should be nil, got %v", err)
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
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	cmd := exec.Command("sh", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper command: %v", err)
	}

	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})
	done := make(chan struct{})
	d.mu.Lock()
	d.cmd = cmd
	d.running = true
	d.done = done
	d.mu.Unlock()

	d.monitor(cmd, done)

	if d.IsRunning() {
		t.Error("daemon should not be running after monitor observes exit")
	}
	if d.Error() == nil {
		t.Fatal("expected monitor to capture unexpected process exit error")
	}
}

func TestDaemonMonitorSuppressesIntentionalStop(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	cmd := exec.Command("sh", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper command: %v", err)
	}

	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})
	done := make(chan struct{})
	d.mu.Lock()
	d.cmd = cmd
	d.running = true
	d.stopping = true // exit is intentional
	d.done = done
	d.mu.Unlock()

	d.monitor(cmd, done)

	if d.Error() != nil {
		t.Errorf("intentional stop should not record an error, got %v", d.Error())
	}
}

func TestDaemonMonitorHandlesNilCommand(t *testing.T) {
	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})
	done := make(chan struct{})
	d.mu.Lock()
	d.running = true
	d.done = done
	d.mu.Unlock()

	d.monitor(nil, done)

	// monitor closes done even for a nil command so Wait()ers unblock.
	select {
	case <-done:
	default:
		t.Error("monitor should close done for a nil command")
	}
	// The nil-cmd path is a pure programming-error guard: it does not touch
	// shared state, so running is left as-is.
	if !d.IsRunning() {
		t.Error("nil-cmd monitor should not mutate running state")
	}
}

func TestDaemonMonitorIgnoresStaleGeneration(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	cmd := exec.Command("sh", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper command: %v", err)
	}

	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})
	staleDone := make(chan struct{})
	currentDone := make(chan struct{})
	d.mu.Lock()
	d.running = true
	d.done = currentDone // a newer generation already owns the daemon
	d.mu.Unlock()

	// This monitor belongs to the superseded (stale) generation.
	d.monitor(cmd, staleDone)

	if !d.IsRunning() {
		t.Error("stale monitor must not clear running for the current generation")
	}
	if d.Error() != nil {
		t.Errorf("stale monitor must not record err for the current generation, got %v", d.Error())
	}
	select {
	case <-staleDone:
	default:
		t.Error("monitor should still close its own done channel")
	}
}

func TestDaemonStopInterruptsRunningProcess(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	cmd := exec.Command("sh", "-c", "trap 'exit 0' INT; while :; do sleep 1; done")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper command: %v", err)
	}

	d := NewDaemon(DaemonConfig{CLIPath: "/usr/bin/signal-cli"})
	done := make(chan struct{})
	d.mu.Lock()
	d.cmd = cmd
	d.running = true
	d.done = done
	d.mu.Unlock()
	go d.monitor(cmd, done)

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
