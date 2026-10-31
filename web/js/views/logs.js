/* logs view — spec 4.7 */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  MM.views = MM.views || {};

  var rootEl = null;
  var refs = null;
  var active = false;
  var paused = false;
  var autoScroll = true;
  var stickToBottom = true;
  var newCount = 0;
  var entries = [];
  var pending = [];
  var searchTerm = '';
  var searchDebounced = null;

  var LEVELS = [
    { label: 'debug', value: 'debug', note: '最详细，含连接细节' },
    { label: 'info', value: 'info', note: '默认' },
    { label: 'warning', value: 'warning' },
    { label: 'error', value: 'error' }
  ];

  function state() { return MM.store.state; }
  function maxLines() { return MM.theme.isLowEnd() ? 300 : 500; }

  function setText(node, text) {
    if (!node) return;
    var s = String(text == null ? '' : text);
    if (node.textContent !== s) node.textContent = s;
  }

  function currentLevel() {
    return (state().settings && state().settings.logLevel) || 'info';
  }

  function typeClass(type) {
    var t = String(type || 'info').toLowerCase();
    if (t === 'warning' || t === 'error' || t === 'debug') return t;
    return 'info';
  }

  /* ---------- mount ---------- */
  function mount(root) {
    rootEl = root;
    root.innerHTML =
      '<div class="logs-shell">' +
        '<div class="logs-toolbar">' +
          '<label class="search">' + MM.icon('search') +
            '<input type="search" data-l="search" placeholder="搜索日志" aria-label="搜索日志" autocomplete="off" spellcheck="false">' +
          '</label>' +
          '<button class="log-toggle on" type="button" data-l="auto" aria-pressed="true"><span class="dot"></span>自动滚动</button>' +
          '<span class="logs-status" data-l="status"><span class="dot"></span><span data-l="statusText">连接中…</span></span>' +
          '<span class="log-paused hidden" data-l="paused">已暂停</span>' +
          '<span class="log-new-count" data-l="newCount"></span>' +
        '</div>' +
        '<div class="log-viewport-wrap">' +
          '<div class="log-viewport" data-l="view" role="log" aria-live="off"></div>' +
          '<div class="logs-empty" data-l="empty">等待日志…</div>' +
          '<button class="log-new hidden" type="button" data-l="latest">回到最新</button>' +
        '</div>' +
      '</div>';

    refs = {
      search: root.querySelector('[data-l="search"]'),
      auto: root.querySelector('[data-l="auto"]'),
      status: root.querySelector('[data-l="status"]'),
      statusText: root.querySelector('[data-l="statusText"]'),
      paused: root.querySelector('[data-l="paused"]'),
      newCount: root.querySelector('[data-l="newCount"]'),
      view: root.querySelector('[data-l="view"]'),
      empty: root.querySelector('[data-l="empty"]'),
      latest: root.querySelector('[data-l="latest"]')
    };

    searchDebounced = MM.debounce(function () {
      searchTerm = String(refs.search.value || '').trim().toLowerCase();
      applyFilter();
    }, 120);
    refs.search.addEventListener('input', searchDebounced);

    refs.auto.addEventListener('click', toggleAutoScroll);
    refs.latest.addEventListener('click', function () {
      stickToBottom = true;
      scrollBottom();
      updateNewest();
    });
    refs.view.addEventListener('scroll', function () {
      stickToBottom = atBottom();
      updateNewest();
    });

    refs.auto.classList.toggle('on', autoScroll);
    refs.auto.setAttribute('aria-pressed', autoScroll ? 'true' : 'false');
    updateEmpty();
  }

  /* ---------- stream ---------- */
  function openStream() {
    MM.sched.sse('logs', '/api/logs/stream', {
      onMessage: onMessage,
      onStatus: setStatus
    });
    setStatus(MM.sched.streamStatus('logs'));
  }

  function closeStream() {
    MM.sched.closeSse('logs');
  }

  function setStatus(s) {
    if (!refs) return;
    refs.status.className = 'logs-status';
    var text = '连接中…';
    if (s === 'ok') { refs.status.classList.add('ok'); text = '已连接'; }
    else if (s === 'reconnecting') { refs.status.classList.add('warn'); text = '重连中'; }
    else if (s === 'connecting') { refs.status.classList.add('warn'); text = '连接中…'; }
    else { refs.status.classList.add('err'); text = '已断开'; }
    setText(refs.statusText, text);
  }

  function onMessage(ev) {
    if (!ev) return;
    if (paused) {
      pending.push(ev);
      while (pending.length > maxLines()) pending.shift();
      newCount = pending.length;
      updatePausedUI();
      return;
    }
    appendEntry(ev);
  }

  /* ---------- rendering ---------- */
  function appendEntry(ev) {
    if (!refs) return;
    var type = String(ev.type || 'info');
    var payload = String(ev.payload == null ? '' : ev.payload);
    var label = type;
    var msg = payload;
    if (payload.indexOf('[面板] ') === 0) {
      label = '面板';
      msg = payload.slice(5);
    }
    var el = document.createElement('div');
    el.className = 'log-line ' + typeClass(type);
    el.innerHTML = '<span class="log-time"></span><span class="log-type"></span><span class="log-msg"></span>';
    el.querySelector('.log-time').textContent = MM.fmt.clock(Number(ev.time) || Math.floor(Date.now() / 1000));
    el.querySelector('.log-type').textContent = '[' + label + ']';
    el.querySelector('.log-msg').textContent = msg;

    var text = (label + ' ' + msg).toLowerCase();
    if (searchTerm && text.indexOf(searchTerm) < 0) el.classList.add('hidden');
    refs.view.appendChild(el);
    entries.push({ el: el, text: text, type: type });

    while (entries.length > maxLines()) {
      var old = entries.shift();
      if (old.el.parentNode) old.el.parentNode.removeChild(old.el);
    }
    updateEmpty();
    if (autoScroll && stickToBottom) {
      scrollBottom();
    } else {
      stickToBottom = atBottom();
    }
    updateNewest();
  }

  function applyFilter() {
    entries.forEach(function (e) {
      e.el.classList.toggle('hidden', !!searchTerm && e.text.indexOf(searchTerm) < 0);
    });
    updateEmpty();
    updateNewest();
  }

  function updateEmpty() {
    if (!refs) return;
    var visible = 0;
    entries.forEach(function (e) {
      if (!e.el.classList.contains('hidden')) visible++;
    });
    if (!entries.length) {
      refs.empty.classList.remove('hidden');
      setText(refs.empty, paused ? '已暂停，等待继续' : '等待日志…');
    } else if (!visible) {
      refs.empty.classList.remove('hidden');
      setText(refs.empty, '没有匹配的日志');
    } else {
      refs.empty.classList.add('hidden');
    }
  }

  function scrollBottom() {
    if (!refs) return;
    refs.view.scrollTop = refs.view.scrollHeight;
  }

  function atBottom() {
    if (!refs) return true;
    var v = refs.view;
    return v.scrollHeight - v.scrollTop - v.clientHeight <= 24;
  }

  function updateNewest() {
    if (!refs) return;
    var show = !stickToBottom && entries.length > 0;
    refs.latest.classList.toggle('hidden', !show);
  }

  function updatePausedUI() {
    if (!refs) return;
    refs.paused.classList.toggle('hidden', !paused);
    setText(refs.newCount, paused && newCount > 0 ? '已暂停 · ' + newCount + ' 条新日志' : '');
    updateEmpty();
  }

  /* ---------- actions ---------- */
  function toggleAutoScroll() {
    autoScroll = !autoScroll;
    refs.auto.classList.toggle('on', autoScroll);
    refs.auto.setAttribute('aria-pressed', autoScroll ? 'true' : 'false');
    if (autoScroll) {
      stickToBottom = true;
      scrollBottom();
    }
    updateNewest();
  }

  function togglePause() {
    if (paused) {
      paused = false;
      var list = pending.slice();
      pending = [];
      newCount = 0;
      list.forEach(appendEntry);
      if (autoScroll) { stickToBottom = true; scrollBottom(); }
    } else {
      paused = true;
      newCount = pending.length;
    }
    updatePausedUI();
    updateNewest();
    MM.router.refreshActions();
  }

  function clearLogs() {
    refs.view.textContent = '';
    entries = [];
    pending = [];
    newCount = 0;
    updateEmpty();
    updateNewest();
    MM.ui.toast('已清屏', 'ok');
  }

  function openLevel() {
    MM.ui.options({
      title: '日志级别',
      value: currentLevel(),
      items: LEVELS
    }).then(function (v) {
      if (!v || v === currentLevel()) return;
      setLevel(v);
    });
  }

  function setLevel(level) {
    var prev = currentLevel();
    var merged = Object.assign({}, state().settings || {}, { logLevel: level });
    MM.store.set({ settings: merged });
    MM.router.refreshActions();
    MM.api('/api/settings', { method: 'PUT', body: { logLevel: level } }).then(function (d) {
      if (d && d.settings) {
        MM.store.set({ settings: Object.assign({}, merged, d.settings) });
      }
      MM.ui.toast('日志级别已切换', 'ok');
    }).catch(function (e) {
      var cur = Object.assign({}, state().settings || {}, { logLevel: prev });
      MM.store.set({ settings: cur });
      MM.router.refreshActions();
      MM.ui.toast(e.message || '切换日志级别失败', 'err');
    });
  }

  /* ---------- lifecycle ---------- */
  function activate(ctx, sec) {
    if (!rootEl) rootEl = sec;
    if (!refs) return;
    active = true;
    setStatus(MM.sched.streamStatus('logs'));
    openStream();
    if (!state().settings) {
      MM.data.loadSettings().then(function () { MM.router.refreshActions(); }, function () { /* ignore */ });
    }
    updatePausedUI();
    updateNewest();
    MM.router.refreshActions();
  }

  function deactivate() {
    active = false;
    closeStream();
    if (searchDebounced) searchDebounced.cancel();
  }

  MM.views.logs = {
    title: function () { return MM.router.TITLES.logs; },
    mount: mount,
    activate: activate,
    deactivate: deactivate,
    actions: function () {
      return [
        { label: currentLevel(), showLabel: true, onClick: openLevel },
        {
          icon: paused ? 'play' : 'pause',
          label: paused ? '继续' : '暂停',
          onClick: togglePause
        },
        { icon: 'trash', label: '清屏', onClick: clearLogs }
      ];
    }
  };
})();
