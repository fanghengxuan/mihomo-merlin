/* 90-main — boot, global data loaders, event wiring */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};

  /* ---------- progress + traffic buses ---------- */
  var progressListeners = [];
  MM.progressBus = {
    sub: function (fn) { progressListeners.push(fn); return function () { var i = progressListeners.indexOf(fn); if (i >= 0) progressListeners.splice(i, 1); }; },
    emit: function (ev) { progressListeners.slice().forEach(function (fn) { try { fn(ev); } catch (e) { /* ignore */ } }); }
  };

  var trafficListeners = [];
  MM.traffic = {
    history: [],
    last: { up: 0, down: 0, upTotal: 0, downTotal: 0 },
    max: function () { return MM.theme.isLowEnd() ? 30 : 60; },
    sub: function (fn) { trafficListeners.push(fn); return function () { var i = trafficListeners.indexOf(fn); if (i >= 0) trafficListeners.splice(i, 1); }; },
    push: function (d) {
      var v = { up: Number(d.up) || 0, down: Number(d.down) || 0 };
      this.last = { up: v.up, down: v.down, upTotal: Number(d.upTotal) || 0, downTotal: Number(d.downTotal) || 0 };
      this.history.push(v);
      while (this.history.length > 60) this.history.shift();
      trafficListeners.slice().forEach(function (fn) { try { fn(this.last, this.history); } catch (e) { /* ignore */ } }, this);
    },
    reset: function () { this.history = []; }
  };

  /* ---------- loaders (single store) ---------- */
  var data = MM.data = {};

  data.loadStatus = function () {
    return MM.sched.once('status', function () {
      return MM.api('/api/status').then(function (d) { MM.store.set({ status: d }); return d; });
    });
  };
  data.loadSettings = function () {
    return MM.sched.once('settings', function () {
      return MM.api('/api/settings').then(function (d) {
        var s = (d && d.settings) || {};
        MM.store.set({ settings: s });
        return s;
      });
    });
  };
  data.loadSubs = function () {
    return MM.sched.once('subs', function () {
      return MM.api('/api/subs').then(function (d) { MM.store.set({ subs: (d && d.subs) || [] }); return d; });
    });
  };
  data.loadIntranet = function () {
    return MM.sched.once('intranet', function () {
      return MM.api('/api/intranet').then(function (d) {
        // H16: /api/intranet 返回扁平字段（lanIp / iface / ipv4 / hostname / panelUrl）
        MM.store.set({ intranet: d || null });
        return d;
      });
    });
  };
  data.loadNetInfo = function (force) {
    return MM.sched.once('netinfo', function () {
      var url = '/api/netinfo' + (force ? '?refresh=1' : '');
      return MM.api(url).then(function (d) {
        if (d && d.pending) {
          setTimeout(function () { MM.sched.markDue('netinfo'); data.loadNetInfo(false).catch(function () {}); }, 2000);
          return null;
        }
        if (d && d.info) MM.store.set({ netinfo: d.info });
        return d;
      });
    });
  };
  data.loadReach = function (force) {
    return MM.sched.once('reach', function () {
      var url = '/api/reachability' + (force ? '?refresh=1' : '');
      return MM.api(url).then(function (d) {
        MM.store.set({ reach: { targets: (d && d.targets) || [], updatedAt: (d && d.updatedAt) || 0 } });
        return d;
      });
    });
  };
  data.loadKernel = function () {
    return MM.sched.once('kernel', function () {
      return MM.api('/api/kernel').then(function (d) { MM.store.set({ kernel: (d && d.kernel) || null }); return d; });
    });
  };
  data.loadService = function () {
    return MM.sched.once('service', function () {
      return MM.api('/api/service').then(function (d) { MM.store.set({ service: d || null }); return d; });
    });
  };
  data.loadStorage = function () {
    return MM.sched.once('storage', function () {
      return MM.api('/api/storage').then(function (d) { MM.store.set({ storage: (d && d.report) || null }); return d; });
    });
  };
  data.loadRuleSet = function () {
    return MM.sched.once('ruleset', function () {
      return MM.api('/api/rules/set').then(function (d) { MM.store.set({ ruleset: d || null }); return d; });
    });
  };

  /* ---------- scheduler ---------- */
  function lowMs(normal, low) { return MM.theme.isLowEnd() ? low : normal; }

  MM.sched.every('status', lowMs(10000, 15000), function () { return data.loadStatus().catch(function () {}); });
  MM.sched.every('netinfo', 30000, function () {
    return MM.router.route() && MM.router.route().view === 'overview' ? data.loadNetInfo(false).catch(function () {}) : null;
  }, function () { return !!MM.router.route() && MM.router.route().view === 'overview'; });
  MM.sched.every('reach', 60000, function () {
    return data.loadReach(false).catch(function () {});
  }, function () { return !!MM.router.route() && MM.router.route().view === 'overview'; });
  MM.sched.every('nodes', 10000, function () {
    var v = MM.views.nodes;
    return v && v.refresh ? v.refresh(true) : null;
  }, function () { return !!MM.router.route() && MM.router.route().view === 'nodes'; });
  MM.sched.every('subs', 30000, function () {
    var v = MM.views.subs;
    return v && v.refresh ? v.refresh(true) : null;
  }, function () {
    var r = MM.router.route();
    return !!r && (r.view === 'subs' || r.view === 'overview');
  });

  /* ---------- streams ---------- */
  function startStreams() {
    MM.sched.sse('traffic', '/api/traffic/stream', {
      onMessage: function (d) { MM.traffic.push(d); }
    });
    MM.sched.sse('progress', '/api/progress', {
      onMessage: function (ev) { MM.progressBus.emit(ev); }
    });
  }

  /* ---------- boot ---------- */
  function boot() {
    MM.theme.init();
    MM.router.start();
    MM.router.bootSkeleton();
    startStreams();
    MM.sched.start();

    var jobs = [
      data.loadSettings(),
      data.loadStatus(),
      data.loadSubs(),
      data.loadIntranet(),
      data.loadNetInfo(false),
      data.loadReach(false)
    ];
    Promise.allSettled(jobs).then(function () {
      MM.store.set({ booted: true, bootAt: Date.now() }, true);
      MM.router.refreshView();
      MM.store.notify();
    });

    MM.sched.onVisible(function () {
      data.loadStatus().catch(function () {});
      var r = MM.router.route();
      if (r && r.view === 'overview') data.loadNetInfo(true).catch(function () {});
    });
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
  else boot();
})();
