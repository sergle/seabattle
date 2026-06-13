// app.js — WebSocket client and view controller. Renders purely from server
// messages (protocol.md); holds no authoritative game state. On reconnect the
// single `state` snapshot rebuilds the whole UI (ui.md §8). All user-facing
// text goes through I18n (i18n.js); switching language re-renders in place.
(function () {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const t = (key, params) => I18n.t(key, params);

  // haptic buzzes via the Vibration API (Android Chrome/Firefox). Number =
  // duration ms; array = on/off pattern. No-op where unsupported (e.g. iOS).
  const haptic = (pattern) => { if (navigator.vibrate) navigator.vibrate(pattern); };
  const ownGrid = $('ownGrid');
  const enemyGrid = $('enemyGrid');
  const statusEl = $('status');

  Board.build(ownGrid);
  Board.build(enemyGrid);

  let ws = null;
  let you = -1;
  let phase = 'connecting';
  let yourTurn = false;
  let placement = null;
  let committed = false;

  // i18n render state — remembered so a language switch can re-render text that
  // was produced by an event rather than static markup.
  let curStatus = { key: 'connecting', params: null, cls: '' };
  let myNameReal = null;   // null until a name is known → falls back to t('me')
  let oppNameReal = null;  // null → falls back to t('waitingOpponent')
  let rematchKey = '';     // '' = no rematch status line shown
  let finishedWin = null;  // bool when an overlay result is showing

  // --- placement controller --------------------------------------------------
  function newPlacement() {
    committed = false;
    // Reuse the single Placement instance across games; creating a fresh one
    // would stack grid listeners (place.js attaches to the persistent grid el).
    if (!placement) {
      placement = Placement(ownGrid, $('palette'), (allPlaced) => {
        $('startBtn').disabled = !allPlaced;
      });
    } else {
      placement.reset();
    }
    $('startBtn').disabled = true;
  }

  $('startBtn').addEventListener('click', () => {
    if (!placement) return;
    const fleet = placement.getFleet();
    const check = Rules.validateFleet(fleet);
    if (!check.ok) { setStatus('invalidLayout', { error: t(check.error) }); return; }
    send({ type: 'place', ships: fleet });
    $('startBtn').disabled = true;
    setStatus('waitingOpponent');
  });

  // --- rematch ---------------------------------------------------------------
  $('rematchBtn').addEventListener('click', () => {
    if (phase !== 'finished') return;
    send({ type: 'rematch' });
    $('rematchBtn').disabled = true;
    setRematchStatus('waitingOpponent');
  });

  // --- language selector -----------------------------------------------------
  $('langSel').addEventListener('change', (ev) => {
    I18n.set(ev.target.value);
    applyI18n();
  });

  // --- enemy grid firing -----------------------------------------------------
  enemyGrid.addEventListener('click', (ev) => {
    if (phase !== 'playing' || !yourTurn) return;
    const el = ev.target;
    if (!el.classList.contains('cell')) return;
    if (el.classList.contains('hit') || el.classList.contains('miss') || el.classList.contains('sunk')) return;
    yourTurn = false;
    enemyGrid.classList.remove('active');
    send({ type: 'fire', x: +el.dataset.x, y: +el.dataset.y });
  });

  // --- websocket -------------------------------------------------------------
  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    ws = new WebSocket(`${proto}://${location.host}/ws`);
    ws.onopen = () => {
      const name = localStorage.getItem('sb_name');
      if (name) send({ type: 'join', name });
      else showNameModal();
    };
    ws.onmessage = (e) => handle(JSON.parse(e.data));
    ws.onclose = () => {
      curStatus = { key: 'reconnecting', params: null, cls: 'offline' }; // lost → red
      applyStatus();
      setTimeout(connect, 1000);
    };
  }

  function send(obj) {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj));
  }

  // --- message handlers ------------------------------------------------------
  function handle(m) {
    switch (m.type) {
      case 'assigned': you = m.player; setMyName(m.name); break;
      case 'full': setStatus('gameFull'); break;
      case 'opponent': onOpponent(m); break;
      case 'state': applyState(m); break;
      case 'placeResult': onPlaceResult(m); break;
      case 'gameStart': onGameStart(m); break;
      case 'fireResult': onFireResult(m); break;
      case 'gameOver': onGameOver(m); break;
      case 'rematch': onRematch(m); break;
      case 'error': onError(m); break;
    }
  }

  function onOpponent(m) {
    setOppName(m.name && m.name.length ? m.name : null);
    $('oppDot').className = 'dot ' + (m.online ? 'on' : 'off');
    if (m.online && phase === 'waiting') {
      // WAITING→PLACING cue: opponent arrived (protocol.md §6).
      phase = 'placing';
      showPlacing();
    } else if (!m.online && phase === 'playing') {
      setStatus('opponentDisconnected');
    }
  }

  function onPlaceResult(m) {
    if (m.ok) {
      committed = true;
      if (placement) placement.lock();
      setStatus('waitingOpponent');
    } else {
      setStatus('serverRejected', { error: m.error || '' });
      $('startBtn').disabled = false;
    }
  }

  function onGameStart(m) {
    phase = 'playing';
    yourTurn = !!m.yourTurn;
    showPlaying();
    setTurn();
  }

  function onFireResult(m) {
    const mine = m.by === you;
    const grid = mine ? enemyGrid : ownGrid;
    if (m.outcome === 'sunk') {
      for (const c of (m.sunkCells || [])) Board.mark(grid, c.x, c.y, 'sunk');
      // shooter sees revealed water as misses; on my own board it's the no-go buffer
      const revealKind = mine ? 'miss' : 'nogo';
      for (const c of (m.revealed || [])) Board.mark(grid, c.x, c.y, revealKind);
    } else {
      Board.mark(grid, m.x, m.y, m.outcome); // 'hit' | 'miss'
    }
    yourTurn = (m.nextTurn === you);

    // haptics (mobile): your shot vs incoming shot get distinct feedback.
    if (m.outcome === 'sunk') haptic(mine ? [60, 40, 120] : 200);
    else if (m.outcome === 'hit') haptic(mine ? 50 : 30);
    else if (!mine && yourTurn) haptic(20); // opponent missed → your turn cue

    if (phase === 'playing') setTurn();
  }

  function onGameOver(m) {
    phase = 'finished';
    enemyGrid.classList.remove('active');
    haptic(m.winner === you ? [80, 40, 80, 40, 120] : 300);
    showResult(m.winner === you);
  }

  function onRematch(m) {
    // Both-agree flow: a status frame arrives until both sides request a
    // rematch, at which point the server resets and a `state` (placing) lands.
    if (m.youReady) {
      $('rematchBtn').disabled = true;
      setRematchStatus('waitingOpponent');
    } else if (m.opponentReady) {
      $('rematchBtn').disabled = false;
      setRematchStatus('opponentWantsRematch');
    }
  }

  function onError(m) {
    if (m.code === 'opponent_offline') setStatus('opponentOffline');
    else if (m.code === 'not_your_turn') { /* ignore stray taps */ }
    else setStatus('errorPrefix', { code: m.code });
    // a rejected fire returns the turn to us
    if (phase === 'playing' && m.code !== 'opponent_offline') { yourTurn = true; setTurn(); }
  }

  // --- full snapshot (initial + reconnect) -----------------------------------
  function applyState(m) {
    phase = m.phase;
    you = m.you;
    yourTurn = !!m.yourTurn;
    $('overlay').classList.add('hidden'); // re-shown only in the finished case
    finishedWin = null;
    setOppName(m.opponentName || null);
    $('oppDot').className = 'dot ' + (m.opponentOnline ? 'on' : 'off');

    Board.clearMarks(ownGrid);
    Board.clearMarks(enemyGrid);
    const own = m.ownBoard || {};
    if (own.ships && own.ships.length) Board.renderShips(ownGrid, own.ships);
    for (const s of (own.incoming || [])) Board.mark(ownGrid, s.x, s.y, s.outcome);
    for (const s of (m.enemyShots || [])) Board.mark(enemyGrid, s.x, s.y, s.outcome);

    switch (phase) {
      case 'waiting':
        showPlacing();
        setStatus('waitingOpponentJoin');
        break;
      case 'placing':
        if (m.youReady) { committed = true; showWaitingReady(); }
        else showPlacing();
        break;
      case 'playing':
        showPlaying();
        setTurn();
        break;
      case 'finished':
        showResult(m.winner === you);
        break;
    }
  }

  // --- screens ---------------------------------------------------------------
  function showPlacing() {
    $('enemyTitle').classList.remove('hidden');
    ownGrid.classList.remove('compact');
    $('enemySection').classList.add('hidden');
    $('ownSection').classList.remove('hidden');
    $('placeSection').classList.remove('hidden');
    if (!placement || committed) newPlacement();
    if (phase === 'placing') setStatus('placeShips');
  }
  function showWaitingReady() {
    $('placeSection').classList.add('hidden');
    $('enemySection').classList.add('hidden');
    setStatus('fleetReady');
  }
  function showPlaying() {
    Board.clearNogo(ownGrid); // hide placement buffer; nogo returns only on a sunk ship
    $('enemyTitle').classList.add('hidden');
    ownGrid.classList.add('compact');
    $('placeSection').classList.add('hidden');
    $('enemySection').classList.remove('hidden');
    $('ownSection').classList.remove('hidden');
  }
  function showResult(win) {
    finishedWin = win;
    const ov = $('overlay');
    ov.classList.remove('hidden', 'win', 'lose');
    ov.classList.add(win ? 'win' : 'lose');
    $('overlayText').textContent = t(win ? 'youWin' : 'youLose');
    $('rematchBtn').disabled = false;
    setRematchStatus('');
  }

  // --- status / labels (i18n-aware) ------------------------------------------
  function applyStatus() {
    statusEl.className = 'status' + (curStatus.cls ? ' ' + curStatus.cls : '');
    const prefix = curStatus.prefix ? t(curStatus.prefix) + ' · ' : '';
    statusEl.textContent = prefix + t(curStatus.key, curStatus.params);
  }
  function setStatus(key, params) { curStatus = { key, params, cls: '' }; applyStatus(); }

  function setTurn() {
    enemyGrid.classList.toggle('active', yourTurn);
    curStatus = yourTurn
      ? { key: 'yourTurnFire', params: null, cls: 'your-turn', prefix: 'enemyWaters' }
      : { key: 'opponentTurn', params: null, cls: 'their-turn', prefix: 'enemyWaters' };
    applyStatus();
  }

  function setRematchStatus(key) { rematchKey = key; renderRematchStatus(); }
  function renderRematchStatus() { $('rematchStatus').textContent = rematchKey ? t(rematchKey) : ''; }

  function setMyName(name) { if (name) myNameReal = name; renderMyName(); }
  function renderMyName() { $('myName').textContent = myNameReal || t('me'); }

  function setOppName(name) { oppNameReal = name || null; renderOppName(); }
  function renderOppName() { $('oppName').textContent = oppNameReal || t('waitingOpponent'); }

  // applyI18n re-renders every piece of user-facing text for the active language.
  function applyI18n() {
    document.title = t('appTitle');
    $('modalTitle').textContent = t('appTitle');
    $('nameLabel').textContent = t('yourName');
    $('nameInput').placeholder = t('captain');
    $('joinBtn').textContent = t('join');
    $('rematchBtn').textContent = t('newGame');
    $('vsLabel').textContent = t('vs');
    $('myName').title = t('changeName');
    $('oppDot').title = t('opponentStatus');
    $('enemyTitle').textContent = t('enemyWaters');
    $('ownTitle').textContent = t('yourFleet');
    $('startBtn').textContent = t('start');
    $('placeHint').textContent = t('placeHint');
    applyStatus();
    renderMyName();
    renderOppName();
    renderRematchStatus();
    if (finishedWin !== null) $('overlayText').textContent = t(finishedWin ? 'youWin' : 'youLose');
    updateLangSelector();
  }
  function updateLangSelector() {
    $('langSel').value = I18n.get();
  }

  // --- name modal ------------------------------------------------------------
  // First visit → join. Click on own name → rename (live; opponent is notified).
  $('myName').addEventListener('click', () => showNameModal({ rename: true }));

  function showNameModal(opts) {
    const rename = opts && opts.rename;
    const modal = $('nameModal');
    const input = $('nameInput');
    input.value = localStorage.getItem('sb_name') || '';
    modal.classList.remove('hidden');
    input.focus();
    const submit = () => {
      const name = input.value.trim();
      if (!name) return;
      localStorage.setItem('sb_name', name);
      modal.classList.add('hidden');
      setMyName(name);
      send({ type: rename ? 'rename' : 'join', name });
    };
    $('joinBtn').onclick = submit;
    input.onkeydown = (e) => { if (e.key === 'Enter') submit(); };
  }

  setMyName(localStorage.getItem('sb_name'));
  setStatus('connecting');
  applyI18n();
  newPlacement();
  connect();
})();
