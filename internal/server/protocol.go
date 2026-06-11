// Package server orchestrates WebSocket connections, player sessions, and the
// single game. The wire format is defined in protocol.md; this file is the
// Go encoding of it. The hub (hub.go) is connection-agnostic and fully
// testable in-process — see hub_test.go.
package server

import (
	"encoding/json"

	"seabattle/internal/game"
)

// inbound is the union of all client→server frames (protocol.md §5).
type inbound struct {
	Type  string     `json:"type"`
	Name  string     `json:"name"`
	Ships []wireShip `json:"ships"`
	X     *int       `json:"x"`
	Y     *int       `json:"y"`
}

type wireShip struct {
	Size int    `json:"size"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Dir  string `json:"dir"`
}

func (w wireShip) toPlacement() game.Placement {
	dir := game.Horizontal
	if w.Dir == "v" {
		dir = game.Vertical
	}
	return game.Placement{Size: w.Size, Origin: game.Coord{X: w.X, Y: w.Y}, Dir: dir}
}

func placementsFrom(ws []wireShip) []game.Placement {
	out := make([]game.Placement, len(ws))
	for i, w := range ws {
		out[i] = w.toPlacement()
	}
	return out
}

// --- outbound builders (server→client, protocol.md §6) ----------------------
// Each returns a marshaled JSON frame. Marshaling small fixed structs cannot
// fail, so errors are ignored.

func marshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func msgAssigned(player int, name string, reconnect bool) []byte {
	return marshal(map[string]any{
		"type": "assigned", "player": player, "name": name, "reconnect": reconnect,
	})
}

func msgFull() []byte {
	return marshal(map[string]any{"type": "full"})
}

// msgOpponent reports opponent presence. When the opponent has not joined yet,
// name is "" and is omitted.
func msgOpponent(name string, online bool) []byte {
	m := map[string]any{"type": "opponent", "online": online}
	if name != "" {
		m["name"] = name
	}
	return marshal(m)
}

func msgPlaceResult(ok bool, errStr string) []byte {
	m := map[string]any{"type": "placeResult", "ok": ok}
	if !ok {
		m["error"] = errStr
	}
	return marshal(m)
}

func msgGameStart(yourTurn bool, firstPlayer int) []byte {
	return marshal(map[string]any{
		"type": "gameStart", "yourTurn": yourTurn, "firstPlayer": firstPlayer,
	})
}

func cells(cs []game.Coord) []map[string]int {
	out := make([]map[string]int, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]int{"x": c.X, "y": c.Y})
	}
	return out
}

func msgFireResult(x, y, by int, r game.Result) []byte {
	m := map[string]any{
		"type": "fireResult", "x": x, "y": y, "by": by,
		"outcome": r.Outcome.String(), "nextTurn": r.NextTurn,
	}
	if r.Outcome == game.Sunk {
		m["sunkCells"] = cells(r.SunkCells)
		m["revealed"] = cells(r.Revealed)
	}
	return marshal(m)
}

func msgGameOver(winner int) []byte {
	return marshal(map[string]any{"type": "gameOver", "winner": winner})
}

// msgRematch reports rematch readiness while FINISHED. Sent to each player when
// one side requests a rematch but both have not yet agreed (protocol.md §6).
func msgRematch(youReady, opponentReady bool) []byte {
	return marshal(map[string]any{
		"type": "rematch", "youReady": youReady, "opponentReady": opponentReady,
	})
}

func msgError(code, detail string) []byte {
	m := map[string]any{"type": "error", "code": code}
	if detail != "" {
		m["detail"] = detail
	}
	return marshal(m)
}

// shotView mirrors game.ShotView on the wire.
func shotViews(vs []game.ShotView) []map[string]any {
	out := make([]map[string]any, 0, len(vs))
	for _, v := range vs {
		out = append(out, map[string]any{"x": v.X, "y": v.Y, "outcome": v.Outcome})
	}
	return out
}

func placementViews(ps []game.Placement) []map[string]any {
	out := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		dir := "h"
		if p.Dir == game.Vertical {
			dir = "v"
		}
		out = append(out, map[string]any{"size": p.Size, "x": p.Origin.X, "y": p.Origin.Y, "dir": dir})
	}
	return out
}

// msgState builds the per-player snapshot. phase is the hub-resolved string
// (may be the synthesized "waiting"); winner -1 maps to JSON null.
func msgState(phase string, you int, v game.StateView, oppName string, oppOnline bool) []byte {
	var winner any
	if v.Winner >= 0 {
		winner = v.Winner
	}
	return marshal(map[string]any{
		"type":          "state",
		"phase":         phase,
		"you":           you,
		"yourTurn":      v.YourTurn,
		"youReady":      v.YouReady,
		"opponentReady": v.OpponentReady,
		"ownBoard": map[string]any{
			"ships":    placementViews(v.OwnShips),
			"incoming": shotViews(v.Incoming),
		},
		"enemyShots":     shotViews(v.EnemyShots),
		"opponentName":   oppName,
		"opponentOnline": oppOnline,
		"winner":         winner,
	})
}
