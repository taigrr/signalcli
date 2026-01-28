# signalcli

Go bindings for [signal-cli](https://github.com/AsamK/signal-cli)'s JSON-RPC API.

## Installation

```bash
go get github.com/taigrr/signalcli
```

## Prerequisites

signal-cli must be running in daemon mode:

```bash
signal-cli -a +1234567890 daemon --http 127.0.0.1:8080
```

## Usage

### Sending Messages

```go
package main

import (
    "context"
    "log"

    "github.com/taigrr/signalcli"
)

func main() {
    client := signalcli.NewClient("http://localhost:8080", "+1234567890")
    ctx := context.Background()

    // Send a simple message
    result, err := client.Send(ctx, signalcli.SendParams{
        Recipient: "recipient-uuid-or-phone",
        Message:   "Hello from Go!",
    })
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Sent at timestamp: %d", result.Timestamp)

    // Send with quote/reply
    _, err = client.Send(ctx, signalcli.SendParams{
        Recipient: "recipient-uuid",
        Message:   "This is a reply",
        Quote: &signalcli.Quote{
            Timestamp: 1234567890,
            Author:    "author-uuid",
        },
    })

    // Send to multiple recipients
    _, err = client.Send(ctx, signalcli.SendParams{
        Recipients: []string{"uuid1", "uuid2"},
        Message:    "Broadcast message",
    })

    // Send to a group
    _, err = client.Send(ctx, signalcli.SendParams{
        GroupID: "group-id",
        Message: "Hello group!",
    })
}
```

### Reactions

```go
// Add reaction
err := client.React(ctx, signalcli.ReactParams{
    Recipient:       "recipient-uuid",
    Emoji:           "👍",
    TargetAuthor:    "message-author-uuid",
    TargetTimestamp: 1234567890,
})

// Remove reaction
err = client.React(ctx, signalcli.ReactParams{
    Recipient:       "recipient-uuid",
    Emoji:           "👍",
    TargetAuthor:    "message-author-uuid",
    TargetTimestamp: 1234567890,
    Remove:          true,
})
```

### Typing Indicator

```go
// Start typing
err := client.SendTyping(ctx, signalcli.TypingParams{
    Recipient: "recipient-uuid",
})

// Stop typing
err = client.SendTyping(ctx, signalcli.TypingParams{
    Recipient: "recipient-uuid",
    Stop:      true,
})
```

### Receiving Messages (SSE)

```go
listener := signalcli.NewListener(client)

err := listener.Listen(ctx, func(env signalcli.Envelope) error {
    if env.DataMessage != nil {
        log.Printf("Message from %s: %s", 
            env.SourceName, 
            env.DataMessage.Message)
    }
    if env.TypingMessage != nil {
        log.Printf("Typing: %s from %s", 
            env.TypingMessage.Action, 
            env.SourceName)
    }
    return nil
})
```

## Message Types

The library handles all signal-cli message types:

- `DataMessage` - Regular text messages with attachments, mentions, reactions
- `SyncMessage` - Messages synced from other devices
- `TypingMessage` - Typing indicators
- `ReceiptMessage` - Delivery and read receipts
- `CallMessage` - Voice/video call events

## Testing

```bash
go test ./...
```

## License

MIT
