package server

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	sendBuffer   = 16
	writeTimeout = 10 * time.Second
	maxFrameSize = 4 << 10 // 4 KiB — a 10-ship fleet is tiny
)

// wsConn adapts a coder/websocket connection to the hub's sender interface.
// trySend is non-blocking (drops to eviction when the buffer is full), and
// outbound frames are serialized by a single writePump goroutine.
type wsConn struct {
	ws        *websocket.Conn
	send      chan []byte
	cancel    context.CancelFunc
	closeOnce sync.Once
}

func (c *wsConn) trySend(b []byte) bool {
	select {
	case c.send <- b:
		return true
	default:
		return false
	}
}

func (c *wsConn) close() {
	c.closeOnce.Do(func() {
		c.cancel()
		c.ws.Close(websocket.StatusNormalClosure, "")
	})
}

// serveWS runs the read/write pumps for one connection until it closes, wiring
// inbound frames to the hub and reporting the eventual disconnect. token is the
// sb_token cookie value identifying the player.
func serveWS(hub *Hub, ws *websocket.Conn, token string) {
	ws.SetReadLimit(maxFrameSize)
	ctx, cancel := context.WithCancel(context.Background())
	conn := &wsConn{ws: ws, send: make(chan []byte, sendBuffer), cancel: cancel}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writePump(ctx, conn)
	}()

	readPump(ctx, hub, conn, token)

	conn.close()
	hub.OnDisconnect(token, conn)
	wg.Wait()
}

func writePump(ctx context.Context, c *wsConn) {
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				c.close()
				return
			}
		}
	}
}

func readPump(ctx context.Context, hub *Hub, c *wsConn, token string) {
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		// Route by message type: `join` (incl. reconnect) vs everything else.
		var probe struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(data, &probe)
		if probe.Type == "join" {
			hub.OnJoin(token, c, data)
		} else {
			hub.OnMessage(token, c, data)
		}
	}
}
