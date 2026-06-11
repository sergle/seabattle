# Sea Battle — WebSocket Protocol (canonical)

This is the **authoritative wire definition**. `architecture.md` and `server.md` defer to this file for message shapes. Domain rules live in `architecture.md` §2.

---

## 1. Transport

- Single WebSocket per client, opened after the HTML page load.
- Endpoint: `GET /ws` (HTTP→WS upgrade). Session token travels as the `sb_token` cookie on the upgrade request.
- All messages are **JSON text frames**, UTF-8.
- One logical message per frame. No batching.
- Server is authoritative: clients send **intents**, server emits **facts**. Clients never compute hits, turns, or win state.

---

## 2. Envelope

Every frame, both directions:

```json
{ "type": "<string>", ... }
```

- `type` is required and selects the payload schema below.
- Unknown `type` → server replies `error {code:"bad_type"}` and ignores it.
- Malformed JSON → server replies `error {code:"bad_json"}` and ignores it.
- Extra/unexpected fields are ignored (forward-compatible).

---

## 3. Types & coordinate system

- Board is 10×10. `x` = column 0–9, `y` = row 0–9. Origin top-left.
- `dir`: `"h"` (horizontal, +x) or `"v"` (vertical, +y).
- A ship of `size` N at origin `(x,y)` with dir `h` occupies `(x,y)..(x+N-1,y)`; dir `v` occupies `(x,y)..(x,y+N-1)`.
- `player`: `0` or `1` (slot index).
- Ships are identified by **size + position**, not by name. Fleet composition: `1×4, 2×3, 3×2, 4×1` (10 ships, 20 cells).

Shared shapes:

```json
// ship placement (client → server in `place`)
{ "size": 4, "x": 2, "y": 0, "dir": "h" }

// cell
{ "x": 5, "y": 7 }
```

---

## 4. Lifecycle

```
connect ──> join ──┬─ assigned (new slot)        ──> PLACING
                   └─ assigned (reconnect)        ──> resync to current phase

PLACING: each client builds fleet locally (no traffic),
         presses Start ──> place {ships}
         server validates ──> placeResult {ok}
         both players placed ──> server picks random first turn
                              ──> gameStart {yourTurn} (to each, broadcast)  ──> PLAYING

PLAYING: active player ──> fire {x,y}
         server resolves ──> fireResult (broadcast to both)
         hit/sunk  ──> same player keeps turn
         miss      ──> turn flips
         last cell ──> gameOver {winner}  ──> FINISHED

any time: socket drop ──> opponent {online:false}, game frozen
          (while frozen, fire ──> error {opponent_offline})
          reconnect (token) ──> full resync (see §7) + opponent {online:true}
```

---

## 5. Client → Server

### `join`
First frame after the socket opens.
```json
{ "type": "join", "name": "Alice" }
```
- `name`: 1–20 chars after trim; server trims, caps, strips control chars. Empty after trim → `error {code:"bad_name"}`.
- Token comes from the `sb_token` cookie, **not** this payload.
- Server response: `assigned` (+ `opponent`, + `state`). If both slots reserved and cookie matches neither → `full`.

### `place`
Sent when the player presses **Start** (this single message *is* the ready/commit — there is no separate `ready` message).
```json
{ "type": "place", "ships": [
  { "size": 4, "x": 0, "y": 0, "dir": "h" },
  { "size": 3, "x": 0, "y": 2, "dir": "h" },
  { "size": 3, "x": 0, "y": 4, "dir": "h" },
  { "size": 2, "x": 0, "y": 6, "dir": "h" },
  { "size": 2, "x": 0, "y": 8, "dir": "h" },
  { "size": 2, "x": 5, "y": 0, "dir": "h" },
  { "size": 1, "x": 5, "y": 2 },
  { "size": 1, "x": 7, "y": 2 },
  { "size": 1, "x": 5, "y": 4 },
  { "size": 1, "x": 7, "y": 4 }
] ]}
```
- Valid only in PLACING and only if this player hasn't already committed.
- Server validates the **whole fleet**: composition counts, bounds, overlap, no-touch (8-neighbour). All-or-nothing.
- Response: `placeResult {ok:true}` or `placeResult {ok:false, error}`. On failure the client may re-edit and resend.
- `dir` defaults to `"h"` for size 1 (ignored).

### `fire`
```json
{ "type": "fire", "x": 4, "y": 6 }
```
- Valid only in PLAYING, only on this player's turn, only at a not-yet-fired enemy cell, **and only while both players are online**.
- If the opponent is offline the game is frozen → `error {code:"opponent_offline"}`, no state change. The active player must wait for `opponent {online:true}` before firing.
- Violations → `error` (see §8), no state change.
- Response: `fireResult` broadcast to both players.

### `rematch` *(Phase 5, optional)*
```json
{ "type": "rematch" }
```
- Valid only in FINISHED. When both send it: boards reset, names + slots + tokens kept, back to PLACING.

### `rename`
```json
{ "type": "rename", "name": "New Name" }
```
- Valid in any phase. Sanitized like `join`; empty → `error {code:"bad_name"}`.
- On success the opponent receives an `opponent` frame with the new name. The requester gets no ack (it already set its own name locally).

---

## 6. Server → Client

### `assigned`
```json
{ "type": "assigned", "player": 0, "name": "Alice", "reconnect": false }
```
- Sent right after `join`. `player` = your slot. `name` = your (sanitized) name echoed back.
- `reconnect:true` when the token rebound to an existing slot.

### `full`
```json
{ "type": "full" }
```
- Both slots reserved and you hold no matching token. Server closes the socket after sending.

### `opponent`
```json
{ "type": "opponent", "name": "Bob", "online": true }
```
- Opponent joined / reconnected / dropped. `name` omitted while no opponent yet (only `online:false`).
- Replaces the old `opponentLeft`.

### `state`
Full per-player snapshot. `state` is a **session/membership snapshot**, not a play-transition message. It is sent exactly in these cases:
- **(a)** once right after that client's own `join` (initial snapshot).
- **(b)** on reconnect, as the resync payload (§7).
- **(c)** to both clients after a `rematch` reset (boards cleared → both need a fresh snapshot to start the new round).

It is **not** emitted on in-play phase transitions. Those are announced by dedicated messages: PLACING→PLAYING by `gameStart`, PLAYING→FINISHED by `gameOver` (after the final `fireResult`). A live client builds its board incrementally from `placeResult`/`gameStart`/`fireResult`/`gameOver`.

**WAITING→PLACING (the waiting player):** when the 2nd player joins, the already-present player is **not** re-sent `state` — its own board is unchanged. It receives `opponent {name, online:true}`, which is its signal to leave the "waiting" screen and begin placing. (The joining player gets its own initial `state` per (a), which already reads `phase:"placing"` since both slots are now occupied.)
```json
{
  "type": "state",
  "phase": "placing",            // waiting|placing|playing|finished
  "you": 0,
  "yourTurn": false,             // meaningful in playing
  "youReady": true,              // you have committed a fleet
  "opponentReady": false,
  "ownBoard": {                  // your ships + shots taken against you
    "ships": [ {"size":4,"x":0,"y":0,"dir":"h"}, ... ],
    "incoming": [ {"x":1,"y":1,"outcome":"miss"}, ... ]
  },
  "enemyShots": [                // your shots at the enemy (no enemy ships revealed)
    {"x":4,"y":6,"outcome":"hit"},
    {"x":4,"y":7,"outcome":"sunk"}
  ],
  "opponentName": "Bob",
  "opponentOnline": true,
  "winner": null                 // 0|1 when finished, else null
}
```
- **Privacy invariant:** `ownBoard.ships` is only ever your own fleet. The server never sends enemy ship layout — only your shot outcomes in `enemyShots`.
- During PLACING `ownBoard.ships` reflects what you committed (empty until you `place`).

### `placeResult`
```json
{ "type": "placeResult", "ok": false, "error": "no_touch: ships at (0,0) and (1,1) touch" }
```
- `ok:true` → fleet accepted, you are now ready. `error` present only when `ok:false`.

### `gameStart`
```json
{ "type": "gameStart", "yourTurn": true, "firstPlayer": 1 }
```
- Emitted to **both** players once both fleets are committed. Phase becomes PLAYING.
- `firstPlayer` chosen by a **server-side seedable RNG** (so headless tests can assert it). `yourTurn` is the per-recipient view.

### `fireResult`
Broadcast to both players after each shot.
```json
{
  "type": "fireResult",
  "x": 4, "y": 7,
  "by": 0,                 // who fired
  "outcome": "sunk",       // miss|hit|sunk
  "sunkCells": [ {"x":4,"y":6},{"x":4,"y":7} ],   // present only when outcome=sunk
  "revealed": [ {"x":3,"y":5},{"x":4,"y":5}, ... ], // auto-miss cells around the sunk ship
  "nextTurn": 0            // whose turn now (= by on hit/sunk, flipped on miss)
}
```
- `sunkCells` lets each client mark the full dead ship; `revealed` are the surrounding cells the no-touch rule guarantees empty (auto-marked miss).
- Clients update their boards purely from this message.

### `gameOver`
```json
{ "type": "gameOver", "winner": 0 }
```
- Sent (after the final `fireResult`) when a fleet is fully destroyed. Phase → FINISHED.
- Each client compares `winner` to its own `player` for win/lose display.

### `rematch`
```json
{ "type": "rematch", "youReady": true, "opponentReady": false }
```
- Sent in FINISHED while one player has requested a rematch but both have not yet agreed.
- `youReady`/`opponentReady` reflect each side's request. Once **both** are ready the server resets the game instead and emits a fresh `state` (phase PLACING) to both — no further `rematch` frame.

### `error`
```json
{ "type": "error", "code": "not_your_turn", "detail": "optional human text" }
```
- Non-fatal protocol/validation error. Socket stays open. No state change. Codes in §8.

---

## 7. Reconnect resync

1. Browser reloads or socket drops; `sb_token` cookie persists.
2. New socket opens, client sends `join {name}` (name may be from `localStorage`; server keeps the stored name on reconnect, the payload name is ignored for an existing slot).
3. Server matches cookie token → existing slot, marks it online, rebinds the socket.
4. Server emits, in order: `assigned {reconnect:true}` → `opponent {name,online}` → **`state`** (full snapshot per §6: phase, your committed board, all your shot outcomes, whose turn).
5. Server broadcasts `opponent {online:true}` to the other player.

The client rebuilds its entire UI from the single `state` snapshot — it holds no authoritative state across reloads.

---

## 8. Error codes

| code | meaning | when |
|------|---------|------|
| `bad_json` | frame not valid JSON | any |
| `bad_type` | unknown `type` | any |
| `bad_name` | empty/invalid name | `join` |
| `not_joined` | sent `place`/`fire` before `join` | any |
| `wrong_phase` | message not allowed in current phase | `place` in PLAYING, `fire` in PLACING, etc. |
| `already_placed` | second `place` after commit | `place` |
| `bad_fleet` | composition/bounds/overlap/no-touch failure | `place` (mirrors `placeResult.error`) |
| `not_your_turn` | fired out of turn | `fire` |
| `already_fired` | cell already shot | `fire` |
| `out_of_bounds` | coord outside 0–9 | `fire`/`place` |
| `opponent_offline` | opponent disconnected — game frozen | `fire` while a slot is offline |

`place` failures are reported via `placeResult {ok:false, error}`; the `error` string is prefixed with the matching code (e.g. `no_touch: ...`, `composition: ...`). `fire` failures use the `error` frame.

---

## 9. Example transcript (happy path)

```
C0→S  join {name:"Alice"}
S→C0  assigned {player:0, name:"Alice", reconnect:false}
S→C0  opponent {online:false}
S→C0  state {phase:"waiting", you:0, ...}      (solo → hub-synthesized "waiting")

C1→S  join {name:"Bob"}
S→C1  assigned {player:1, name:"Bob"}
S→C1  opponent {name:"Alice", online:true}
S→C1  state {phase:"placing", you:1, ...}      (C1's initial snapshot, both present)
S→C0  opponent {name:"Bob", online:true}       (C0's WAITING→PLACING signal — no new `state`)

C0→S  place {ships:[...]}            (Alice presses Start)
S→C0  placeResult {ok:true}
C1→S  place {ships:[...]}            (Bob presses Start)
S→C1  placeResult {ok:true}

S→C0  gameStart {yourTurn:false, firstPlayer:1}
S→C1  gameStart {yourTurn:true,  firstPlayer:1}

C1→S  fire {x:4,y:6}
S→C0  fireResult {x:4,y:6, by:1, outcome:"hit", nextTurn:1}
S→C1  fireResult {x:4,y:6, by:1, outcome:"hit", nextTurn:1}   (Bob keeps turn)
C1→S  fire {x:0,y:0}
S→both fireResult {by:1, outcome:"miss", nextTurn:0}          (turn flips to Alice)
...
S→both fireResult {by:0, outcome:"sunk", sunkCells:[...], revealed:[...], nextTurn:0}
...
S→both fireResult {... final hit ...}
S→both gameOver {winner:0}
```
