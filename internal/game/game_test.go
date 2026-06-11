package game

import (
	"math/rand"
	"testing"
)

// standardFleet is a canonical legal fleet (1×4,2×3,3×2,4×1) satisfying
// bounds, overlap, and no-touch on a 10×10 board. Returns a fresh slice.
func standardFleet() []Placement {
	return []Placement{
		{Size: 4, Origin: Coord{0, 0}, Dir: Horizontal},
		{Size: 3, Origin: Coord{0, 2}, Dir: Horizontal},
		{Size: 3, Origin: Coord{0, 4}, Dir: Horizontal},
		{Size: 2, Origin: Coord{0, 6}, Dir: Horizontal},
		{Size: 2, Origin: Coord{0, 8}, Dir: Horizontal},
		{Size: 2, Origin: Coord{5, 0}, Dir: Horizontal},
		{Size: 1, Origin: Coord{5, 2}, Dir: Horizontal},
		{Size: 1, Origin: Coord{7, 2}, Dir: Horizontal},
		{Size: 1, Origin: Coord{5, 4}, Dir: Horizontal},
		{Size: 1, Origin: Coord{9, 4}, Dir: Horizontal},
	}
}

// allShipCells returns every occupied cell of a fleet (for win/sink scripting).
func allShipCells(ps []Placement) []Coord {
	var out []Coord
	for _, p := range ps {
		out = append(out, p.cells()...)
	}
	return out
}

// --- A1 composition ---------------------------------------------------------

func TestPlaceFleet_ValidComposition(t *testing.T) {
	g := NewGame()
	if err := g.PlaceFleet(0, standardFleet()); err != nil {
		t.Fatalf("valid fleet rejected: %v", err)
	}
	if !g.Ready(0) {
		t.Fatal("player 0 not marked ready after valid place")
	}
}

func TestPlaceFleet_BadComposition(t *testing.T) {
	cases := map[string]func() []Placement{
		"missing ship": func() []Placement { return standardFleet()[:9] },
		"extra ship": func() []Placement {
			return append(standardFleet(), Placement{Size: 1, Origin: Coord{0, 0}, Dir: Horizontal})
		},
		"illegal size 5": func() []Placement {
			f := standardFleet()
			f[0].Size = 5 // 1×5 instead of 1×4
			return f
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			g := NewGame()
			err := g.PlaceFleet(0, build())
			assertFleetCode(t, err, "composition")
		})
	}
}

// --- A2 bounds --------------------------------------------------------------

func TestPlaceFleet_Bounds(t *testing.T) {
	t.Run("horizontal overflow", func(t *testing.T) {
		f := standardFleet()
		f[0].Origin = Coord{7, 0} // size4 → x 7..10
		assertFleetCode(t, NewGame().PlaceFleet(0, f), "bounds")
	})
	t.Run("vertical overflow", func(t *testing.T) {
		f := standardFleet()
		f[0] = Placement{Size: 4, Origin: Coord{0, 7}, Dir: Vertical} // rows 7..10
		assertFleetCode(t, NewGame().PlaceFleet(0, f), "bounds")
	})
	t.Run("boundary fits", func(t *testing.T) {
		f := standardFleet()
		f[0].Origin = Coord{6, 0} // size4 → x 6..9, hugs the right edge
		f[5].Origin = Coord{8, 8} // relocate the (5,0) size2 clear of the moved size4
		if err := NewGame().PlaceFleet(0, f); err != nil {
			t.Fatalf("boundary placement rejected: %v", err)
		}
	})
}

// --- A3 overlap -------------------------------------------------------------

func TestPlaceFleet_Overlap(t *testing.T) {
	f := standardFleet()
	f[6].Origin = Coord{0, 0} // size1 onto the size4's first cell
	assertFleetCode(t, NewGame().PlaceFleet(0, f), "overlap")
}

// --- A4 no-touch ------------------------------------------------------------

func TestPlaceFleet_NoTouch(t *testing.T) {
	t.Run("edge touch", func(t *testing.T) {
		f := standardFleet()
		f[6].Origin = Coord{0, 1} // directly below the size4 at (0,0)
		assertFleetCode(t, NewGame().PlaceFleet(0, f), "no_touch")
	})
	t.Run("diagonal touch", func(t *testing.T) {
		f := standardFleet()
		f[6].Origin = Coord{4, 1} // diagonal to size4 cell (3,0)
		assertFleetCode(t, NewGame().PlaceFleet(0, f), "no_touch")
	})
}

// --- A5 fire resolution + turn ---------------------------------------------

// startedGame places standard fleets on both boards, starts with a seed, and
// returns the game plus the first-turn slot.
func startedGame(t *testing.T, seed int64) (*Game, int) {
	t.Helper()
	g := NewGame()
	if err := g.PlaceFleet(0, standardFleet()); err != nil {
		t.Fatal(err)
	}
	if err := g.PlaceFleet(1, standardFleet()); err != nil {
		t.Fatal(err)
	}
	first, err := g.Start(rand.New(rand.NewSource(seed)))
	if err != nil {
		t.Fatal(err)
	}
	if g.Phase() != Playing {
		t.Fatalf("phase = %v, want playing", g.Phase())
	}
	return g, first
}

func TestFire_MissFlipsTurn(t *testing.T) {
	g, p := startedGame(t, 1)
	res, err := g.Fire(p, Coord{9, 9}) // empty corner of opponent board
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Miss {
		t.Fatalf("outcome = %v, want miss", res.Outcome)
	}
	if res.NextTurn != 1-p {
		t.Fatalf("nextTurn = %d, want %d (flip)", res.NextTurn, 1-p)
	}
}

func TestFire_HitKeepsTurn(t *testing.T) {
	g, p := startedGame(t, 1)
	res, err := g.Fire(p, Coord{0, 0}) // first cell of opponent's size4 (not last)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != HitShip {
		t.Fatalf("outcome = %v, want hit", res.Outcome)
	}
	if res.NextTurn != p {
		t.Fatalf("nextTurn = %d, want %d (keep)", res.NextTurn, p)
	}
}

func TestFire_SunkRevealAndKeepTurn(t *testing.T) {
	g, p := startedGame(t, 1)
	// sink the size-1 ship at (9,4) in one shot.
	res, err := g.Fire(p, Coord{9, 4})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Sunk {
		t.Fatalf("outcome = %v, want sunk", res.Outcome)
	}
	if len(res.SunkCells) != 1 || res.SunkCells[0] != (Coord{9, 4}) {
		t.Fatalf("sunkCells = %v, want [(9,4)]", res.SunkCells)
	}
	if res.NextTurn != p {
		t.Fatalf("nextTurn = %d, want %d (keep)", res.NextTurn, p)
	}
	// revealed cells must be in-bounds, exclude the ship cell, and be marked shot.
	opp := g.boards[1-p]
	for _, r := range res.Revealed {
		if !r.inBounds() {
			t.Fatalf("revealed cell %v out of bounds", r)
		}
		if r == (Coord{9, 4}) {
			t.Fatal("revealed includes the ship cell")
		}
		if !opp.shots[r] {
			t.Fatalf("revealed cell %v not recorded as shot", r)
		}
	}
	// A8.2 — re-firing a revealed cell is rejected as already fired.
	if len(res.Revealed) > 0 {
		_, err := g.Fire(p, res.Revealed[0])
		if err != ErrAlreadyFired {
			t.Fatalf("re-fire revealed: err = %v, want already_fired", err)
		}
	}
}

func TestFire_Guards(t *testing.T) {
	g, p := startedGame(t, 1)
	if _, err := g.Fire(1-p, Coord{0, 0}); err != ErrNotYourTurn {
		t.Fatalf("wrong player: err = %v, want not_your_turn", err)
	}
	if _, err := g.Fire(p, Coord{10, 0}); err != ErrOutOfBounds {
		t.Fatalf("oob: err = %v, want out_of_bounds", err)
	}
	// hit keeps the turn, so the same attacker can re-fire the same cell.
	if _, err := g.Fire(p, Coord{0, 0}); err != nil { // hit, keeps turn
		t.Fatal(err)
	}
	if _, err := g.Fire(p, Coord{0, 0}); err != ErrAlreadyFired {
		t.Fatalf("repeat cell: err = %v, want already_fired", err)
	}
}

// --- A6 win condition -------------------------------------------------------

func TestFire_WinOnLastCell(t *testing.T) {
	g, p := startedGame(t, 1)
	cells := allShipCells(standardFleet()) // opponent uses the same layout
	var last Result
	for i, c := range cells {
		res, err := g.Fire(p, c)
		if err != nil {
			t.Fatalf("fire %v: %v", c, err)
		}
		if i < len(cells)-1 && res.GameOver {
			t.Fatalf("game over early after %d/%d cells", i+1, len(cells))
		}
		last = res
	}
	if !last.GameOver || last.Winner != p {
		t.Fatalf("final shot: gameOver=%v winner=%d, want true/%d", last.GameOver, last.Winner, p)
	}
	if g.Phase() != Finished {
		t.Fatalf("phase = %v, want finished", g.Phase())
	}
	if _, err := g.Fire(p, Coord{0, 0}); err != ErrWrongPhase {
		t.Fatalf("fire after finish: err = %v, want wrong_phase", err)
	}
}

// --- A7 start / RNG ---------------------------------------------------------

func TestStart_NotBothReady(t *testing.T) {
	g := NewGame()
	if err := g.PlaceFleet(0, standardFleet()); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Start(rand.New(rand.NewSource(1))); err != ErrNotReady {
		t.Fatalf("start with one ready: err = %v, want not_ready", err)
	}
	if g.Phase() != Placing {
		t.Fatalf("phase = %v, want placing", g.Phase())
	}
}

func TestStart_RNGProducesBothPlayers(t *testing.T) {
	seen := [2]bool{}
	for seed := int64(0); seed < 100; seed++ {
		g := NewGame()
		_ = g.PlaceFleet(0, standardFleet())
		_ = g.PlaceFleet(1, standardFleet())
		first, err := g.Start(rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatal(err)
		}
		seen[first] = true
	}
	if !seen[0] || !seen[1] {
		t.Fatalf("first-turn RNG stuck: seen = %v", seen)
	}
}

func TestPlaceFleet_DoublePlaceRejected(t *testing.T) {
	g := NewGame()
	if err := g.PlaceFleet(0, standardFleet()); err != nil {
		t.Fatal(err)
	}
	if err := g.PlaceFleet(0, standardFleet()); err != ErrAlreadyPlaced {
		t.Fatalf("second place: err = %v, want already_placed", err)
	}
}

// --- A8.1 auto-reveal clipped at corner ------------------------------------

func TestFire_RevealClippedAtCorner(t *testing.T) {
	// Fleet with a size-1 ship in the (0,0) corner; sink it and check reveal
	// is clipped to in-bounds neighbours only.
	f := standardFleet()
	f[9].Origin = Coord{9, 9} // move the spare size1 to the opposite corner
	g := NewGame()
	if err := g.PlaceFleet(0, f); err != nil {
		t.Fatal(err)
	}
	if err := g.PlaceFleet(1, f); err != nil {
		t.Fatal(err)
	}
	first, _ := g.Start(rand.New(rand.NewSource(1)))
	res, err := g.Fire(first, Coord{9, 9})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != Sunk {
		t.Fatalf("outcome = %v, want sunk", res.Outcome)
	}
	// corner ship → exactly 3 in-bounds neighbours.
	if len(res.Revealed) != 3 {
		t.Fatalf("revealed = %v, want 3 in-bounds cells", res.Revealed)
	}
	for _, r := range res.Revealed {
		if !r.inBounds() {
			t.Fatalf("revealed %v out of bounds", r)
		}
	}
}

// --- Snapshot privacy + fidelity -------------------------------------------

func TestSnapshot_PrivacyAndOutcomes(t *testing.T) {
	g, p := startedGame(t, 1)
	// p sinks the opponent's (9,4) size1 and misses once.
	if _, err := g.Fire(p, Coord{9, 4}); err != nil { // sunk, keeps turn
		t.Fatal(err)
	}
	if _, err := g.Fire(p, Coord{9, 9}); err != nil { // miss, flips
		t.Fatal(err)
	}
	v := g.Snapshot(p)
	if len(v.OwnShips) != 10 {
		t.Fatalf("own ships = %d, want 10", len(v.OwnShips))
	}
	// enemyShots must report the sunk cell as "sunk" and the miss as "miss".
	var sawSunk, sawMiss bool
	for _, s := range v.EnemyShots {
		if s.X == 9 && s.Y == 4 && s.Outcome == "sunk" {
			sawSunk = true
		}
		if s.X == 9 && s.Y == 9 && s.Outcome == "miss" {
			sawMiss = true
		}
	}
	if !sawSunk {
		t.Fatalf("enemyShots missing sunk (9,4): %+v", v.EnemyShots)
	}
	if !sawMiss {
		t.Fatalf("enemyShots missing miss (9,9): %+v", v.EnemyShots)
	}
}

// helper ---------------------------------------------------------------------

func assertFleetCode(t *testing.T, err error, code string) {
	t.Helper()
	fe, ok := err.(*FleetError)
	if !ok {
		t.Fatalf("err = %v (%T), want *FleetError{%s}", err, err, code)
	}
	if fe.Code != code {
		t.Fatalf("FleetError code = %q, want %q (detail: %s)", fe.Code, code, fe.Detail)
	}
}
