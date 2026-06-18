package ws

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// TokenAuthenticator validates an auth token. Returns true when valid.
// The implementation lives in the auth package; we accept a function
// here to keep the ws package independent of auth.
type TokenAuthenticator func(token string) bool

// Upgrader configures the WebSocket handshake.
//
// CheckOrigin is permissive because this endpoint requires an auth
// token via Sec-WebSocket-Protocol subprotocol or ?token= query param.
// CSRF on WebSockets is mitigated by the auth requirement, not by origin.
var Upgrader = websocket.Upgrader{
	ReadBufferSize:   1024,
	WriteBufferSize:  4096,
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin:      func(r *http.Request) bool { return true },
}

// AuthSubprotocol is the canonical subprotocol token format. Clients
// send `Sec-WebSocket-Protocol: stackly-auth-v1, <token>` and we
// extract <token> for validation. Format chosen to be parseable without
// making the token visible in URL logs.
const AuthSubprotocol = "stackly-auth-v1"

// HandleScanWS upgrades the connection to a WebSocket and streams events
// for the requested job until:
//   - the client disconnects, OR
//   - a terminal event (completed/failed) is sent and acknowledged, OR
//   - the configured idle timeout expires.
//
// jobID is the trailing path segment after /api/ws/scan/.
//
// Auth: the WS endpoint is in the middleware skip list because browsers
// can't easily set custom headers on the WS handshake. Instead we accept
// the token via one of:
//   - Sec-WebSocket-Protocol subprotocol: "stackly-auth-v1, <token>"
//   - ?token=<token> query parameter (less secure — visible in proxy logs)
//
// When authenticator is nil, all connections are allowed (dev mode).
func (h *Hub) HandleScanWS(w http.ResponseWriter, r *http.Request, authenticate TokenAuthenticator) {
	jobID := strings.TrimPrefix(r.URL.Path, "/api/ws/scan/")
	if jobID == "" || strings.Contains(jobID, "/") || strings.Contains(jobID, "..") {
		http.Error(w, `{"error":"invalid job id"}`, http.StatusBadRequest)
		return
	}

	// Auth check before upgrade — we want to reject unauthorized clients
	// with a clean HTTP 401, not a torn-down WebSocket.
	if authenticate != nil {
		token := extractWSToken(r)
		if token == "" || !authenticate(token) {
			http.Error(w, `{"error":"missing or invalid token (use Sec-WebSocket-Protocol: stackly-auth-v1, <token> or ?token=)"}`,
				http.StatusUnauthorized)
			return
		}
	}

	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response.
		return
	}

	sub := h.Subscribe(jobID, conn, true)

	// Reader pump: detect client disconnects + handle ping/pong.
	clientGone := make(chan struct{})
	go func() {
		defer close(clientGone)
		conn.SetReadLimit(512)
		_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
			return nil
		})
		for {
			mt, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Treat any client message as a keep-alive ping.
			if mt == websocket.TextMessage || mt == websocket.BinaryMessage {
				_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
			}
		}
	}()

	// Writer pump: send events + periodic heartbeats.
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	idleTimeout := time.NewTimer(10 * time.Minute)
	defer idleTimeout.Stop()

	defer func() {
		h.Unsubscribe(sub)
		_ = conn.Close()
	}()

	for {
		select {
		case <-clientGone:
			return
		case <-idleTimeout.C:
			// Client idle too long.
			return
		case <-pingTicker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case ev, ok := <-sub.Send:
			if !ok {
				// Hub closed the channel; connection is being torn down.
				return
			}
			if err := sub.WriteJSON(ev, time.Now().Add(10*time.Second)); err != nil {
				return
			}
			// Terminal events end the stream.
			if ev.Type == "completed" || ev.Type == "failed" {
				// Give the client a moment to receive before close.
				time.Sleep(100 * time.Millisecond)
				return
			}
		}
	}
}

// extractWSToken pulls the auth token from either the Sec-WebSocket-Protocol
// subprotocol header or the ?token= query param. Subprotocol is preferred
// since it doesn't end up in proxy access logs.
//
// Subprotocol format expected: "stackly-auth-v1, <token>" (comma-separated
// per RFC 6455 — multi-protocol negotiation). We also accept just the
// token alone for clients that don't include the prefix.
func extractWSToken(r *http.Request) string {
	if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
		for _, p := range strings.Split(proto, ",") {
			p = strings.TrimSpace(p)
			if p == AuthSubprotocol {
				// Bare prefix with no token — skip.
				continue
			}
			// Strip prefix if present (e.g., "stackly-auth-v1 <token>")
			if strings.HasPrefix(p, AuthSubprotocol+" ") {
				return strings.TrimSpace(p[len(AuthSubprotocol)+1:])
			}
			// Or accept any other subprotocol as a token (some clients
			// only set one header value).
			if p != "" {
				return p
			}
		}
	}
	return r.URL.Query().Get("token")
}