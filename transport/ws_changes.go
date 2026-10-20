package transport

import (
	"encoding/json"
	"io"
	"time"

	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
	"github.com/gorilla/websocket"
)

// WriteChanges sends one complete protocol response without encoding all bodies
// into a second, response-sized JSON buffer. WebSocket fragments remain one message.
func (c *webSocketConnection) WriteChanges(message syncsvc.ServerMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return err
	}
	defer c.conn.SetWriteDeadline(time.Time{})
	writer, err := c.conn.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}
	if err := encodeChanges(writer, message); err != nil {
		// Abort before closing the writer: a partial response must never become
		// a completed message that a client could use to advance its checkpoint.
		_ = c.conn.Close()
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		_ = c.conn.Close()
		return err
	}
	return nil
}

func encodeChanges(writer io.Writer, message syncsvc.ServerMessage) error {
	// Marshal only the small envelope; its fields retain the existing wire contract.
	changes := message.Changes
	message.Changes = nil
	metadata, err := json.Marshal(message)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &fields); err != nil {
		return err
	}
	delete(fields, "changes")
	metadata, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	if _, err := writer.Write(metadata[:len(metadata)-1]); err != nil {
		return err
	}
	if changes == nil {
		_, err := io.WriteString(writer, `,"changes":null}`)
		return err
	}
	if _, err := io.WriteString(writer, `,"changes":[`); err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	for index, change := range changes {
		if index > 0 {
			if _, err := io.WriteString(writer, ","); err != nil {
				return err
			}
		}
		if err := encoder.Encode(change); err != nil {
			return err
		}
	}
	_, err = io.WriteString(writer, "]}")
	return err
}
