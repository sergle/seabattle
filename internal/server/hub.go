package server

import (
	"encoding/json"
	"errors"
	"log"
	"math/rand"

	"seabattle/internal/game"
)

type cmdKind int

const (
	cmdJoin cmdKind = iota
	cmdMessage
	cmdDisconnect
)

// command is the unit of work on the hub goroutine. token identifies the
// player (sb_token cookie); out is the connection that produced it.
type command struct {
	kind  cmdKind
	token string
	out   sender
	raw   []byte
}

// Hub owns all mutable game/session state. Every field below is touched only
// on the goroutine running Run() (or a test calling handle() directly), so no
// mutexes are needed (server.md §4/§9).
type Hub struct {
	cmds    chan command
	game    *game.Game
	slots   [2]*Session
	tokens  map[string]*Session
	rng     *rand.Rand
	rematch [2]bool // per-slot "wants a rematch", valid only while FINISHED
}

// NewHub builds a hub with an injected RNG (seedable for deterministic tests).
func NewHub(rng *rand.Rand) *Hub {
	return &Hub{
		cmds:   make(chan command, 64),
		game:   game.NewGame(),
		tokens: map[string]*Session{},
		rng:    rng,
	}
}

// Run drains the command channel on a single goroutine until it is closed.
func (h *Hub) Run() {
	for c := range h.cmds {
		h.handle(c)
	}
}

// Enqueue helpers — safe to call from any connection goroutine.
func (h *Hub) OnJoin(token string, out sender, raw []byte) {
	h.cmds <- command{cmdJoin, token, out, raw}
}
func (h *Hub) OnMessage(token string, out sender, raw []byte) {
	h.cmds <- command{cmdMessage, token, out, raw}
}
func (h *Hub) OnDisconnect(token string, out sender) {
	h.cmds <- command{cmdDisconnect, token, out, nil}
}

// handle processes one command. Runs on the hub goroutine only.
func (h *Hub) handle(c command) {
	switch c.kind {
	case cmdJoin:
		h.handleJoin(c)
	case cmdMessage:
		h.handleMessage(c)
	case cmdDisconnect:
		h.handleDisconnect(c)
	}
}

// --- join -------------------------------------------------------------------

func (h *Hub) handleJoin(c command) {
	var in inbound
	if err := json.Unmarshal(c.raw, &in); err != nil {
		c.out.trySend(msgError("bad_json", ""))
		return
	}
	name, ok := sanitizeName(in.Name)

	// reconnect: a session already holds this token.
	if sess := h.tokens[c.token]; sess != nil {
		if sess.out != nil {
			sess.out.close() // drop any stale socket
		}
		sess.out = c.out
		sess.Online = true
		oppName, oppOnline := h.opponentInfo(sess.Slot)
		sess.out.trySend(msgAssigned(sess.Slot, sess.Name, true))
		sess.out.trySend(msgOpponent(oppName, oppOnline))
		sess.out.trySend(h.stateFor(sess))
		h.notifyOpponent(sess.Slot, msgOpponent(sess.Name, true))
		log.Printf("player #%d reconnected: %q", sess.Slot, sess.Name)
		return
	}

	// new player needs a valid name.
	if !ok {
		c.out.trySend(msgError("bad_name", "name is empty"))
		return
	}

	slot := h.freeSlot()
	if slot < 0 {
		c.out.trySend(msgFull())
		c.out.close()
		log.Printf("join rejected (game full): %q", name)
		return
	}
	sess := &Session{Token: c.token, Slot: slot, Name: name, Online: true, out: c.out}
	h.tokens[c.token] = sess
	h.slots[slot] = sess

	oppName, oppOnline := h.opponentInfo(slot)
	sess.out.trySend(msgAssigned(slot, name, false))
	sess.out.trySend(msgOpponent(oppName, oppOnline))
	sess.out.trySend(h.stateFor(sess))

	// The already-present player's WAITING→PLACING cue is this opponent message
	// (protocol.md §6); it is NOT re-sent a full state.
	h.notifyOpponent(slot, msgOpponent(name, true))
	log.Printf("player #%d connected: %q", slot, name)
}

// --- message dispatch -------------------------------------------------------

func (h *Hub) handleMessage(c command) {
	sess := h.tokens[c.token]
	if sess == nil {
		c.out.trySend(msgError("not_joined", ""))
		return
	}
	var in inbound
	if err := json.Unmarshal(c.raw, &in); err != nil {
		sess.out.trySend(msgError("bad_json", ""))
		return
	}
	switch in.Type {
	case "place":
		h.handlePlace(sess, in)
	case "fire":
		h.handleFire(sess, in)
	case "rematch":
		h.handleRematch(sess)
	case "rename":
		h.handleRename(sess, in)
	default:
		sess.out.trySend(msgError("bad_type", in.Type))
	}
}

func (h *Hub) handlePlace(sess *Session, in inbound) {
	err := h.game.PlaceFleet(sess.Slot, placementsFrom(in.Ships))
	if err != nil {
		var fe *game.FleetError
		switch {
		case errors.As(err, &fe):
			sess.out.trySend(msgPlaceResult(false, fe.Error()))
		case errors.Is(err, game.ErrAlreadyPlaced):
			sess.out.trySend(msgError("already_placed", ""))
		default: // wrong phase
			sess.out.trySend(msgError("wrong_phase", ""))
		}
		return
	}
	sess.out.trySend(msgPlaceResult(true, ""))

	if h.game.BothReady() {
		first, err := h.game.Start(h.rng)
		if err != nil {
			return
		}
		for slot := 0; slot < 2; slot++ {
			h.sendTo(h.slots[slot], msgGameStart(slot == first, first))
		}
		log.Printf("game started: %q vs %q",
			h.slots[first].Name, h.slots[1-first].Name)
	}
}

func (h *Hub) handleFire(sess *Session, in inbound) {
	if !h.bothOnline() {
		sess.out.trySend(msgError("opponent_offline", ""))
		return
	}
	if in.X == nil || in.Y == nil {
		sess.out.trySend(msgError("out_of_bounds", "missing coordinate"))
		return
	}
	x, y := *in.X, *in.Y
	res, err := h.game.Fire(sess.Slot, game.Coord{X: x, Y: y})
	if err != nil {
		sess.out.trySend(msgError(fireErrorCode(err), ""))
		return
	}
	frame := msgFireResult(x, y, sess.Slot, res)
	h.broadcast(frame)
	if res.GameOver {
		h.broadcast(msgGameOver(res.Winner))
		var winName string
		if w := h.slots[res.Winner]; w != nil {
			winName = w.Name
		}
		log.Printf("game finished: player #%d %q won", res.Winner, winName)
	}
}

// handleRematch implements protocol.md §5 `rematch`: only valid in FINISHED,
// and only when BOTH players have requested it does the game reset to PLACING
// (slots/names/tokens kept). Until then, each request is acknowledged with a
// `rematch` status frame so both sides see who is ready.
func (h *Hub) handleRematch(sess *Session) {
	if h.game.Phase() != game.Finished {
		sess.out.trySend(msgError("wrong_phase", ""))
		return
	}
	if h.rematch[sess.Slot] {
		return // idempotent: ignore a repeat request
	}
	h.rematch[sess.Slot] = true

	if h.rematch[0] && h.rematch[1] {
		// both agreed → fresh board, back to placement. A new full `state`
		// snapshot moves each client out of FINISHED into PLACING.
		h.game = game.NewGame()
		h.rematch = [2]bool{}
		for slot := 0; slot < 2; slot++ {
			h.sendTo(h.slots[slot], h.stateFor(h.slots[slot]))
		}
		return
	}

	// one side ready → tell both who wants the rematch.
	for slot := 0; slot < 2; slot++ {
		h.sendTo(h.slots[slot], msgRematch(h.rematch[slot], h.rematch[1-slot]))
	}
}

// handleRename changes a player's display name at any time. The new name is
// sanitized; on success the opponent is notified via an `opponent` frame so its
// banner updates live. The requester already knows its own name locally.
func (h *Hub) handleRename(sess *Session, in inbound) {
	name, ok := sanitizeName(in.Name)
	if !ok {
		sess.out.trySend(msgError("bad_name", "name is empty"))
		return
	}
	sess.Name = name
	h.notifyOpponent(sess.Slot, msgOpponent(name, sess.Online))
}

func fireErrorCode(err error) string {
	switch {
	case errors.Is(err, game.ErrNotYourTurn):
		return "not_your_turn"
	case errors.Is(err, game.ErrAlreadyFired):
		return "already_fired"
	case errors.Is(err, game.ErrOutOfBounds):
		return "out_of_bounds"
	default:
		return "wrong_phase"
	}
}

// --- disconnect -------------------------------------------------------------

func (h *Hub) handleDisconnect(c command) {
	sess := h.tokens[c.token]
	if sess == nil {
		return
	}
	// idempotent: ignore a stale disconnect for a connection already replaced.
	if sess.out != c.out {
		return
	}
	h.markOffline(sess)
}

// markOffline drops a session's socket and either frees its slot (only while
// still hub-Waiting: a single occupant that never had an opponent) or keeps it
// reserved and tells the opponent it went offline.
func (h *Hub) markOffline(sess *Session) {
	if !sess.Online {
		return // already offline → idempotent (no duplicate opponent frame)
	}
	sess.Online = false
	if sess.out != nil {
		sess.out.close()
	}
	log.Printf("player #%d disconnected: %q", sess.Slot, sess.Name)
	if h.occupied() == 1 {
		// only ever this player → free the slot for a fresh visitor.
		delete(h.tokens, sess.Token)
		h.slots[sess.Slot] = nil
		return
	}
	h.notifyOpponent(sess.Slot, msgOpponent(sess.Name, false))
}

// --- helpers ----------------------------------------------------------------

func (h *Hub) freeSlot() int {
	for i := 0; i < 2; i++ {
		if h.slots[i] == nil {
			return i
		}
	}
	return -1
}

func (h *Hub) occupied() int {
	n := 0
	for i := 0; i < 2; i++ {
		if h.slots[i] != nil {
			n++
		}
	}
	return n
}

func (h *Hub) bothOnline() bool {
	return h.slots[0] != nil && h.slots[1] != nil && h.slots[0].Online && h.slots[1].Online
}

func (h *Hub) opponentInfo(slot int) (name string, online bool) {
	if o := h.slots[1-slot]; o != nil {
		return o.Name, o.Online
	}
	return "", false
}

// phaseString resolves the wire phase, synthesizing "waiting" while only one
// slot is occupied (protocol.md §6, server.md §4).
func (h *Hub) phaseString() string {
	if h.occupied() < 2 {
		return "waiting"
	}
	return h.game.Phase().String()
}

func (h *Hub) stateFor(sess *Session) []byte {
	view := h.game.Snapshot(sess.Slot)
	oppName, oppOnline := h.opponentInfo(sess.Slot)
	return msgState(h.phaseString(), sess.Slot, view, oppName, oppOnline)
}

// notifyOpponent sends to the other slot, ignoring send failure (a raw,
// non-evicting send to avoid re-entrant eviction during offline cascades).
func (h *Hub) notifyOpponent(slot int, frame []byte) {
	if o := h.slots[1-slot]; o != nil && o.Online && o.out != nil {
		o.out.trySend(frame)
	}
}

// sendTo writes to a session and evicts it inline if its buffer is full
// (server.md §4 — never round-trip a disconnect through h.cmds).
func (h *Hub) sendTo(sess *Session, frame []byte) {
	if sess == nil || !sess.Online || sess.out == nil {
		return
	}
	if !sess.out.trySend(frame) {
		h.markOffline(sess)
	}
}

func (h *Hub) broadcast(frame []byte) {
	for i := 0; i < 2; i++ {
		h.sendTo(h.slots[i], frame)
	}
}
