package signalcli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestListenerReceivesDataMessage(t *testing.T) {
	sseData := `{"account":"+1234567890","envelope":{"source":"+9876543210","sourceUuid":"sender-uuid","sourceName":"Test User","sourceDevice":1,"timestamp":1700000000000,"dataMessage":{"timestamp":1700000000000,"message":"Hello from SSE"}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
			return
		}

		fmt.Fprintf(w, "data:%s\n\n", sseData)
		flusher.Flush()

		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(server.URL, "+1234567890")
	listener := NewListener(client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var received []Envelope
	var mu sync.Mutex

	go func() {
		_ = listener.Listen(ctx, func(env Envelope) error {
			mu.Lock()
			received = append(received, env)
			mu.Unlock()
			cancel()
			return nil
		})
	}()

	<-ctx.Done()

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one envelope")
	}

	env := received[0]
	if env.SourceUUID != "sender-uuid" {
		t.Errorf("expected sourceUuid 'sender-uuid', got %q", env.SourceUUID)
	}
	if env.DataMessage == nil {
		t.Fatal("expected DataMessage to be set")
	}
	if env.DataMessage.Message != "Hello from SSE" {
		t.Errorf("expected message 'Hello from SSE', got %q", env.DataMessage.Message)
	}
}

func TestListenerReceivesDirectEnvelope(t *testing.T) {
	sseData := `{"source":"+9876543210","sourceUuid":"direct-uuid","timestamp":1700000000000,"dataMessage":{"timestamp":1700000000000,"message":"Direct envelope"}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		fmt.Fprintf(w, "event:message\ndata:%s\n\n", sseData)
		flusher.Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(server.URL, "+1234567890")
	listener := NewListener(client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var received []Envelope
	var mu sync.Mutex

	go func() {
		_ = listener.Listen(ctx, func(env Envelope) error {
			mu.Lock()
			received = append(received, env)
			mu.Unlock()
			cancel()
			return nil
		})
	}()

	<-ctx.Done()

	mu.Lock()
	defer mu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected at least one envelope")
	}

	if received[0].SourceUUID != "direct-uuid" {
		t.Errorf("expected sourceUuid 'direct-uuid', got %q", received[0].SourceUUID)
	}
}

func TestListenerSSEComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		fmt.Fprint(w, ": this is a comment\n")
		fmt.Fprint(w, "data:{\"source\":\"+1\",\"sourceUuid\":\"u1\",\"timestamp\":1}\n\n")
		flusher.Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(server.URL, "+1234567890")
	listener := NewListener(client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var count int
	var mu sync.Mutex

	go func() {
		_ = listener.Listen(ctx, func(env Envelope) error {
			mu.Lock()
			count++
			mu.Unlock()
			cancel()
			return nil
		})
	}()

	<-ctx.Done()

	mu.Lock()
	defer mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 event (comment should be skipped), got %d", count)
	}
}

func TestListenerReconnectsOnError(t *testing.T) {
	var attempts int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		attempt := attempts
		mu.Unlock()

		if attempt == 1 {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		fmt.Fprint(w, "data:{\"source\":\"+1\",\"sourceUuid\":\"reconnect-uuid\",\"timestamp\":1}\n\n")
		flusher.Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(server.URL, "+1234567890")
	listener := NewListener(client)
	listener.httpClient = &http.Client{Timeout: 0}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var received []Envelope
	var recvMu sync.Mutex

	go func() {
		_ = listener.Listen(ctx, func(env Envelope) error {
			recvMu.Lock()
			received = append(received, env)
			recvMu.Unlock()
			cancel()
			return nil
		})
	}()

	<-ctx.Done()

	recvMu.Lock()
	defer recvMu.Unlock()

	if len(received) == 0 {
		t.Fatal("expected envelope after reconnect")
	}
	if received[0].SourceUUID != "reconnect-uuid" {
		t.Errorf("expected 'reconnect-uuid', got %q", received[0].SourceUUID)
	}
}
