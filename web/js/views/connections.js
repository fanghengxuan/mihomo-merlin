/* connections view — spec 4.6 */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  MM.views = MM.views || {};

  var rootEl = null;
  var refs = null;
  var active = false;
  var pollTimer = null;
  var renderTimer = null;
  var fetching = false;
  var errorMsg = '';
  var closing = {};
  var closeAllBusy = false;
  var skeletonTimer = null;
  var showSkel = false;

  function state() { return MM.store.state; }
  function conns() { return state().conns || {}; }

  function setText(node, text) {
    if (!node) return;
    var s = String(text == null ? '' : text);
    if (node.textContent !== s) node.textContent = s;
  }

  function clearKeyed(host) {
    if (!host) return;
    host.textContent = '';
    host.__mmKeyed = null;
  }

  /* ---------- mount ---------- */
  function mount(root) {
    rootEl = root;
    root.innerHTML =
      '<div class="conn-stats">' +
        '<div class="stat-tile"><div class="stat-label">连接数</div><div class="stat-value" data-c="count">0</div><div class="stat-sub">活动连接</div></div>' +
        '<div class="stat-tile"><div class="stat-label">累计上行</div><div class="stat-value" data-c="up">0 B</div><div class="stat-sub">当前连接</div></div>' +
        '<div class="stat-tile"><div class="stat-label">累计下行</div><div class="stat-value" data-c="down">0 B</div><div class="stat-sub">当前连接</div></div>' +
      '</div>' +
      '<div class="toolbar">' +
        '<label class="search">' + MM.icon('search') +
          '<input type="search" data-c="search" placeholder="搜索主机 / 规则 / 链路" aria-label="搜索连接" autocomplete="off" spellcheck="false">' +
          '<button class="clear hidden" type="button" data-c="clear" aria-label="清空搜索">' + MM.icon('close') + '</button>' +
        '</label>' +
        '<div class="toolbar-actions">' +
          '<button class="btn btn-text btn-small btn-destructive only-mobile" type="button" data-c="closeAll2">全部关闭</button>' +
        '</div>' +
      '</div>' +
      '<div class="toolbar-note" data-c="updated">—</div>' +
      '<div class="error-slot" data-c="errors"></div>' +
      '<div class="conn-note hidden" data-c="note"></div>' +
      '<div class="conn-table" data-c="table">' +
        '<div class="conn-head"><span>主机</span><span>规则</span><span>链路</span><span>流量</span><span>时长</span><span></span></div>' +
        '<div data-c="list"></div>' +
      '</div>' +
      '<div class="hidden" data-c="empty"></div>';

    refs = {
      count: root.querySelector('[data-c="count"]'),
      up: root.querySelector('[data-c="up"]'),
      down: root.querySelector('[data-c="down"]'),
      search: root.querySelector('[data-c="search"]'),
      clear: root.querySelector('[data-c="clear"]'),
      closeAll2: root.querySelector('[data-c="closeAll2"]'),
      updated: root.querySelector('[data-c="updated"]'),
      errors: root.querySelector('[data-c="errors"]'),
      note: root.querySelector('[data-c="note"]'),
      table: root.querySelector('[data-c="table"]'),
      list: root.querySelector('[data-c="list"]'),
      empty: root.querySelector('[data-c="empty"]')
    };

    var debounced = MM.debounce(function () {
      state().ui.connQuery = refs.search.value;
      refs.clear.classList.toggle('hidden', !refs.search.value);
      render();
    }, 120);

    refs.search.addEventListener('input', function () {
      refs.clear.classList.toggle('hidden', !refs.search.value);
      debounced();
    });
    refs.search.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && refs.search.value) {
        e.preventDefault();
        clearSearch();
      }
    });
    refs.clear.addEventListener('click', clearSearch);
    refs.closeAll2.addEventListener('click', closeAll);

    document.addEventListener('visibilitychange', function () {
      if (active && !document.hidden) tick();
    });
  }

  function clearSearch() {
    refs.search.value = '';
    state().ui.connQuery = '';
    refs.clear.classList.add('hidden');
    render();
    try { refs.search.focus(); } catch (e) { /* ignore */ }
  }

  function focusSearch() {
    try { refs.search.focus(); } catch (e) { /* ignore */ }
  }

  /* ---------- polling ---------- */
  function tick() {
    if (!active || document.hidden) return;
    fetchConns();
  }

  function startPoll() {
    stopPoll();
    tick();
    pollTimer = setInterval(tick, MM.theme.isLowEnd() ? 5000 : 2000);
  }

  function stopPoll() {
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
  }

  function fetchConns() {
    if (fetching) return;
    fetching = true;
    MM.sched.once('conns', function () {
      return MM.api('/api/connections?legacy=0');
    }).then(function (d) {
      errorMsg = '';
      var rows = (d && Array.isArray(d.rows)) ? d.rows : [];
      MM.store.set({
        conns: {
          rows: rows,
          count: Number(d && d.count) || rows.length,
          up: Number(d && d.up) || 0,
          down: Number(d && d.down) || 0,
          at: Date.now(),
          err: (d && d.err) || ''
        }
      });
      scheduleRender();
    }).catch(function (e) {
      errorMsg = (e && e.message) || '读取连接失败';
      scheduleRender();
    }).finally(function () {
      fetching = false;
    });
  }

  function scheduleRender() {
    if (renderTimer) return;
    renderTimer = setTimeout(function () {
      renderTimer = null;
      if (active) render();
    }, 300);
  }

  /* ---------- rendering ---------- */
  function render() {
    if (!refs) return;
    var c = conns();
    var known = !!state().status;
    var running = MM.store.running();
    var stopped = c.err === '内核未运行' || (known && !running);

    renderStats(c, stopped, known);
    renderError(c);
    setText(refs.updated, c.at ? '更新于 ' + MM.fmt.clock(c.at / 1000) : '—');
    refs.closeAll2.disabled = closeAllBusy;

    var all = c.rows || [];
    if (!c.at && !errorMsg) {
      hideTable();
      if (showSkel) showEmpty('skeleton');
      else { scheduleSkeleton(); showEmpty('loading'); }
      return;
    }
    if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
    showSkel = false;
    if (errorMsg && !all.length) {
      hideTable();
      showEmpty('error');
      return;
    }
    if (stopped) {
      hideTable();
      showEmpty('stopped');
      return;
    }

    var items = filtered(all);
    var limit = MM.theme.isLowEnd() ? 100 : 200;
    var shown = items.slice(0, limit);

    if (c.err && !all.length) {
      hideTable();
      showEmpty('error');
      return;
    }
    if (!shown.length) {
      hideTable();
      if (String(state().ui.connQuery || '').trim()) showEmpty('search');
      else showEmpty('empty');
      return;
    }

    showTable();
    if (items.length > limit) {
      refs.note.textContent = '仅显示前 ' + limit + ' 条 / 共 ' + items.length + ' 条';
      refs.note.classList.remove('hidden');
    } else {
      refs.note.classList.add('hidden');
    }
    MM.keyedList(refs.list, shown, function (r) { return r.id; }, createRow, updateRow, { tag: 'conns' });
  }

  function renderStats(c, stopped, known) {
    if (!known && !c.at) {
      setText(refs.count, '—');
      setText(refs.up, '—');
      setText(refs.down, '—');
      return;
    }
    setText(refs.count, String(stopped ? 0 : (Number(c.count) || 0)));
    setText(refs.up, stopped ? '0 B' : MM.fmt.bytes(c.up || 0));
    setText(refs.down, stopped ? '0 B' : MM.fmt.bytes(c.down || 0));
  }

  function renderError(c) {
    refs.errors.textContent = '';
    var msg = errorMsg || (c.err && (c.rows || []).length ? c.err : '');
    if (!msg) return;
    if (!(c.rows || []).length) return;
    if (c.at) msg += ' · 显示上次数据 ' + MM.fmt.clock(c.at / 1000);
    refs.errors.appendChild(MM.ui.errorBar(msg, function () {
      errorMsg = '';
      refs.errors.textContent = '';
      fetchConns();
    }));
  }

  function scheduleSkeleton() {
    if (skeletonTimer || showSkel) return;
    skeletonTimer = setTimeout(function () {
      skeletonTimer = null;
      if (!active) return;
      showSkel = true;
      render();
    }, 400);
  }

  function showTable() {
    refs.table.classList.remove('hidden');
    refs.empty.classList.add('hidden');
    refs.empty.textContent = '';
  }

  function hideTable() {
    refs.table.classList.add('hidden');
  }

  function showEmpty(kind) {
    refs.table.classList.add('hidden');
    var host = refs.empty;
    host.classList.remove('hidden');
    host.textContent = '';
    if (kind === 'skeleton') {
      host.appendChild(MM.ui.skeleton('list'));
      return;
    }
    var opts;
    if (kind === 'loading') {
      opts = { icon: 'conn', title: '正在读取连接…', text: '首次加载会稍等片刻' };
    } else if (kind === 'stopped') {
      opts = {
        icon: 'conn', title: '内核未运行', text: '启动内核后，活动连接会显示在这里',
        cta: { label: '去启动', onClick: function () { MM.router.go('/overview'); } }
      };
    } else if (kind === 'error') {
      opts = {
        kind: 'error', icon: 'warning', title: '暂时读不到连接数据',
        text: conns().err || errorMsg || '请稍后重试',
        cta: { label: '重试', onClick: function () { errorMsg = ''; fetchConns(); } }
      };
    } else if (kind === 'search') {
      opts = {
        icon: 'search', title: '没有匹配的连接', text: '换个关键词试试',
        cta: { label: '清空搜索', onClick: clearSearch }
      };
    } else {
      opts = { icon: 'conn', title: '暂无连接', text: '当前没有活动连接' };
    }
    host.appendChild(MM.ui.empty(opts));
  }

  function filtered(rows) {
    var q = String(state().ui.connQuery || '').trim().toLowerCase();
    var arr = rows.slice();
    if (q) {
      arr = arr.filter(function (r) {
        var text = (r.host || '') + ' ' + (r.dest || '') + ' ' + (r.rule || '') + ' ' +
          (r.rulePayload || '') + ' ' + ((r.chains || []).join(' ')) + ' ' + (r.network || '') + ' ' + (r.type || '');
        return text.toLowerCase().indexOf(q) >= 0;
      });
    }
    arr.sort(function (a, b) {
      return ((Number(b.download) || 0) + (Number(b.upload) || 0)) -
        ((Number(a.download) || 0) + (Number(a.upload) || 0));
    });
    return arr;
  }

  function durationOf(row) {
    if (!row.start) return '—';
    var t = Date.parse(row.start);
    if (!isFinite(t)) return '—';
    return MM.fmt.durClock((Date.now() - t) / 1000);
  }

  function createRow() {
    var row = document.createElement('div');
    row.className = 'conn-row';
    row.innerHTML =
      '<span class="conn-host"></span>' +
      '<span class="conn-rule"></span>' +
      '<span class="conn-chain"></span>' +
      '<span class="conn-flow"></span>' +
      '<span class="conn-dur"></span>' +
      '<button class="conn-close" type="button" aria-label="关闭连接" title="关闭连接">' + MM.icon('close') + '</button>';
    row.__r = {
      host: row.querySelector('.conn-host'),
      rule: row.querySelector('.conn-rule'),
      chain: row.querySelector('.conn-chain'),
      flow: row.querySelector('.conn-flow'),
      dur: row.querySelector('.conn-dur'),
      close: row.querySelector('.conn-close')
    };
    row.__r.close.addEventListener('click', function () {
      closeOne(row.__item);
    });
    return row;
  }

  function updateRow(row, data) {
    var r = row.__r;
    row.__item = data;
    var host = data.host || data.dest || '—';
    setText(r.host, host);
    r.host.title = host;
    var rule = data.rule || '';
    if (data.rulePayload) rule += ' · ' + data.rulePayload;
    setText(r.rule, rule || '—');
    r.rule.title = rule;
    var chain = (data.chains || []).join(' → ');
    if (!chain && data.type) chain = data.type;
    setText(r.chain, chain || '—');
    r.chain.title = chain;
    setText(r.flow, '↓ ' + MM.fmt.bytes(data.download) + '  ↑ ' + MM.fmt.bytes(data.upload));
    setText(r.dur, durationOf(data));
    var busy = !!closing[data.id];
    r.close.disabled = busy;
    r.close.setAttribute('aria-label', '关闭连接' + (host !== '—' ? ' ' + host : ''));
  }

  /* ---------- actions ---------- */
  function closeOne(item) {
    if (!item || !item.id || closing[item.id]) return;
    closing[item.id] = true;
    render();
    MM.api('/api/connections/' + encodeURIComponent(item.id), { method: 'DELETE' }).then(function () {
      MM.ui.toast('连接已关闭', 'ok');
      fetchConns();
    }).catch(function (e) {
      MM.ui.toast(e.message || '关闭连接失败', 'err');
    }).finally(function () {
      delete closing[item.id];
      if (active) render();
    });
  }

  function closeAll() {
    if (closeAllBusy) return;
    var c = conns();
    var count = Number(c.count) || (c.rows || []).length;
    if (!count) { MM.ui.toast('没有可关闭的连接', 'err'); return; }
    MM.ui.confirm({
      title: '关闭全部连接？',
      body: '将关闭当前 ' + count + ' 条连接，正在传输的会话会中断。',
      confirm: '全部关闭',
      destructive: true
    }).then(function (ok) {
      if (!ok) return;
      closeAllBusy = true;
      MM.ui.setBusy(refs.closeAll2, true);
      render();
      MM.api('/api/connections', { method: 'DELETE' }).then(function (d) {
        MM.ui.toast('已关闭 ' + (Number(d && d.closed) || 0) + ' 条连接', 'ok');
        fetchConns();
      }).catch(function (e) {
        MM.ui.toast(e.message || '关闭全部连接失败', 'err');
      }).finally(function () {
        closeAllBusy = false;
        MM.ui.setBusy(refs.closeAll2, false);
        if (active) render();
      });
    });
  }

  /* ---------- lifecycle ---------- */
  function activate(ctx, sec) {
    if (!rootEl) rootEl = sec;
    if (!refs) return;
    active = true;
    render();
    startPoll();
  }

  function deactivate() {
    active = false;
    stopPoll();
    if (renderTimer) { clearTimeout(renderTimer); renderTimer = null; }
    if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
    showSkel = false;
  }

  MM.views.connections = {
    title: function () { return MM.router.TITLES.connections; },
    mount: mount,
    activate: activate,
    deactivate: deactivate,
    actions: function () {
      return [
        { icon: 'search', label: '搜索', desktopOnly: true, onClick: focusSearch },
        {
          icon: 'close', label: '全部关闭', showLabel: true, destructive: true,
          desktopOnly: true, onClick: closeAll
        }
      ];
    }
  };
})();
