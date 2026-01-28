package signalcli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("http://localhost:8080", "+1234567890")

	if c.BaseURL() != "http://localhost:8080" {
		t.Errorf("expected baseURL 'http://localhost:8080', got %q", c.BaseURL())
	}

	if c.Account() != "+1234567890" {
		t.Errorf("expected account '+1234567890', got %q", c.Account())
	}
}

func TestSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/rpc" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		var req RPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Method != "send" {
			t.Errorf("expected method 'send', got %q", req.Method)
		}

		resp := RPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`{"timestamp":1234567890,"results":[{"recipientAddress":{"uuid":"test-uuid"},"type":"SUCCESS"}]}`),
			ID:      req.ID,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	ctx := context.Background()

	result, err := c.Send(ctx, SendParams{
		Recipient: "recipient-uuid",
		Message:   "Hello, World!",
	})

	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if result.Timestamp != 1234567890 {
		t.Errorf("expected timestamp 1234567890, got %d", result.Timestamp)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}

	if result.Results[0].Type != "SUCCESS" {
		t.Errorf("expected type 'SUCCESS', got %q", result.Results[0].Type)
	}
}

func TestSendWithQuote(t *testing.T) {
	var receivedParams map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedParams = req.Params.(map[string]interface{})

		resp := RPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`{"timestamp":1234567890,"results":[]}`),
			ID:      req.ID,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	ctx := context.Background()

	_, err := c.Send(ctx, SendParams{
		Recipient: "recipient-uuid",
		Message:   "Reply",
		Quote: &Quote{
			Timestamp: 9999,
			Author:    "author-uuid",
			Message:   "Original",
		},
	})

	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if receivedParams["quoteTimestamp"] != float64(9999) {
		t.Errorf("quote timestamp not set correctly")
	}

	if receivedParams["quoteAuthor"] != "author-uuid" {
		t.Errorf("quote author not set correctly")
	}
}

func TestReact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method != "sendReaction" {
			t.Errorf("expected method 'sendReaction', got %q", req.Method)
		}

		resp := RPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`{}`),
			ID:      req.ID,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	ctx := context.Background()

	err := c.React(ctx, ReactParams{
		Recipient:       "recipient-uuid",
		Emoji:           "👍",
		TargetAuthor:    "author-uuid",
		TargetTimestamp: 1234567890,
	})

	if err != nil {
		t.Fatalf("React failed: %v", err)
	}
}

func TestSendTyping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method != "sendTyping" {
			t.Errorf("expected method 'sendTyping', got %q", req.Method)
		}

		resp := RPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`{}`),
			ID:      req.ID,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	ctx := context.Background()

	err := c.SendTyping(ctx, TypingParams{
		Recipient: "recipient-uuid",
	})

	if err != nil {
		t.Fatalf("SendTyping failed: %v", err)
	}
}

func TestRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := RPCResponse{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    -1,
				Message: "Test error",
			},
			ID: "test",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	ctx := context.Background()

	_, err := c.Send(ctx, SendParams{
		Recipient: "recipient-uuid",
		Message:   "Hello",
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("expected *RPCError, got %T", err)
	}

	if rpcErr.Code != -1 {
		t.Errorf("expected code -1, got %d", rpcErr.Code)
	}
}

func TestWithHTTPClient(t *testing.T) {
	customClient := &http.Client{}
	c := NewClient("http://localhost:8080", "+1234567890").WithHTTPClient(customClient)

	if c.httpClient != customClient {
		t.Error("custom HTTP client not set")
	}
}
