# Sea Battle — Server Plan (detailed)

Go server: HTTP page + WebSocket game. Authoritative, in-memory, single game, 2 players. Wire format = [`protocol.md`](./protocol.md). Domain rules = [`architecture.md`](./architecture.md) §2.

---

## 1. Packages & responsibilities

```
internal/game/      pure domain — NO net/http, NO json, NO concurrency
  board.go          Board: 10x10 grid, place, fire-resolution, sunk/auto-reveal
  ship.go           Ship, ShipKind/size, fleet composition spec, placement geometry
  fleet.go          fleet validation: composition, bounds, overlap, no-touch
  game.go           Game aggregate: phase machine, ready flags, turn logic, win
  game_test.go      table-driven unit tests

internal/server/    orchestration — owns sockets, sessions, json
  hub.go            single owner of *game.Game + [2]*Session; command loop goroutine
  session.go        Session struct, token map, name sanitize, cookie helpers
  client.go         per-conn read/write pumps, ping/pong
  protocol.go       wire structs (in/out) + (de)serialize, maps domain↔wire
  handlers.go       HTTP: serve embedded web, GET /ws upgrade (cookie read/set)
  hub_test.go       headless integration tests (two fake clients)

internal/netutil/
  ip.go             outbound LAN IP for the startup banner

main.go             flags, embed web/, build hub, ListenAndServe, banner
web/                static client (see ui.md)
```

**Hard rule:** `internal/game` imports only stdlib `errors`/`fmt`/`math/rand`. No transport leak. This is what makes the domain headlessly testable.

---

## 2. Domain layer (`internal/game`)

### Types
```go
type CellState uint8
const ( Empty CellState = iota; Ship; Hit; Miss )

type Coord struct{ X, Y int }
type Orientation uint8
const ( Horizontal Orientation = iota; Vertical )

type Placement struct {        // input to PlaceFleet
    Size int
    Origin Coord
    Dir  Orientation
}

type shipState struct {        // internal, after placement
    cells map[Coord]bool       // remaining (un-hit) cells
    all   []Coord              // full footprint
    size  int
}

type Board struct {
    grid  [10][10]CellState
    ships []*shipState
    shots map[Coord]bool   // every cell fired AT this board (hit or miss) — Fire's already-shot guard + Snapshot history read from here
}

type Phase uint8
const ( Waiting Phase = iota; Placing; Playing; Finished )

type Game struct {
    phase  Phase
    boards [2]*Board
    ready  [2]bool
    turn   int        // 0|1, valid in Playing
    winner int        // -1 until Finished
}
```

### Fleet spec (single source)
```go
// required multiset of ship sizes
var FleetSpec = map[int]int{4:1, 3:2, 2:3, 1:4} // size→count, total 10 ships / 20 cells
```

### Validation (`fleet.go`) — order matters, all-or-nothing
1. **count** — exactly 10 placements.
2. **composition** — sizes match `FleetSpec` multiset exactly.
3. **bounds** — every cell of every ship within 0..9.
4. **overlap** — no cell shared by two ships.
5. **no-touch** — for every ship cell, none of its 8 neighbours belongs to a *different* ship.

Return a typed error so the wire layer can map to a code:
```go
type FleetError struct{ Code, Detail string } // Code ∈ composition|bounds|overlap|no_touch
func (e *FleetError) Error() string
```

### Core methods
```go
func NewGame() *Game                                  // phase=Placing once both joined; see note
func (g *Game) PlaceFleet(p int, ps []Placement) error // validates, builds Board, ready[p]=true
func (g *Game) BothReady() bool
func (g *Game) Start(rng *rand.Rand) (firstTurn int, err error) // both ready → Playing, pick turn
func (g *Game) Fire(p int, c Coord) (Result, error)
func (g *Game) Phase() Phase
func (g *Game) Snapshot(p int) StateView               // for `state` message (per-player view)
func (g *Game) Reset()                                 // Phase S5: FINISHED→Placing, clear boards + ready flags
```

`Fire` algorithm (against opponent board `b`):
1. guard: phase==Playing, p==turn, c in bounds, `!b.shots[c]` (not already shot). Add `b.shots[c]=true`.
2. hit-test: empty→`Miss`, set grid Miss; ship cell→`Hit`, remove from `shipState.cells`.
3. if that ship now empty → `Sunk`; compute `Revealed` = union of 8-neighbours of the ship minus ship cells, mark them Miss **and add to `b.shots`** (so resync reports them as fired misses).
4. win check: all opponent ships empty → `winner=p`, `phase=Finished`, `GameOver=true`.
5. turn: Miss → `turn=1-p`; Hit/Sunk → unchanged. `NextTurn=g.turn`.

```go
type Result struct {
    Outcome   Outcome // Miss|Hit|Sunk
    SunkCells []Coord // when Sunk
    Revealed  []Coord // auto-miss around sunk ship
    NextTurn  int
    GameOver  bool
    Winner    int     // valid when GameOver
}
```

**Snapshot derivation:** there is no `Sunk` cell state. `Snapshot(p)` reconstructs the wire view from `Board.shots` + `shipState` liveness:
- each `c` in opponent's `shots` → outcome `hit` if it's a ship cell, else `miss`; **upgraded to `sunk`** for cells of any ship whose `cells` set is now empty.
- `enemyShots` = p's shots against the opponent board; `ownBoard.incoming` = opponent's shots against p's board; `ownBoard.ships` = p's own placements.
This is why `shots` is an explicit field, not inferable from `grid` alone (a `Miss` grid cell can be a real miss or an auto-revealed cell — `shots` disambiguates and is the authority for `already_fired`).

**RNG injection:** `Start(rng *rand.Rand)`. Production passes `rand.New(rand.NewSource(time.Now().UnixNano()))`; tests pass a fixed seed → deterministic first turn. The domain never calls the global `rand`.

**Note on phase:** `Game` starts in `Placing` (the hub only creates/exposes it once 2 players are present; `Waiting` is a hub-level concept tracked by slot occupancy, not a domain transition). Keeps the domain free of connection concerns.

---

## 3. Session layer (`internal/server/session.go`)

```go
type Session struct {
    Token  string        // uuid v4 (cookie sb_token)
    Slot   int           // 0|1
    Name   string        // sanitized, 1..20
    Online bool
    send   chan []byte   // buffered; replaced on reconnect, nil-guarded when offline
    conn   *websocket.Conn
}
```

- `sanitizeName(string) (string, bool)` — trim, strip control chars, cap 20 runes, reject empty.
- Token issued by `handlers.go` on the page GET (`Set-Cookie`), **not** here.
- Hub keeps `tokens map[string]*Session` and `slots [2]*Session`.

---

## 4. Hub — concurrency model (`internal/server/hub.go`)

**One goroutine owns all mutable state** (`*game.Game`, slots, token map). Everything else talks to it via a command channel. No mutexes on game state.

```go
type command struct {
    sess *Session
    raw  []byte        // inbound frame (join/place/fire/...)
    kind cmdKind       // join | message | disconnect
}

type Hub struct {
    cmds   chan command
    game   *game.Game
    slots  [2]*Session
    tokens map[string]*Session
    rng    *rand.Rand
}

func (h *Hub) Run()  // for cmd := range h.cmds { h.handle(cmd) }
```

Handlers (all run on the hub goroutine, so sequential & race-free):

- **join** (`cmd.kind==join`, carries token from cookie via session):
  - token known → reconnect: rebind `sess` into existing slot, `Online=true`, send `assigned{reconnect:true}`, `opponent{...}`, full `state`; broadcast `opponent{online:true}` to other.
  - token unknown & a free slot → assign slot, store, send `assigned{reconnect:false}`, `opponent` (each other), `state`.
  - token unknown & both slots reserved → send `full`, close.

  **Phase synthesis (hub-level `Waiting`):** the domain `Game` has no `Waiting` state — the hub derives it from slot occupancy. When emitting `state.phase`:
  - exactly **one** slot occupied → emit `"waiting"` (solo player is waiting for an opponent).
  - **both** slots occupied → emit the domain phase (`"placing"` / `"playing"` / `"finished"`).
  This rule is the single place `"waiting"` is produced; `protocol.md` §9 transcript and §6 enum follow it (the lone first player sees `phase:"waiting"`).
- **message**: decode envelope, dispatch by `type`:
  - `place` → `game.PlaceFleet`; on err → `placeResult{ok:false,error}`; on ok → `placeResult{ok:true}`; then `if game.BothReady()` → `first=game.Start(h.rng)` → broadcast `gameStart{yourTurn,firstPlayer}` to each.
  - `fire` → guard both online (else `error{opponent_offline}`) → `game.Fire`; on err → `error{code}`; on ok → broadcast `fireResult` to both; if `GameOver` → broadcast `gameOver{winner}`.
  - `rematch` → valid only in FINISHED; mark `rematch[p]=true` (idempotent per slot); while only one ready → send `rematch{youReady,opponentReady}` status to both; when both ready → reset (`h.game = game.NewGame()`, clear flags; slots/tokens/names kept) → broadcast fresh `state{phase:"placing"}` to both.
  - `rename` → sanitize name (empty → `error{bad_name}`); on ok set `sess.Name` and notify the opponent with `opponent{name,online}`; no ack to the requester. Valid in any phase.
  - bad/unknown → `error{bad_json|bad_type}`.
- **disconnect**: idempotent — ignore if the slot is already offline or its conn was already replaced (guard on `sess.conn == thisConn`). Otherwise mark slot `Online=false`, `send=nil`. **Free-slot discriminator:** free the slot **only if still hub-`Waiting`** (single occupant, no opponent has *ever* bound a slot). In every other case — once a 2nd player has joined — keep the slot **reserved** (offline-grace = none, held until process exit) and broadcast `opponent{online:false}`. The discriminator is "has a 2nd player ever occupied a slot", **not** the domain phase or ready flags.

Outbound helpers: `send(slot, msg)`, `broadcast(msg)`, `sendRaw`. All marshal via `protocol.go`. Offline slot (`send==nil`) → drop silently (state will resync on reconnect).

**Write-channel backpressure:** `send` buffered (e.g. 16). If a non-blocking send (`select { case sess.send<-msg: default: }`) finds the buffer full (slow/dead client) → **evict inline on the hub goroutine**: mark `Online=false`, set `send=nil`, close the conn directly, and run the same disconnect bookkeeping synchronously. **Never enqueue a disconnect into `h.cmds` from the hub goroutine** — the hub is the sole consumer of `cmds`, so self-enqueueing during a broadcast can self-deadlock. Eviction must be a direct function call, not a channel round-trip.

---

## 5. Client pumps (`internal/server/client.go`)

Per connection, two goroutines:
- **readPump**: `conn.Read` loop → wrap bytes into `command{kind:message}` → `hub.cmds`. On read error/close → `command{kind:disconnect}` then return.
- **writePump**: range over `sess.send` → `conn.Write`. Periodic ping; on pong-timeout → close → triggers disconnect.

Read/write deadlines + ping interval are constants. Max frame size cap (e.g. 4 KiB — a fleet of 10 ships is tiny) to bound memory.

---

## 6. HTTP (`internal/server/handlers.go`)

- `GET /` and static assets → serve from `embed.FS` (`web/`). On `/`, if no `sb_token` cookie → mint uuid, `Set-Cookie: sb_token=...; Path=/; HttpOnly; SameSite=Lax`.
- `GET /ws` → read `sb_token` cookie (must exist; if missing, mint + set on the upgrade response *before* Accept), upgrade, build `Session{Token:cookie}`, start pumps, enqueue `join` once the client sends its `join` frame.
- Bind `0.0.0.0:<port>` (flag `-port`, default 8080).

---

## 7. main.go

```
parse flags → build *rand.Rand (seed=now) → hub := NewHub(rng); go hub.Run()
→ mux: "/"=static, "/ws"=wsHandler(hub)
→ print banner with netutil.LANIP() → http.ListenAndServe
```

Banner:
```
Sea Battle on :8080
  this device : http://localhost:8080
  other phone : http://192.168.1.42:8080   (same WiFi)
```

---

## 8. Build phases (server-only, maps to architecture.md §10)

| Phase | Deliverable | Done when |
|-------|-------------|-----------|
| S1 | `internal/game` complete + unit tests | `go test ./internal/game` green; full game playable in a Go test with seeded RNG |
| S2 | hub + sessions + handlers, echo dispatch | two fake clients connect → `assigned` 0/1 + names exchanged; 3rd → `full`; reload rebinds slot |
| S3 | wire `place`/`fire` to domain, broadcasts | headless integration test plays a full scripted game to `gameOver` |
| S4 | reconnect resync + disconnect freeze | drop+rejoin mid-PLAYING → `state` restores board & turn |
| S5 | ping/pong, backpressure, rematch (opt) | slow-client eviction; rematch resets boards keep slots |

---

## 9. Concurrency invariants (review checklist)

- All `*game.Game` access is on the hub goroutine only. ✔ no mutex.
- `Session.send` writes only from hub goroutine; reads only in writePump. Channel = the boundary.
- `tokens`/`slots` maps mutated only on hub goroutine.
- Pumps own the socket; they never touch game state directly.
- RNG (`h.rng`) used only on hub goroutine → no data race, deterministic under seed.

---

## 10. Failure & edge handling

| Situation | Server behavior |
|-----------|-----------------|
| 3rd connection | `full`, close socket |
| `place` before `join` | `error{not_joined}` |
| second `place` after commit | `error{already_placed}` (or `placeResult{ok:false}`) |
| `fire` out of turn | `error{not_your_turn}` |
| `fire` on shot cell | `error{already_fired}` |
| `fire` out of bounds | `error{out_of_bounds}` |
| opponent offline when you fire | allowed? **No** — game frozen while a slot offline; hub rejects `fire` with `error{opponent_offline}` until both online |
| malformed JSON | `error{bad_json}` |
| process restart | tokens void → next visitor starts fresh game |
| reconnect while FINISHED | `assigned{reconnect:true}` + `state{phase:"finished", winner, ...full boards}`; client re-shows win/lose. `Snapshot` must populate `winner` and both boards in FINISHED. |
| both slots offline at once | both stay reserved indefinitely (offline-grace = none); game frozen; first to reconnect gets `opponent{online:false}` until the other returns. |
| `fire` while opponent offline | `error{opponent_offline}`, no state change (see protocol §5). |

**Wire mapping note:** domain `winner == -1` (no winner) maps to JSON `null` in `protocol.go`; never emit `-1` on the wire. Same for `turn` outside PLAYING.
