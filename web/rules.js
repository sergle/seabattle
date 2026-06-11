// rules.js — client-side mirror of the server placement rules (game.FleetSpec /
// no-touch). Convenience only: instant feedback so the player never submits an
// invalid fleet. The Go server is the authority (ui.md §7).
(function (global) {
  'use strict';

  const SIZE = 10;
  // Fleet composition: 1×4, 2×3, 3×2, 4×1 → 10 ships, 20 cells.
  const FLEET = [4, 3, 3, 2, 2, 2, 1, 1, 1, 1];

  function inBounds(x, y) {
    return x >= 0 && x < SIZE && y >= 0 && y < SIZE;
  }

  // cells of a ship placement {x,y,size,dir:'h'|'v'}.
  function cellsOf(s) {
    const out = [];
    for (let i = 0; i < s.size; i++) {
      out.push(s.dir === 'v' ? { x: s.x, y: s.y + i } : { x: s.x + i, y: s.y });
    }
    return out;
  }

  function neighbours(x, y) {
    const out = [];
    for (let dy = -1; dy <= 1; dy++) {
      for (let dx = -1; dx <= 1; dx++) {
        if (dx === 0 && dy === 0) continue;
        if (inBounds(x + dx, y + dy)) out.push({ x: x + dx, y: y + dy });
      }
    }
    return out;
  }

  const key = (x, y) => y * SIZE + x;

  // fits checks one candidate placement against a set of already-placed ships:
  // bounds, overlap, and no-touch (8-neighbour). `others` excludes the candidate.
  function fits(cand, others) {
    const occupied = new Map(); // key → true
    for (const s of others) {
      for (const c of cellsOf(s)) occupied.set(key(c.x, c.y), true);
    }
    const candCells = cellsOf(cand);
    for (const c of candCells) {
      if (!inBounds(c.x, c.y)) return false;          // bounds
      if (occupied.has(key(c.x, c.y))) return false;  // overlap
      for (const n of neighbours(c.x, c.y)) {         // no-touch
        if (occupied.has(key(n.x, n.y))) return false;
      }
    }
    return true;
  }

  // validateFleet checks a complete fleet (used as a final guard before Start).
  function validateFleet(ships) {
    if (ships.length !== FLEET.length) return { ok: false, error: 'rule_count' };
    for (let i = 0; i < ships.length; i++) {
      const others = ships.filter((_, j) => j !== i);
      if (!fits(ships[i], others)) {
        return { ok: false, error: 'rule_invalid' };
      }
    }
    return { ok: true };
  }

  global.Rules = { SIZE, FLEET, inBounds, cellsOf, neighbours, fits, validateFleet };
})(window);
