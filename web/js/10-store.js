/* 10-store — one store, one source of truth */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};

  var state = {
    status: null,
    settings: null,
    subs: [],
    kernel: null,
    groups: [],
    preview: false,
    rules: [],
    rulesAt: 0,
    conns: { rows: [], count: 0, up: 0, down: 0, at: 0, err: '' },
    intranet: null,
    netinfo: null,
    reach: null,
    ipq: null,
    storage: null,
    ruleset: null,
    service: null,
    logs: null,
    ui: {
      activeGroup: '',
      nodeSort: 'default',
      nodeQuery: '',
      connQuery: '',
      ruleQuery: '',
      ruleFilter: '',
      lastExport: '',
      lastImport: ''
    },
    booted: false,
    bootAt: Date.now()
  };

  var listeners = [];
  var notifyScheduled = false;

  function notify() {
    if (notifyScheduled) return;
    notifyScheduled = true;
    MM.rafBatch(function () {
      notifyScheduled = false;
      listeners.slice().forEach(function (fn) {
        try { fn(state); } catch (e) { /* view error must not break store */ }
      });
    });
  }

  MM.store = {
    state: state,
    get: function (key) { return key ? state[key] : state; },
    set: function (patch, silent) {
      if (!patch) return;
      Object.keys(patch).forEach(function (k) { state[k] = patch[k]; });
      if (!silent) notify();
    },
    patchUI: function (patch) {
      Object.keys(patch).forEach(function (k) { state.ui[k] = patch[k]; });
      notify();
    },
    sub: function (fn) {
      listeners.push(fn);
      return function () { var i = listeners.indexOf(fn); if (i >= 0) listeners.splice(i, 1); };
    },
    notify: notify,
    running: function () { return !!(state.status && state.status.engine && state.status.engine.running); },
    engine: function () { return (state.status && state.status.engine) || {}; },
    setting: function (k, fallback) { return state.settings && state.settings[k] !== undefined ? state.settings[k] : fallback; }
  };
})();
