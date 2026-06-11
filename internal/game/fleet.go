package game

import "fmt"

// FleetSpec is the required multiset of ship sizes: size → count.
// 1×4 + 2×3 + 3×2 + 4×1 = 10 ships, 20 cells. Single source of truth.
var FleetSpec = map[int]int{4: 1, 3: 2, 2: 3, 1: 4}

// FleetError reports why a fleet was rejected. Code is one of:
// composition | bounds | overlap | no_touch. The wire layer maps Code to a
// protocol error code (protocol.md §8).
type FleetError struct {
	Code   string
	Detail string
}

func (e *FleetError) Error() string { return e.Code + ": " + e.Detail }

// validateFleet checks a set of placements against all rules, in order, and is
// strictly all-or-nothing. Order: composition → bounds → overlap → no-touch.
// On success it returns the built ship footprints (one per placement).
func validateFleet(ps []Placement) ([]*shipState, *FleetError) {
	// 1. composition — exact multiset match against FleetSpec.
	counts := map[int]int{}
	for _, p := range ps {
		counts[p.Size]++
	}
	for size, want := range FleetSpec {
		if counts[size] != want {
			return nil, &FleetError{"composition",
				fmt.Sprintf("expected %d ship(s) of size %d, got %d", want, size, counts[size])}
		}
	}
	// reject any ship of a size not in the spec (e.g. size 5).
	for size := range counts {
		if _, ok := FleetSpec[size]; !ok {
			return nil, &FleetError{"composition", fmt.Sprintf("illegal ship size %d", size)}
		}
	}

	// 2. bounds — every cell of every ship on the board.
	for _, p := range ps {
		for _, c := range p.cells() {
			if !c.inBounds() {
				return nil, &FleetError{"bounds",
					fmt.Sprintf("ship size %d at (%d,%d) leaves the board", p.Size, p.Origin.X, p.Origin.Y)}
			}
		}
	}

	// occupancy map cell → owning ship index, used for overlap + no-touch.
	owner := map[Coord]int{}
	ships := make([]*shipState, len(ps))
	for i, p := range ps {
		cells := p.cells()
		// 3. overlap — no cell shared.
		for _, c := range cells {
			if _, taken := owner[c]; taken {
				return nil, &FleetError{"overlap",
					fmt.Sprintf("cell (%d,%d) occupied by two ships", c.X, c.Y)}
			}
			owner[c] = i
		}
		cellset := make(map[Coord]bool, len(cells))
		for _, c := range cells {
			cellset[c] = true
		}
		ships[i] = &shipState{size: p.Size, all: cells, cells: cellset}
	}

	// 4. no-touch — no cell of ship i may be 8-adjacent to a cell of a different ship.
	for c, idx := range owner {
		for _, n := range c.neighbours() {
			if other, taken := owner[n]; taken && other != idx {
				return nil, &FleetError{"no_touch",
					fmt.Sprintf("ships at (%d,%d) and (%d,%d) touch", c.X, c.Y, n.X, n.Y)}
			}
		}
	}

	return ships, nil
}
