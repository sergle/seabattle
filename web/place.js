// place.js — ship-placement interactions on the own grid (ui.md §4). Manual
// only: select from palette → tap to place, drag to move, double-tap to rotate.
// All moves are validated against Rules (bounds/overlap/no-touch); the board is
// never left in an invalid state. The committed fleet is read via getFleet().
(function (global) {
  'use strict';

  function Placement(gridEl, paletteEl, onChange) {
    const ships = Rules.FLEET.map((size, id) => ({ id, size, dir: 'h', x: null, y: null }));
    let selected = null;   // id of a palette ship awaiting placement
    let drag = null;       // { id, grabIndex, cand, valid }
    let lastTap = { id: -1, t: 0 };
    let locked = false;

    // --- model helpers ------------------------------------------------------
    const placed = () => ships.filter(s => s.x !== null);
    const others = (id) => placed().filter(s => s.id !== id);
    const shipAt = (x, y) =>
      placed().find(s => Rules.cellsOf(s).some(c => c.x === x && c.y === y)) || null;
    const allPlaced = () => ships.every(s => s.x !== null);

    // --- palette ------------------------------------------------------------
    function buildPalette() {
      paletteEl.innerHTML = '';
      for (const s of ships) {
        const item = document.createElement('div');
        item.className = 'ship-item';
        item.dataset.id = s.id;
        for (let i = 0; i < s.size; i++) {
          const seg = document.createElement('span');
          seg.className = 'seg';
          item.appendChild(seg);
        }
        item.addEventListener('click', () => selectShip(s.id));
        paletteEl.appendChild(item);
      }
    }

    function selectShip(id) {
      if (locked) return;
      const s = ships[id];
      if (s.x !== null) return; // already placed
      selected = selected === id ? null : id;
      syncPalette();
    }

    function syncPalette() {
      for (const item of paletteEl.children) {
        const id = +item.dataset.id;
        item.classList.toggle('placed', ships[id].x !== null);
        item.classList.toggle('selected', id === selected);
      }
    }

    // --- rendering ----------------------------------------------------------
    function render() {
      Board.clearMarks(gridEl);
      const draggedId = drag ? drag.id : -1;
      const shown = placed().filter(s => s.id !== draggedId);

      // ship cells
      for (const s of shown) {
        for (const c of Rules.cellsOf(s)) Board.mark(gridEl, c.x, c.y, 'ship');
      }
      // no-go shading around placed ships
      const occ = new Set(shown.flatMap(s => Rules.cellsOf(s)).map(c => c.x + ',' + c.y));
      for (const s of shown) {
        for (const c of Rules.cellsOf(s)) {
          for (const n of Rules.neighbours(c.x, c.y)) {
            if (!occ.has(n.x + ',' + n.y)) Board.mark(gridEl, n.x, n.y, 'nogo');
          }
        }
      }
      // drag preview
      if (drag && drag.cand) {
        const kind = drag.valid ? 'preview-ok' : 'preview-bad';
        for (const c of Rules.cellsOf(drag.cand)) {
          if (Rules.inBounds(c.x, c.y)) Board.cell(gridEl, c.x, c.y).classList.add(kind);
        }
      }
    }

    // --- interactions -------------------------------------------------------
    function onDown(ev) {
      if (locked) return;
      const hit = Board.cellFromEvent(gridEl, ev);
      if (!hit) return;
      ev.preventDefault();
      const s = shipAt(hit.x, hit.y);
      if (s) {
        // begin dragging an existing ship; keep the grabbed segment under finger
        const grabIndex = s.dir === 'h' ? hit.x - s.x : hit.y - s.y;
        drag = { id: s.id, grabIndex, cand: { ...s }, valid: true };
        gridEl.setPointerCapture(ev.pointerId);
        render();
        return;
      }
      if (selected !== null) {
        // place the selected ship with its first cell at the tapped cell
        tryPlace(selected, hit.x, hit.y);
      }
    }

    function onMove(ev) {
      if (!drag) return;
      const hit = Board.cellFromEvent(gridEl, ev);
      if (!hit) return;
      const s = ships[drag.id];
      const ox = s.dir === 'h' ? hit.x - drag.grabIndex : hit.x;
      const oy = s.dir === 'v' ? hit.y - drag.grabIndex : hit.y;
      drag.cand = { id: s.id, size: s.size, dir: s.dir, x: ox, y: oy };
      drag.valid = Rules.fits(drag.cand, others(s.id));
      render();
    }

    function onUp(ev) {
      if (!drag) return;
      const wasTap = drag.cand && drag.cand.x === ships[drag.id].x && drag.cand.y === ships[drag.id].y;
      if (drag.valid && drag.cand) {
        ships[drag.id].x = drag.cand.x;
        ships[drag.id].y = drag.cand.y;
      }
      const id = drag.id;
      drag = null;
      render();

      // double-tap on a placed ship → rotate
      if (wasTap) {
        const now = Date.now();
        if (lastTap.id === id && now - lastTap.t < 300) {
          rotate(id);
          lastTap = { id: -1, t: 0 };
        } else {
          lastTap = { id, t: now };
        }
      }
      changed();
    }

    function tryPlace(id, x, y) {
      const s = ships[id];
      const cand = { id, size: s.size, dir: s.dir, x, y };
      if (Rules.fits(cand, others(id))) {
        s.x = x; s.y = y;
        selected = nextUnplaced(); // auto-advance to the next ship to place
        syncPalette();
        render();
        changed();
      } else {
        flashBad(cand);
      }
    }

    // nextUnplaced returns the id of the first not-yet-placed ship, or null.
    const nextUnplaced = () => {
      const s = ships.find(s => s.x === null);
      return s ? s.id : null;
    };

    function rotate(id) {
      const s = ships[id];
      const dir = s.dir === 'h' ? 'v' : 'h';
      const cand = { id, size: s.size, dir, x: s.x, y: s.y };
      if (Rules.fits(cand, others(id))) {
        s.dir = dir;
        render();
        changed();
      } else {
        flashBad(cand);
      }
    }

    function flashBad(cand) {
      for (const c of Rules.cellsOf(cand)) {
        if (Rules.inBounds(c.x, c.y)) Board.cell(gridEl, c.x, c.y).classList.add('preview-bad');
      }
      setTimeout(render, 250);
    }

    function changed() {
      syncPalette();
      if (onChange) onChange(allPlaced());
    }

    // --- public api ---------------------------------------------------------
    function getFleet() {
      return placed().map(s => ({ size: s.size, x: s.x, y: s.y, dir: s.dir }));
    }
    function lock() { locked = true; }

    // reset clears all placement state so the SAME instance (and its single set
    // of grid listeners) can be reused for a rematch — avoids stacking
    // listeners that creating a fresh Placement would cause.
    function reset() {
      for (const s of ships) { s.x = null; s.y = null; s.dir = 'h'; }
      selected = nextUnplaced();
      drag = null;
      lastTap = { id: -1, t: 0 };
      locked = false;
      buildPalette();
      render();
      changed();
    }

    gridEl.addEventListener('pointerdown', onDown);
    gridEl.addEventListener('pointermove', onMove);
    gridEl.addEventListener('pointerup', onUp);
    gridEl.addEventListener('pointercancel', () => { drag = null; render(); });

    buildPalette();
    selected = nextUnplaced(); // first ship pre-selected on load
    syncPalette();
    render();
    return { getFleet, lock, reset };
  }

  global.Placement = Placement;
})(window);
