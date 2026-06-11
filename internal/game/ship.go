// Package game holds the pure Sea Battle domain: board, fleet, and game-state
// logic. It imports only the standard library (errors/fmt/math/rand) and has no
// knowledge of HTTP, JSON, WebSockets, or concurrency — see server.md §1.
package game

// BoardSize is the edge length of the (square) board.
const BoardSize = 10

// CellState is the state of a single board cell.
type CellState uint8

const (
	CellEmpty CellState = iota
	CellShip
	CellHit
	CellMiss
)

// Coord is a zero-based board coordinate. X is the column (0..9), Y the row.
type Coord struct {
	X, Y int
}

func (c Coord) inBounds() bool {
	return c.X >= 0 && c.X < BoardSize && c.Y >= 0 && c.Y < BoardSize
}

// neighbours returns the up-to-8 in-bounds cells surrounding c (diagonals
// included — all 8 are returned). Used for the no-touch rule and auto-reveal.
func (c Coord) neighbours() []Coord {
	out := make([]Coord, 0, 8)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			n := Coord{c.X + dx, c.Y + dy}
			if n.inBounds() {
				out = append(out, n)
			}
		}
	}
	return out
}

// Orientation is a ship's axis.
type Orientation uint8

const (
	Horizontal Orientation = iota // +X
	Vertical                      // +Y
)

// Placement is a requested ship position: a size-N ship whose first cell is
// Origin, extending along Dir. It is the input to Game.PlaceFleet.
type Placement struct {
	Size   int
	Origin Coord
	Dir    Orientation
}

// cells returns the footprint of the placement. Cells may fall out of bounds;
// validation (fleet.go) is responsible for rejecting those.
func (p Placement) cells() []Coord {
	out := make([]Coord, 0, p.Size)
	for i := 0; i < p.Size; i++ {
		if p.Dir == Vertical {
			out = append(out, Coord{p.Origin.X, p.Origin.Y + i})
		} else {
			out = append(out, Coord{p.Origin.X + i, p.Origin.Y})
		}
	}
	return out
}

// shipState is the internal, post-placement representation of one ship.
type shipState struct {
	size  int
	all   []Coord        // full footprint
	cells map[Coord]bool // remaining (un-hit) cells; empty ⇒ sunk
}

func (s *shipState) sunk() bool { return len(s.cells) == 0 }
