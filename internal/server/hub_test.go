package server

import (
	"encoding/json"
	"math/rand"
	"sync"
	"testing"
)

// --- fake connection --------------------------------------------------------

type fakeConn struct {
	frames [][]byte
	closed bool
	full   bool // when true, trySend reports a full buffer (slow client)
}

func (f *fakeConn) trySend(b []byte) bool {
	if f.full || f.closed {
		return false
	}
	f.frames = append(f.frames, append([]byte(nil), b...))
	return true
}
func (f *fakeConn) close() { f.closed = true }

// --- test client (drives the hub synchronously) -----------------------------

type client struct {
	t     *testing.T
	h     *Hub
	token string
	c     *fakeConn
}

func newClient(t *testing.T, h *Hub, token string) *client {
	return &client{t: t, h: h, token: token, c: &fakeConn{}}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func (cl *client) join(name string) {
	cl.h.handle(command{cmdJoin, cl.token, cl.c, mustJSON(map[string]any{"type": "join", "name": name})})
}
func (cl *client) reconnect() {
	cl.c = &fakeConn{} // a fresh socket carrying the same token
	cl.h.handle(command{cmdJoin, cl.token, cl.c, mustJSON(map[string]any{"type": "join", "name": "ignored"})})
}
func (cl *client) place(ships []wireShip) {
	cl.h.handle(command{cmdMessage, cl.token, cl.c, mustJSON(map[string]any{"type": "place", "ships": ships})})
}
func (cl *client) fire(x, y int) {
	cl.h.handle(command{cmdMessage, cl.token, cl.c, mustJSON(map[string]any{"type": "fire", "x": x, "y": y})})
}
func (cl *client) rematch() {
	cl.h.handle(command{cmdMessage, cl.token, cl.c, mustJSON(map[string]any{"type": "rematch"})})
}
func (cl *client) raw(b []byte) { cl.h.handle(command{cmdMessage, cl.token, cl.c, b}) }
func (cl *client) disconnect()  { cl.h.handle(command{cmdDisconnect, cl.token, cl.c, nil}) }

// frame access (FIFO over everything sent to this socket).
func (cl *client) next() map[string]any {
	cl.t.Helper()
	if len(cl.c.frames) == 0 {
		cl.t.Fatal("expected a frame, got none")
	}
	var m map[string]any
	if err := json.Unmarshal(cl.c.frames[0], &m); err != nil {
		cl.t.Fatalf("bad frame json: %v", err)
	}
	cl.c.frames = cl.c.frames[1:]
	return m
}
func (cl *client) expect(typ string) map[string]any {
	cl.t.Helper()
	m := cl.next()
	if m["type"] != typ {
		cl.t.Fatalf("frame type = %v, want %q (frame: %v)", m["type"], typ, m)
	}
	return m
}
func (cl *client) expectNone() {
	cl.t.Helper()
	if len(cl.c.frames) != 0 {
		var m map[string]any
		_ = json.Unmarshal(cl.c.frames[0], &m)
		cl.t.Fatalf("expected no frame, got %v", m)
	}
}
func (cl *client) drain() { cl.c.frames = nil }

// --- fixtures ---------------------------------------------------------------

func standardWireFleet() []wireShip {
	return []wireShip{
		{Size: 4, X: 0, Y: 0, Dir: "h"},
		{Size: 3, X: 0, Y: 2, Dir: "h"},
		{Size: 3, X: 0, Y: 4, Dir: "h"},
		{Size: 2, X: 0, Y: 6, Dir: "h"},
		{Size: 2, X: 0, Y: 8, Dir: "h"},
		{Size: 2, X: 5, Y: 0, Dir: "h"},
		{Size: 1, X: 5, Y: 2},
		{Size: 1, X: 7, Y: 2},
		{Size: 1, X: 5, Y: 4},
		{Size: 1, X: 9, Y: 4},
	}
}

// every occupied cell of the standard fleet, in firing order.
func standardCells() [][2]int {
	var out [][2]int
	for _, s := range standardWireFleet() {
		for i := 0; i < s.Size; i++ {
			if s.Dir == "v" {
				out = append(out, [2]int{s.X, s.Y + i})
			} else {
				out = append(out, [2]int{s.X + i, s.Y})
			}
		}
	}
	return out
}

func newHub() *Hub { return NewHub(rand.New(rand.NewSource(1))) }

// --- B1 join & assignment ---------------------------------------------------

func TestJoin_AssignAndExchange(t *testing.T) {
	h := newHub()
	a := newClient(t, h, "tokA")
	a.join("Alice")
	if m := a.expect("assigned"); m["player"].(float64) != 0 || m["reconnect"].(bool) {
		t.Fatalf("assigned = %v, want player 0 reconnect false", m)
	}
	a.expect("opponent") // online:false
	if m := a.expect("state"); m["phase"] != "waiting" {
		t.Fatalf("solo phase = %v, want waiting (B2.1)", m["phase"])
	}
	a.expectNone()

	b := newClient(t, h, "tokB")
	b.join("Bob")
	if m := b.expect("assigned"); m["player"].(float64) != 1 {
		t.Fatalf("B player = %v, want 1", m["player"])
	}
	if m := b.expect("opponent"); m["name"] != "Alice" || !m["online"].(bool) {
		t.Fatalf("B opponent = %v, want Alice online", m)
	}
	if m := b.expect("state"); m["phase"] != "placing" {
		t.Fatalf("B state phase = %v, want placing", m["phase"])
	}
	b.expectNone()

	// B2.3 — A gets opponent{online:true} only, NO new state.
	if m := a.expect("opponent"); m["name"] != "Bob" || !m["online"].(bool) {
		t.Fatalf("A opponent = %v, want Bob online", m)
	}
	a.expectNone()
}

func TestJoin_ThirdIsFull(t *testing.T) {
	h := newHub()
	newClientJoined(t, h, "tokA", "Alice")
	newClientJoined(t, h, "tokB", "Bob")
	c := newClient(t, h, "tokC")
	c.join("Carol")
	c.expect("full")
	if !c.c.closed {
		t.Fatal("third connection not closed after full")
	}
}

func TestJoin_BadName(t *testing.T) {
	h := newHub()
	a := newClient(t, h, "tokA")
	a.join("   ")
	if m := a.expect("error"); m["code"] != "bad_name" {
		t.Fatalf("error code = %v, want bad_name", m["code"])
	}
	if h.occupied() != 0 {
		t.Fatal("bad name consumed a slot")
	}
}

func TestJoin_NameCapped(t *testing.T) {
	h := newHub()
	a := newClient(t, h, "tokA")
	a.join("ABCDEFGHIJKLMNOPQRSTUVWXYZ") // 26 chars
	m := a.expect("assigned")
	if len([]rune(m["name"].(string))) != maxNameLen {
		t.Fatalf("name len = %d, want %d", len(m["name"].(string)), maxNameLen)
	}
}

// helper: join and discard the handshake frames.
func newClientJoined(t *testing.T, h *Hub, token, name string) *client {
	cl := newClient(t, h, token)
	cl.join(name)
	cl.drain()
	return cl
}

// --- B3 placement -----------------------------------------------------------

func TestPlace_ValidWaitsForOpponent(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	a.drain()
	b.drain()
	a.place(standardWireFleet())
	if m := a.expect("placeResult"); m["ok"] != true {
		t.Fatalf("placeResult = %v, want ok", m)
	}
	a.expectNone() // no gameStart yet
}

func TestPlace_BadComposition(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	newClientJoined(t, h, "tokB", "Bob")
	a.drain()
	a.place(standardWireFleet()[:9]) // missing a ship
	m := a.expect("placeResult")
	if m["ok"] != false || m["error"] == nil {
		t.Fatalf("placeResult = %v, want ok:false with error", m)
	}
}

func TestPlace_BeforeJoin(t *testing.T) {
	h := newHub()
	c := newClient(t, h, "tokX")
	c.place(standardWireFleet())
	if m := c.expect("error"); m["code"] != "not_joined" {
		t.Fatalf("error = %v, want not_joined", m)
	}
}

func TestPlace_BothReadyStartsWithRNG(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	a.drain()
	b.drain()
	a.place(standardWireFleet())
	a.expect("placeResult")
	b.place(standardWireFleet())
	b.expect("placeResult")
	// both get gameStart; firstPlayer agreed.
	ga := a.expect("gameStart")
	gb := b.expect("gameStart")
	if ga["firstPlayer"] != gb["firstPlayer"] {
		t.Fatalf("firstPlayer disagreement: %v vs %v", ga["firstPlayer"], gb["firstPlayer"])
	}
	first := int(ga["firstPlayer"].(float64))
	if ga["yourTurn"].(bool) != (first == 0) || gb["yourTurn"].(bool) != (first == 1) {
		t.Fatalf("yourTurn wrong for first=%d: a=%v b=%v", first, ga["yourTurn"], gb["yourTurn"])
	}
}

// --- B4 full game -----------------------------------------------------------

func TestFullGame(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	a.drain()
	b.drain()
	a.place(standardWireFleet())
	a.expect("placeResult")
	b.place(standardWireFleet())
	b.expect("placeResult")
	first := int(a.expect("gameStart")["firstPlayer"].(float64))
	b.expect("gameStart")
	a.drain()
	b.drain()

	shooter, target := a, b
	if first == 1 {
		shooter, target = b, a
	}
	// Shooter fires every opponent ship cell; each is a hit/sunk → keeps turn.
	cells := standardCells()
	for i, xy := range cells {
		shooter.fire(xy[0], xy[1])
		fr := shooter.expect("fireResult") // broadcast — read shooter's copy
		target.expect("fireResult")        // and target's copy
		if i == len(cells)-1 {
			if fr["outcome"] != "sunk" {
				t.Fatalf("last shot outcome = %v, want sunk", fr["outcome"])
			}
			if g := shooter.expect("gameOver"); int(g["winner"].(float64)) != first {
				t.Fatalf("winner = %v, want %d", g["winner"], first)
			}
			target.expect("gameOver")
		}
	}
}

// --- B5 invalid fire --------------------------------------------------------

func TestFire_Invalid(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	a.place(standardWireFleet())
	b.place(standardWireFleet())
	a.drain()
	b.drain()
	first := h.game.Turn()
	shooter, waiter := a, b
	if first == 1 {
		shooter, waiter = b, a
	}

	// non-turn player fires → error to sender only, no broadcast.
	waiter.fire(0, 0)
	if m := waiter.expect("error"); m["code"] != "not_your_turn" {
		t.Fatalf("error = %v, want not_your_turn", m)
	}
	shooter.expectNone()

	// out of bounds.
	shooter.fire(10, 0)
	if m := shooter.expect("error"); m["code"] != "out_of_bounds" {
		t.Fatalf("error = %v, want out_of_bounds", m)
	}

	// unknown type / bad json.
	shooter.raw(mustJSON(map[string]any{"type": "foo"}))
	if m := shooter.expect("error"); m["code"] != "bad_type" {
		t.Fatalf("error = %v, want bad_type", m)
	}
	shooter.raw([]byte("{not json"))
	if m := shooter.expect("error"); m["code"] != "bad_json" {
		t.Fatalf("error = %v, want bad_json", m)
	}
}

// --- B6 disconnect / freeze / reconnect ------------------------------------

func TestReconnect_FreezeAndResync(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	a.place(standardWireFleet())
	b.place(standardWireFleet())
	a.drain()
	b.drain()
	first := h.game.Turn()
	shooter := a
	if first == 1 {
		shooter = b
	}
	// shooter sinks the opponent's (9,4) size1 before the disconnect.
	shooter.fire(9, 4)
	a.drain()
	b.drain()

	// opponent (the non-shooter) disconnects.
	dropped, other := b, a
	if first == 1 {
		dropped, other = a, b
	}
	dropped.disconnect()
	if m := other.expect("opponent"); m["online"].(bool) {
		t.Fatalf("expected opponent offline, got %v", m)
	}

	// B6.2 — firing while frozen is rejected.
	if h.slots[first].Online {
		shooter.fire(0, 5)
		if m := shooter.expect("error"); m["code"] != "opponent_offline" {
			t.Fatalf("frozen fire = %v, want opponent_offline", m)
		}
	}

	// B6.3/B6.5 — reconnect → assigned{reconnect} + opponent + full state with
	// the sunk cell preserved.
	dropped.reconnect()
	if m := dropped.expect("assigned"); !m["reconnect"].(bool) {
		t.Fatalf("reconnect assigned = %v, want reconnect true", m)
	}
	dropped.expect("opponent")
	st := dropped.expect("state")
	if st["phase"] != "playing" {
		t.Fatalf("resync phase = %v, want playing", st["phase"])
	}
	// the dropped player's own board recorded the incoming sunk at (9,4).
	incoming := st["ownBoard"].(map[string]any)["incoming"].([]any)
	var sawSunk bool
	for _, s := range incoming {
		sm := s.(map[string]any)
		if int(sm["x"].(float64)) == 9 && int(sm["y"].(float64)) == 4 && sm["outcome"] == "sunk" {
			sawSunk = true
		}
	}
	if !sawSunk {
		t.Fatalf("resync incoming missing sunk (9,4): %v", incoming)
	}
	// other player told opponent back online.
	if m := other.expect("opponent"); !m["online"].(bool) {
		t.Fatalf("reconnect: other expected opponent online, got %v", m)
	}
}

func TestDisconnect_FreesSlotWhileWaiting(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	a.disconnect()
	if h.occupied() != 0 {
		t.Fatal("solo disconnect did not free the slot (B6.6)")
	}
	// fresh visitor reuses slot 0.
	c := newClient(t, h, "tokC")
	c.join("Carol")
	if m := c.expect("assigned"); m["player"].(float64) != 0 {
		t.Fatalf("reused slot = %v, want 0", m["player"])
	}
}

func TestDisconnect_ReservesSlotAfterOpponentJoined(t *testing.T) {
	h := newHub()
	newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	b.disconnect() // B6.7 — slot reserved, not freed
	if h.occupied() != 2 {
		t.Fatalf("occupied = %d, want 2 (slot reserved)", h.occupied())
	}
	c := newClient(t, h, "tokC")
	c.join("Carol")
	c.expect("full") // fresh token rejected
}

func TestDisconnect_Idempotent(t *testing.T) {
	h := newHub()
	newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	a := h.slots[0]
	b.disconnect()
	a.out.(*fakeConn).frames = nil
	b.disconnect() // B6.10 — second disconnect is a no-op
	// no duplicate opponent frame to A.
	if len(a.out.(*fakeConn).frames) != 0 {
		t.Fatalf("double disconnect produced extra frames: %v", a.out.(*fakeConn).frames)
	}
}

// --- B6.8 reconnect while FINISHED -----------------------------------------

func TestReconnect_WhileFinished(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	a.place(standardWireFleet())
	b.place(standardWireFleet())
	a.drain()
	b.drain()

	first := h.game.Turn()
	shooter, loser := a, b
	if first == 1 {
		shooter, loser = b, a
	}
	for _, xy := range standardCells() {
		shooter.fire(xy[0], xy[1])
	}
	if h.game.Phase().String() != "finished" {
		t.Fatalf("phase = %v, want finished", h.game.Phase())
	}
	loser.drain()

	// loser disconnects (slot reserved, game over) then reconnects.
	loser.disconnect()
	loser.reconnect()
	loser.expect("assigned")
	loser.expect("opponent")
	st := loser.expect("state")
	if st["phase"] != "finished" {
		t.Fatalf("resync phase = %v, want finished", st["phase"])
	}
	if int(st["winner"].(float64)) != first {
		t.Fatalf("resync winner = %v, want %d", st["winner"], first)
	}
}

// --- B7.2 concurrent Run() flood (real channel + goroutine, race evidence) --

func TestHub_ConcurrentRunNoRace(t *testing.T) {
	h := newHub()
	done := make(chan struct{})
	go func() { h.Run(); close(done) }()

	a, b := &fakeConn{}, &fakeConn{}
	// ordered setup through the command channel.
	h.OnJoin("A", a, mustJSON(map[string]any{"type": "join", "name": "Alice"}))
	h.OnJoin("B", b, mustJSON(map[string]any{"type": "join", "name": "Bob"}))
	h.OnMessage("A", a, mustJSON(map[string]any{"type": "place", "ships": standardWireFleet()}))
	h.OnMessage("B", b, mustJSON(map[string]any{"type": "place", "ships": standardWireFleet()}))

	// two goroutines flood fire commands concurrently; the hub serializes them.
	var wg sync.WaitGroup
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				h.OnMessage("A", a, mustJSON(map[string]any{"type": "fire", "x": i % 10, "y": (i / 10) % 10}))
				h.OnMessage("B", b, mustJSON(map[string]any{"type": "fire", "x": i % 10, "y": (i / 10) % 10}))
			}
		}()
	}
	wg.Wait()
	close(h.cmds)
	<-done // hub goroutine fully stopped → safe to inspect

	if p := h.game.Phase().String(); p != "playing" && p != "finished" {
		t.Fatalf("unexpected phase after flood: %q", p)
	}
}

// --- B7.3 slow client eviction ---------------------------------------------

func TestSlowClientEvictedInline(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	a.place(standardWireFleet())
	b.place(standardWireFleet())
	a.drain()
	b.drain()
	first := h.game.Turn()
	shooter, slow := a, b
	if first == 1 {
		shooter, slow = b, a
	}
	slow.c.full = true // slow client's buffer is full
	shooter.fire(0, 0) // hit → broadcast tries to reach the slow client
	// slow client evicted inline; not online anymore.
	if h.slots[slow.h.tokens[slow.token].Slot].Online {
		t.Fatal("slow client not evicted on full buffer (C1)")
	}
	// shooter still served (got its own fireResult).
	if _, ok := firstFrameType(shooter); !ok {
		t.Fatal("shooter not served after peer eviction")
	}
}

func firstFrameType(cl *client) (string, bool) {
	if len(cl.c.frames) == 0 {
		return "", false
	}
	var m map[string]any
	_ = json.Unmarshal(cl.c.frames[0], &m)
	s, _ := m["type"].(string)
	return s, true
}

// --- B8 privacy -------------------------------------------------------------

func TestPrivacy_NoEnemyShipsLeaked(t *testing.T) {
	h := newHub()
	a := newClient(t, h, "tokA")
	a.join("Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	_ = b
	// inspect every frame Alice has received: none may carry an enemy `ships`
	// list other than her own ownBoard.ships.
	for _, raw := range a.c.frames {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m["type"] == "state" {
			// only ownBoard.ships is allowed; there is no enemy ships field.
			if _, bad := m["enemyShips"]; bad {
				t.Fatal("state leaked enemyShips")
			}
		}
	}
}

// --- B9 rematch (both-agree) -----------------------------------------------

// playToFinished drives a fresh game to FINISHED and returns the winning slot.
func playToFinished(t *testing.T, h *Hub, a, b *client) int {
	t.Helper()
	a.place(standardWireFleet())
	b.place(standardWireFleet())
	first := h.game.Turn()
	shooter := a
	if first == 1 {
		shooter = b
	}
	for _, xy := range standardCells() {
		shooter.fire(xy[0], xy[1])
	}
	if h.game.Phase().String() != "finished" {
		t.Fatalf("phase = %v, want finished", h.game.Phase())
	}
	return first
}

func TestRematch_BothAgree(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	playToFinished(t, h, a, b)
	a.drain()
	b.drain()

	// First request: no reset, both sides get a status frame.
	a.rematch()
	if h.game.Phase().String() != "finished" {
		t.Fatalf("phase after one rematch = %v, want finished", h.game.Phase())
	}
	ra := a.expect("rematch")
	if ra["youReady"] != true || ra["opponentReady"] != false {
		t.Fatalf("requester status = %v, want youReady:true opponentReady:false", ra)
	}
	rb := b.expect("rematch")
	if rb["youReady"] != false || rb["opponentReady"] != true {
		t.Fatalf("opponent status = %v, want youReady:false opponentReady:true", rb)
	}

	// Repeat request from same slot is idempotent (no new frame, no reset).
	a.drain()
	b.drain()
	a.rematch()
	a.expectNone()
	b.expectNone()

	// Second player agrees → reset to PLACING, fresh state to both.
	b.rematch()
	if h.game.Phase().String() != "placing" {
		t.Fatalf("phase after both rematch = %v, want placing", h.game.Phase())
	}
	for _, cl := range []*client{a, b} {
		st := cl.expect("state")
		if st["phase"] != "placing" {
			t.Fatalf("reset state phase = %v, want placing", st["phase"])
		}
		if st["youReady"] != false {
			t.Fatalf("reset youReady = %v, want false", st["youReady"])
		}
		if st["winner"] != nil {
			t.Fatalf("reset winner = %v, want nil", st["winner"])
		}
	}
	if h.rematch[0] || h.rematch[1] {
		t.Fatalf("rematch flags not cleared: %v", h.rematch)
	}
}

func TestRematch_WrongPhase(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	newClientJoined(t, h, "tokB", "Bob")
	a.drain()
	a.rematch() // game is in PLACING, not FINISHED
	if m := a.expect("error"); m["code"] != "wrong_phase" {
		t.Fatalf("error code = %v, want wrong_phase", m["code"])
	}
}

// --- B10 rename -------------------------------------------------------------

func TestRename_NotifiesOpponent(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	b := newClientJoined(t, h, "tokB", "Bob")
	a.drain()
	b.drain()

	a.h.handle(command{cmdMessage, "tokA", a.c, mustJSON(map[string]any{"type": "rename", "name": "Captain"})})

	if h.slots[0].Name != "Captain" {
		t.Fatalf("self name = %q, want Captain", h.slots[0].Name)
	}
	if op := b.expect("opponent"); op["name"] != "Captain" {
		t.Fatalf("opponent frame name = %v, want Captain", op["name"])
	}
	a.expectNone() // requester gets no ack
}

func TestRename_EmptyRejected(t *testing.T) {
	h := newHub()
	a := newClientJoined(t, h, "tokA", "Alice")
	newClientJoined(t, h, "tokB", "Bob")
	a.drain()
	a.h.handle(command{cmdMessage, "tokA", a.c, mustJSON(map[string]any{"type": "rename", "name": "  "})})
	if m := a.expect("error"); m["code"] != "bad_name" {
		t.Fatalf("error code = %v, want bad_name", m["code"])
	}
	if h.slots[0].Name != "Alice" {
		t.Fatalf("name changed on invalid rename: %q", h.slots[0].Name)
	}
}
