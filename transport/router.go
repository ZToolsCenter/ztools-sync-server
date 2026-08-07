package transport

import (
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/ZToolsCenter/ztools-sync-server/auth"
	"github.com/ZToolsCenter/ztools-sync-server/store"
	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
)

// Router exposes only authentication, document sync, checkpoints and attachments.
type Router struct {
	auth              *auth.Service
	sync              *syncsvc.Service
	hub               *Hub
	ws                *WSHandler
	allowRegistration bool
}

// RouterOptions configures standalone registration and optional external telemetry.
type RouterOptions struct {
	AllowRegistration bool
	ActivityObserver  ActivityObserver
}

// NewRouter creates the public synchronization HTTP and WebSocket router.
func NewRouter(authService *auth.Service, syncService *syncsvc.Service, hub *Hub, options RouterOptions) *Router {
	return &Router{
		auth:              authService,
		sync:              syncService,
		hub:               hub,
		ws:                NewWSHandlerWithObserver(authService, syncService, hub, options.ActivityObserver),
		allowRegistration: options.AllowRegistration,
	}
}

// RegisterRoutes mounts core API routes into a caller-owned mux.
func (r *Router) RegisterRoutes(mux *http.ServeMux) {
	authHandler := NewAuthHandler(r.auth, r.allowRegistration)
	attachmentHandler := NewAttachmentHandler(r.auth, r.sync)

	mux.HandleFunc("/api/auth", authHandler.Auth)
	mux.HandleFunc("/api/auth/captcha-config", authHandler.CaptchaConfig)
	mux.HandleFunc("/api/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("/api/login", authHandler.Login)
	if r.allowRegistration {
		mux.HandleFunc("/api/register", authHandler.Register)
	}
	mux.HandleFunc("/api/sync/attachments", attachmentHandler.List)
	mux.HandleFunc("/api/sync/attachments/", attachmentHandler.Item)
	mux.HandleFunc("/health", r.Health)
	mux.HandleFunc("/ws", r.ServeWS)
}

// Handler returns a complete standalone handler, including the root WebSocket endpoint.
func (r *Router) Handler() http.Handler {
	mux := http.NewServeMux()
	r.RegisterRoutes(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" {
			if websocket.IsWebSocketUpgrade(req) {
				r.ServeWS(w, req)
				return
			}
			r.Index(w, req)
			return
		}
		mux.ServeHTTP(w, req)
	})
}

// ServeWS upgrades and serves one synchronization connection.
func (r *Router) ServeWS(w http.ResponseWriter, req *http.Request) {
	r.ws.ServeHTTP(w, req)
}

// Health reports standalone edition and active WebSocket count.
func (r *Router) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"edition":     "standalone",
		"connections": r.hub.Count(),
	})
}

// Index reports the server edition and synchronization protocol version.
func (r *Router) Index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":            "ZTools Sync Server",
		"edition":         "standalone",
		"protocolVersion": store.ProtocolVersion,
	})
}
