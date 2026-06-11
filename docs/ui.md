# Sea Battle — UI Plan (web client)

Mobile-first web client served by the Go server. Plain HTML/CSS/JS, no framework, no build step, bundled via `go:embed`. Wire format = [`protocol.md`](./protocol.md). Server is authoritative; the UI mirrors rules only for instant feedback.

UI is **bilingual (English / Ukrainian)** via a tiny i18n layer (`i18n.js`). A language **dropdown** (`<select>`, flag-only options GB / UA) sits fixed in the **top-right corner**. Default language = browser preference (`navigator.languages`), falling back to English; the choice is persisted in `localStorage.sb_lang` (alongside `sb_name`). Switching re-renders all text in place — both static markup and event-produced strings (status, turn, overlay, rematch line, names).

---

## 1. Layout

Single page, vertical stack (portrait phone first):

```
┌─────────────────────────────┐
│  You vs Bob      ● online    │   header banner (own name clickable)
├─────────────────────────────┤
│  status line: "Your turn"    │   phase / turn / result text
├─────────────────────────────┤
│        ENEMY WATERS          │   target grid (tap to fire) — playing
│        10 × 10 grid          │
├─────────────────────────────┤
│        YOUR FLEET            │   own grid (place ships) — placing
│        10 × 10 grid          │
├─────────────────────────────┤
│  ship palette (placing only) │   list of ships to place
│  [Start]                     │   enabled when all 10 placed
└─────────────────────────────┘
```

- Two **10×10 grids**, CSS grid, square cells, coordinates implicit (no labels needed; optional A–J / 1–10).
- During PLACING the enemy grid is dimmed/disabled; during PLAYING the palette/own-grid editing is locked.
- Cell size responsive: `min(9vw, 36px)` so both boards fit a phone screen.

---

## 2. Screens / view states (driven by server `phase`)

| phase | shown |
|-------|-------|
| name prompt | modal over everything (first load only) |
| `placing` | own grid + ship palette + Start button; status "Place your ships" / "Waiting for opponent" after Start |
| `playing` | both grids active; status "Your turn" / "Opponent's turn" |
| `finished` | result overlay "YOU WIN" / "YOU LOSE" + **New Game** (rematch) button + readiness status |

All transitions are server-driven via `state`/`gameStart`/`gameOver`. The client never advances phase on its own.

---

## 3. Name prompt & rename

- On load: if no `localStorage.sb_name`, show a modal: text input + "Join".
- On submit: store in `localStorage`, open WS, send `join {name}`.
- Reconnect (cookie present, name cached): skip modal, auto-join with cached name (server keeps the stored name anyway).
- **Rename:** the own name in the banner is a button. Tapping it re-opens the same modal; on submit it updates `localStorage` + the banner label and sends `rename {name}`. The opponent's banner updates live via an `opponent` frame.

---

## 4. Ship placement (PLACING) — interaction spec

### Palette
- Lists the fleet to place, grouped by size: **1×4, 2×3, 3×2, 4×1** (10 entries / 4 rows).
- Each entry shows the ship shape. State per entry: **available** (normal) → **placed** (greyed out, non-selectable).
- A ship is "placed" once it sits on a valid cell on the own grid.

### Place a new ship
1. The **first unplaced ship is auto-selected** on load (and after rematch). Tapping a palette entry selects a different ship; tapping the selected one deselects.
2. Tap a cell on the own grid → ship's origin = that cell; ship renders on the grid.
3. If the drop position is **valid** (in-bounds, no overlap, no-touch) → ship placed, palette entry greys out, and the **next unplaced ship is auto-selected** — so the whole fleet can be placed by tapping the grid only, no palette taps.
4. If **invalid** → ship is not committed; cell flashes red; active ship stays selected for another try.

### Move an already-placed ship (re-shift)
- **Press-and-drag** a placed ship → it follows the finger/pointer; a ghost preview shows the target footprint.
- Live validity highlight while dragging: **green** footprint = valid drop, **red** = invalid.
- Release on **valid** → ship moves there. Release on **invalid** → **snap back** to its previous valid position (no rule ever broken on the board).
- This is the "shift" capability: any placed ship can be repositioned freely until Start.

### Rotate
- **Double-tap / double-click** a placed (or active) ship → toggles horizontal ↔ vertical about its origin cell.
- Rotation is applied only if the rotated footprint is valid (in-bounds, no overlap, no-touch). Invalid → rotation rejected, brief red flash, ship stays as-is.

### Rule enforcement (client-side mirror)
- The UI enforces the same constraints as the server so the board is never visibly invalid: **bounds, overlap, no-touch (8-neighbour)**.
- This logic is a convenience mirror — see §7. The server remains the final authority.
- Cells adjacent to a placed ship may be subtly shaded ("no-go zone") to make the no-touch rule visible.

### Start
- **Start** button is **disabled** until all 10 ships are placed (palette fully greyed).
- Press Start → send `place {ships:[...]}` (the full committed fleet), lock editing, status → "Waiting for opponent…".
- If the server rejects (`placeResult {ok:false}`) — should not happen given the client mirror — re-enable editing, show the `error` text, let the player fix and press Start again. (Defensive path; client and server rules must stay in sync.)

---

## 5. Game start & turn (PLAYING)

- On `gameStart {yourTurn}`: hide palette/Start, enable enemy grid if `yourTurn`, set status.
- The server randomly assigned the first turn — the UI just reflects `yourTurn`; it does not decide.
- **"Your turn"** indicator: prominent status text + enemy grid interactive (cells get tappable affordance). When not your turn: status "Opponent's turn", enemy grid inert.

### Firing
- Tap an un-shot enemy cell on your turn → send `fire {x,y}`, optimistic "pending" marker, disable further taps until `fireResult` arrives.
- On `fireResult`:
  - Mark the cell on the relevant grid: enemy grid for your shots, own grid for incoming shots.
  - `miss` → dot; `hit` → red mark; `sunk` → mark `sunkCells` as a dead ship + `revealed` cells as misses.
  - Update status from `nextTurn`: keep/return turn accordingly (hit/sunk keeps the shooter's turn).
  - **Haptics** (Vibration API, Android Chrome; no-op on iOS): your hit `50`, your sunk `[60,40,120]`; incoming hit `30`, your ship sunk `200`; opponent miss returning the turn `20`.

---

## 6. Finish

- On `gameOver {winner}`: full-screen overlay.
  - `winner == you` → **"YOU WIN"** (green) + win haptic `[80,40,80,40,120]`.
  - else → **"YOU LOSE"** (red) + lose haptic `300`.
- Both grids shown beneath the overlay for review.
- **New Game (rematch), both-agree:** overlay shows a New Game button + status line. Click → send `rematch`, button disabled, "Waiting for opponent…". `rematch{opponentReady}` frames update the status ("Opponent wants a rematch!"). When **both** clicked, the server resets and emits `state{phase:"placing"}` → overlay hidden, back to placement (same single Placement instance is `reset()`, not recreated, to avoid stacking grid listeners).

---

## 7. Client/server rule duplication (explicit)

The placement validator (bounds/overlap/no-touch/composition) exists in **both** the JS client and the Go server. This is intentional:
- **Client copy** = instant feedback, prevents the player ever submitting an invalid fleet.
- **Server copy** = authority and anti-cheat; the only one that counts.

They must encode identical rules. If they ever diverge, the server's `placeResult {ok:false}` / `error` is the source of truth and the UI must surface it (§4 Start, defensive path). Keep the rule constants (fleet composition, board size) in one JS module to ease parity with `game.FleetSpec`.

---

## 8. Reconnect behavior (client)

- On socket close: show "Reconnecting…" status in **red** (`.status.offline`), retry WS with backoff (cookie carries the token). The red clears on the next `setStatus` after a successful resync.
- On reconnect the server sends a full `state` snapshot → **rebuild the entire UI from it**: own ships, all shot marks (incoming + enemy), current phase, whose turn, opponent name/online. The client keeps no authoritative state across reloads.

---

## 9. Files

```
web/
  index.html     grids, palette, modal, overlays, language selector
  style.css      responsive grid, ship/cell states, no-go shading, lang selector
  i18n.js        en/uk dictionary + t()/get()/set(); language detection
  app.js         WS client, message handlers, render-from-state, i18n re-render
  rules.js       shared placement rules (mirror of game.FleetSpec/no-touch)
  board.js       grid render + hit/miss/ship drawing
  place.js       palette + drag/rotate placement interactions
```

---

## 10. Accessibility / mobile notes

- Touch targets ≥ 32px; double-tap rotate must not trigger browser zoom (`touch-action: manipulation`).
- Drag uses Pointer Events (works mouse + touch). Prevent scroll while dragging a ship over the grid.
- Color is not the only signal: hit/miss/sunk also differ by icon/shape (colour-blind safe).
- Status text is `aria-live="polite"` so turn changes are announced.
- Haptic feedback (Vibration API) on shot results and game over; Android only, silently no-op where unsupported (iOS).
