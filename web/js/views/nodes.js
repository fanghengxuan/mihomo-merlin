/* nodes view — spec 4.3 */
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
  var preview = false;
  var testingNodes = {};
  var testingGroups = {};
  var selecting = {};
  var testedMap = {};
  var testingAll = false;
  var skeletonTimer = null;
  var renderToken = 0;

  function setText(node, text) {
    if (!node) return;
    var s = String(text == null ? '' : text);
    if (node.textContent !== s) node.textContent = s;
  }

  function state() { return MM.store.state; }

  function clearKeyed(host) {
    if (!host) return;
    host.textContent = '';
    host.__mmKeyed = null;
  }

  function resolveGroup() {
    var groups = state().groups || [];
    if (!groups.length) return null;
    var name = state().ui.activeGroup;
    for (var i = 0; i < groups.length; i++) {
      if (groups[i].name === name) return groups[i];
    }
    return groups[0];
  }

  /* ---------- mount ---------- */
  function mount(root) {
    rootEl = root;
    root.innerHTML =
      '<div class="toolbar nodes-toolbar">' +
        '<label class="search">' + MM.icon('search') +
          '<input type="search" data-n="search" placeholder="搜索节点 / 订阅" aria-label="搜索节点" autocomplete="off" spellcheck="false">' +
          '<button class="clear hidden" type="button" data-n="clear" aria-label="清空搜索">' + MM.icon('close') + '</button>' +
        '</label>' +
        '<div class="toolbar-actions">' +
          '<button class="icon-btn" type="button" data-n="sort" aria-label="排序" title="排序">' + MM.icon('sort') + '</button>' +
        '</div>' +
      '</div>' +
      '<div class="group-chips" data-n="chips" role="tablist" aria-label="代理组"></div>' +
      '<div class="toolbar-note hidden" data-n="preview"></div>' +
      '<div class="error-slot" data-n="errors"></div>' +
      '<div class="node-panel" data-n="panel">' +
        '<div class="node-pin" data-n="pin">' +
          '<div class="pin-main">' +
            '<div class="pin-title" data-n="pinTitle"></div>' +
            '<div class="pin-sub" data-n="pinSub"></div>' +
          '</div>' +
          '<button class="btn btn-tonal btn-small" type="button" data-n="pinTest">测速本组</button>' +
        '</div>' +
        '<div data-n="list"></div>' +
      '</div>' +
      '<div class="hidden" data-n="empty"></div>';

    refs = {
      search: root.querySelector('[data-n="search"]'),
      clear: root.querySelector('[data-n="clear"]'),
      sort: root.querySelector('[data-n="sort"]'),
      chips: root.querySelector('[data-n="chips"]'),
      preview: root.querySelector('[data-n="preview"]'),
      errors: root.querySelector('[data-n="errors"]'),
      panel: root.querySelector('[data-n="panel"]'),
      pinTitle: root.querySelector('[data-n="pinTitle"]'),
      pinSub: root.querySelector('[data-n="pinSub"]'),
      pinTest: root.querySelector('[data-n="pinTest"]'),
      list: root.querySelector('[data-n="list"]'),
      empty: root.querySelector('[data-n="empty"]')
    };

    var debounced = MM.debounce(function () {
      state().ui.nodeQuery = refs.search.value;
      refs.clear.classList.toggle('hidden', !refs.search.value);
      renderNodes();
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
    refs.sort.addEventListener('click', openSort);
    refs.pinTest.addEventListener('click', function () {
      var g = resolveGroup();
      if (g) testGroup(g);
    });
  }

  function clearSearch() {
    refs.search.value = '';
    state().ui.nodeQuery = '';
    refs.clear.classList.add('hidden');
    renderNodes();
    try { refs.search.focus(); } catch (e) { /* ignore */ }
  }

  function openSort() {
    MM.ui.options({
      title: '排序节点',
      value: state().ui.nodeSort || 'default',
      items: [
        { label: '默认顺序', value: 'default', note: '按内核返回的分组顺序' },
        { label: '延迟从低到高', value: 'delay', note: '未测与超时排在最后' },
        { label: '按名称', value: 'name' }
      ]
    }).then(function (v) {
      if (v === undefined || v === null || v === false) return;
      state().ui.nodeSort = v;
      renderNodes();
    });
  }

  /* ---------- data ---------- */
  function refresh(force) {
    if (loadingPromise) return loadingPromise;
    if (!force && Date.now() - loadedAt < 8000 && (state().groups || []).length) {
      renderAll();
      return Promise.resolve();
    }
    loading = true;
    scheduleSkeleton();
    loadingPromise = MM.sched.once('nodes', function () {
      return MM.api('/api/nodes');
    }).then(function (d) {
      applyData(d);
      return d;
    }).catch(function (e) {
      setError(e && e.message ? e.message : '读取节点失败');
    }).finally(function () {
      loading = false;
      loadingPromise = null;
      if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
      if (active) renderAll();
    });
    return loadingPromise;
  }

  function applyData(d) {
    var groups = (d && Array.isArray(d.groups)) ? d.groups : [];
    preview = !!(d && d.preview);
    groups.forEach(function (g) {
      (g.nodes || []).forEach(function (n) {
        if (Number(n.delay) > 0) testedMap[n.name] = true;
      });
    });
    MM.store.set({ groups: groups });
    loadedAt = Date.now();
    errorMsg = '';
    if (refs) refs.errors.textContent = '';
    if (active) renderAll();
  }

  function scheduleSkeleton() {
    if ((state().groups || []).length) return;
    if (skeletonTimer) clearTimeout(skeletonTimer);
    skeletonTimer = setTimeout(function () {
      skeletonTimer = null;
      if (active && loading && !(state().groups || []).length) showEmpty('skeleton');
    }, 400);
  }

  function setError(msg) {
    errorMsg = msg || '读取节点失败';
    if (!refs) return;
    refs.errors.textContent = '';
    if (!(state().groups || []).length) return;
    refs.errors.appendChild(MM.ui.errorBar(errorMsg, function () {
      errorMsg = '';
      refs.errors.textContent = '';
      return refresh(true);
    }));
  }

  /* ---------- rendering ---------- */
  function renderAll() {
    if (!refs) return;
    renderChips();
    renderPin();
    renderNodes();
  }

  function renderChips() {
    var groups = state().groups || [];
    var host = refs.chips;
    if (!groups.length) {
      clearKeyed(host);
      return;
    }
    MM.keyedList(host, groups, function (g) { return g.name; }, createChip, updateChip, { tag: 'node-chips' });
  }

  function createChip() {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'group-chip';
    b.setAttribute('role', 'tab');
    b.innerHTML = '<span class="chip-name"></span><span class="cnt"></span>';
    b.__r = { name: b.querySelector('.chip-name'), cnt: b.querySelector('.cnt') };
    b.addEventListener('click', function () {
      var name = b.__name;
      if (!name) return;
      state().ui.activeGroup = name;
      renderAll();
    });
    return b;
  }

  function updateChip(b, group) {
    var current = resolveGroup();
    var on = !!current && current.name === group.name;
    b.__name = group.name;
    b.classList.toggle('active', on);
    b.setAttribute('aria-selected', on ? 'true' : 'false');
    setText(b.__r.name, group.name);
    setText(b.__r.cnt, String((group.nodes || []).length));
  }

  function renderPin() {
    var g = resolveGroup();
    if (!g) return;
    setText(refs.pinTitle, g.name);
    setText(refs.pinSub, '当前 ' + (g.now || '未选择') + ' · ' + (g.nodes || []).length + ' 个节点');
    var busy = !!testingGroups[g.name];
    refs.pinTest.disabled = busy || preview;
    setText(refs.pinTest, busy ? '测速中…' : '测速本组');
    refs.pinTest.title = preview ? '内核未运行，无法测速' : '';
    refs.preview.textContent = preview ? '内核未运行，显示订阅预览；启动后可切换节点与测速' : '';
    refs.preview.classList.toggle('hidden', !preview);
  }

  function rank(node) {
    var d = Number(node.delay) || 0;
    if (d > 0) return d;
    if (testedMap[node.name]) return 900000;
    return 1000000;
  }

  function filteredNodes(group) {
    var q = String(state().ui.nodeQuery || '').trim().toLowerCase();
    var arr = (group.nodes || []).slice();
    if (q) {
      arr = arr.filter(function (n) {
        return ((n.name || '') + ' ' + (n.sub || '') + ' ' + (n.type || '')).toLowerCase().indexOf(q) >= 0;
      });
    }
    var sort = state().ui.nodeSort || 'default';
    if (sort === 'delay') {
      arr.sort(function (a, b) {
        return rank(a) - rank(b) || String(a.name).localeCompare(String(b.name), 'zh');
      });
    } else if (sort === 'name') {
      arr.sort(function (a, b) { return String(a.name).localeCompare(String(b.name), 'zh'); });
    }
    return arr;
  }

  function delayState(node) {
    if (testingNodes[node.name]) return { cls: 'lat-testing', text: '测试…' };
    var d = Number(node.delay) || 0;
    if (d > 0) {
      return d < 600 ? { cls: 'lat-ok', text: d + ' ms' } : { cls: 'lat-warn', text: d + ' ms' };
    }
    if (testedMap[node.name]) return { cls: 'lat-err', text: '超时' };
    return { cls: 'lat-none', text: '未测' };
  }

  function keyOf(node) { return node.name; }

  function createRow() {
    var row = document.createElement('div');
    row.className = 'node-row';
    row.setAttribute('role', 'option');
    row.setAttribute('tabindex', '0');
    row.innerHTML =
      '<span class="node-check"></span>' +
      '<span class="node-well"></span>' +
      '<span class="node-main"><span class="node-name"></span><span class="node-sub"></span></span>' +
      '<span class="lat node-delay"></span>' +
      '<button class="node-test" type="button" aria-label="测速">' + MM.icon('bolt') + '</button>';
    row.__r = {
      check: row.querySelector('.node-check'),
      well: row.querySelector('.node-well'),
      name: row.querySelector('.node-name'),
      sub: row.querySelector('.node-sub'),
      delay: row.querySelector('.node-delay'),
      test: row.querySelector('.node-test')
    };
    row.addEventListener('click', function (e) {
      if (e.target.closest('.node-test')) return;
      selectNode(row.__group, row.__item);
    });
    row.addEventListener('keydown', function (e) {
      if (e.target.closest('.node-test')) return;
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        selectNode(row.__group, row.__item);
      }
    });
    row.__r.test.addEventListener('click', function (e) {
      e.stopPropagation();
      testNode(row.__group, row.__item);
    });
    return row;
  }

  function updateRow(row, node, group) {
    var r = row.__r;
    row.__item = node;
    row.__group = group;
    var selected = group.now === node.name;
    var checkHTML = selected ? MM.icon('check') : '';
    if (r.check.innerHTML !== checkHTML) r.check.innerHTML = checkHTML;
    row.setAttribute('aria-selected', selected ? 'true' : 'false');
    setText(r.well, MM.protoShort(node.type));
    setText(r.name, node.name);
    var sub = node.type || '节点';
    if (node.sub) sub += ' · ' + node.sub;
    if (node.isGroup) sub += ' · 分组';
    setText(r.sub, sub);
    var info = delayState(node);
    r.delay.className = 'lat node-delay ' + info.cls;
    setText(r.delay, info.text);
    var testing = !!testingNodes[node.name];
    r.test.disabled = testing || !!node.isGroup || preview;
    r.test.classList.toggle('testing', testing);
    r.test.setAttribute('aria-label', '测速 ' + node.name);
    row.style.opacity = selecting[node.name] ? '0.7' : '';
  }

  function renderNodes() {
    if (!refs) return;
    var group = resolveGroup();
    if (!group) {
      showEmpty(errorMsg ? 'error' : 'no-groups');
      return;
    }
    var items = filteredNodes(group);
    if (!items.length) {
      if (String(state().ui.nodeQuery || '').trim()) showEmpty('search');
      else showEmpty('group-empty');
      return;
    }
    refs.empty.classList.add('hidden');
    refs.empty.textContent = '';
    refs.panel.classList.remove('hidden');

    var tag = 'nodes:' + group.name;
    var container = refs.list;
    var batch = MM.theme.isLowEnd() ? 50 : 100;
    var token = ++renderToken;
    var st = container.__mmKeyed;

    if (st && st.tag === tag) {
      MM.keyedList(container, items, keyOf, createRow, function (row, node) { updateRow(row, node, group); }, { tag: tag });
      return;
    }

    clearKeyed(container);
    if (items.length <= batch) {
      MM.keyedList(container, items, keyOf, createRow, function (row, node) { updateRow(row, node, group); }, { tag: tag });
      return;
    }

    var upto = batch;
    MM.keyedList(container, items.slice(0, upto), keyOf, createRow, function (row, node) { updateRow(row, node, group); }, { tag: tag });
    function next() {
      if (token !== renderToken || !active) return;
      upto = Math.min(items.length, upto + batch);
      MM.keyedList(container, items.slice(0, upto), keyOf, createRow, function (row, node) { updateRow(row, node, group); }, { tag: tag });
      if (upto < items.length) requestAnimationFrame(next);
    }
    requestAnimationFrame(next);
  }

  function showEmpty(kind) {
    if (!refs) return;
    refs.panel.classList.add('hidden');
    var host = refs.empty;
    host.classList.remove('hidden');
    host.textContent = '';
    if (kind === 'skeleton') {
      host.appendChild(MM.ui.skeleton('list'));
      return;
    }
    var opts;
    if (kind === 'error') {
      opts = {
        kind: 'error', icon: 'warning', title: '暂时读不到节点数据',
        text: errorMsg || '请稍后重试',
        cta: { label: '重试', onClick: function () { errorMsg = ''; refresh(true); } }
      };
    } else if (kind === 'search') {
      opts = {
        icon: 'search', title: '没有匹配的节点', text: '换个关键词试试',
        cta: { label: '清空搜索', onClick: clearSearch }
      };
    } else if (kind === 'group-empty') {
      opts = { icon: 'nodes', title: '该分组暂无节点', text: '换一个代理组，或更新订阅后重试' };
    } else {
      opts = {
        icon: 'nodes', title: '还没有可用节点', text: '添加订阅后节点会出现在这里',
        cta: { label: '去添加订阅', onClick: function () { MM.router.go('/subs'); } }
      };
    }
    host.appendChild(MM.ui.empty(opts));
  }

  /* ---------- actions ---------- */
  function selectNode(group, node) {
    if (!group || !node || node.isGroup) return;
    if (preview) { MM.ui.toast('内核未运行，无法切换节点', 'err'); return; }
    if (group.now === node.name || selecting[node.name]) return;
    selecting[node.name] = true;
    renderNodes();
    MM.api('/api/select', { method: 'PUT', body: { group: group.name, node: node.name } }).then(function () {
      group.now = node.name;
      MM.ui.toast('已切换到 ' + node.name, 'ok');
      renderPin();
      renderNodes();
    }).catch(function (e) {
      MM.ui.toast(e.message || '切换节点失败', 'err');
    }).finally(function () {
      delete selecting[node.name];
      renderNodes();
    });
  }

  function testNode(group, node) {
    if (!node || node.isGroup) return;
    if (preview) { MM.ui.toast('内核未运行，无法测速', 'err'); return; }
    if (testingNodes[node.name]) return;
    testingNodes[node.name] = true;
    renderNodes();
    MM.api('/api/delay?name=' + encodeURIComponent(node.name)).then(function (d) {
      node.delay = Number(d && d.delay) || 0;
      testedMap[node.name] = true;
      renderNodes();
    }).catch(function (e) {
      MM.ui.toast(e.message || '测速失败', 'err');
    }).finally(function () {
      delete testingNodes[node.name];
      if (active) renderNodes();
    });
  }

  function applyResults(results, names) {
    var set = {};
    names.forEach(function (n) { set[n] = true; });
    (state().groups || []).forEach(function (g) {
      (g.nodes || []).forEach(function (n) {
        if (set[n.name]) {
          n.delay = Number(results[n.name]) || 0;
          testedMap[n.name] = true;
        }
      });
    });
  }

  function testGroup(group) {
    if (!group) return;
    if (preview) { MM.ui.toast('内核未运行，无法测速', 'err'); return; }
    if (testingGroups[group.name]) return;
    var names = (group.nodes || []).filter(function (n) { return !n.isGroup; }).map(function (n) { return n.name; }).slice(0, 200);
    if (!names.length) { MM.ui.toast('该分组没有可测速节点', 'err'); return; }
    testingGroups[group.name] = true;
    names.forEach(function (n) { testingNodes[n] = true; });
    renderPin();
    renderNodes();
    MM.api('/api/delay', { method: 'POST', body: { names: names, group: group.name } }).then(function (d) {
      applyResults((d && d.results) || {}, names);
    }).catch(function (e) {
      MM.ui.toast(e.message || '批量测速失败', 'err');
    }).finally(function () {
      delete testingGroups[group.name];
      names.forEach(function (n) { delete testingNodes[n]; });
      if (active) { renderPin(); renderNodes(); }
    });
  }

  function testAll() {
    if (preview) { MM.ui.toast('内核未运行，无法测速', 'err'); return; }
    if (testingAll) return;
    var seen = {};
    var names = [];
    (state().groups || []).forEach(function (g) {
      (g.nodes || []).forEach(function (n) {
        if (!n.isGroup && !seen[n.name] && names.length < 200) {
          seen[n.name] = true;
          names.push(n.name);
        }
      });
    });
    if (!names.length) { MM.ui.toast('没有可测速的节点', 'err'); return; }
    testingAll = true;
    names.forEach(function (n) { testingNodes[n] = true; });
    renderNodes();
    MM.api('/api/delay', { method: 'POST', body: { names: names } }).then(function (d) {
      applyResults((d && d.results) || {}, names);
      MM.ui.toast('测速完成', 'ok');
    }).catch(function (e) {
      MM.ui.toast(e.message || '批量测速失败', 'err');
    }).finally(function () {
      testingAll = false;
      names.forEach(function (n) { delete testingNodes[n]; });
      if (active) { renderPin(); renderNodes(); }
      MM.router.refreshActions();
    });
  }

  /* ---------- lifecycle ---------- */
  function activate(ctx, sec) {
    if (!rootEl) rootEl = sec;
    if (!refs) return;
    active = true;
    var q = ctx && ctx.query ? ctx.query.q : undefined;
    if (q !== undefined && q !== null && refs.search.value !== String(q)) {
      refs.search.value = String(q);
      state().ui.nodeQuery = String(q);
      refs.clear.classList.toggle('hidden', !refs.search.value);
    }
    renderAll();
    refresh(false);
  }

  function deactivate() {
    active = false;
    renderToken++;
    if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
  }

  MM.views.nodes = {
    title: function () { return MM.router.TITLES.nodes; },
    mount: mount,
    activate: activate,
    deactivate: deactivate,
    refresh: refresh,
    actions: function () {
      return [
        {
          icon: 'search', label: '搜索', desktopOnly: true,
          onClick: function () { try { refs.search.focus(); } catch (e) { /* ignore */ } }
        },
        { icon: 'sort', label: '排序', desktopOnly: true, onClick: openSort },
        { id: 'nodesTestAll', icon: 'bolt', label: '全部测速', primary: true, onClick: testAll }
      ];
    }
  };
})();
