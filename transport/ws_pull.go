package transport

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log"
	"time"

	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
	"github.com/gorilla/websocket"
)

type pullWriteStats struct {
	changes int
	bytes   int64
}

type pullWriter struct {
	io.Writer
	bytes int64
}

func (w *pullWriter) Write(body []byte) (int, error) {
	n, err := w.Writer.Write(body)
	w.bytes += int64(n)
	return n, err
}

type pullStream func(syncsvc.ChangeConsumer) (int64, error)

// writePull streams one legacy-compatible JSON message. An interrupted response
// is closed without a final WebSocket frame and cannot acknowledge a checkpoint.
func (c *webSocketConnection) writePull(ctx context.Context, message syncsvc.ServerMessage, stream pullStream) (pullWriteStats, error) {
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var stats pullWriteStats
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return stats, err
		}
		defer c.conn.SetWriteDeadline(time.Time{})
	}
	writer, err := c.conn.NextWriter(websocket.TextMessage)
	if err != nil {
		return stats, err
	}
	stats, err = encodePullResponse(ctx, writer, message, stream)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = c.Close()
		_ = writer.Close()
		return stats, err
	}
	if err := writer.Close(); err != nil {
		_ = c.Close()
		return stats, err
	}
	return stats, nil
}

func encodePullResponse(ctx context.Context, destination io.Writer, message syncsvc.ServerMessage, stream pullStream) (pullWriteStats, error) {
	writer := &pullWriter{Writer: destination}
	stats := pullWriteStats{}
	_, err := io.WriteString(writer, `{"changes":[`)
	if err != nil {
		return stats, err
	}
	encoder := json.NewEncoder(writer)
	seq, err := stream(func(change syncsvc.FullChangeEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if stats.changes > 0 {
			if _, err := io.WriteString(writer, ","); err != nil {
				return err
			}
		}
		if err := encoder.Encode(change); err != nil {
			return err
		}
		stats.changes++
		return nil
	})
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		message.Seq = seq
		metadata, metadataErr := changesMetadata(message)
		err = metadataErr
		if err == nil {
			if _, err = io.WriteString(writer, "],"); err == nil {
				_, err = writer.Write(metadata[1:])
			}
		}
	}
	stats.bytes = writer.bytes
	return stats, err
}

func changesMetadata(message syncsvc.ServerMessage) ([]byte, error) {
	message.Changes = nil
	metadata, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &fields); err != nil {
		return nil, err
	}
	delete(fields, "changes")
	return json.Marshal(fields)
}

func (h *WSHandler) sendPull(parent context.Context, peer *webSocketConnection, uid string, request syncsvc.ClientMessage, protocolVersion int) error {
	ctx, cancel := context.WithTimeout(parent, syncsvc.MaxPullDuration)
	defer cancel()
	started := time.Now()
	snapshot := request.Snapshot
	if !snapshot {
		var err error
		snapshot, err = h.sync.ShouldSnapshotPull(ctx, uid, request.Since, protocolVersion)
		if err != nil {
			return err
		}
	}
	epoch, err := h.sync.SyncEpochContext(ctx)
	if err != nil {
		return err
	}
	message := syncsvc.ServerMessage{Type: "changes", SyncEpoch: epoch, Snapshot: snapshot, Reset: snapshot}
	stats, err := peer.writePull(ctx, message, func(consume syncsvc.ChangeConsumer) (int64, error) {
		if snapshot {
			return h.sync.StreamSnapshotChanges(ctx, uid, consume)
		}
		return h.sync.StreamChanges(ctx, uid, request.Since, protocolVersion, consume)
	})
	uidHash := sha256.Sum256([]byte(uid))
	log.Printf("[Pull] user=%x protocol=%d snapshot=%t requested_snapshot=%t since=%d changes=%d bytes=%d duration=%s error=%v", uidHash[:6], protocolVersion, snapshot, request.Snapshot, request.Since, stats.changes, stats.bytes, time.Since(started).Round(time.Millisecond), err)
	return err
}
