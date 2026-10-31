/* 50-router — hash routes, legacy redirects, chrome, scroll memory */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  var router = MM.router = {};

  var NAV = [
    { id: 'overview', path: '/overview', label: '概览', icon: 'overview' },
    { id: 'nodes', path: '/nodes', label: '节点', icon: 'nodes' },
    { id: 'subs', path: '/subs', label: '订阅', icon: 'subs' },
    { id: 'rules', path: '/rules', label: '规则', icon: 'rules' },
    { id: 'connections', path: '/connections', label: '连接', icon: 'conn' },
    { id: 'logs', path: '/logs', label: '日志', icon: 'logs' },
    { id: 'settings', path: '/settings', label: '设置', icon: 'settings' }
  ];
  var DOCK = ['overview', 'nodes', 'subs', 'connections', 'settings'];
  var ROOT = { overview: 1, nodes: 1, subs: 1, connections: 1, settings: 1 };
  var PUSH_VIEWS = { rules: 1, logs: 1 };
  var TITLES = {
    overview: '概览', nodes: '节点', subs: '订阅', rules: '规则',
    connections: '连接', logs: '日志', settings: '设置'
  };
  var SUB_TITLES = {
    proxy: '代理与端口', dns: 'DNS', subscription: '订阅拉取', ruleset: '国内 IP 规则',
    autoselect: '自动选择', schedule: '定时重启', kernel: '内核', backup: '备份与恢复',
    storage: '存储', appearance: '外观', security: '启动与访问', diagnostics: '诊断与关于'
  };
  var SETTING_ORDER = ['proxy', 'dns', 'subscription', 'ruleset', 'autoselect', 'schedule', 'kernel', 'backup', 'storage', 'appearance', 'security', 'diagnostics'];

  var mql = window.matchMedia ? window.matchMedia('(max-width:1023px)') : null;
  var containers = {};
  var current = null;
  var scrolls = {};
  var lastRoot = '/overview';
  var started = false;

  router.NAV = NAV;
  router.DOCK = DOCK;
  router.SUB_TITLES = SUB_TITLES;
  router.SETTING_ORDER = SETTING_ORDER;
  router.TITLES = TITLES;

  router.isMobile = function () { return mql ? mql.matches : window.innerWidth < 1024; };
  router.route = function () { return current; };

  function parseHash() {
    var h = location.hash || '';
    if (h === '' || h === '#') return { path: '/overview', legacy: false };
    if (h.charAt(0) !== '#') return { path: '/overview', legacy: false };
    if (h.charAt(1) === '/') return { path: h.slice(1), legacy: false };
    var legacy = h.slice(1);
    if (/^(overview|nodes|subs|rules|connections|logs|settings)$/.test(legacy)) {
      return { path: '/' + legacy, legacy: true };
    }
    return { path: '/overview', legacy: false, bad: true };
  }

  function parsePath(path) {
    var bits = path.split('?');
    var clean = bits[0].replace(/^\/+/, '').replace(/\/+$/, '');
    var parts = clean.split('/').filter(Boolean);
    var view = parts[0] || 'overview';
    var sub = parts[1] || '';
    var query = {};
    if (bits[1]) {
      try {
        new URLSearchParams(bits[1]).forEach(function (v, k) { query[k] = v; });
      } catch (e) { /* ignore */ }
    }
    if (!TITLES[view]) { view = 'overview'; sub = ''; }
    return { view: view, sub: sub, query: query, path: '/' + view + (sub ? '/' + sub : '') };
  }

  function isPush(ctx) {
    if (!router.isMobile()) return false;
    if (PUSH_VIEWS[ctx.view]) return true;
    if (ctx.view === 'settings' && ctx.sub) return true;
    return false;
  }

  function titleOf(ctx) {
    var v = MM.views[ctx.view];
    if (v && typeof v.title === 'function') return v.title(ctx);
    if (ctx.view === 'settings' && ctx.sub) return SUB_TITLES[ctx.sub] || '设置';
    return TITLES[ctx.view] || '概览';
  }

  function rootOf(ctx) {
    if (ctx.view === 'rules' || ctx.view === 'logs') return lastRoot;
    if (ctx.view === 'settings') return '/settings';
    return '/' + ctx.view;
  }

  router.go = function (path, replace) {
    var target = '#' + path;
    if (location.hash === target) { apply(); return; }
    if (replace) {
      try { history.replaceState(null, '', target); } catch (e) { location.hash = target; }
      apply();
    } else {
      location.hash = target;
    }
  };
  router.replace = function (path) { router.go(path, true); };
  router.back = function () {
    if (current) router.go(rootOf(current));
    else router.go('/overview');
  };

  function ensureContainer(id) {
    if (containers[id]) return containers[id];
    var sec = document.createElement('section');
    sec.className = 'view';
    sec.dataset.view = id;
    MM.$('#viewRoot').appendChild(sec);
    containers[id] = sec;
    if (MM.views[id] && MM.views[id].mount) MM.views[id].mount(sec);
    return sec;
  }

  function activate(ctx) {
    var sec = ensureContainer(ctx.view);
    Object.keys(containers).forEach(function (k) {
      containers[k].classList.toggle('view-active', k === ctx.view);
      containers[k].setAttribute('aria-hidden', k === ctx.view ? 'false' : 'true');
    });
    var view = MM.views[ctx.view];
    if (view && view.activate) view.activate(ctx, sec);
  }

  function deactivate(ctx) {
    var view = ctx && MM.views[ctx.view];
    if (view && view.deactivate) { try { view.deactivate(ctx); } catch (e) { /* ignore */ } }
  }

  function renderNav(ctx) {
    var id = ctx.view;
    MM.$$('#railNav .rail-item, #dock .dock-item').forEach(function (btn) {
      var route = btn.getAttribute('data-route') || '';
      var active = route === '/' + id;
      btn.classList.toggle('active', active);
      if (active) btn.setAttribute('aria-current', 'page');
      else btn.removeAttribute('aria-current');
    });
    var push = isPush(ctx);
    document.documentElement.setAttribute('data-push', push ? '1' : '0');
    var root = ROOT[id] && !push;
    var more = MM.$('#btnMore');
    if (more) more.classList.toggle('hidden', !(router.isMobile() && root));
  }

  function renderActions(ctx) {
    var box = MM.$('#appbarActions');
    if (!box) return;
    box.innerHTML = '';
    var view = MM.views[ctx.view];
    var list = (view && view.actions) ? (view.actions(ctx) || []) : [];
    list.forEach(function (a) {
      if (!a) return;
      if (a.mobileOnly && !router.isMobile()) return;
      if (a.desktopOnly && router.isMobile()) return;
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = a.primary ? 'btn btn-filled btn-small' : a.destructive ? 'btn btn-text btn-small btn-destructive' : (a.label ? 'btn btn-text btn-small' : 'icon-btn');
      btn.setAttribute('aria-label', a.label || '操作');
      btn.title = a.label || '';
      if (a.id) btn.id = a.id;
      if (a.disabled) btn.disabled = true;
      btn.innerHTML = (a.icon ? MM.icon(a.icon) : '') + (a.label && a.showLabel ? '<span>' + MM.esc(a.label) + '</span>' : '');
      btn.addEventListener('click', function (e) {
        e.preventDefault();
        if (a.onClick) a.onClick(e, btn);
      });
      box.appendChild(btn);
    });
    var back = MM.$('#btnBack');
    if (back) back.classList.toggle('hidden', !isPush(ctx));
    MM.$('#pageTitle').textContent = titleOf(ctx);
    document.title = titleOf(ctx) + ' · Mihomo 魔方';
  }

  router.refreshActions = function () { if (current) renderActions(current); };
  router.refreshView = function () {
    if (!current) return;
    var v = MM.views[current.view];
    if (v && v.activate) { try { v.activate(current, containers[current.view]); } catch (e) { /* ignore */ } }
  };

  function renderStatus(st) {
    var running = !!(st && st.status && st.status.engine && st.status.engine.running);
    var dot = MM.$('#railDot'), label = MM.$('#railState'), ver = MM.$('#railVersion');
    if (dot) dot.className = 'dot' + (running ? ' on' : '');
    if (label) label.textContent = running ? '运行中' : '未运行';
    if (ver) ver.textContent = 'v' + ((st && st.status && st.status.panel && st.status.panel.version) || '2.0.3');
  }

  function apply() {
    var parsed = parseHash();
    if (parsed.legacy) {
      try { history.replaceState(null, '', '#' + parsed.path); } catch (e) { /* ignore */ }
    }
    var ctx = parsePath(parsed.path);
    if (ctx.view === 'settings') {
      if (router.isMobile()) {
        if (ctx.sub && SETTING_ORDER.indexOf(ctx.sub) < 0) { router.replace('/settings'); return; }
      } else {
        if (!ctx.sub) { router.replace('/settings/proxy'); return; }
        if (SETTING_ORDER.indexOf(ctx.sub) < 0) { router.replace('/settings/proxy'); return; }
      }
    }
    ctx.path = '/' + ctx.view + (ctx.sub ? '/' + ctx.sub : '');
    ctx.push = isPush(ctx);
    ctx.mobile = router.isMobile();
    if (current && current.path === ctx.path) {
      // 同一路由 repeat：只刷新 chrome，避免整页重建
      renderNav(ctx); renderActions(ctx);
      if (MM.views[ctx.view] && MM.views[ctx.view].activate) MM.views[ctx.view].activate(ctx, containers[ctx.view]);
      return;
    }
    if (current) {
      scrolls[current.path] = window.scrollY || 0;
      deactivate(current);
    }
    if (ROOT[ctx.view] && !ctx.push) lastRoot = ctx.path;
    current = ctx;
    activate(ctx);
    renderNav(ctx);
    renderActions(ctx);
    var y = scrolls[ctx.path] || 0;
    requestAnimationFrame(function () { window.scrollTo(0, y); });
  }

  var bootSkeletonTimer = null;
  router.start = function () {
    if (started) return;
    started = true;
    MM.store.sub(renderStatus);
    renderStatus(MM.store.state);
    MM.$('#btnBack').addEventListener('click', function () { router.back(); });
    MM.$('#btnMore').addEventListener('click', function () {
      if (MM.views.more && MM.views.more.open) MM.views.more.open(MM.$('#btnMore'));
    });
    MM.$('#railFoot').addEventListener('click', function () { router.go('/overview'); });
    MM.$('#railNav').addEventListener('click', function (e) {
      var b = e.target.closest('[data-route]');
      if (b) router.go(b.getAttribute('data-route'));
    });
    MM.$('#dock').addEventListener('click', function (e) {
      var b = e.target.closest('[data-route]');
      if (b) router.go(b.getAttribute('data-route'));
    });
    window.addEventListener('hashchange', apply);
    if (mql) {
      var onMq = function () { apply(); };
      if (mql.addEventListener) mql.addEventListener('change', onMq);
      else if (mql.addListener) mql.addListener(onMq);
    }
    apply();
  };

  // 400ms 后才显示骨架；首屏不做 opacity:0 动画。
  router.bootSkeleton = function () {
    if (bootSkeletonTimer) return;
    bootSkeletonTimer = setTimeout(function () {
      if (MM.store.state.booted) return;
      var root = MM.$('#viewRoot');
      if (root && !root.querySelector('.view-active .skeleton-cards')) {
        var sec = root.querySelector('.view-active');
        if (sec && !sec.children.length) sec.appendChild(MM.ui.skeleton('cards'));
      }
    }, 400);
  };
})();
