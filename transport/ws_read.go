package transport

import (
	"context"

	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
)

// readMessages keeps reading disconnects while the handler is generating a pull.
// The bounded inbox prevents a peer from queueing unlimited request bodies.
func (c *webSocketConnection) readMessages(parent context.Context) (context.Context, context.CancelFunc, <-chan syncsvc.ClientMessage) {
	ctx, cancel := context.WithCancel(parent)
	messages := make(chan syncsvc.ClientMessage, 4)
	go func() {
		defer cancel()
		defer c.Close()
		defer close(messages)
		for {
			var message syncsvc.ClientMessage
			if err := c.conn.ReadJSON(&message); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case messages <- message:
			default:
				if message.Type == "ping" {
					continue
				}
				return
			}
		}
	}()
	return ctx, cancel, messages
}
