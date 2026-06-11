package game

// Board is one player's grid plus the shots fired at it.
type Board struct {
	grid     [BoardSize][BoardSize]CellState
	ships    []*shipState
	cellShip map[Coord]*shipState // ship cell → owning ship
	shots    map[Coord]bool       // every cell fired AT this board (hit or miss)
}

// newBoard builds a board from already-validated ship footprints.
func newBoard(ships []*shipState) *Board {
	b := &Board{
		ships:    ships,
		cellShip: map[Coord]*shipState{},
		shots:    map[Coord]bool{},
	}
	for _, s := range ships {
		for _, c := range s.all {
			b.grid[c.Y][c.X] = CellShip
			b.cellShip[c] = s
		}
	}
	return b
}

// allSunk reports whether every ship on the board is destroyed.
func (b *Board) allSunk() bool {
	for _, s := range b.ships {
		if !s.sunk() {
			return false
		}
	}
	return true
}

// placements reconstructs the original placements (for a player's own-board view).
func (b *Board) placements() []Placement {
	out := make([]Placement, 0, len(b.ships))
	for _, s := range b.ships {
		first := s.all[0]
		dir := Horizontal
		if len(s.all) > 1 && s.all[1].Y != first.Y {
			dir = Vertical
		}
		out = append(out, Placement{Size: s.size, Origin: first, Dir: dir})
	}
	return out
}

// outcomeAt classifies a single already-fired cell on this board as
// "miss", "hit", or "sunk" (a hit on a ship that is now fully destroyed).
func (b *Board) outcomeAt(c Coord) string {
	s, ok := b.cellShip[c]
	if !ok {
		return "miss"
	}
	if s.sunk() {
		return "sunk"
	}
	return "hit"
}
