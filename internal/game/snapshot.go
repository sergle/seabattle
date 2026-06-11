package game

import "sort"

// ShotView is one fired cell and its classified outcome (miss|hit|sunk).
type ShotView struct {
	X, Y    int
	Outcome string
}

// StateView is the per-player snapshot the wire layer turns into a `state`
// message (protocol.md §6). Phase is the domain phase; the hub overlays the
// hub-synthesized "waiting" and player names/online flags.
type StateView struct {
	Phase         Phase
	YourTurn      bool
	YouReady      bool
	OpponentReady bool
	OwnShips      []Placement // your fleet (never the opponent's)
	Incoming      []ShotView  // shots fired at your board
	EnemyShots    []ShotView  // your shots at the enemy board
	Winner        int         // -1 until decided
}

// Snapshot builds the view for player p. Safe to call in any phase, including
// before either player has placed a fleet.
func (g *Game) Snapshot(p int) StateView {
	v := StateView{
		Phase:         g.phase,
		YourTurn:      g.phase == Playing && g.turn == p,
		YouReady:      g.ready[p],
		OpponentReady: g.ready[1-p],
		Winner:        g.winner,
	}
	if own := g.boards[p]; own != nil {
		v.OwnShips = own.placements()
		v.Incoming = shotViews(own)
	}
	if opp := g.boards[1-p]; opp != nil {
		v.EnemyShots = shotViews(opp)
	}
	return v
}

// shotViews returns every shot fired at board b, classified and sorted
// (row-major) for deterministic output.
func shotViews(b *Board) []ShotView {
	out := make([]ShotView, 0, len(b.shots))
	for c := range b.shots {
		out = append(out, ShotView{X: c.X, Y: c.Y, Outcome: b.outcomeAt(c)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		return out[i].X < out[j].X
	})
	return out
}
