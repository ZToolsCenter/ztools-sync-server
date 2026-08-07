package transport

import (
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ZToolsCenter/ztools-sync-server/auth"
	"github.com/ZToolsCenter/ztools-sync-server/store"
	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
)

type WSHandler struct {
	auth             *auth.Service
	sync             *syncsvc.Service
	hub              *Hub
	activityObserver ActivityObserver
	upgrader         websocket.Upgrader
}

// ActivityEvent describes authenticated synchronization traffic without prescribing analytics storage.
type ActivityEvent struct {
	UID       string
	DeviceID  string
	IP        string
	UserAgent string
}

// ActivityObserver receives optional synchronization activity events.
type ActivityObserver func(ActivityEvent)

func NewWSHandler(authService *auth.Service, syncService *syncsvc.Service, hub *Hub) *WSHandler {
	return NewWSHandlerWithObserver(authService, syncService, hub, nil)
}

// NewWSHandlerWithObserver creates a protocol handler with an optional external activity hook.
func NewWSHandlerWithObserver(authService *auth.Service, syncService *syncsvc.Service, hub *Hub, observer ActivityObserver) *WSHandler {
	return &WSHandler{
		auth:             authService,
		sync:             syncService,
		hub:              hub,
		activityObserver: observer,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (h *WSHandler) observeActivity(uid string, deviceID string, r *http.Request) {
	if h.activityObserver == nil {
		return
	}
	h.activityObserver(ActivityEvent{
		UID:       uid,
		DeviceID:  deviceID,
		IP:        requestIP(r),
		UserAgent: r.UserAgent(),
	})
}

func requestIP(r *http.Request) string {
	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		if ip := strings.TrimSpace(strings.Split(forwardedFor, ",")[0]); ip != "" {
			return ip
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

/**
 * ServeHTTP 升级 WebSocket 连接并顺序处理认证、拉取和推送消息。
 * @param w HTTP 响应写入器。
 * @param r 当前 HTTP 请求。
 * @returns 无返回值。
 */
func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] upgrade failed: %v", err)
		return
	}
	peer := newWebSocketConnection(conn)
	defer peer.Close()

	var uid string
	var deviceID string
	var authenticated atomic.Bool
	clientProtocolVersion := 1
	authTimer := time.AfterFunc(30*time.Second, func() {
		if !authenticated.Load() {
			_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Authentication timeout"})
			_ = peer.Close()
		}
	})
	defer authTimer.Stop()

	for {
		var msg syncsvc.ClientMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if uid != "" && deviceID != "" {
				h.hub.Remove(uid, deviceID, peer)
			}
			return
		}
		switch msg.Type {
		case "auth":
			verifiedUID, err := h.auth.Verify(msg.Token)
			if err != nil {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Invalid token"})
				_ = peer.Close()
				return
			}
			authTimer.Stop()
			uid = verifiedUID
			authenticated.Store(true)
			deviceID = msg.DeviceID
			if deviceID == "" {
				deviceID = "unknown"
			}
			if msg.ProtocolVersion > 0 {
				clientProtocolVersion = msg.ProtocolVersion
			}
			if clientProtocolVersion < 1 {
				clientProtocolVersion = 1
			}
			h.hub.Add(uid, deviceID, peer)
			_ = h.sync.UpsertDeviceState(uid, deviceID, msg.DeviceName)
			h.observeActivity(uid, deviceID, r)
			serverSeq, _ := h.sync.MaxSeq(uid)
			_ = peer.WriteJSON(syncsvc.ServerMessage{
				Type:             "auth_ok",
				ServerInstanceID: h.sync.ServerInstanceID(),
				ServerSeq:        serverSeq,
				ProtocolVersion:  store.ProtocolVersion,
				SyncEpoch:        h.sync.SyncEpoch(),
				Features: map[string]bool{
					"revisionRetention": true,
					"snapshotPull":      true,
				},
			})
		case "pull":
			if uid == "" {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Not authenticated"})
				continue
			}
			if msg.Snapshot {
				changes, err := h.sync.SnapshotChanges(uid)
				if err != nil {
					_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: err.Error()})
					continue
				}
				seq, _ := h.sync.MaxSeq(uid)
				h.observeActivity(uid, deviceID, r)
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "changes", Changes: changes, Seq: seq, Snapshot: true, Reset: true, SyncEpoch: h.sync.SyncEpoch()})
				continue
			}
			changes, seq, err := h.sync.Changes(uid, msg.Since, clientProtocolVersion)
			if err != nil {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: err.Error()})
				continue
			}
			h.observeActivity(uid, deviceID, r)
			_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "changes", Changes: changes, Seq: seq, SyncEpoch: h.sync.SyncEpoch()})
		case "get_checkpoint":
			if uid == "" {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Not authenticated"})
				continue
			}
			checkpoint, err := h.sync.GetCheckpoint(uid, msg.CheckpointID)
			if err != nil {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Get checkpoint failed: " + err.Error()})
				continue
			}
			if checkpoint == nil {
				checkpoint = &syncsvc.CheckpointPayload{ID: msg.CheckpointID}
			}
			_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "checkpoint", Checkpoint: checkpoint})
		case "put_checkpoint":
			if uid == "" {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Not authenticated"})
				continue
			}
			if msg.Checkpoint == nil {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Missing checkpoint"})
				continue
			}
			checkpoint, err := h.sync.PutCheckpoint(uid, *msg.Checkpoint)
			if err != nil {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Put checkpoint failed: " + err.Error()})
				continue
			}
			_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "checkpoint_ok", Checkpoint: checkpoint})
		case "push":
			if uid == "" {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Not authenticated"})
				continue
			}
			if len(msg.Changes) == 0 {
				h.observeActivity(uid, deviceID, r)
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "push_ok", Seq: 0})
				continue
			}
			protocolVersion := msg.ProtocolVersion
			if protocolVersion < 1 {
				protocolVersion = clientProtocolVersion
			}
			result, err := h.sync.Push(uid, deviceID, msg.Changes, protocolVersion)
			// 分段事务已提交的 change 必须立即广播，即使后续分段失败也不能丢失实时通知。
			for _, broadcast := range result.Broadcasts {
				h.hub.Broadcast(broadcast)
			}
			if err != nil {
				if result.LastSeq > 0 {
					_ = h.sync.UpdateDeviceSeq(uid, deviceID, result.LastSeq)
				}
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Push failed: " + err.Error()})
				continue
			}
			if len(result.MissingDigests) > 0 {
				_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "push_missing", Message: "Missing attachment blobs", MissingDigests: result.MissingDigests})
				continue
			}
			_ = h.sync.UpdateDeviceSeq(uid, deviceID, result.LastSeq)
			h.observeActivity(uid, deviceID, r)
			_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "push_ok", Seq: result.LastSeq})
		case "ping":
			_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "pong"})
		default:
			_ = peer.WriteJSON(syncsvc.ServerMessage{Type: "error", Message: "Unknown message type: " + msg.Type})
		}
	}
}
