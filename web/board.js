// board.js — render a 10×10 grid and mark cells. Pure DOM helpers; no game
// logic. Both the own-fleet and enemy-waters grids are built with this.
(function (global) {
  'use strict';
  const SIZE = 10;

  // build fills `el` with 100 .cell divs carrying data-x/data-y.
  function build(el) {
    el.innerHTML = '';
    for (let y = 0; y < SIZE; y++) {
      for (let x = 0; x < SIZE; x++) {
        const c = document.createElement('div');
        c.className = 'cell';
        c.dataset.x = x;
        c.dataset.y = y;
        el.appendChild(c);
      }
    }
  }

  function cell(el, x, y) {
    return el.children[y * SIZE + x];
  }

  function cellFromEvent(el, ev) {
    const t = document.elementFromPoint(ev.clientX, ev.clientY);
    if (!t || !t.classList.contains('cell') || t.parentElement !== el) return null;
    return { x: +t.dataset.x, y: +t.dataset.y, el: t };
  }

  // clearMarks removes shot/ship/preview classes (keeps the grid structure).
  function clearMarks(el) {
    for (const c of el.children) {
      c.className = 'cell';
    }
  }

  function clearPreview(el) {
    for (const c of el.children) c.classList.remove('preview-ok', 'preview-bad');
  }

  function mark(el, x, y, kind) {
    if (!Rules.inBounds(x, y)) return;
    cell(el, x, y).classList.add(kind);
  }

  // renderShips paints own-board ship cells plus their no-go shading.
  function renderShips(el, ships) {
    for (const s of ships) {
      for (const c of Rules.cellsOf(s)) {
        const cl = cell(el, c.x, c.y);
        cl.classList.add('ship');
        cl.dataset.ship = '1';
      }
    }
  }

  global.Board = { build, cell, cellFromEvent, clearMarks, clearPreview, mark, renderShips };
})(window);
