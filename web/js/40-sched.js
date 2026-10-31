/* 40-sched — polling, SSE, visibility, dedupe, backoff */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  var sched = MM.sched = {};
  var tasks = {};
  var inflight = {};
  var streams = {};
  var timer = null;
  var visibleFns = [];

  sched.every = function (name, ms, fn, enabled) {
    tasks[name] = {
      name: name, ms: ms, fn: fn, enabled: enabled || function () { return true; },
      last: 0, running: false, errors: 0
    };
  };

  function canRun(t) {
    try { return !!t.enabled(); } catch (e) { return false; }
  }

  function runTask(t) {
    if (t.running || document.hidden || !canRun(t)) return Promise.resolve();
    t.running = true;
    t.last = Date.now();
    return Promise.resolve().then(t.fn).catch(function () {
      // 失败保留旧数据；具体错误由视图的错误条呈现。
      t.errors++;
    }).finally(function () {
      t.running = false;
      if (t.errors) t.errors = 0;
    });
  }

  sched.run = function (name) {
    var t = tasks[name];
    return t ? runTask(t) : Promise.resolve();
  };

  sched.markDue = function (name) {
    if (tasks[name]) tasks[name].last = 0;
  };

  function tick() {
    if (document.hidden) return;
    var now = Date.now();
    Object.keys(tasks).forEach(function (k) {
      var t = tasks[k];
      if (now - t.last >= t.ms) runTask(t);
    });
  }

  sched.start = function () {
    if (timer) return;
    timer = setInterval(tick, 500);
    document.addEventListener('visibilitychange', function () {
      if (document.hidden) return;
      Object.keys(tasks).forEach(function (k) {
        var t = tasks[k];
        if (canRun(t)) t.last = 0;
      });
      visibleFns.slice().forEach(function (fn) { try { fn(); } catch (e) { /* ignore */ } });
      tick();
    });
  };

  sched.onVisible = function (fn) { visibleFns.push(fn); };

  // 去重：同名请求只保留一个，避免切页/轮询叠加。
  sched.once = function (name, fn) {
    if (inflight[name]) return inflight[name];
    var ac = typeof AbortController !== 'undefined' ? new AbortController() : null;
    var p;
    try { p = fn(ac ? ac.signal : undefined); }
    catch (e) { p = Promise.reject(e); }
    inflight[name] = Promise.resolve(p).finally(function () { delete inflight[name]; });
    return inflight[name];
  };

  sched.abort = function (name) {
    // AbortController 由调用方持有；这里只清去重锁。
    delete inflight[name];
  };

  // 指数退避重试：网络/服务临时不可用时使用。
  sched.retry = function (name, fn, opts) {
    opts = opts || {};
    var base = opts.base || 2000, max = opts.max || 30000, attempt = 0;
    var stopped = false;
    function attemptRun() {
      if (stopped) return;
      Promise.resolve().then(fn).then(function (ok) {
        if (ok !== false) return;
        schedule();
      }).catch(schedule);
    }
    function schedule() {
      if (stopped) return;
      var wait = Math.min(max, base * Math.pow(2, attempt++));
      setTimeout(attemptRun, wait);
    }
    attemptRun();
    return function stop() { stopped = true; };
  };

  function parseSSE(ev) {
    try { return JSON.parse(ev.data); } catch (e) { return null; }
  }

  sched.sse = function (name, url, opts) {
    opts = opts || {};
    if (streams[name]) return streams[name];
    var es = new EventSource(url);
    var st = { es: es, status: 'connecting', opts: opts };
    streams[name] = st;
    function setStatus(s) {
      st.status = s;
      if (opts.onStatus) { try { opts.onStatus(s); } catch (e) { /* ignore */ } }
    }
    es.onopen = function () { setStatus('ok'); };
    es.onerror = function () {
      // EventSource 自带重连；只更新状态，不弹 toast。
      setStatus(es.readyState === 2 ? 'closed' : 'reconnecting');
    };
    es.onmessage = function (ev) {
      var data = parseSSE(ev);
      if (data != null && opts.onMessage) {
        try { opts.onMessage(data); } catch (e) { /* one bad event must not kill stream */ }
      }
    };
    return st;
  };

  sched.closeSse = function (name) {
    var st = streams[name];
    if (st && st.es) { try { st.es.close(); } catch (e) { /* ignore */ } }
    delete streams[name];
  };

  sched.streamStatus = function (name) {
    return streams[name] ? streams[name].status : 'closed';
  };

  sched.isVisiblePage = function () { return !document.hidden; };
})();
