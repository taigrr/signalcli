// Package signalcli provides Go bindings for signal-cli's JSON-RPC API.
//
// signal-cli is a command-line interface for Signal that exposes a JSON-RPC
// API when run in daemon mode. This package provides a type-safe Go client
// for that API.
//
// Example:
//
//	client := signalcli.NewClient("http://localhost:8080", "+1234567890")
//	err := client.Send(ctx, signalcli.SendParams{
//	    Recipient: "recipient-uuid",
//	    Message:   "Hello!",
//	})
package signalcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client handles JSON-RPC communication with signal-cli.
type Client struct {
	baseURL    string
	account    string
	httpClient *http.Client
}

// NewClient creates a new signal-cli client.
//
// baseURL is the signal-cli REST API URL (e.g., "http://localhost:8080").
// account is the phone number or UUID of the account to use.
func NewClient(baseURL, account string) *Client {
	return &Client{
		baseURL: baseURL,
		account: account,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithHTTPClient sets a custom HTTP client.
func (c *Client) WithHTTPClient(client *http.Client) *Client {
	c.httpClient = client
	return c
}

// Account returns the configured account.
func (c *Client) Account() string {
	return c.account
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// RPCRequest represents a JSON-RPC request.
type RPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      string `json:"id"`
}

// RPCResponse represents a JSON-RPC response.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      string          `json:"id"`
}

// RPCError represents a JSON-RPC error.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("signal-cli error %d: %s", e.Code, e.Message)
}

// Call makes a JSON-RPC call to signal-cli.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	req := RPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      uuid.New().String(),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected http status %s: %s", resp.Status, responsePreview(respBody))
	}

	var rpcResp RPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}

	return rpcResp.Result, nil
}

func responsePreview(body []byte) string {
	const maxResponsePreview = 512

	preview := strings.TrimSpace(string(body))
	if len(preview) <= maxResponsePreview {
		return preview
	}
	return preview[:maxResponsePreview] + "..."
}

// SendParams contains parameters for sending a message.
type SendParams struct {
	Recipient   string    `json:"recipient,omitempty"`  // Single recipient (UUID or phone)
	Recipients  []string  `json:"recipients,omitempty"` // Multiple recipients
	GroupID     string    `json:"groupId,omitempty"`    // Group ID for group messages
	Message     string    `json:"message"`
	Attachments []string  `json:"attachments,omitempty"` // Paths to attachment files
	Quote       *Quote    `json:"quote,omitempty"`       // Quote/reply
	Mentions    []Mention `json:"mentions,omitempty"`    // @mentions
}

// Quote represents a quoted/replied message.
type Quote struct {
	Timestamp int64  `json:"timestamp"`
	Author    string `json:"author"`
	Message   string `json:"message,omitempty"`
}

// Mention represents a user mention.
type Mention struct {
	Start  int    `json:"start"`
	Length int    `json:"length"`
	UUID   string `json:"uuid"`
}

// SendResult contains the result of sending a message.
type SendResult struct {
	Timestamp int64             `json:"timestamp"`
	Results   []RecipientResult `json:"results"`
}

// RecipientResult contains the result for a single recipient.
type RecipientResult struct {
	RecipientAddress    Address `json:"recipientAddress"`
	Type                string  `json:"type"` // "SUCCESS", "UNREGISTERED_FAILURE", etc.
	NetworkFailure      bool    `json:"networkFailure,omitempty"`
	UnregisteredFailure bool    `json:"unregisteredFailure,omitempty"`
}

// Address represents a Signal address.
type Address struct {
	UUID     string `json:"uuid,omitempty"`
	Number   string `json:"number,omitempty"`
	Username string `json:"username,omitempty"`
}

// Send sends a message.
func (c *Client) Send(ctx context.Context, params SendParams) (*SendResult, error) {
	// Build the params map
	p := map[string]any{
		"account": c.account,
		"message": params.Message,
	}

	if params.Recipient != "" {
		p["recipient"] = params.Recipient
	}
	if len(params.Recipients) > 0 {
		p["recipients"] = params.Recipients
	}
	if params.GroupID != "" {
		p["groupId"] = params.GroupID
	}
	if len(params.Attachments) > 0 {
		p["attachment"] = params.Attachments
	}
	if params.Quote != nil {
		p["quoteTimestamp"] = params.Quote.Timestamp
		p["quoteAuthor"] = params.Quote.Author
		if params.Quote.Message != "" {
			p["quoteMessage"] = params.Quote.Message
		}
	}
	if len(params.Mentions) > 0 {
		p["mentions"] = params.Mentions
	}

	result, err := c.Call(ctx, "send", p)
	if err != nil {
		return nil, err
	}

	// Try direct unmarshal first
	var direct SendResult
	if err := json.Unmarshal(result, &direct); err == nil && direct.Timestamp > 0 {
		return &direct, nil
	}

	// Try wrapped in response
	var wrapped struct {
		Response SendResult `json:"response"`
	}
	if err := json.Unmarshal(result, &wrapped); err != nil {
		return nil, fmt.Errorf("unmarshal send result: %w", err)
	}
	return &wrapped.Response, nil
}

// ReactParams contains parameters for sending a reaction.
type ReactParams struct {
	Recipient       string `json:"recipient"`         // Recipient UUID or phone
	Emoji           string `json:"emoji"`             // Emoji to react with
	TargetAuthor    string `json:"targetAuthor"`      // Author of target message
	TargetTimestamp int64  `json:"targetTimestamp"`   // Timestamp of target message
	Remove          bool   `json:"remove,omitempty"`  // Remove reaction instead of add
	GroupID         string `json:"groupId,omitempty"` // For group messages
}

// React sends a reaction to a message.
func (c *Client) React(ctx context.Context, params ReactParams) error {
	p := map[string]any{
		"account":             c.account,
		"recipient":           params.Recipient,
		"emoji":               params.Emoji,
		"targetAuthor":        params.TargetAuthor,
		"targetSentTimestamp": params.TargetTimestamp,
	}
	if params.Remove {
		p["remove"] = true
	}
	if params.GroupID != "" {
		p["groupId"] = params.GroupID
	}

	_, err := c.Call(ctx, "sendReaction", p)
	return err
}

// TypingParams contains parameters for sending typing indicator.
type TypingParams struct {
	Recipient string `json:"recipient"`
	GroupID   string `json:"groupId,omitempty"`
	Stop      bool   `json:"stop,omitempty"`
}

// SendTyping sends a typing indicator.
func (c *Client) SendTyping(ctx context.Context, params TypingParams) error {
	p := map[string]any{
		"account":   c.account,
		"recipient": params.Recipient,
	}
	if params.GroupID != "" {
		p["groupId"] = params.GroupID
	}
	if params.Stop {
		p["stop"] = true
	}

	_, err := c.Call(ctx, "sendTyping", p)
	return err
}

// MarkRead marks messages as read.
func (c *Client) MarkRead(ctx context.Context, recipient string, timestamps []int64) error {
	p := map[string]any{
		"account":    c.account,
		"recipient":  recipient,
		"timestamps": timestamps,
	}

	_, err := c.Call(ctx, "sendReceipt", p)
	return err
}

// GetProfile retrieves a user's profile.
func (c *Client) GetProfile(ctx context.Context, recipient string) (*Profile, error) {
	p := map[string]any{
		"account":   c.account,
		"recipient": recipient,
	}

	result, err := c.Call(ctx, "getUserStatus", p)
	if err != nil {
		return nil, err
	}

	var profiles []Profile
	if err := json.Unmarshal(result, &profiles); err != nil {
		return nil, err
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("profile not found")
	}

	return &profiles[0], nil
}

// Profile represents a Signal user profile.
type Profile struct {
	Address   Address `json:"address"`
	Name      string  `json:"name"`
	IsBlocked bool    `json:"isBlocked"`
	ExpiresIn int     `json:"messageExpirationTime"`
}

// Group represents a Signal group.
type Group struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	IsMember               bool     `json:"isMember"`
	IsBlocked              bool     `json:"isBlocked"`
	Members                []string `json:"members"`
	PendingMembers         []string `json:"pendingMembers"`
	RequestingMembers      []string `json:"requestingMembers"`
	Admins                 []string `json:"admins"`
	PermissionAddMember    string   `json:"permissionAddMember"`
	PermissionEditDetails  string   `json:"permissionEditDetails"`
	PermissionSendMessages string   `json:"permissionSendMessage"`
	GroupInviteLink        string   `json:"groupInviteLink"`
}

// Contact represents a Signal contact.
type Contact struct {
	Number    string  `json:"number"`
	UUID      string  `json:"uuid"`
	Name      string  `json:"name"`
	IsBlocked bool    `json:"isBlocked"`
	Color     string  `json:"color"`
	Address   Address `json:"address"`
}

// ListGroups retrieves all groups for the configured account.
func (c *Client) ListGroups(ctx context.Context) ([]Group, error) {
	p := map[string]any{
		"account": c.account,
	}

	result, err := c.Call(ctx, "listGroups", p)
	if err != nil {
		return nil, err
	}

	var groups []Group
	if err := json.Unmarshal(result, &groups); err != nil {
		return nil, fmt.Errorf("unmarshal groups: %w", err)
	}

	return groups, nil
}

// ListContacts retrieves all contacts for the configured account.
func (c *Client) ListContacts(ctx context.Context) ([]Contact, error) {
	p := map[string]any{
		"account": c.account,
	}

	result, err := c.Call(ctx, "listContacts", p)
	if err != nil {
		return nil, err
	}

	var contacts []Contact
	if err := json.Unmarshal(result, &contacts); err != nil {
		return nil, fmt.Errorf("unmarshal contacts: %w", err)
	}

	return contacts, nil
}

// UpdateProfileParams contains parameters for updating the user profile.
type UpdateProfileParams struct {
	Name       string `json:"name,omitempty"`
	About      string `json:"about,omitempty"`
	AboutEmoji string `json:"aboutEmoji,omitempty"`
	Avatar     string `json:"avatar,omitempty"` // Path to avatar file
}

// UpdateProfile updates the profile for the configured account.
func (c *Client) UpdateProfile(ctx context.Context, params UpdateProfileParams) error {
	p := map[string]any{
		"account": c.account,
	}
	if params.Name != "" {
		p["givenName"] = params.Name
	}
	if params.About != "" {
		p["about"] = params.About
	}
	if params.AboutEmoji != "" {
		p["aboutEmoji"] = params.AboutEmoji
	}
	if params.Avatar != "" {
		p["avatar"] = params.Avatar
	}

	_, err := c.Call(ctx, "updateProfile", p)
	return err
}

// SetExpiration sets the disappearing message timer for a conversation.
func (c *Client) SetExpiration(ctx context.Context, recipient string, seconds int) error {
	p := map[string]any{
		"account":    c.account,
		"recipient":  recipient,
		"expiration": seconds,
	}

	_, err := c.Call(ctx, "setExpirationTimer", p)
	return err
}

// BlockParams contains parameters for blocking/unblocking.
type BlockParams struct {
	Recipient string `json:"recipient,omitempty"`
	GroupID   string `json:"groupId,omitempty"`
}

// Block blocks a recipient or group.
func (c *Client) Block(ctx context.Context, params BlockParams) error {
	p := map[string]any{
		"account": c.account,
	}
	if params.Recipient != "" {
		p["recipient"] = params.Recipient
	}
	if params.GroupID != "" {
		p["groupId"] = params.GroupID
	}

	_, err := c.Call(ctx, "block", p)
	return err
}

// Unblock unblocks a recipient or group.
func (c *Client) Unblock(ctx context.Context, params BlockParams) error {
	p := map[string]any{
		"account": c.account,
	}
	if params.Recipient != "" {
		p["recipient"] = params.Recipient
	}
	if params.GroupID != "" {
		p["groupId"] = params.GroupID
	}

	_, err := c.Call(ctx, "unblock", p)
	return err
}
