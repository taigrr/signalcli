package signalcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
