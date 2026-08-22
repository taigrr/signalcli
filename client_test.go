package signalcli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	var receivedParams map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedParams = req.Params.(map[string]any)

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

func TestSendWithAttachments(t *testing.T) {
	var receivedParams map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedParams = req.Params.(map[string]any)

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
		Recipient:   "recipient-uuid",
		Message:     "Check these out",
		Attachments: []string{"/tmp/photo1.jpg", "/tmp/photo2.jpg"},
	})

	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	attachments, ok := receivedParams["attachment"].([]any)
	if !ok {
		t.Fatal("expected attachment to be an array")
	}
	if len(attachments) != 2 {
		t.Errorf("expected 2 attachments, got %d", len(attachments))
	}
}

func TestListGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method != "listGroups" {
			t.Errorf("expected method 'listGroups', got %q", req.Method)
		}

		resp := RPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`[{"id":"group1","name":"Test Group","isMember":true,"members":["uuid1","uuid2"]}]`),
			ID:      req.ID,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	groups, err := c.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "Test Group" {
		t.Errorf("expected group name 'Test Group', got %q", groups[0].Name)
	}
	if len(groups[0].Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(groups[0].Members))
	}
}

func TestListContacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method != "listContacts" {
			t.Errorf("expected method 'listContacts', got %q", req.Method)
		}

		resp := RPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`[{"name":"Alice","uuid":"alice-uuid","number":"+15551234567"}]`),
			ID:      req.ID,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	contacts, err := c.ListContacts(context.Background())
	if err != nil {
		t.Fatalf("ListContacts failed: %v", err)
	}

	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	if contacts[0].Name != "Alice" {
		t.Errorf("expected contact name 'Alice', got %q", contacts[0].Name)
	}
}

func TestUpdateProfile(t *testing.T) {
	var receivedParams map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		receivedParams = req.Params.(map[string]any)

		if req.Method != "updateProfile" {
			t.Errorf("expected method 'updateProfile', got %q", req.Method)
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
	err := c.UpdateProfile(context.Background(), UpdateProfileParams{
		Name:  "Test User",
		About: "Hello world",
	})

	if err != nil {
		t.Fatalf("UpdateProfile failed: %v", err)
	}

	if receivedParams["givenName"] != "Test User" {
		t.Errorf("expected givenName 'Test User', got %v", receivedParams["givenName"])
	}
	if receivedParams["about"] != "Hello world" {
		t.Errorf("expected about 'Hello world', got %v", receivedParams["about"])
	}
}

func TestSetExpiration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method != "setExpirationTimer" {
			t.Errorf("expected method 'setExpirationTimer', got %q", req.Method)
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
	err := c.SetExpiration(context.Background(), "recipient-uuid", 3600)

	if err != nil {
		t.Fatalf("SetExpiration failed: %v", err)
	}
}

func TestBlockUnblock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)

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

	if err := c.Block(ctx, BlockParams{Recipient: "bad-uuid"}); err != nil {
		t.Fatalf("Block failed: %v", err)
	}

	if err := c.Unblock(ctx, BlockParams{Recipient: "bad-uuid"}); err != nil {
		t.Fatalf("Unblock failed: %v", err)
	}
}

func TestGetProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := RPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`[{"address":{"uuid":"test-uuid"},"name":"Test User","isBlocked":false}]`),
			ID:      req.ID,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	profile, err := c.GetProfile(context.Background(), "test-uuid")

	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}
	if profile.Name != "Test User" {
		t.Errorf("expected name 'Test User', got %q", profile.Name)
	}
}

func TestGetProfileNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := RPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`[]`),
			ID:      req.ID,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	_, err := c.GetProfile(context.Background(), "nonexistent-uuid")

	if err == nil {
		t.Fatal("expected error for empty profile list")
	}
}

func TestRPCErrorError(t *testing.T) {
	err := (&RPCError{Code: 42, Message: "boom"}).Error()
	if err != "signal-cli error 42: boom" {
		t.Fatalf("unexpected error string: %q", err)
	}
}

func TestCallInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	_, err := c.Call(context.Background(), "send", map[string]string{"message": "hi"})
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestCallHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "daemon unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	_, err := c.Call(context.Background(), "send", map[string]string{"message": "hi"})
	if err == nil {
		t.Fatal("expected http status error")
	}
	if !strings.Contains(err.Error(), "unexpected http status 503 Service Unavailable") {
		t.Fatalf("expected status in error, got %q", err)
	}
	if !strings.Contains(err.Error(), "daemon unavailable") {
		t.Fatalf("expected response body preview in error, got %q", err)
	}
}

func TestSendWrappedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		resp := RPCResponse{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`{"response":{"timestamp":1234567890,"results":[{"recipientAddress":{"uuid":"wrapped-uuid"},"type":"SUCCESS"}]}}`),
			ID:      req.ID,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	result, err := c.Send(context.Background(), SendParams{
		Recipient: "recipient-uuid",
		Message:   "wrapped",
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if result.Timestamp != 1234567890 {
		t.Fatalf("expected wrapped timestamp, got %d", result.Timestamp)
	}
	if len(result.Results) != 1 || result.Results[0].RecipientAddress.UUID != "wrapped-uuid" {
		t.Fatalf("unexpected wrapped result: %+v", result.Results)
	}
}

func TestMarkRead(t *testing.T) {
	var req RPCRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := RPCResponse{JSONRPC: "2.0", Result: json.RawMessage(`{}`), ID: req.ID}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(server.URL, "+1234567890")
	err := c.MarkRead(context.Background(), "recipient-uuid", []int64{111, 222})
	if err != nil {
		t.Fatalf("MarkRead failed: %v", err)
	}
	if req.Method != "sendReceipt" {
		t.Fatalf("expected method sendReceipt, got %q", req.Method)
	}
	params, ok := req.Params.(map[string]any)
	if !ok {
		t.Fatalf("expected params map, got %T", req.Params)
	}
	if params["recipient"] != "recipient-uuid" {
		t.Fatalf("unexpected recipient: %v", params["recipient"])
	}
	stamps, ok := params["timestamps"].([]any)
	if !ok || len(stamps) != 2 {
		t.Fatalf("unexpected timestamps: %#v", params["timestamps"])
	}
}
