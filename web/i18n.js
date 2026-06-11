// i18n.js — minimal translation layer for the web client. Exposes a global
// `I18n` with t(key, params), get()/set(lang), and the supported LANGS. The
// active language is chosen from localStorage (`sb_lang`), else the browser
// preference, else English. {placeholders} in strings are filled from params.
(function (global) {
  'use strict';

  const DICT = {
    en: {
      appTitle: 'Sea Battle',
      yourName: 'Your name',
      captain: 'Captain',
      join: 'Join',
      newGame: 'New Game',
      vs: 'vs',
      me: 'You',
      changeName: 'Click to change your name',
      opponentStatus: 'opponent status',
      enemyWaters: 'Enemy waters',
      yourFleet: 'Your fleet',
      start: 'Start',
      placeHint: 'Tap a ship, then the grid to place. Drag to move, double-tap to rotate.',
      language: 'Language',
      connecting: 'Connecting…',
      reconnecting: 'Reconnecting…',
      waitingOpponent: 'Waiting for opponent…',
      waitingOpponentJoin: 'Waiting for an opponent to join…',
      gameFull: 'Game is full — two players already connected.',
      opponentDisconnected: 'Opponent disconnected — waiting for them to return…',
      opponentOffline: 'Opponent is offline — wait for them to return.',
      placeShips: 'Place your ships, then press Start.',
      fleetReady: 'Fleet ready — waiting for opponent…',
      yourTurnFire: 'Your turn — fire!',
      opponentTurn: "Opponent's turn…",
      youWin: 'YOU WIN',
      youLose: 'YOU LOSE',
      opponentWantsRematch: 'Opponent wants a rematch!',
      invalidLayout: 'Invalid layout: {error}',
      serverRejected: 'Server rejected fleet: {error} — adjust and press Start.',
      errorPrefix: 'Error: {code}',
      rule_count: 'wrong ship count',
      rule_invalid: 'ships overlap, touch, or leave the board',
    },
    uk: {
      appTitle: 'Морський бій',
      yourName: "Ваше ім'я",
      captain: 'Капітан',
      join: 'Увійти',
      newGame: 'Нова гра',
      vs: 'проти',
      me: 'Ви',
      changeName: "Натисніть, щоб змінити ім'я",
      opponentStatus: 'статус суперника',
      enemyWaters: 'Води суперника',
      yourFleet: 'Ваш флот',
      start: 'Почати',
      placeHint: 'Торкніться корабля, потім поля для розміщення. Перетягуйте для переміщення, подвійний дотик — поворот.',
      language: 'Мова',
      connecting: 'Підключення…',
      reconnecting: 'Перепідключення…',
      waitingOpponent: 'Очікування суперника…',
      waitingOpponentJoin: 'Очікування приєднання суперника…',
      gameFull: 'Гру заповнено — вже підключені двоє гравців.',
      opponentDisconnected: 'Суперник відключився — очікування повернення…',
      opponentOffline: 'Суперник офлайн — зачекайте на його повернення.',
      placeShips: 'Розставте кораблі, потім натисніть «Почати».',
      fleetReady: 'Флот готовий — очікування суперника…',
      yourTurnFire: 'Ваш хід — вогонь!',
      opponentTurn: 'Хід суперника…',
      youWin: 'ВИ ПЕРЕМОГЛИ',
      youLose: 'ВИ ПРОГРАЛИ',
      opponentWantsRematch: 'Суперник хоче реванш!',
      invalidLayout: 'Невірне розташування: {error}',
      serverRejected: 'Сервер відхилив флот: {error} — виправте і натисніть «Почати».',
      errorPrefix: 'Помилка: {code}',
      rule_count: 'невірна кількість кораблів',
      rule_invalid: 'кораблі перетинаються, торкаються або виходять за поле',
    },
  };

  const LANGS = ['en', 'uk'];
  let lang = 'en';

  function detect() {
    const stored = localStorage.getItem('sb_lang');
    if (stored && DICT[stored]) return stored;
    const navs = navigator.languages || [navigator.language || ''];
    for (const l of navs) {
      const code = (l || '').toLowerCase();
      if (code.startsWith('uk')) return 'uk';
      if (code.startsWith('en')) return 'en';
    }
    return 'en';
  }

  function t(key, params) {
    let s = (DICT[lang] && DICT[lang][key]) || DICT.en[key] || key;
    if (params) {
      for (const k in params) s = s.replace('{' + k + '}', params[k]);
    }
    return s;
  }

  function get() { return lang; }

  function set(l) {
    if (!DICT[l]) return;
    lang = l;
    localStorage.setItem('sb_lang', l);
    document.documentElement.lang = l;
  }

  set(detect());

  global.I18n = { t, get, set, LANGS };
})(window);
