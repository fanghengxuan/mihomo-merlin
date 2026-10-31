/* 00-core — DOM/format/network helpers, no framework */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  MM.VERSION = '2.0.3';

  MM.$ = function (sel, root) { return (root || document).querySelector(sel); };
  MM.$$ = function (sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); };

  MM.esc = function (s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  };

  MM.icon = function (name, cls, label) {
    var a = label ? ' role="img" aria-label="' + MM.esc(label) + '"' : ' aria-hidden="true"';
    return '<svg class="' + (cls || '') + '"' + a + '><use href="#i-' + name + '"/></svg>';
  };

  function num(v) { var n = Number(v); return isFinite(n) ? n : 0; }
  function trimZero(s) { return s.replace(/\.0+$/, ''); }

  MM.fmt = {
    bytes: function (n) {
      var v = num(n);
      if (v < 1024) return Math.round(v) + ' B';
      var u = ['KB', 'MB', 'GB', 'TB', 'PB'], x = v / 1024, i = 0;
      while (x >= 1024 && i < u.length - 1) { x /= 1024; i++; }
      return trimZero(x.toFixed(x < 10 ? 2 : 1)) + ' ' + u[i];
    },
    speed: function (n) {
      var v = num(n);
      if (v < 1024) return Math.round(v) + ' B/s';
      var u = ['KB/s', 'MB/s', 'GB/s'], x = v / 1024, i = 0;
      while (x >= 1024 && i < u.length - 1) { x /= 1024; i++; }
      return trimZero(x.toFixed(x < 10 ? 2 : 1)) + ' ' + u[i];
    },
    pct: function (n) { return Math.max(0, Math.min(100, num(n))).toFixed(0) + '%'; },
    dur: function (sec) {
      var s = Math.max(0, Math.floor(num(sec)));
      if (s < 60) return s + ' 秒';
      var m = Math.floor(s / 60);
      if (m < 60) return m + ' 分钟';
      var h = Math.floor(m / 60);
      if (h < 24) return h + ' 时 ' + (m % 60) + ' 分';
      return Math.floor(h / 24) + ' 天 ' + (h % 24) + ' 时';
    },
    durShort: function (sec) {
      var s = Math.max(0, Math.floor(num(sec)));
      var h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
      if (h > 0) return h + ' 时' + (m ? ' ' + m + ' 分' : '');
      if (m > 0) return m + ' 分钟';
      return s + ' 秒';
    },
    // 连接时长：00:12 / 1:02:03
    durClock: function (sec) {
      var s = Math.max(0, Math.floor(num(sec)));
      var h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
      var p = function (x) { return String(x).padStart(2, '0'); };
      return h > 0 ? h + ':' + p(m) + ':' + p(r) : p(m) + ':' + p(r);
    },
    time: function (ts) {
      if (!ts) return '—';
      var d = new Date(Number(ts) * 1000);
      if (isNaN(d.getTime()) || d.getFullYear() < 1971) return '—';
      var p = function (x) { return String(x).padStart(2, '0'); };
      return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
    },
    date: function (ts) {
      if (!ts) return '—';
      var d = new Date(Number(ts) * 1000);
      if (isNaN(d.getTime()) || d.getFullYear() < 1971) return '—';
      var p = function (x) { return String(x).padStart(2, '0'); };
      return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate());
    },
    clock: function (ts) {
      var d = ts instanceof Date ? ts : (ts ? new Date(Number(ts) * 1000) : new Date());
      if (isNaN(d.getTime())) d = new Date();
      var p = function (x) { return String(x).padStart(2, '0'); };
      return p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
    },
    ago: function (ts) {
      if (!ts) return '—';
      var d = ts instanceof Date ? ts : new Date(Number(ts) * 1000);
      var s = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000));
      return MM.fmt.dur(s) + '前';
    },
    left: function (ts) {
      if (!ts) return '—';
      var s = Number(ts) - Math.floor(Date.now() / 1000);
      if (s <= 0) return '即将';
      return MM.fmt.dur(s) + '后';
    }
  };

  // 延迟阈值（spec）：<600 ok，>=600 warn，0/超时 err，null/未测 variant
  MM.delayInfo = function (delay, testing) {
    if (testing) return { cls: 'lat-testing', text: '测试…', state: 'testing' };
    if (delay === null || delay === undefined || delay === '') return { cls: 'lat-none', text: '未测', state: 'untested' };
    var d = Number(delay);
    if (!d || d <= 0) return { cls: 'lat-err', text: '超时', state: 'err' };
    if (d < 600) return { cls: 'lat-ok', text: d + ' ms', state: 'ok' };
    return { cls: 'lat-warn', text: d + ' ms', state: 'warn' };
  };

  MM.PROTO_SHORT = {
    Shadowsocks: 'SS', ShadowsocksR: 'SSR', Vmess: 'VM', Vless: 'VL', Trojan: 'TR',
    Hysteria: 'HY', Hysteria2: 'HY2', Tuic: 'TU', Socks5: 'SO', Http: 'HT',
    WireGuard: 'WG', Snell: 'SN', Mieru: 'MI', AnyTLS: 'AT', SSH: 'SH',
    Selector: 'SEL', URLTest: 'AUTO', Fallback: 'FB', LoadBalance: 'LB', Relay: 'RL'
  };
  MM.protoShort = function (type) {
    if (!type) return '·';
    return MM.PROTO_SHORT[type] || String(type).slice(0, 2).toUpperCase();
  };

  MM.copy = function (text) {
    var t = String(text == null ? '' : text);
    if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(t);
    return new Promise(function (resolve, reject) {
      var ta = document.createElement('textarea');
      ta.value = t; ta.style.position = 'fixed'; ta.style.opacity = '0';
      document.body.appendChild(ta); ta.select();
      try { document.execCommand('copy') ? resolve() : reject(new Error('复制失败')); }
      catch (e) { reject(e); } finally { ta.remove(); }
    });
  };

  MM.debounce = function (fn, ms) {
    var t = null, lastArg = null;
    function run() { t = null; var a = lastArg; lastArg = null; fn(a); }
    var wrapped = function (arg) { lastArg = arg; if (t) clearTimeout(t); t = setTimeout(run, ms); };
    wrapped.cancel = function () { if (t) { clearTimeout(t); t = null; lastArg = null; } };
    wrapped.flush = function () { if (t) { clearTimeout(t); run(); } };
    return wrapped;
  };

  var rafPending = false;
  var rafFns = [];
  MM.rafBatch = function (fn) {
    rafFns.push(fn);
    if (rafPending) return;
    rafPending = true;
    requestAnimationFrame(function () {
      rafPending = false;
      var list = rafFns; rafFns = [];
      list.forEach(function (f) { try { f(); } catch (e) { /* keep loop alive */ } });
    });
  };

  // 增量列表：按 key 复用节点，只更新文本/class，避免整表重建。
  MM.keyedList = function (container, items, keyFn, create, update, opts) {
    opts = opts || {};
    var tag = opts.tag || '';
    var st = container.__mmKeyed;
    if (!st || st.tag !== tag) {
      container.textContent = '';
      st = container.__mmKeyed = { tag: tag, map: new Map(), nodes: [] };
    }
    var seen = new Set();
    var order = [];
    items.forEach(function (item, idx) {
      var k = String(keyFn(item, idx));
      seen.add(k);
      var node = st.map.get(k);
      if (!node) {
        node = create(item, idx);
        st.map.set(k, node);
      }
      try { update(node, item, idx); } catch (e) { /* one bad row must not blank the list */ }
      order.push(node);
    });
    st.map.forEach(function (node, k) {
      if (!seen.has(k)) { if (node.parentNode) node.parentNode.removeChild(node); st.map.delete(k); }
    });
    for (var i = order.length - 1; i >= 0; i--) {
      var node = order[i];
      var next = container.children[i] || null;
      if (node !== next) container.insertBefore(node, next);
    }
    st.nodes = order;
  };

  MM.node = function (tag, attrs, html) {
    var n = document.createElement(tag);
    if (attrs) Object.keys(attrs).forEach(function (k) {
      if (k === 'class') n.className = attrs[k];
      else if (k === 'text') n.textContent = attrs[k];
      else if (k === 'html') n.innerHTML = attrs[k];
      else if (k.slice(0, 2) === 'on' && typeof attrs[k] === 'function') n.addEventListener(k.slice(2), attrs[k]);
      else if (attrs[k] !== null && attrs[k] !== undefined && attrs[k] !== false) n.setAttribute(k, attrs[k]);
    });
    if (html != null) n.innerHTML = html;
    return n;
  };

  // HTTP helper：统一 JSON、错误文案与 401 登录入口。
  MM.api = async function (path, opts) {
    opts = opts || {};
    var opt = { method: opts.method || 'GET', credentials: 'same-origin', headers: {} };
    if (opts.signal) opt.signal = opts.signal;
    if (opts.body !== undefined) {
      opt.headers['Content-Type'] = 'application/json';
      opt.body = JSON.stringify(opts.body);
    }
    var res;
    try { res = await fetch(path, opt); }
    catch (e) {
      if (e && e.name === 'AbortError') throw e;
      throw new Error('无法连接面板服务');
    }
    if (res.status === 401 && path !== '/api/login') {
      if (MM.ui && MM.ui.login) MM.ui.login();
      throw new Error('未登录');
    }
    var data = null;
    var ct = res.headers.get('content-type') || '';
    if (ct.indexOf('json') >= 0) {
      try { data = await res.json(); } catch (e) { data = null; }
    } else if (opts.raw) {
      data = await res.blob();
    } else {
      data = await res.text();
    }
    if (!res.ok || (data && data.ok === false)) {
      var msg = (data && (data.error || data.err)) || ('HTTP ' + res.status);
      throw new Error(msg);
    }
    return data;
  };

  MM.qs = function (obj) {
    return Object.keys(obj).filter(function (k) { return obj[k] !== undefined && obj[k] !== null && obj[k] !== ''; })
      .map(function (k) { return encodeURIComponent(k) + '=' + encodeURIComponent(obj[k]); }).join('&');
  };

  MM.clamp = function (n, a, b) { n = Number(n); return Math.max(a, Math.min(b, isFinite(n) ? n : a)); };
})();
