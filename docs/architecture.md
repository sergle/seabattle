# Sea Battle — Architecture & Build Plan

Local LAN battleship game. Two players on mobile phones connect to a Go server over IP. Authoritative server, browser client, in-memory state.

---

## 1. Goals & Constraints

| Item | Decision |
|------|----------|
| Players | Exactly 2 |
| Client | Web page served by Go (no app install) |
| Network | Same LAN/WiFi, direct IP access |
| Transport | WebSocket (real-time) + HTTP (page load) |
| State | In-memory, single game per server process |
| Persistence | None (process restart = new game) |
| Identity | Player name (entered on first load) + session token (HTTP cookie) |
| Reconnect | Yes — cookie token rebinds player to their slot + preserved board |
| Auth | None beyond session token (trusted LAN) |
| Server language | Go (stdlib + one WS lib) |

**Non-goals (v1):** matchmaking, multiple concurrent games, accounts, spectators, mobile native app, internet play / NAT traversal.

---

## 2. Game Rules (canonical)

- Board: **10×10** grid, coords `x` 0–9, `y` 0–9.
- Fleet (classic): 1×(4-cell), 2×(3-cell), 3×(2-cell), 4×(1-cell). **10 ships, 20 cells.**
- Ships: horizontal or vertical, no overlap, no out-of-bounds.
- **No-touch rule (mandatory):** ships cannot touch each other — not edge-to-edge, not diagonally. Every ship surrounded by ≥1 empty cell (or board border).
- Turn-based. Active player fires one shot at enemy grid.
- Shot result: `miss`, `hit`, or `sunk` (hit that destroys a ship).
- **Extra turn on hit:** hit (or sunk) → same player fires again. Miss → turn passes to opponent.
- Win: all 20 enemy cells hit.

---

## 3. State Machine

```
WAITING  → both players connected
PLACING  → both submit valid fleets
PLAYING  → turn loop until a fleet destroyed
FINISHED → winner declared
```

Transitions are server-driven and announced by **dedicated messages** — PLACING→PLAYING by `gameStart`, PLAYING→FINISHED by `gameOver`. `state` is a session snapshot sent only on initial join, reconnect, and rematch reset (see protocol.md §6); it is **not** broadcast on in-play transitions. The waiting player's WAITING→PLACING cue is the `opponent{online:true}` message.

| From | Event | To |
|------|-------|-----|
| WAITING | 2nd player joins | PLACING |
| PLACING | a player presses Start → `place` valid | that player marked ready (stays PLACING) |
| PLACING | **both** players ready | PLAYING — server picks first turn via seedable RNG, emits `gameStart` |
| PLAYING | a fleet fully sunk | FINISHED |
| WAITING | a player disconnects | WAITING (slot freed) |
| PLACING/PLAYING | a player disconnects | **stays in same phase, slot reserved** (game frozen, awaits reconnect) |
| any | disconnected player reconnects (token match) | resumes prior phase + board |

**Connection vs slot:** a slot can be *occupied-disconnected* (player has token, socket dead) vs *occupied-live*. Game logic only runs when both slots live; otherwise frozen.

---

## 3a. Identity & Session

**First visit:**
1. Page GET: if no `sb_token` cookie, server mints UUID + sets `sb_token=<uuid>; Path=/; HttpOnly; SameSite=Lax` on the **HTML response** (token exists before slot/name).
2. No cached name → UI shows **name prompt** (modal). Name cached in `localStorage` (prefill later).
3. Client opens WS (cookie auto-sent on upgrade), sends `join {name}`.
4. Server: binds token → free slot 0/1 + name, stores `Session{slot, name, token, online}`.
5. Server sends opponent's name to this client and this client's name to opponent (`opponent {name, online}`).

**Reconnect (reload / network blip / phone sleep):**
1. WS upgrade request carries `sb_token` cookie automatically.
2. Server matches token → existing slot. Re-binds new socket to that slot, marks live.
3. Server resends full personal view: `state` + own board + enemy hit/miss history + opponent name.
4. Broadcasts `opponent {online:true}` to the other player.

**Slot ownership rules:**
- Fresh visitor (no/unknown token) when **both slots exist** → `full`, even if one is disconnected (slot is *reserved* for its token holder).
- Match requires exact token. No token guessing (UUID v4).
- Token lifetime = server process lifetime (in-memory map). Process restart → all tokens void → new game.

**Name handling:** server-side trim, length cap (e.g. 1–20 chars), strip control chars. Names are display-only, not identity — token is identity.

---

## 4. Project Layout

```
seabattle/
  go.mod
  main.go              # bootstrap: print LAN IP, start HTTP server
  internal/
    game/
      board.go         # 10x10 grid, cell state, shot resolution
      ship.go          # ship types, sizes, placement validation
      game.go          # Game aggregate: state machine, turn logic, win check
      game_test.go
    server/
      hub.go           # owns the single Game + 2 player slots, command loop
      session.go       # token↔slot map, player name, online flag, cookie issue
      client.go        # per-connection WS read/write pumps
      protocol.go      # message structs (in/out), encode/decode
      handlers.go      # HTTP routes: serve web, WS upgrade (reads sb_token cookie)
    netutil/
      ip.go            # detect outbound LAN IP for startup banner
  web/
    index.html
    app.js
    style.css
```

**Layering:** `game` is pure domain (no network imports, fully unit-testable). `server` orchestrates connections and calls into `game`. `web` is dumb client — renders state, sends intents.

---

## 5. Domain Model (`internal/game`)

> **Illustrative only.** The authoritative type/method definitions for implementation live in [`server.md`](./server.md) §2 (e.g. unexported `shipState`/`Board.grid`, the `shots` field, `Result`/`Snapshot` shapes). Where this section and server.md §2 differ, **server.md §2 wins**. The sketch below conveys intent.

```go
type CellState int // Empty, Ship, Hit, Miss

type Coord struct{ X, Y int }

type Orientation int // Horizontal, Vertical

type Ship struct {
    Kind   ShipKind
    Size   int
    Origin Coord
    Dir    Orientation
    Hits   int        // cells hit; Sunk when Hits == Size
}

type Board struct {
    Ships []Ship
    Grid  [10][10]CellState // server truth
}

type Phase int // Waiting, Placing, Playing, Finished

type Game struct {
    Phase   Phase
    Boards  [2]*Board   // index by player slot 0/1
    Placed  [2]bool
    Turn    int         // 0 or 1 — whose turn
    Winner  int         // -1 none
}
```

Core methods (all validation server-side):
- `PlaceFleet(player int, ships []Ship) error` — bounds, overlap, fleet composition, no-touch. Marks player ready.
- `BothReady() bool` + `Start(rng *rand.Rand) int` — when both ready, RNG picks first player (slot), phase → PLAYING. RNG injected for deterministic tests.
- `Fire(player int, c Coord) (Result, error)` — checks turn + phase, resolves; **miss → flip turn, hit/sunk → keep turn**; auto-reveals around sunk ship; detects win.
- `Result` = `{Outcome: miss|hit|sunk, SunkCells []Coord, Revealed []Coord, NextTurn int, GameOver bool, Winner int}`.

**Invariants enforced:** correct fleet composition (1×4, 2×3, 3×2, 4×1), no overlap, no out-of-bounds, **no-touch** (8-neighbour gap between ships), fire only on own turn in PLAYING, no firing same cell twice.

**No-touch validation:** for each ship cell, check all 8 neighbours — none may belong to a *different* ship. Reject fleet on violation.

**Auto-reveal on sunk:** when a ship sinks, server auto-marks its surrounding cells as `miss` (no-touch guarantees they're empty) and broadcasts them — standard UX so players don't waste shots around a dead ship.

---

## 6. Connection Layer (`internal/server`)

**Hub** — single instance, owns the `Game`, 2 slots, and the session map.
- Serializes all game mutations through one goroutine (channel of commands) → no mutex races on game state.
- On `join`: match `sb_token` cookie → reconnect to existing slot, else assign free slot 0/1 + new token. Both slots reserved → `full`.
- On disconnect **before** game start (WAITING) → free slot. During PLACING/PLAYING → keep slot reserved, mark offline, broadcast `opponent {online:false}`, freeze game.
- On reconnect → rebind socket, mark online, resend personal view, broadcast `opponent {online:true}`.

```go
type Session struct {
    Token  string // UUID v4, cookie value
    Slot   int    // 0 or 1
    Name   string
    Online bool
    send   chan []byte // nil when offline
}
```

**Client** — per WS connection.
- Read pump: decode inbound JSON → send command to hub.
- Write pump: receive broadcasts → encode JSON → socket.
- Ping/pong keepalive to detect dead phones.

Concurrency model:
```
WS conn ──read──> hub.commands (chan) ──> single game goroutine
WS conn <─write── client.send (chan)  <── hub broadcast
```

---

## 7. WebSocket Protocol

**Canonical definition lives in [`protocol.md`](./protocol.md).** Do not duplicate message tables here — that file is the single source of truth for envelope, message types, payloads, error codes, reconnect resync, and example transcripts.

Summary of intent:
- Client sends intents: `join`, `place` (Start/commit), `fire`, `rematch`, `rename`.
- Server emits facts: `assigned`, `full`, `opponent`, `state`, `placeResult`, `gameStart`, `fireResult`, `gameOver`, `rematch`, `error`.
- **View privacy:** server sends each client only its own ship layout; enemy board is shot outcomes only — never un-hit enemy ships.
- `place` is the per-player Start/commit (no separate `ready`). Both committed → server picks first turn via seedable RNG → `gameStart`.

---

## 8. Client (`web/`)

**Canonical UI spec lives in [`ui.md`](./ui.md).** Summary:
- **Bilingual EN/UK** (`i18n.js`); flag selector top-right, default from browser, stored in `localStorage.sb_lang`.
- **Name prompt** on first load → `join {name}`; reconnect skips it. Banner shows **「You」 vs 「opponent」** + online dot; tapping your own name → `rename`.
- Two 10×10 grids: **own fleet** + **enemy waters**.
- **Placing:** manual only — ship palette, click-to-place, drag to move, double-tap to rotate, rules enforced client-side (mirror of server). The next unplaced ship is **auto-selected** (place the whole fleet by tapping the grid only). **Start** enables once all 10 placed → sends `place`.
- **Playing:** "your turn" indicator; tap enemy cell on your turn → `fire`. **Haptics** on shot results/game over (Vibration API, Android only).
- **Finished:** win/lose overlay + **New Game** (both-agree `rematch`) → back to placement.
- Connection lost → red status. Renders purely from server messages. Plain vanilla JS, CSS grid, `go:embed`. No framework, no build step.

---

## 9. Networking / Run

- `main.go` detects LAN IP (`netutil.ip`), prints banner:
  ```
  Sea Battle running:
    This phone/PC : http://localhost:8080
    Other phone   : http://192.168.1.42:8080
  ```
- Bind `0.0.0.0:8080`. Port configurable via `-port` flag / `PORT` env.
- Both phones on same WiFi. Host firewall must allow inbound TCP 8080.

---

## 10. Build Plan (phased)

### Phase 1 — Domain core (no network)
- `board.go`, `ship.go`, `game.go` with full state machine.
- Unit tests: placement validation, fire resolution, sunk detection, turn flip, win condition.
- **DoD:** `go test ./internal/game` green; full game playable in a test.

### Phase 2 — Server skeleton + sessions
- HTTP server, serve static `web/`, WS upgrade.
- Hub with session map: `join` → token cookie, 2-slot assignment + 3rd-rejection.
- Reconnect: cookie token → rebind to slot, mark online/offline on disconnect.
- Name exchange via `opponent` messages.
- **DoD:** two tabs connect, get `assigned` 0/1 + see each other's names; 3rd gets `full`; reload rebinds same slot (no new slot consumed).

### Phase 3 — Wire protocol to domain
- `place` → `Game.PlaceFleet` → `placeResult` + phase advance.
- `fire` → `Game.Fire` → broadcast `fireResult` → turn flip → `gameOver`.
- Per-player view privacy.
- **DoD:** full game completable via raw WS messages.

### Phase 4 — Client UI
- Grids, ship placement (manual + random), turn indicator, fire interaction, win screen.
- **DoD:** two phones play a full game start→finish.

### Phase 5 — Polish
- ✅ Rematch flow (both-agree; reset boards, keep slots + names).
- ✅ Rename (live, opponent notified); auto-select next ship; haptics; offline red status; Ukrainian UI.
- Keepalive ping/pong + offline-grace timer.
- Full state resync hardening on reconnect mid-PLAYING.

---

## 11. Testing Strategy

- **Domain:** table-driven unit tests for every rule + invariant. Target high coverage on `game`.
- **Server:** integration test with two in-process WS clients playing a scripted game.
- **Manual:** two real phones on LAN for final acceptance.

---

## 12. Decisions (all locked)

**Decided:**
- Offline-grace timeout = **none** — slot reserved until process exit.
- Ship placement = **manual only**, no random/auto helper.
- Extra turn on hit = **yes**. No-touch fleet rule = **mandatory**.

No open decisions remain.

---

## 13. Dependencies

- Go 1.22+ (stdlib `net/http`, `embed` for bundling `web/`).
- WebSocket: `nhooyr.io/websocket` (or `github.com/gorilla/websocket`). Single dependency.
- No frontend deps.
