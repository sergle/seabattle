package server

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
)

// wsClient is a real WebSocket client over httptest, carrying its sb_token via
// a cookie jar (so reconnect identity works exactly like a browser).
type wsClient struct {
	t   *testing.T
	ws  *websocket.Conn
	ctx context.Context
}

func dialWS(t *testing.T, srv *httptest.Server) *wsClient {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar}
	// GET the page first so the server sets sb_token on the jar.
	resp, err := hc.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	ctx := context.Background()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	ws, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: hc})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return &wsClient{t: t, ws: ws, ctx: ctx}
}

func (c *wsClient) send(v any) {
	b, _ := json.Marshal(v)
	if err := c.ws.Write(c.ctx, websocket.MessageText, b); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *wsClient) read() map[string]any {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
	defer cancel()
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		c.t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// readType reads frames until one of the wanted type appears (skips the rest).
func (c *wsClient) readType(want string) map[string]any {
	c.t.Helper()
	for i := 0; i < 20; i++ {
		m := c.read()
		if m["type"] == want {
			return m
		}
	}
	c.t.Fatalf("never saw frame type %q", want)
	return nil
}

func TestE2E_RealTransportFullGame(t *testing.T) {
	hub := NewHub(rand.New(rand.NewSource(1)))
	go hub.Run()
	srv := httptest.NewServer(NewMux(hub, fstest.MapFS{}))
	defer srv.Close()

	a := dialWS(t, srv)
	a.send(map[string]any{"type": "join", "name": "Alice"})
	if m := a.readType("assigned"); int(m["player"].(float64)) != 0 {
		t.Fatalf("A player = %v, want 0", m["player"])
	}

	b := dialWS(t, srv)
	b.send(map[string]any{"type": "join", "name": "Bob"})
	if m := b.readType("assigned"); int(m["player"].(float64)) != 1 {
		t.Fatalf("B player = %v, want 1", m["player"])
	}

	a.send(map[string]any{"type": "place", "ships": standardWireFleet()})
	b.send(map[string]any{"type": "place", "ships": standardWireFleet()})

	gs := a.readType("gameStart")
	first := int(gs["firstPlayer"].(float64))
	_ = b.readType("gameStart")

	shooter, target := a, b
	if first == 1 {
		shooter, target = b, a
	}

	cells := standardCells()
	for i, xy := range cells {
		shooter.send(map[string]any{"type": "fire", "x": xy[0], "y": xy[1]})
		fr := shooter.readType("fireResult")
		_ = target.readType("fireResult")
		if i == len(cells)-1 {
			if fr["outcome"] != "sunk" {
				t.Fatalf("last outcome = %v, want sunk", fr["outcome"])
			}
			if g := shooter.readType("gameOver"); int(g["winner"].(float64)) != first {
				t.Fatalf("winner = %v, want %d", g["winner"], first)
			}
		}
	}
}

func TestE2E_ReconnectCarriesToken(t *testing.T) {
	hub := NewHub(rand.New(rand.NewSource(1)))
	go hub.Run()
	srv := httptest.NewServer(NewMux(hub, fstest.MapFS{}))
	defer srv.Close()

	// A joins and places; B joins.
	jarA, _ := cookiejar.New(nil)
	hcA := &http.Client{Jar: jarA}
	resp, _ := hcA.Get(srv.URL + "/")
	resp.Body.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx := context.Background()
	wsA, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: hcA})
	if err != nil {
		t.Fatal(err)
	}
	a := &wsClient{t: t, ws: wsA, ctx: ctx}
	a.send(map[string]any{"type": "join", "name": "Alice"})
	a.readType("assigned")

	// B must join so A's slot is RESERVED on disconnect (not freed as a solo).
	b := dialWS(t, srv)
	b.send(map[string]any{"type": "join", "name": "Bob"})
	b.readType("assigned")

	// drop A's socket, then reconnect with the SAME cookie jar (same token).
	wsA.Close(websocket.StatusNormalClosure, "")
	time.Sleep(50 * time.Millisecond)

	wsA2, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: hcA})
	if err != nil {
		t.Fatalf("redial: %v", err)
	}
	a2 := &wsClient{t: t, ws: wsA2, ctx: ctx}
	a2.send(map[string]any{"type": "join", "name": "Alice"})
	m := a2.readType("assigned")
	if !m["reconnect"].(bool) {
		t.Fatalf("reconnect assigned = %v, want reconnect:true (same token rebind)", m)
	}
	if int(m["player"].(float64)) != 0 {
		t.Fatalf("reconnect player = %v, want same slot 0", m["player"])
	}
}
