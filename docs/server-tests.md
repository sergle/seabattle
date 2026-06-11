# Sea Battle — Server Test Cases (headless, no UI)

Validates the server with **zero browser involvement**. Two tiers:

- **Tier A — Domain unit tests** (`internal/game`): pure functions, table-driven, seeded RNG.
- **Tier B — Protocol integration tests** (`internal/server`): two in-process fake WS clients driving scripted message sequences against a real hub; assert emitted frames.

Specs map to future `*_test.go`. Each case: **setup → action → expected**. Wire shapes per [`protocol.md`](./protocol.md); behavior per [`server.md`](./server.md).

---

## Test harness (Tier B)

```go
// fakeClient: in-process stand-in for a browser socket
type fakeClient struct {
    token string          // its sb_token cookie
    in    chan []byte     // frames server→client (drain & assert)
}
func (h *Hub) connect(token string) *fakeClient   // simulates GET /ws with cookie
func (c *fakeClient) send(v any)                   // marshal + enqueue as inbound command
func (c *fakeClient) expect(t *testing.T, type_ string) map[string]any // next frame, assert type
func (c *fakeClient) expectNone(t *testing.T)      // no frame within small drain window
```

- Hub built with a **fixed-seed** RNG: `NewHub(rand.New(rand.NewSource(1)))` → first-turn deterministic.
- No real network: `connect` injects a session with the given cookie token; `send` feeds `h.cmds`; outbound goes to `c.in`.
- "Reconnect" = `connect(sameToken)` again (old fake client's socket considered dropped first via a `disconnect` command).

**Determinism note:** with seed 1, assert the *concrete* `firstPlayer` value the seed yields (compute once, pin it). Also include a property check: over seeds 0..99, both 0 and 1 occur (RNG not stuck).

---

## Tier A — Domain unit tests (`internal/game`)

### A1. Fleet composition
| # | placements | expect |
|---|-----------|--------|
| A1.1 | exactly 1×4,2×3,3×2,4×1, all legal | `PlaceFleet` ok, `ready[p]=true` |
| A1.2 | 9 ships (missing one 1-cell) | `FleetError{composition}` |
| A1.3 | 11 ships (extra 1-cell) | `FleetError{composition}` |
| A1.4 | right count, wrong sizes (2×4 instead of 1×4+one 3) | `FleetError{composition}` |

### A2. Bounds
| # | placement | expect |
|---|-----------|--------|
| A2.1 | size4 h at (7,0) → cells 7..10 | `FleetError{bounds}` |
| A2.2 | size4 v at (0,7) → rows 7..10 | `FleetError{bounds}` |
| A2.3 | size4 h at (6,0) → cells 6..9 | ok (boundary, fits) |

### A3. Overlap
| # | setup | expect |
|---|-------|--------|
| A3.1 | two ships share a cell | `FleetError{overlap}` |
| A3.2 | size1 dropped on existing ship cell | `FleetError{overlap}` |

### A4. No-touch (8-neighbour)
| # | setup | expect |
|---|-------|--------|
| A4.1 | ship at (0,0) h size2; ship at (0,1) → edge-touch below | `FleetError{no_touch}` |
| A4.2 | ship at (0,0); ship at (1,1) → diagonal touch | `FleetError{no_touch}` |
| A4.3 | ship at (0,0) h size2; ship at (0,2) → one-gap row | ok |
| A4.4 | ship at (0,0); ship at (2,0) → one-gap col | ok |

### A5. Fire resolution + turn
Setup: known fleets on both boards, `Start(seed)` → known `turn`.
| # | action | expect |
|---|--------|--------|
| A5.1 | fire empty cell | `Outcome=Miss`, `NextTurn=1-p` (flips) |
| A5.2 | fire ship cell (ship size≥2, not last) | `Outcome=Hit`, `NextTurn=p` (keeps) |
| A5.3 | fire last cell of a multi-cell ship | `Outcome=Sunk`, `SunkCells`=full ship, `Revealed`=8-neighbour ring (no dupes, no ship cells, in-bounds), `NextTurn=p` |
| A5.4 | sink a size-1 ship | `Outcome=Sunk`, `SunkCells`=1 cell, `Revealed`=up to 8 surrounding |
| A5.5 | fire same cell twice | 2nd → error `already_fired`, no state change |
| A5.6 | fire out of bounds | error `out_of_bounds` |
| A5.7 | fire when phase≠Playing | error `wrong_phase` |
| A5.8 | wrong player fires | error `not_your_turn` |

### A6. Win condition
| # | action | expect |
|---|--------|--------|
| A6.1 | sink all 10 enemy ships (20 cells) | last `Fire` → `GameOver=true`, `Winner=p`, `phase=Finished` |
| A6.2 | after GameOver, any `Fire` | error `wrong_phase` |
| A6.3 | 19 cells hit, 1 ship cell remains | `GameOver=false` |

### A7. Start / RNG
| # | setup | expect |
|---|-------|--------|
| A7.1 | both ready, `Start(seed=1)` | returns the pinned `firstPlayer`; phase→Playing; `turn=firstPlayer` |
| A7.2 | only one ready, `Start` | error (not both ready); phase stays Placing |
| A7.3 | seeds 0..99 | both 0 and 1 appear as firstPlayer |

### A8. Auto-reveal correctness (no-touch interaction)
| # | setup | expect |
|---|-------|--------|
| A8.1 | sunk ship in board corner | `Revealed` clipped to in-bounds cells only |
| A8.2 | revealed cells added to `shots` | re-firing a revealed cell → `already_fired` |

---

## Tier B — Protocol integration tests (`internal/server`)

### B1. Join & assignment
| # | script | expect |
|---|--------|--------|
| B1.1 | C0 connect(tokenA) + `join{name:"Alice"}` | C0 gets `assigned{player:0,reconnect:false}`, `opponent{online:false}`, `state{phase:"waiting"}` |
| B1.2 | then C1 connect(tokenB) + `join{name:"Bob"}` | C1 `assigned{player:1}` + `opponent{name:"Alice",online:true}` + own initial `state{phase:"placing"}`; **C0 gets `opponent{name:"Bob",online:true}` only — NO new `state`** (its WAITING→PLACING cue is the opponent message; board unchanged) |
| B1.3 | C2 connect(tokenC) + `join` while 2 slots reserved | C2 `full`, socket closed; C0/C1 unaffected |
| B1.4 | name `"  "` (whitespace) | `error{bad_name}`, no slot consumed |
| B1.5 | name 30 chars | accepted, echoed `assigned.name` capped to 20 |

### B2. Solo-player phase synthesis
| # | script | expect |
|---|--------|--------|
| B2.1 | only C0 joined | C0's `state.phase == "waiting"` (hub-synthesized, not domain) |
| B2.2 | C1 joins then disconnects | C0 sees `opponent{online:false}`; C0's later resync `state.phase` reflects real domain phase (`placing`), **not** `waiting` (2nd player has bound a slot) |
| B2.3 | C1 joins (C0 already present) | C0 receives `opponent{online:true}` and **no** `state` frame (assert `expectNone` after the opponent frame) — confirms WAITING→PLACING is signaled by `opponent`, not `state` |

### B3. Placement over the wire
| # | script | expect |
|---|--------|--------|
| B3.1 | C0 `place{valid fleet}` | C0 `placeResult{ok:true}`; no `gameStart` yet (C1 not ready) |
| B3.2 | C0 `place{9 ships}` | C0 `placeResult{ok:false, error:"composition: ..."}`; may resend |
| B3.3 | C0 `place` valid, then `place` again | 2nd → `error{already_placed}` (or `placeResult{ok:false}`) |
| B3.4 | C0 valid place, C1 valid place | both get `gameStart{firstPlayer:<pinned>, yourTurn:<per-recipient>}`; phase→Playing |
| B3.5 | `place` before `join` | `error{not_joined}` |
| B3.6 | `place` during PLAYING | `error{wrong_phase}` |

### B4. Full game (happy path, scripted)
Seed-pinned `firstPlayer`. Script both fleets at known coords so every shot outcome is predetermined.
| step | action | expect (broadcast to both) |
|------|--------|----------------------------|
| 1 | both `place` valid | `gameStart` |
| 2 | first player fires a known ship cell | `fireResult{outcome:"hit", nextTurn:first}` |
| 3 | same player fires empty cell | `fireResult{outcome:"miss", nextTurn:other}` |
| 4 | other player fires last cell of a ship | `fireResult{outcome:"sunk", sunkCells, revealed, nextTurn:other}` |
| 5 | … continue until one fleet gone | final `fireResult` then `gameOver{winner}` |
| post | loser/winner `player` vs `winner` | each client can derive win/lose |

### B5. Out-of-turn / invalid fire
| # | script | expect |
|---|--------|--------|
| B5.1 | non-turn player fires | `error{not_your_turn}` to sender only; no broadcast |
| B5.2 | turn player fires already-shot cell | `error{already_fired}`; no broadcast |
| B5.3 | fire `{x:10}` | `error{out_of_bounds}` |
| B5.4 | unknown `type:"foo"` | `error{bad_type}` |
| B5.5 | non-JSON frame | `error{bad_json}` |

### B6. Disconnect / freeze / reconnect  ← key reconnect coverage
| # | script | expect |
|---|--------|--------|
| B6.1 | mid-PLAYING, C1 socket drops | C0 `opponent{online:false}`; game frozen |
| B6.2 | while frozen, C0 (its turn) fires | `error{opponent_offline}`; no state change |
| B6.3 | C1 reconnect(tokenB) | C1 `assigned{reconnect:true}` → `opponent{...}` → **`state`** full snapshot (own ships, all incoming shots w/ outcomes incl. `sunk`, `enemyShots`, `yourTurn`, phase=playing); C0 `opponent{online:true}` |
| B6.4 | after B6.3, resume fire | proceeds normally |
| B6.5 | reconnect resync sunk fidelity | a ship sunk before disconnect appears in resync `state` as `sunk` outcomes on the right cells + revealed misses (validates I3 shot-tracking) |
| B6.6 | disconnect in hub-`Waiting` (only C0, never had opponent) | slot freed; a fresh `connect(tokenC)+join` gets `player:0` (slot reusable) |
| B6.7 | disconnect after opponent joined, in PLACING | slot **reserved**; fresh token → `full` |
| B6.8 | reconnect while FINISHED | `state{phase:"finished", winner, both boards}` |
| B6.9 | both offline, then C0 reconnects | C0 `opponent{online:false}` until C1 returns |
| B6.10 | double disconnect (evict + readPump both fire) | idempotent: second disconnect a no-op, no panic, no duplicate `opponent` frame |

### B7. Concurrency / race
| # | method | expect |
|---|--------|--------|
| B7.1 | run B4 full game under `go test -race` | no race reports |
| B7.2 | flood: one client sends 100 frames rapidly while game runs | hub processes sequentially, no race, no dropped state corruption |
| B7.3 | slow client (never drains `in`) during broadcast | hub evicts inline (C1 fix), does not block/deadlock; other client still served |

### B8. Privacy invariant
| # | check | expect |
|---|-------|--------|
| B8.1 | inspect every frame C0 receives | never contains C1's un-hit ship positions; `enemyShots` only carries C0's own shot outcomes |
| B8.2 | reconnect `state` for C0 | `ownBoard.ships` = C0 fleet only; no enemy `ships` field present |

### B9. Rematch (both-agree)
| # | check | expect |
|---|-------|--------|
| B9.1 | in FINISHED, one player sends `rematch` | both get `rematch{youReady,opponentReady}`; phase stays `finished`; requester `youReady:true`, opponent `opponentReady:true` |
| B9.2 | same player sends `rematch` again | idempotent: no new frame, no reset |
| B9.3 | second player sends `rematch` | game reset; both get `state{phase:"placing", youReady:false, winner:null}`; hub `rematch` flags cleared |
| B9.4 | `rematch` outside FINISHED (e.g. PLACING) | `error{wrong_phase}`; no reset |

### B10. Rename
| # | check | expect |
|---|-------|--------|
| B10.1 | C0 sends `rename{name}` | `sess.Name` updated; opponent gets `opponent{name,online}`; requester gets no ack |
| B10.2 | C0 sends `rename` with blank name | `error{bad_name}`; name unchanged |

---

## Coverage targets

- `internal/game`: ≥ 95% line coverage; every `FleetError` code and every `Outcome` exercised.
- `internal/server`: all message types in protocol §5/§6 exercised in at least one B-case; every `error` code in protocol §8 produced by a test.
- CI: `go test -race ./...` green is the gate.

## Traceability

| Risk (from review) | Covered by |
|--------------------|-----------|
| C1 hub deadlock on slow client | B7.3 |
| C2 solo phase = waiting | B2.1, B2.2 |
| C3 free-slot discriminator | B6.6, B6.7 |
| C4 opponent_offline | B6.2 |
| I1 no `state` on live transition | B3.4 (only `gameStart`), B4 |
| I1/F4 WAITING→PLACING via `opponent`, not `state` | B2.3 |
| F3 rematch reset re-sends `state` (clause c) | B9.3 |
| Rename notifies opponent | B10.1 |
| I3 sunk/shot resync | A8.2, B6.5 |
| RNG determinism/testability | A7, harness seed |
