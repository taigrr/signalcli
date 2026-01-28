package signalcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Listener handles Server-Sent Events from signal-cli for receiving messages.
type Listener struct {
	client     *Client
	httpClient *http.Client
}

// NewListener creates a new SSE listener.
func NewListener(client *Client) *Listener {
	return &Listener{
		client: client,
		httpClient: &http.Client{
			Timeout: 0, // No timeout for SSE
		},
	}
}

// Envelope is the top-level message envelope from signal-cli.
type Envelope struct {
	Source            string          `json:"source"`
	SourceNumber      string          `json:"sourceNumber"`
	SourceUUID        string          `json:"sourceUuid"`
	SourceName        string          `json:"sourceName"`
	SourceDevice      int             `json:"sourceDevice"`
	Timestamp         int64           `json:"timestamp"`
	ServerReceivedAt  int64           `json:"serverReceivedTimestamp"`
	ServerDeliveredAt int64           `json:"serverDeliveredTimestamp"`
	DataMessage       *DataMessage    `json:"dataMessage"`
	SyncMessage       *SyncMessage    `json:"syncMessage"`
	TypingMessage     *TypingMessage  `json:"typingMessage"`
	ReceiptMessage    *ReceiptMessage `json:"receiptMessage"`
	CallMessage       *CallMessage    `json:"callMessage"`
}

// DataMessage contains a regular text message.
type DataMessage struct {
	Timestamp        int64         `json:"timestamp"`
	Message          string        `json:"message"`
	ExpiresInSeconds int           `json:"expiresInSeconds"`
	ViewOnce         bool          `json:"viewOnce"`
	GroupInfo        *GroupInfo    `json:"groupInfo"`
	Quote            *QuoteInfo    `json:"quote"`
	Attachments      []Attachment  `json:"attachments"`
	Mentions         []MentionInfo `json:"mentions"`
	Reaction         *Reaction     `json:"reaction"`
	Sticker          *Sticker      `json:"sticker"`
}

// SyncMessage contains a sync message (sent from another device).
type SyncMessage struct {
	SentMessage  *SentMessage  `json:"sentMessage"`
	ReadMessages []ReadMessage `json:"readMessages"`
}

// SentMessage is a message sent from another device.
type SentMessage struct {
	Destination      string       `json:"destination"`
	DestinationUUID  string       `json:"destinationUuid"`
	Timestamp        int64        `json:"timestamp"`
	Message          string       `json:"message"`
	ExpiresInSeconds int          `json:"expiresInSeconds"`
	GroupInfo        *GroupInfo   `json:"groupInfo"`
	Attachments      []Attachment `json:"attachments"`
}

// ReadMessage indicates a message was read.
type ReadMessage struct {
	Sender    string `json:"sender"`
	Timestamp int64  `json:"timestamp"`
}

// TypingMessage indicates typing status.
type TypingMessage struct {
	Action    string `json:"action"` // "STARTED" or "STOPPED"
	Timestamp int64  `json:"timestamp"`
	GroupID   string `json:"groupId,omitempty"`
}

// ReceiptMessage indicates message receipt status.
type ReceiptMessage struct {
	Type       string  `json:"type"` // "DELIVERY" or "READ"
	Timestamps []int64 `json:"timestamps"`
}

// CallMessage represents a call event.
type CallMessage struct {
	OfferMessage  *OfferMessage  `json:"offerMessage"`
	AnswerMessage *AnswerMessage `json:"answerMessage"`
	HangupMessage *HangupMessage `json:"hangupMessage"`
	BusyMessage   *BusyMessage   `json:"busyMessage"`
}

type OfferMessage struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type AnswerMessage struct {
	ID int64 `json:"id"`
}

type HangupMessage struct {
	ID int64 `json:"id"`
}

type BusyMessage struct {
	ID int64 `json:"id"`
}

// GroupInfo contains group chat information.
type GroupInfo struct {
	GroupID string `json:"groupId"`
	Type    string `json:"type"`
}

// QuoteInfo contains a quoted message.
type QuoteInfo struct {
	ID          int64        `json:"id"`
	Author      string       `json:"author"`
	AuthorUUID  string       `json:"authorUuid"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
}

// Attachment represents a file attachment.
type Attachment struct {
	ContentType     string `json:"contentType"`
	Filename        string `json:"filename"`
	ID              string `json:"id"`
	Size            int64  `json:"size"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	Caption         string `json:"caption"`
	UploadTimestamp int64  `json:"uploadTimestamp"`
}

// MentionInfo represents a user mention in a message.
type MentionInfo struct {
	Start  int    `json:"start"`
	Length int    `json:"length"`
	UUID   string `json:"uuid"`
}

// Reaction contains reaction info.
type Reaction struct {
	Emoji            string `json:"emoji"`
	TargetAuthor     string `json:"targetAuthor"`
	TargetAuthorUUID string `json:"targetAuthorUuid"`
	TargetTimestamp  int64  `json:"targetSentTimestamp"`
	IsRemove         bool   `json:"isRemove"`
}

// Sticker represents a sticker.
type Sticker struct {
	PackID    string `json:"packId"`
	StickerID int    `json:"stickerId"`
}

// MessageEnvelope wraps the envelope with account info.
type MessageEnvelope struct {
	Account  string   `json:"account"`
	Envelope Envelope `json:"envelope"`
}

// EnvelopeHandler is called for each received envelope.
type EnvelopeHandler func(Envelope) error

// Listen connects to SSE and calls the handler for each envelope.
// It automatically reconnects on connection errors.
func (l *Listener) Listen(ctx context.Context, handler EnvelopeHandler) error {
	url := fmt.Sprintf("%s/api/v1/receive/%s", l.client.baseURL, l.client.account)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := l.connect(ctx, url, handler); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Reconnect after delay
			time.Sleep(5 * time.Second)
		}
	}
}

func (l *Listener) connect(ctx context.Context, url string, handler EnvelopeHandler) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SSE connect failed: %d %s", resp.StatusCode, string(body))
	}

	return l.readEvents(ctx, resp.Body, handler)
}

// SSE event structure
type sseEvent struct {
	Type string
	Data string
	ID   string
}

func (l *Listener) readEvents(ctx context.Context, r io.Reader, handler EnvelopeHandler) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line

	var event sseEvent
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		if line == "" {
			// Empty line = end of event
			if event.Data != "" {
				if err := l.handleEvent(event, handler); err != nil {
					// Log but continue
				}
			}
			event = sseEvent{}
			continue
		}

		if strings.HasPrefix(line, ":") {
			// Comment, ignore
			continue
		}

		if strings.HasPrefix(line, "event:") {
			event.Type = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += data
		} else if strings.HasPrefix(line, "id:") {
			event.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		}
	}

	return scanner.Err()
}

func (l *Listener) handleEvent(event sseEvent, handler EnvelopeHandler) error {
	if event.Data == "" {
		return nil
	}

	// Try parsing as MessageEnvelope first
	var msgEnv MessageEnvelope
	if err := json.Unmarshal([]byte(event.Data), &msgEnv); err == nil && msgEnv.Envelope.Timestamp > 0 {
		return handler(msgEnv.Envelope)
	}

	// Try parsing as direct Envelope
	var env Envelope
	if err := json.Unmarshal([]byte(event.Data), &env); err != nil {
		return fmt.Errorf("parse envelope: %w", err)
	}

	return handler(env)
}
