package transport

import (
	stdsync "sync"

	"github.com/gorilla/websocket"

	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
)

type Hub struct {
	mu    stdsync.RWMutex
	pools map[string]map[string]*webSocketConnection
}

type webSocketConnection struct {
	conn    *websocket.Conn
	writeMu stdsync.Mutex
}

/**
 * newWebSocketConnection 包装 WebSocket 连接并统一串行化所有写操作。
 * @param conn 原始 Gorilla WebSocket 连接。
 * @returns 可安全并发调用写方法的连接包装器。
 */
func newWebSocketConnection(conn *websocket.Conn) *webSocketConnection {
	return &webSocketConnection{conn: conn}
}

/**
 * WriteJSON 串行写入一条 JSON 消息，避免响应与广播并发操作同一连接。
 * @param value 待编码并发送的消息。
 * @returns 写入成功时返回 nil，否则返回 WebSocket 错误。
 */
func (c *webSocketConnection) WriteJSON(value interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(value)
}

/**
 * Close 在等待当前写操作结束后关闭底层 WebSocket 连接。
 * @returns 关闭成功时返回 nil，否则返回网络错误。
 */
func (c *webSocketConnection) Close() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Close()
}

/**
 * NewHub 创建用于维护在线设备连接的 Hub。
 * @returns 初始化后的连接 Hub。
 */
func NewHub() *Hub {
	return &Hub{pools: map[string]map[string]*webSocketConnection{}}
}

/**
 * Add 将已认证设备连接加入用户连接池。
 * @param uid 已认证用户标识。
 * @param deviceID 设备标识。
 * @param conn 串行写入的 WebSocket 连接。
 * @returns 无返回值。
 */
func (h *Hub) Add(uid string, deviceID string, conn *webSocketConnection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pools[uid] == nil {
		h.pools[uid] = map[string]*webSocketConnection{}
	}
	h.pools[uid][deviceID] = conn
}

/**
 * Remove 仅在连接仍是设备当前连接时将其移出连接池。
 * @param uid 已认证用户标识。
 * @param deviceID 设备标识。
 * @param conn 准备移除的连接。
 * @returns 无返回值。
 */
func (h *Hub) Remove(uid string, deviceID string, conn *webSocketConnection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pools[uid] == nil {
		return
	}
	if h.pools[uid][deviceID] != conn {
		return
	}
	delete(h.pools[uid], deviceID)
	if len(h.pools[uid]) == 0 {
		delete(h.pools, uid)
	}
}

/**
 * Broadcast 将 change 发送给同一用户除来源设备外的所有在线连接。
 * @param change 已提交的同步广播消息。
 * @returns 无返回值。
 */
func (h *Hub) Broadcast(change syncsvc.Broadcast) {
	// 先复制目标连接，避免网络写阻塞在线状态的增删操作。
	h.mu.RLock()
	targets := make([]*webSocketConnection, 0, len(h.pools[change.UID]))
	for deviceID, conn := range h.pools[change.UID] {
		if deviceID == change.SenderDeviceID {
			continue
		}
		targets = append(targets, conn)
	}
	h.mu.RUnlock()

	msg := syncsvc.ServerMessage{Type: "change", Change: &change.Change}
	for _, conn := range targets {
		_ = conn.WriteJSON(msg)
	}
}

func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, pool := range h.pools {
		total += len(pool)
	}
	return total
}

func (h *Hub) IsOnline(uid string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.pools[uid]) > 0
}

func (h *Hub) OnlineDevices(uid string) []map[string]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	devices := []map[string]string{}
	for deviceID := range h.pools[uid] {
		devices = append(devices, map[string]string{"id": deviceID, "name": deviceID})
	}
	return devices
}
