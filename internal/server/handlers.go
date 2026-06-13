package server

import (
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"log"
	"net/http"

	"github.com/coder/websocket"
)

const cookieName = "sb_token"

// NewMux wires the HTTP routes: static client assets (with sb_token cookie
// minting) and the /ws WebSocket upgrade. webFS is the embedded web/ directory.
func NewMux(hub *Hub, webFS fs.FS) *http.ServeMux {
	mux := http.NewServeMux()
	files := http.FileServer(http.FS(webFS))

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		wsHandler(hub, w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ensureToken(w, r) // set sb_token on the HTML response if absent
		// Embedded assets carry no modtime, so the browser would otherwise
		// heuristically cache a stale style/script. Force a revalidation on
		// every load; the assets are tiny so a full refetch is cheap.
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		files.ServeHTTP(w, r)
	})
	return mux
}

// mintToken returns a random 128-bit hex token (UUID-strength, unguessable).
func mintToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ensureToken sets the sb_token cookie if the request doesn't carry one, and
// returns the effective token. The cookie is set on the page GET so it exists
// before the WebSocket upgrade (protocol.md §3a / server.md §6).
func ensureToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return c.Value
	}
	tok := mintToken()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tok,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60, // 30d: must outlive a browser close so reconnect keeps the same slot
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return tok
}

func wsHandler(hub *Hub, w http.ResponseWriter, r *http.Request) {
	// A token must exist; if the upgrade arrives without the cookie (e.g. /ws
	// hit directly), mint and set it on the 101 response before Accept.
	token := ensureToken(w, r)

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // trusted LAN: phones connect by raw IP
	})
	if err != nil {
		log.Printf("ws accept failed from %s: %v", r.RemoteAddr, err)
		return
	}
	serveWS(hub, ws, token)
}
