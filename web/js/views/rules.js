/* rules view — spec 4.5 */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  MM.views = MM.views || {};

  var rootEl = null;
  var refs = null;
  var active = false;
  var loading = false;
  var loadingPromise = null;
  var loadedAt = 0;
  var errorMsg = '';
  var updating = false;
  var skeletonTimer = null;
  var showSkel = false;
  var renderToken = 0;

  function state() { return MM.store.state; }

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
      '<div class="rules-meta">' +
        '<span class="count" data-r="count">生效规则 — / —</span>' +
        '<button class="link-btn" type="button" data-r="ruleset">规则源：— →</button>' +
        '<button class="btn btn-text btn-small" type="button" data-r="update">更新规则源</button>' +
      '</div>' +
      '<div class="toolbar rule-toolbar">' +
        '<label class="search">' + MM.icon('search') +
          '<input type="search" data-r="search" placeholder="搜索类型 / 内容 / 策略" aria-label="搜索规则" autocomplete="off" spellcheck="false">' +
          '<button class="clear hidden" type="button" data-r="clear" aria-label="清空搜索">' + MM.icon('close') + '</button>' +
        '</label>' +
        '<div class="toolbar-actions">' +
          '<button class="btn btn-tonal btn-small" type="button" data-r="filter">全部策略</button>' +
        '</div>' +
      '</div>' +
      '<div class="error-slot" data-r="errors"></div>' +
      '<div class="rule-table" data-r="table">' +
        '<div class="rule-head"><span>类型</span><span>内容</span><span>策略</span></div>' +
        '<div data-r="list"></div>' +
      '</div>' +
      '<div class="hidden" data-r="empty"></div>';

    refs = {
      count: root.querySelector('[data-r="count"]'),
      ruleset: root.querySelector('[data-r="ruleset"]'),
      update: root.querySelector('[data-r="update"]'),
      search: root.querySelector('[data-r="search"]'),
      clear: root.querySelector('[data-r="clear"]'),
      filter: root.querySelector('[data-r="filter"]'),
      errors: root.querySelector('[data-r="errors"]'),
      table: root.querySelector('[data-r="table"]'),
      list: root.querySelector('[data-r="list"]'),
      empty: root.querySelector('[data-r="empty"]')
    };

    var debounced = MM.debounce(function () {
      state().ui.ruleQuery = refs.search.value;
      refs.clear.classList.toggle('hidden', !refs.search.value);
      renderList();
    }, 120);

    refs.search.addEventListener('input', function () {
      refs.clear.classList.toggle('hidden', !refs.search.value);
      debounced();
    });
    refs.search.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && refs.search.value) {
        e.preventDefault();
        clearFilters();
      }
    });
    refs.clear.addEventListener('click', clearFilters);
    refs.filter.addEventListener('click', openFilter);
    refs.ruleset.addEventListener('click', function () { MM.router.go('/settings/ruleset'); });
    refs.update.addEventListener('click', updateRuleSet);
  }

  function clearFilters() {
    refs.search.value = '';
    state().ui.ruleQuery = '';
    state().ui.ruleFilter = '';
    refs.clear.classList.add('hidden');
    updateFilterLabel();
    renderList();
    try { refs.search.focus(); } catch (e) { /* ignore */ }
  }

  /* ---------- data ---------- */
  function refresh() {
    if (loadingPromise) return loadingPromise;
    loading = true;
    startSkeleton();
    loadingPromise = Promise.allSettled([
      loadRules(),
      MM.data.loadRuleSet().catch(function () { /* 规则源失败不阻塞规则列表 */ })
    ]).then(function () {
      loading = false;
      if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
      showSkel = false;
      if (active) { render(); MM.router.refreshActions(); }
    });
    return loadingPromise;
  }

  function loadRules() {
    return MM.sched.once('rules', function () {
      return MM.api('/api/rules');
    }).then(function (d) {
      var rules = (d && Array.isArray(d.rules)) ? d.rules : [];
      MM.store.set({ rules: rules, rulesAt: Date.now() });
      loadedAt = Date.now();
      errorMsg = '';
      if (refs) refs.errors.textContent = '';
    }).catch(function (e) {
      errorMsg = (e && e.message) || '读取规则失败';
      renderError();
    });
  }

  function startSkeleton() {
    if ((state().rules || []).length) return;
    if (skeletonTimer) clearTimeout(skeletonTimer);
    skeletonTimer = setTimeout(function () {
      skeletonTimer = null;
      if (loading && !(state().rules || []).length) {
        showSkel = true;
        if (active) render();
      }
    }, 400);
  }

  function renderError() {
    if (!refs) return;
    refs.errors.textContent = '';
    if (!errorMsg) return;
    if (!(state().rules || []).length) return;
    var msg = errorMsg;
    if (loadedAt) msg += ' · 显示上次数据 ' + MM.fmt.clock(loadedAt / 1000);
    refs.errors.appendChild(MM.ui.errorBar(msg, function () {
      errorMsg = '';
      refs.errors.textContent = '';
      return refresh();
    }));
  }

  /* ---------- rendering ---------- */
  function render() {
    if (!refs) return;
    renderError();
    renderMeta();
    renderList();
  }

  function presetName() {
    var rs = state().ruleset;
    if (!rs) return '—';
    var id = rs.preset || '';
    if (id === 'custom') return '自定义地址';
    var presets = rs.presets || [];
    for (var i = 0; i < presets.length; i++) {
      if (presets[i].id === id) return presets[i].name || id;
    }
    return id || '—';
  }

  function renderMeta() {
    var rs = state().ruleset;
    var name = presetName();
    setText(refs.ruleset, '规则源：' + name + ' →');
    if (rs && rs.meta) {
      var bits = [];
      if (rs.meta.count) bits.push(Number(rs.meta.count) + ' 条');
      if (rs.meta.bytes) bits.push(MM.fmt.bytes(rs.meta.bytes));
      if (rs.meta.updatedAt) bits.push('更新于 ' + MM.fmt.time(rs.meta.updatedAt));
      refs.ruleset.title = bits.join(' · ');
    } else {
      refs.ruleset.title = '';
    }
    refs.update.disabled = updating;
    setText(refs.update, updating ? '更新中…' : '更新规则源');
  }

  function updateFilterLabel() {
    var v = state().ui.ruleFilter || '';
    setText(refs.filter, v ? v : '全部策略');
    refs.filter.title = v ? '策略：' + v : '全部策略';
  }

  function allRules() { return state().rules || []; }

  function filteredRules() {
    var q = String(state().ui.ruleQuery || '').trim().toLowerCase();
    var fp = state().ui.ruleFilter || '';
    var arr = allRules();
    if (fp) arr = arr.filter(function (r) { return r.proxy === fp; });
    if (q) {
      arr = arr.filter(function (r) {
        return ((r.type || '') + ' ' + (r.payload || '') + ' ' + (r.proxy || '')).toLowerCase().indexOf(q) >= 0;
      });
    }
    return arr;
  }

  function openFilter() {
    var seen = {};
    var items = [{ label: '全部策略', value: '' }];
    allRules().forEach(function (r) {
      var p = r.proxy || '';
      if (p && !seen[p]) {
        seen[p] = true;
        items.push({ label: p, value: p });
      }
    });
    MM.ui.options({
      title: '按策略筛选',
      value: state().ui.ruleFilter || '',
      items: items
    }).then(function (v) {
      if (v === undefined || v === null || v === false) return;
      state().ui.ruleFilter = v;
      updateFilterLabel();
      renderList();
    });
  }

  function keyOf(rule, idx) { return String(idx); }

  function createRuleRow() {
    var row = document.createElement('div');
    row.className = 'rule-row';
    row.innerHTML = '<span class="rule-type"></span><span class="rule-payload"></span><span class="rule-proxy"><span class="chip chip-neutral"></span></span>';
    row.__r = {
      type: row.querySelector('.rule-type'),
      payload: row.querySelector('.rule-payload'),
      proxy: row.querySelector('.rule-proxy .chip')
    };
    return row;
  }

  function updateRuleRow(row, rule) {
    setText(row.__r.type, rule.type || '—');
    setText(row.__r.payload, rule.payload || '—');
    var proxy = rule.proxy || '—';
    setText(row.__r.proxy, proxy);
    row.__r.proxy.title = proxy;
  }

  function renderList() {
    if (!refs) return;
    var rules = allRules();
    var items = filteredRules();
    setText(refs.count, '生效规则 ' + items.length + ' / ' + rules.length);
    updateFilterLabel();

    if (!items.length) {
      refs.table.classList.add('hidden');
      var kind;
      if (!rules.length && errorMsg) kind = 'error';
      else if (rules.length) kind = 'search';
      else if (MM.store.running()) kind = 'empty';
      else kind = 'stopped';
      showEmpty(kind);
      return;
    }

    refs.empty.classList.add('hidden');
    refs.empty.textContent = '';
    refs.table.classList.remove('hidden');

    var container = refs.list;
    var batch = MM.theme.isLowEnd() ? 200 : 0;
    var token = ++renderToken;
    var st = container.__mmKeyed;

    if (st && st.tag === 'rules') {
      MM.keyedList(container, items, keyOf, createRuleRow, updateRuleRow, { tag: 'rules' });
      return;
    }
    clearKeyed(container);
    if (!batch || items.length <= batch) {
      MM.keyedList(container, items, keyOf, createRuleRow, updateRuleRow, { tag: 'rules' });
      return;
    }
    var upto = batch;
    MM.keyedList(container, items.slice(0, upto), keyOf, createRuleRow, updateRuleRow, { tag: 'rules' });
    function next() {
      if (token !== renderToken || !active) return;
      upto = Math.min(items.length, upto + batch);
      MM.keyedList(container, items.slice(0, upto), keyOf, createRuleRow, updateRuleRow, { tag: 'rules' });
      if (upto < items.length) requestAnimationFrame(next);
    }
    requestAnimationFrame(next);
  }

  function showEmpty(kind) {
    if (!refs) return;
    refs.table.classList.add('hidden');
    var host = refs.empty;
    host.classList.remove('hidden');
    host.textContent = '';
    if (showSkel) {
      host.appendChild(MM.ui.skeleton('list'));
      return;
    }
    var opts;
    if (kind === 'error') {
      opts = {
        kind: 'error', icon: 'warning', title: '暂时读不到规则数据',
        text: errorMsg || '请稍后重试',
        cta: { label: '重试', onClick: function () { errorMsg = ''; refs.errors.textContent = ''; refresh(); } }
      };
    } else if (kind === 'search') {
      opts = {
        icon: 'search', title: '没有匹配的规则', text: '换个关键词或策略试试',
        cta: { label: '清空筛选', onClick: clearFilters }
      };
    } else if (kind === 'stopped') {
      opts = {
        icon: 'rules', title: '内核未运行', text: '启动内核后可查看规则列表',
        cta: { label: '去启动', onClick: function () { MM.router.go('/overview'); } }
      };
    } else {
      opts = { icon: 'rules', title: '暂无规则', text: '当前配置没有返回规则' };
    }
    host.appendChild(MM.ui.empty(opts));
  }

  /* ---------- rule set update ---------- */
  function updateRuleSet() {
    if (updating) return;
    updating = true;
    renderMeta();
    var rs = state().ruleset || {};
    var body = {};
    if (rs.preset && rs.preset !== 'custom') body.preset = rs.preset;
    else if (rs.customURL) body.url = rs.customURL;
    MM.api('/api/rules/update', { method: 'POST', body: body }).then(function (d) {
      if (d && d.meta) {
        var cur = state().ruleset || {};
        cur.meta = d.meta;
        MM.store.set({ ruleset: cur });
      }
      MM.ui.toast('规则源已更新', 'ok');
      return Promise.allSettled([loadRules(), MM.data.loadRuleSet()]);
    }).then(function () {
      if (active) render();
    }).catch(function (e) {
      MM.ui.toast(e.message || '更新规则源失败', 'err');
    }).finally(function () {
      updating = false;
      renderMeta();
    });
  }

  /* ---------- lifecycle ---------- */
  function activate(ctx, sec) {
    if (!rootEl) rootEl = sec;
    if (!refs) return;
    active = true;
    if (ctx && ctx.query && ctx.query.q != null && refs.search.value !== String(ctx.query.q)) {
      refs.search.value = String(ctx.query.q);
      state().ui.ruleQuery = String(ctx.query.q);
      refs.clear.classList.toggle('hidden', !refs.search.value);
    }
    render();
    refresh();
  }

  function deactivate() {
    active = false;
    renderToken++;
    if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
  }

  MM.views.rules = {
    title: function () { return MM.router.TITLES.rules; },
    mount: mount,
    activate: activate,
    deactivate: deactivate,
    actions: function () {
      return [
        {
          icon: 'search', label: '搜索',
          onClick: function () { try { refs.search.focus(); } catch (e) { /* ignore */ } }
        },
        { icon: 'sort', label: '策略筛选', onClick: openFilter }
      ];
    }
  };
})();
