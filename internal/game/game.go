package game

import (
	"errors"
	"math/rand"
)

// Phase is the domain game phase. Waiting is a hub-level concept (slot
// occupancy) and is never set here; a Game begins in Placing.
type Phase uint8

const (
	Waiting Phase = iota // never used by the domain; reserved for the wire enum
	Placing
	Playing
	Finished
)

func (p Phase) String() string {
	switch p {
	case Waiting:
		return "waiting"
	case Placing:
		return "placing"
	case Playing:
		return "playing"
	case Finished:
		return "finished"
	default:
		return "unknown"
	}
}

// Outcome is the result of a single shot.
type Outcome uint8

const (
	Miss Outcome = iota
	HitShip
	Sunk
)

func (o Outcome) String() string {
	switch o {
	case Miss:
		return "miss"
	case HitShip:
		return "hit"
	case Sunk:
		return "sunk"
	default:
		return "unknown"
	}
}

// Result is returned by Fire.
type Result struct {
	Outcome   Outcome
	SunkCells []Coord // populated when Outcome == Sunk
	Revealed  []Coord // auto-miss cells around a sunk ship
	NextTurn  int     // whose turn after this shot
	GameOver  bool
	Winner    int // valid when GameOver
}

// Sentinel errors. The wire layer maps these to protocol error codes (§8).
var (
	ErrWrongPhase    = errors.New("wrong_phase")
	ErrNotYourTurn   = errors.New("not_your_turn")
	ErrAlreadyFired  = errors.New("already_fired")
	ErrOutOfBounds   = errors.New("out_of_bounds")
	ErrAlreadyPlaced = errors.New("already_placed")
	ErrNotReady      = errors.New("not_ready")
)

// Game is the authoritative state for a single 2-player match.
type Game struct {
	phase  Phase
	boards [2]*Board
	ready  [2]bool
	turn   int
	winner int
}

// NewGame returns a game ready for fleet placement.
func NewGame() *Game {
	return &Game{phase: Placing, winner: -1}
}

// Phase returns the current domain phase.
func (g *Game) Phase() Phase { return g.phase }

// Turn returns whose turn it is (valid only in Playing).
func (g *Game) Turn() int { return g.turn }

// Winner returns the winning slot, or -1 if undecided.
func (g *Game) Winner() int { return g.winner }

// Ready reports whether player p has committed a valid fleet.
func (g *Game) Ready(p int) bool { return g.ready[p] }

// BothReady reports whether both players have committed fleets.
func (g *Game) BothReady() bool { return g.ready[0] && g.ready[1] }

// PlaceFleet validates and commits player p's fleet. On success the player is
// marked ready. Re-placing after a commit is rejected.
func (g *Game) PlaceFleet(p int, ps []Placement) error {
	if g.phase != Placing {
		return ErrWrongPhase
	}
	if g.ready[p] {
		return ErrAlreadyPlaced
	}
	ships, ferr := validateFleet(ps)
	if ferr != nil {
		return ferr
	}
	g.boards[p] = newBoard(ships)
	g.ready[p] = true
	return nil
}

// Start moves a both-ready game into Playing and picks the first turn using the
// injected RNG (so tests are deterministic). Returns the chosen first slot.
func (g *Game) Start(rng *rand.Rand) (int, error) {
	if g.phase != Placing || !g.BothReady() {
		return 0, ErrNotReady
	}
	g.turn = rng.Intn(2)
	g.phase = Playing
	return g.turn, nil
}

// Fire resolves player p's shot at c against the opponent's board.
func (g *Game) Fire(p int, c Coord) (Result, error) {
	if g.phase != Playing {
		return Result{}, ErrWrongPhase
	}
	if p != g.turn {
		return Result{}, ErrNotYourTurn
	}
	if !c.inBounds() {
		return Result{}, ErrOutOfBounds
	}
	opp := g.boards[1-p]
	if opp.shots[c] {
		return Result{}, ErrAlreadyFired
	}
	opp.shots[c] = true

	res := Result{Winner: -1}
	ship, isShip := opp.cellShip[c]
	if !isShip {
		opp.grid[c.Y][c.X] = CellMiss
		res.Outcome = Miss
		g.turn = 1 - p // miss flips turn
		res.NextTurn = g.turn
		return res, nil
	}

	// hit
	opp.grid[c.Y][c.X] = CellHit
	delete(ship.cells, c)
	if ship.sunk() {
		res.Outcome = Sunk
		res.SunkCells = append([]Coord(nil), ship.all...)
		res.Revealed = revealAround(opp, ship)
	} else {
		res.Outcome = HitShip
	}

	if opp.allSunk() {
		g.phase = Finished
		g.winner = p
		res.GameOver = true
		res.Winner = p
	}
	// hit/sunk keeps the turn
	res.NextTurn = g.turn
	return res, nil
}

// revealAround marks the empty cells surrounding a just-sunk ship as Miss and
// records them as shots (no-touch guarantees they hold no other ship), then
// returns them. De-duplicated across the ship's footprint.
func revealAround(b *Board, s *shipState) []Coord {
	seen := map[Coord]bool{}
	for _, sc := range s.all {
		seen[sc] = true // exclude ship's own cells
	}
	var out []Coord
	for _, sc := range s.all {
		for _, n := range sc.neighbours() {
			if seen[n] {
				continue
			}
			seen[n] = true
			b.grid[n.Y][n.X] = CellMiss
			b.shots[n] = true
			out = append(out, n)
		}
	}
	return out
}
