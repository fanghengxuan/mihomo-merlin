/* settings view — spec 4.8 / 4.9 / 3.3 / 3.6 */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  MM.views = MM.views || {};

  var SUB_TITLES = {
    proxy: '代理与端口', dns: 'DNS', subscription: '订阅拉取', ruleset: '国内 IP 规则',
    autoselect: '自动选择', schedule: '定时重启', kernel: '内核', backup: '备份与恢复',
    storage: '存储', appearance: '外观', security: '启动与访问', diagnostics: '诊断与关于'
  };
  var GROUPS = [
    { title: '连接', subs: ['proxy', 'dns'] },
    { title: '网络', subs: ['subscription', 'ruleset', 'autoselect'] },
    { title: '运行', subs: ['schedule', 'kernel'] },
    { title: '数据', subs: ['backup', 'storage'] },
    { title: '面板', subs: ['appearance', 'security', 'diagnostics'] }
  ];
  var PAGE_FIELDS = {
    proxy: ['mode', 'rulePreset', 'redirPort', 'mixedPort', 'allowLan', 'blockQuic', 'sniffer', 'routerSelf', 'lanIf'],
    dns: ['dnsEnable', 'dnsMode', 'dnsListen', 'dnsNameserver', 'dnsFallback', 'dnsFakeIpRange'],
    subscription: ['subUA', 'subUACustom', 'subProxyMode', 'subInsecure', 'subInterval'],
    ruleset: ['ruleSourcePreset', 'ruleSourceURL', 'ruleAutoUpdate', 'ruleIntervalH'],
    autoselect: ['autoIntervalSec', 'autoToleranceMs'],
    schedule: ['autoRestart', 'restartEveryHours', 'restartAt'],
    security: ['autoStart', 'panelPassword']
  };
  var FORM_PAGES = { proxy: 1, dns: 1, subscription: 1, ruleset: 1, autoselect: 1, schedule: 1, security: 1 };
  var MODE_LABEL = { rule: '规则', global: '全局', direct: '直连' };
  var RULE_PRESET_LABEL = { 'cn-direct': '国内直连，其余走代理', 'proxy-all': '全部走代理' };
  var RULE_SOURCE_FALLBACK = { chnroutes2: 'chnroutes2', '17mon': '17mon', loyalsoldier: 'Loyalsoldier' };
  var UA_LABEL = {
    'default': '默认', curl: 'curl', v2rayn: 'V2rayN', v2rayng: 'V2rayNG',
    shadowrocket: 'Shadowrocket', custom: '自定义'
  };
  var PROXY_MODE_LABEL = { auto: '自动', direct: '仅直连', proxy: '仅走代理' };

  var rootEl = null;
  var refs = null;
  var active = false;
  var storeUn = null;
  var currentCtx = null;
  var currentSub = '';
  var draftSub = '';
  var draft = null;
  var baseline = null;
  var dirty = false;
  var fieldErrors = {};
  var pageBusy = false;
  var pageError = '';
  var restoreSnap = null;

  var diag = { active: false, token: 0, timer: null, ipqBusy: false, ipqAt: 0, refreshing: false };
  var kernel = { busy: false, check: null, uploading: false };

  var refreshActionsDebounced = MM.debounce(function () { if (active) MM.router.refreshActions(); }, 60);

  function state() { return MM.store.state; }
  function esc(v) { return MM.esc(v); }
  function setText(node, text) {
    if (!node) return;
    var s = String(text == null ? '' : text);
    if (node.textContent !== s) node.textContent = s;
  }
  function clone(o) { return o ? Object.assign({}, o) : o; }
  function pick(keys, src) {
    var out = {};
    keys.forEach(function (k) { out[k] = src ? src[k] : undefined; });
    return out;
  }
  function isForm(sub) { return !!FORM_PAGES[sub]; }
  function intVal(v) {
    var n = Number(v);
    return isFinite(n) && Math.floor(n) === n ? n : NaN;
  }
  function shortVersion(v) {
    var m = String(v || '').match(/v?\d+\.\d+\.\d+/);
    return m ? m[0] : '';
  }
  function modeLabel(v) { return MODE_LABEL[v] || v || '规则'; }
  function rulePresetLabel(v) { return RULE_PRESET_LABEL[v] || v || '—'; }
  function ruleSourceName(id) {
    if (!id) return '—';
    if (id === 'custom') return '自定义地址';
    var rs = state().ruleset;
    var list = (rs && rs.presets) || [];
    for (var i = 0; i < list.length; i++) {
      if (list[i].id === id) return list[i].name || id;
    }
    return RULE_SOURCE_FALLBACK[id] || id;
  }
  function accentName(id) {
    return (MM.theme.ACCENT_NAMES && MM.theme.ACCENT_NAMES[id]) || id;
  }

  function summaryOf(sub) {
    var s = state().settings || {};
    if (sub === 'proxy') return modeLabel(s.mode) + ' · ' + (s.redirPort || '—') + '/' + (s.mixedPort || '—');
    if (sub === 'dns') return s.dnsEnable ? (s.dnsMode || 'redir-host') : '关闭';
    if (sub === 'subscription') return s.subInterval ? '自动 · ' + s.subInterval + ' 小时' : '关闭';
    if (sub === 'ruleset') {
      var hours = s.ruleIntervalH != null ? s.ruleIntervalH : 24;
      return ruleSourceName(s.ruleSourcePreset) + ' · ' + hours + ' 小时';
    }
    if (sub === 'autoselect') return (s.autoIntervalSec != null ? s.autoIntervalSec : '—') + ' 秒 / ' +
      (s.autoToleranceMs != null ? s.autoToleranceMs : '—') + ' ms';
    if (sub === 'schedule') {
      if (!s.autoRestart) return '关闭';
      return '每 ' + (s.restartEveryHours || 24) + ' 小时' + (s.restartAt ? ' · ' + s.restartAt : '');
    }
    if (sub === 'kernel') {
      var k = state().kernel;
      var ver = shortVersion((k && k.version) || MM.store.engine().version);
      return ver || '—';
    }
    if (sub === 'backup') return '上次 ' + ((state().ui && state().ui.lastExport) || '—');
    if (sub === 'storage') {
      var rep = state().storage;
      if (!rep) return '—';
      return MM.fmt.bytes(rep.total) + ' / 可用 ' + MM.fmt.bytes(rep.free);
    }
    if (sub === 'appearance') return MM.theme.summary();
    if (sub === 'security') return (s.panelPassword ? '已设密码' : '免密') + ' · ' +
      (s.autoStart ? '开机自启' : '未开机自启');
    if (sub === 'diagnostics') {
      var svc = state().service;
      if (!svc) return '—';
      return svc.core && svc.core.reachable ? '服务可达' : '服务不可达';
    }
    return '';
  }

  function updateSummaries() {
    if (!rootEl) return;
    MM.$$('[data-sum]', rootEl).forEach(function (el) {
      setText(el, summaryOf(el.getAttribute('data-sum')));
    });
  }

  /* ---------- HTML building blocks ---------- */
  function groupCard(title, inner) {
    return (title ? '<h3 class="section-title">' + esc(title) + '</h3>' : '') +
      '<div class="group-card settings-group">' + inner + '</div>';
  }
  function block(label, inner, hint) {
    return '<div class="setting-block">' +
      '<p class="field-label">' + esc(label) + '</p>' + inner +
      (hint ? '<p class="field-hint">' + esc(hint) + '</p>' : '') + '</div>';
  }
  function switchRow(key, label, desc, disabled, warnDesc) {
    return '<div class="list-row setting-row' + (disabled ? ' is-disabled' : '') + '">' +
      '<div class="row-main"><span class="row-title">' + esc(label) + '</span>' +
      (desc ? '<span class="row-sub' + (warnDesc ? ' warn' : '') + '">' + esc(desc) + '</span>' : '') + '</div>' +
      '<label class="switch"><input type="checkbox" data-f="' + key + '" aria-label="' + esc(label) + '"' +
      (disabled ? ' disabled' : '') + '><span class="track"></span></label></div>';
  }
  function inputRow(key, label, opts) {
    opts = opts || {};
    var attrs = ' type="' + (opts.type || 'text') + '"';
    if (opts.min != null) attrs += ' min="' + opts.min + '"';
    if (opts.max != null) attrs += ' max="' + opts.max + '"';
    if (opts.type === 'number') attrs += ' inputmode="numeric" step="1"';
    else if (opts.inputmode) attrs += ' inputmode="' + opts.inputmode + '"';
    if (opts.placeholder) attrs += ' placeholder="' + esc(opts.placeholder) + '"';
    if (opts.autocomplete) attrs += ' autocomplete="' + opts.autocomplete + '"';
    if (opts.disabled) attrs += ' disabled';
    return '<div class="list-row setting-row setting-row-input' + (opts.disabled ? ' is-disabled' : '') + '">' +
      '<div class="row-main"><span class="row-title">' + esc(label) + '</span>' +
      (opts.hint ? '<span class="row-sub">' + esc(opts.hint) + '</span>' : '') +
      '<p class="field-error hidden" data-err="' + key + '"></p></div>' +
      '<input class="input setting-input" data-f="' + key + '"' + attrs + ' aria-label="' + esc(label) + '">' +
      '</div>';
  }
  function stackRow(key, label, opts) {
    opts = opts || {};
    var attrs = ' type="' + (opts.type || 'text') + '"';
    if (opts.inputmode) attrs += ' inputmode="' + opts.inputmode + '"';
    if (opts.placeholder) attrs += ' placeholder="' + esc(opts.placeholder) + '"';
    if (opts.disabled) attrs += ' disabled';
    if (opts.autocomplete) attrs += ' autocomplete="' + opts.autocomplete + '"';
    return '<div class="setting-block setting-stack' + (opts.disabled ? ' is-disabled' : '') + '">' +
      '<label class="field-label">' + esc(label) + '</label>' +
      '<input class="input" data-f="' + key + '"' + attrs + ' aria-label="' + esc(label) + '">' +
      (opts.hint ? '<p class="field-hint">' + esc(opts.hint) + '</p>' : '') +
      '<p class="field-error hidden" data-err="' + key + '"></p></div>';
  }
  function segBlock(label, key, items, disabled, hint, warn) {
    var inner = '';
    items.forEach(function (it) {
      inner += '<button type="button" data-v="' + esc(it.value) + '"' + (disabled ? ' disabled' : '') + '>' +
        esc(it.label) + '</button>';
    });
    return block(label, '<div class="segmented" data-seg="' + key + '">' + inner + '</div>' +
      (hint ? '<p class="field-hint' + (warn ? ' warn' : '') + '">' + esc(hint) + '</p>' : ''));
  }
  function optionRow(key, label, note) {
    return '<button class="list-row setting-row" type="button" data-opt="' + key + '">' +
      '<span class="row-main"><span class="row-title">' + esc(label) + '</span>' +
      (note ? '<span class="row-sub">' + esc(note) + '</span>' : '') + '</span>' +
      '<span class="row-value" data-val="' + key + '"></span>' +
      '<svg class="chev"><use href="#i-chevron"/></svg></button>';
  }
  function readonlyRow(label, value, cls) {
    return '<div class="list-row setting-row"><div class="row-main"><span class="row-title">' +
      esc(label) + '</span></div><span class="row-value ' + (cls || '') + '">' + esc(value) + '</span></div>';
  }
  function copyRow(label, value, mono) {
    var has = value != null && String(value) !== '';
    return '<div class="list-row setting-row"><div class="row-main"><span class="row-title">' +
      esc(label) + '</span></div><span class="row-value ' + (mono ? 'mono' : '') + '">' +
      esc(has ? value : '—') + '</span>' +
      (has ? '<button class="icon-btn copy-btn" type="button" data-copy="' + esc(value) + '" aria-label="复制' +
        esc(label) + '" title="复制">' + MM.icon('copy') + '</button>' : '') + '</div>';
  }
  function actionRow(name, label, desc, kind) {
    var cls = kind === 'destructive' ? 'btn btn-text btn-destructive btn-small' :
      kind === 'text' ? 'btn btn-text btn-small' : 'btn btn-tonal btn-small';
    return '<div class="list-row setting-row setting-action-row">' +
      '<div class="row-main"><span class="row-title">' + esc(label) + '</span>' +
      (desc ? '<span class="row-sub">' + esc(desc) + '</span>' : '') + '</div>' +
      '<button class="' + cls + '" type="button" data-act="' + name + '">' + esc(label) + '</button></div>';
  }
  function noteBlock(text) {
    return '<div class="setting-note">' + esc(text) + '</div>';
  }
  function navRow(id) {
    return '<button class="list-row" type="button" data-sub-nav="' + id + '">' +
      '<span class="row-main"><span class="row-title">' + esc(SUB_TITLES[id]) + '</span></span>' +
      '<span class="row-value" data-sum="' + id + '"></span>' +
      '<svg class="chev"><use href="#i-chevron"/></svg></button>';
  }
  function navHtml() {
    var html = '<div class="settings-nav-title">设置</div>';
    GROUPS.forEach(function (g) {
      html += '<div class="settings-nav-group"><div class="settings-nav-title">' + esc(g.title) + '</div>';
      g.subs.forEach(function (id) {
        html += '<button class="settings-nav-row" type="button" data-sub-nav="' + id + '">' +
          '<span class="settings-check">' + MM.icon('check') + '</span>' +
          '<span class="row-main"><span class="row-title">' + esc(SUB_TITLES[id]) + '</span></span>' +
          '<span class="row-value" data-sum="' + id + '"></span>' +
          '<svg class="chev"><use href="#i-chevron"/></svg></button>';
      });
      html += '</div>';
    });
    return html;
  }
  function mobileListHtml() {
    var html = '';
    GROUPS.forEach(function (g) {
      html += '<h3 class="section-title">' + esc(g.title) + '</h3><div class="group-card">';
      g.subs.forEach(function (id) { html += navRow(id); });
      html += '</div>';
    });
    return html;
  }

  /* ---------- mount ---------- */
  function mount(root) {
    rootEl = root;
    root.innerHTML =
      '<div class="settings-shell">' +
        '<nav class="settings-nav" aria-label="设置分组">' + navHtml() + '</nav>' +
        '<div class="settings-pane">' +
          '<div class="settings-pane-head">' +
            '<div class="settings-pane-title"><h2 data-r="paneTitle">设置</h2>' +
              '<span class="settings-pane-sub" data-r="paneSub"></span></div>' +
            '<button class="btn btn-filled btn-small settings-save hidden" type="button" data-r="save" disabled>' +
              '保存<span class="save-dot"></span></button>' +
          '</div>' +
          '<div class="error-slot" data-r="errors"></div>' +
          '<div class="settings-sub" data-r="sub"></div>' +
        '</div>' +
      '</div>';

    refs = {
      nav: root.querySelector('.settings-nav'),
      paneTitle: root.querySelector('[data-r="paneTitle"]'),
      paneSub: root.querySelector('[data-r="paneSub"]'),
      errors: root.querySelector('[data-r="errors"]'),
      save: root.querySelector('[data-r="save"]'),
      sub: root.querySelector('[data-r="sub"]')
    };

    root.addEventListener('click', function (e) {
      var nav = e.target.closest('[data-sub-nav]');
      if (nav) {
        e.preventDefault();
        var id = nav.getAttribute('data-sub-nav');
        if (id) MM.router.go('/settings/' + id);
        return;
      }
      var copy = e.target.closest('[data-copy]');
      if (copy) {
        e.preventDefault();
        MM.copy(copy.getAttribute('data-copy') || '').then(function () {
          MM.ui.toast('已复制', 'ok');
        }, function () {
          MM.ui.toast('复制失败，请手动选择文本', 'err');
        });
      }
    });

    refs.save.addEventListener('click', function () { saveCurrent(); });
  }

  /* ---------- draft / validation ---------- */
  function startDraft(sub) {
    var keys = PAGE_FIELDS[sub] || [];
    var s = state().settings || {};
    draft = pick(keys, s);
    baseline = pick(keys, s);
    draftSub = sub;
    dirty = false;
    fieldErrors = {};
  }
  function ensureDraft(sub) {
    if (!isForm(sub)) { draft = null; baseline = null; draftSub = ''; return; }
    if (draftSub !== sub || !draft || !baseline) { startDraft(sub); return; }
    if (!dirty) {
      var s = state().settings || {};
      (PAGE_FIELDS[sub] || []).forEach(function (k) { draft[k] = s[k]; baseline[k] = s[k]; });
      fieldErrors = {};
    }
  }
  function validateField(key, v, all) {
    if (key === 'redirPort' || key === 'mixedPort') {
      var n = intVal(v);
      if (!isFinite(n) || n < 1 || n > 65535) return '端口需为 1-65535 的整数';
      if (key === 'mixedPort' && intVal(all.redirPort) === n) return '不能与透明代理端口相同';
      return '';
    }
    if (key === 'subInterval') {
      var si = intVal(v);
      return (!isFinite(si) || si < 0 || si > 168) ? '需为 0-168 的整数（0 = 关闭）' : '';
    }
    if (key === 'ruleIntervalH') {
      var ri = intVal(v);
      return (!isFinite(ri) || ri < 1 || ri > 720) ? '需为 1-720 的整数' : '';
    }
    if (key === 'autoIntervalSec') {
      var ai = intVal(v);
      return (!isFinite(ai) || ai < 30 || ai > 3600) ? '需为 30-3600 秒' : '';
    }
    if (key === 'autoToleranceMs') {
      var at = intVal(v);
      return (!isFinite(at) || at < 0 || at > 1000) ? '需为 0-1000 毫秒' : '';
    }
    if (key === 'restartEveryHours') {
      var rh = intVal(v);
      return (!isFinite(rh) || rh < 1 || rh > 168) ? '需为 1-168 小时' : '';
    }
    if (key === 'restartAt') {
      var t = String(v == null ? '' : v).trim();
      return (t && !/^([01]\d|2[0-3]):[0-5]\d$/.test(t)) ? '格式应为 HH:MM（如 04:30）' : '';
    }
    if (key === 'subUACustom' && all.subUA === 'custom' && !String(v == null ? '' : v).trim()) {
      return '选择自定义 UA 时需填写';
    }
    return '';
  }
  function validateAll() {
    if (!draft || !isForm(currentSub)) { fieldErrors = {}; return; }
    fieldErrors = {};
    (PAGE_FIELDS[currentSub] || []).forEach(function (k) {
      var msg = validateField(k, draft[k], draft);
      if (msg) fieldErrors[k] = msg;
    });
  }
  function computeDirty() {
    if (!draft || !baseline || !isForm(currentSub)) { dirty = false; return; }
    dirty = (PAGE_FIELDS[currentSub] || []).some(function (k) { return draft[k] !== baseline[k]; });
  }
  function canSave() {
    if (!isForm(currentSub) || !dirty || pageBusy || !draft) return false;
    return Object.keys(fieldErrors).length === 0;
  }
  function updateErrors(container) {
    MM.$$('[data-err]', container).forEach(function (el) {
      var k = el.getAttribute('data-err');
      var msg = fieldErrors[k];
      if (msg) { el.textContent = msg; el.classList.remove('hidden'); }
      else { el.textContent = ''; el.classList.add('hidden'); }
    });
  }
  function updateSaveUI() {
    if (!refs) return;
    computeDirty();
    validateAll();
    var form = isForm(currentSub);
    refs.save.classList.toggle('hidden', !form);
    if (form) {
      refs.save.disabled = !canSave();
      refs.save.classList.toggle('has-dot', dirty);
    }
    refreshActionsDebounced();
  }
  function markChanged(key, cfg) {
    validateAll();
    if (refs && refs.sub) updateErrors(refs.sub);
    updateSaveUI();
    if (cfg && cfg.rerenderOn && cfg.rerenderOn[key]) rerenderDraft();
    else if (cfg && cfg.onChange) cfg.onChange(key);
  }
  function rerenderDraft() {
    if (currentCtx && active) renderSub(currentCtx);
  }

  /* ---------- form binding ---------- */
  function bindInputs(container, cfg) {
    MM.$$('[data-f]', container).forEach(function (el) {
      var k = el.getAttribute('data-f');
      if (el.type === 'checkbox') {
        el.checked = !!draft[k];
        el.addEventListener('change', function () {
          draft[k] = el.checked;
          markChanged(k, cfg);
        });
        return;
      }
      el.value = draft[k] == null ? '' : String(draft[k]);
      el.addEventListener('input', function () {
        if (el.type === 'number') {
          var raw = el.value.trim();
          if (raw === '') draft[k] = null;
          else {
            var n = Number(raw);
            draft[k] = isFinite(n) && Math.floor(n) === n ? n : raw;
          }
        } else {
          draft[k] = el.value;
        }
        markChanged(k, cfg);
      });
    });
  }
  function bindSegs(container, cfg) {
    MM.$$('[data-seg]', container).forEach(function (seg) {
      var key = seg.getAttribute('data-seg');
      MM.$$('button', seg).forEach(function (b) {
        b.classList.toggle('active', String(draft[key]) === b.getAttribute('data-v'));
        b.addEventListener('click', function () {
          if (b.disabled) return;
          draft[key] = b.getAttribute('data-v');
          MM.$$('button', seg).forEach(function (x) { x.classList.toggle('active', x === b); });
          markChanged(key, cfg);
        });
      });
    });
  }
  function bindOptionRows(container) {
    MM.$$('[data-opt]', container).forEach(function (row) {
      row.addEventListener('click', function () { openOption(row.getAttribute('data-opt')); });
    });
  }
  function bindActions(container, handlers) {
    MM.$$('[data-act]', container).forEach(function (btn) {
      var name = btn.getAttribute('data-act');
      if (handlers && handlers[name]) {
        btn.addEventListener('click', function (e) { e.preventDefault(); handlers[name](btn); });
      }
    });
  }
  function bindPage(container, cfg) {
    cfg = cfg || {};
    bindInputs(container, cfg);
    bindSegs(container, cfg);
    bindOptionRows(container);
    bindActions(container, cfg.actions || {});
    updateErrors(container);
    updateOptionLabels(container);
  }

  function optionItems(key) {
    if (key === 'rulePreset') {
      return [
        { label: '国内直连，其余走代理', value: 'cn-direct', note: 'CN CIDR 直连，其他流量走代理' },
        { label: '全部走代理', value: 'proxy-all', note: '所有流量交给当前策略组' }
      ];
    }
    if (key === 'subUA') {
      return [
        { label: '默认', value: 'default', note: 'Mihomo 默认 UA' },
        { label: 'curl', value: 'curl' },
        { label: 'V2rayN', value: 'v2rayn' },
        { label: 'V2rayNG', value: 'v2rayng' },
        { label: 'Shadowrocket', value: 'shadowrocket' },
        { label: '自定义', value: 'custom' }
      ];
    }
    if (key === 'subProxyMode') {
      return [
        { label: '自动', value: 'auto', note: '直连失败后再走代理' },
        { label: '仅直连', value: 'direct' },
        { label: '仅走代理', value: 'proxy' }
      ];
    }
    if (key === 'ruleSourcePreset') {
      var rs = state().ruleset;
      var list = (rs && rs.presets) || [
        { id: 'chnroutes2', name: 'chnroutes2（推荐）', note: '稳定直连列表', lines: '≈3900' },
        { id: '17mon', name: '17mon china_ip_list', note: '覆盖更广', lines: '≈7400' },
        { id: 'loyalsoldier', name: 'Loyalsoldier/geoip', note: '持续维护', lines: '≈10000' }
      ];
      var items = list.map(function (p) {
        return { label: p.name || p.id, value: p.id, note: (p.note || '') + (p.lines ? ' · ' + p.lines : '') };
      });
      items.push({ label: '自定义地址', value: 'custom', note: '使用下方填写的规则源地址' });
      return items;
    }
    return [];
  }
  function optionTitle(key) {
    if (key === 'rulePreset') return '规则预设';
    if (key === 'subUA') return '请求 UA 预设';
    if (key === 'subProxyMode') return '下载方式';
    if (key === 'ruleSourcePreset') return '规则源预设';
    return '选择';
  }
  function optionLabel(key, value) {
    if (key === 'rulePreset') return rulePresetLabel(value);
    if (key === 'subUA') return UA_LABEL[value] || value || '—';
    if (key === 'subProxyMode') return PROXY_MODE_LABEL[value] || value || '—';
    if (key === 'ruleSourcePreset') return ruleSourceName(value);
    return value == null ? '—' : String(value);
  }
  function updateOptionLabels(container) {
    MM.$$('[data-val]', container).forEach(function (el) {
      var k = el.getAttribute('data-val');
      var v = draft ? draft[k] : (state().settings || {})[k];
      setText(el, optionLabel(k, v));
    });
  }
  function openOption(key) {
    if (!draft) return;
    var items = optionItems(key);
    if (!items.length) return;
    MM.ui.options({ title: optionTitle(key), value: draft[key], items: items }).then(function (v) {
      if (v === undefined || v === null) return;
      draft[key] = v;
      if (refs && refs.sub) updateOptionLabels(refs.sub);
      updateSaveUI();
      if (key === 'subUA' || key === 'ruleSourcePreset') rerenderDraft();
    });
  }

  /* ---------- page chrome ---------- */
  function updateNavActive(sub) {
    if (!refs || !refs.nav) return;
    MM.$$('[data-sub-nav]', refs.nav).forEach(function (b) {
      var on = b.getAttribute('data-sub-nav') === sub;
      b.classList.toggle('active', on);
      if (on) b.setAttribute('aria-current', 'page');
      else b.removeAttribute('aria-current');
    });
  }
  function setPaneHead(sub) {
    if (!refs) return;
    updateNavActive(sub);
    setText(refs.paneTitle, sub ? (SUB_TITLES[sub] || '设置') : '设置');
    setText(refs.paneSub, sub ? summaryOf(sub) : '12 项设置');
    var form = isForm(sub);
    refs.save.classList.toggle('hidden', !form);
    if (form) {
      refs.save.disabled = !canSave();
      refs.save.classList.toggle('has-dot', dirty);
    }
  }
  function showError(msg) {
    pageError = msg || '';
    if (!refs) return;
    refs.errors.textContent = '';
    if (!pageError) return;
    refs.errors.appendChild(MM.ui.errorBar(pageError));
  }
  function clearError() {
    pageError = '';
    if (refs) refs.errors.textContent = '';
  }

  function renderSub(ctx) {
    if (!refs) return;
    var sub = (ctx && ctx.sub) || '';
    currentSub = sub;
    setPaneHead(sub);
    if (!sub) {
      if (MM.router.isMobile()) refs.sub.innerHTML = mobileListHtml();
      else refs.sub.textContent = '';
      updateSummaries();
      return;
    }
    if (isForm(sub)) ensureDraft(sub);
    else { draft = null; baseline = null; draftSub = ''; }
    if (sub === 'proxy') renderProxy(refs.sub);
    else if (sub === 'dns') renderDns(refs.sub);
    else if (sub === 'subscription') renderSubscription(refs.sub);
    else if (sub === 'ruleset') renderRuleset(refs.sub);
    else if (sub === 'autoselect') renderAutoselect(refs.sub);
    else if (sub === 'schedule') renderSchedule(refs.sub);
    else if (sub === 'kernel') renderKernel(refs.sub);
    else if (sub === 'backup') renderBackup(refs.sub);
    else if (sub === 'storage') renderStorage(refs.sub);
    else if (sub === 'appearance') renderAppearance(refs.sub);
    else if (sub === 'security') renderSecurity(refs.sub);
    else if (sub === 'diagnostics') renderDiagnostics(refs.sub);
    else refs.sub.textContent = '';
    updateSummaries();
  }

  /* ---------- save ---------- */
  function saveCurrent() {
    if (!canSave()) return;
    var sub = currentSub;
    var body = {};
    (PAGE_FIELDS[sub] || []).forEach(function (k) {
      var v = draft[k];
      if (typeof v === 'string' && (k === 'lanIf' || k === 'restartAt')) v = v.trim();
      body[k] = v;
    });
    pageBusy = true;
    clearError();
    updateSaveUI();
    MM.api('/api/settings', { method: 'PUT', body: body }).then(function (d) {
      var got = (d && d.settings) || {};
      MM.store.set({ settings: Object.assign({}, state().settings || {}, got) });
      MM.ui.toast('设置已保存', 'ok');
      return MM.data.loadSettings().catch(function () { return null; });
    }).then(function () {
      if (!active || currentSub !== sub) return;
      startDraft(sub);
      renderSub(currentCtx);
      updateSummaries();
    }).catch(function (e) {
      showError((e && e.message) || '保存设置失败');
      MM.ui.toast((e && e.message) || '保存设置失败', 'err');
    }).finally(function () {
      pageBusy = false;
      if (active) updateSaveUI();
    });
  }

  /* ---------- engines ---------- */
  function reloadStatusService() {
    MM.sched.markDue('status');
    return Promise.allSettled([
      MM.data.loadStatus(),
      (MM.data.loadService ? MM.data.loadService() : Promise.resolve(null))
    ]);
  }
  function engineOp(path, okMsg, btn) {
    if (btn) MM.ui.setBusy(btn, true);
    return MM.api(path, { method: 'POST' }).then(function (d) {
      MM.ui.toast((d && d.message) || okMsg, 'ok');
      return reloadStatusService();
    }).catch(function (e) {
      MM.ui.toast((e && e.message) || '操作失败', 'err');
    }).finally(function () {
      if (btn) MM.ui.setBusy(btn, false);
    });
  }

  /* ---------- 4.9.1 proxy ---------- */
  function renderProxy(container) {
    container.innerHTML =
      groupCard('运行模式',
        segBlock('运行模式', 'mode', [
          { label: '规则', value: 'rule' }, { label: '全局', value: 'global' }, { label: '直连', value: 'direct' }
        ]) +
        optionRow('rulePreset', '规则预设', 'CN CIDR 直连，其余流量交给策略组')) +
      groupCard('端口与转发',
        inputRow('redirPort', '透明代理端口', { type: 'number', min: 1, max: 65535 }) +
        inputRow('mixedPort', '混合端口（HTTP/SOCKS）', { type: 'number', min: 1, max: 65535 }) +
        switchRow('allowLan', '允许局域网访问代理端口') +
        switchRow('blockQuic', '阻断 QUIC（UDP 443）', '避免 UDP 绕过透明代理，逼回 TCP') +
        switchRow('sniffer', '域名嗅探') +
        switchRow('routerSelf', '同时代理路由器自身（实验）', '可能造成回环，默认关闭', false, true)) +
      groupCard('高级',
        inputRow('lanIf', '局域网接口', { placeholder: 'br0', hint: '路由器 LAN 接口，默认 br0' })) +
      groupCard('维护',
        actionRow('reload', '重载配置', '重新生成配置并热重载，不中断已有连接', 'tonal') +
        actionRow('restart', '重启内核', '内核会短暂中断，当前连接会断开', 'destructive'));

    bindPage(container, {
      actions: {
        reload: function (btn) { engineOp('/api/engine/reload', '配置已重载', btn); },
        restart: function () {
          MM.ui.confirm({
            title: '重启内核？',
            body: '重启期间代理会短暂中断，已建立的连接会被断开。',
            confirm: '重启', destructive: true
          }).then(function (ok) { if (ok) engineOp('/api/engine/restart', '内核已重启'); });
        }
      }
    });
  }

  /* ---------- 4.9.2 DNS ---------- */
  function renderDns(container) {
    var on = !!draft.dnsEnable;
    var fake = draft.dnsMode === 'fake-ip';
    container.innerHTML =
      groupCard('DNS',
        switchRow('dnsEnable', '启用 mihomo DNS', '默认交给路由器 dnsmasq') +
        segBlock('模式', 'dnsMode', [
          { label: 'redir-host', value: 'redir-host' }, { label: 'fake-ip', value: 'fake-ip' }
        ], !on, fake ? 'fake-ip 会接管域名解析，请确认上游可用' : '', fake) +
        stackRow('dnsListen', '监听地址', { placeholder: '0.0.0.0:1053', hint: '留空表示不监听', disabled: !on }) +
        stackRow('dnsNameserver', '上游 DNS', { placeholder: '223.5.5.5,119.29.29.29', hint: '逗号分隔', disabled: !on }) +
        stackRow('dnsFallback', '回落 DNS', { placeholder: '1.1.1.1,8.8.8.8', hint: '逗号分隔', disabled: !on }) +
        (fake ? stackRow('dnsFakeIpRange', 'fake-ip 网段', {
          placeholder: '198.18.0.1/16', disabled: !on
        }) : ''));
    bindPage(container, { rerenderOn: { dnsEnable: 1, dnsMode: 1 } });
  }

  /* ---------- 4.9.3 subscription ---------- */
  function renderSubscription(container) {
    container.innerHTML =
      groupCard('拉取',
        optionRow('subUA', '请求 UA 预设', '部分机场按 UA 返回不同格式') +
        (draft.subUA === 'custom' ? stackRow('subUACustom', '自定义 UA', {
          placeholder: '例如 Clash/1.0', autocomplete: 'off'
        }) : '') +
        optionRow('subProxyMode', '下载方式', '自动：直连失败后再走代理') +
        switchRow('subInsecure', '跳过订阅证书校验', '路由器时间不准时必需')) +
      groupCard('更新',
        inputRow('subInterval', '订阅自动更新间隔（小时）', {
          type: 'number', min: 0, max: 168, hint: '0 = 关闭，单个订阅可在编辑里覆盖'
        })) +
      groupCard('说明',
        noteBlock('单个订阅的更新间隔在订阅编辑 sheet 里设置；填 0 表示跟随这里的全局间隔。'));
    bindPage(container, { rerenderOn: { subUA: 1 } });
  }

  /* ---------- 4.9.4 ruleset ---------- */
  function rulesetMetaRows() {
    var rs = state().ruleset;
    var meta = (rs && rs.meta) || null;
    if (!meta) {
      return readonlyRow('条数', '—') + readonlyRow('大小', '—') +
        readonlyRow('更新时间', '—') + readonlyRow('来源', '—');
    }
    return readonlyRow('条数', meta.count != null ? Number(meta.count) + ' 条' : '—') +
      readonlyRow('大小', meta.bytes != null ? MM.fmt.bytes(meta.bytes) : '—') +
      readonlyRow('更新时间', meta.updatedAt ? MM.fmt.time(meta.updatedAt) : '—') +
      readonlyRow('来源', meta.source || '—');
  }
  function renderRuleset(container) {
    container.innerHTML =
      groupCard('当前规则', '<div data-r="meta">' + rulesetMetaRows() + '</div>') +
      groupCard('规则源',
        optionRow('ruleSourcePreset', '预设来源', '用于 RULE-SET,cn-ip,DIRECT') +
        (draft.ruleSourcePreset === 'custom' ? stackRow('ruleSourceURL', '自定义地址', {
          placeholder: 'CIDR 文本地址', autocomplete: 'off', hint: '留空时使用内置镜像'
        }) : '')) +
      groupCard('更新',
        switchRow('ruleAutoUpdate', '自动更新') +
        inputRow('ruleIntervalH', '更新间隔（小时）', { type: 'number', min: 1, max: 720 }) +
        actionRow('ruleupdate', '立即更新', '下载、校验 CIDR 并热重载内核', 'tonal')) +
      groupCard('说明',
        noteBlock('规则页右上角的「规则源」入口会回到这里；修改来源并保存后会立即重新下载。'));
    bindPage(container, {
      rerenderOn: { ruleSourcePreset: 1 },
      actions: { ruleupdate: updateRuleSetNow }
    });
  }
  function updateRuleSetNow(btn) {
    if (pageBusy) return;
    validateAll();
    if (Object.keys(fieldErrors).length) { updateErrors(refs.sub); return; }
    var body = {};
    if (draft.ruleSourcePreset === 'custom') body.url = String(draft.ruleSourceURL || '').trim();
    else if (draft.ruleSourcePreset) body.preset = draft.ruleSourcePreset;
    pageBusy = true;
    if (btn) MM.ui.setBusy(btn, true);
    updateSaveUI();
    var prog = MM.ui.progress('正在更新国内 IP 规则…');
    prog.add({ msg: '准备下载规则源…', at: Date.now() / 1000 });
    MM.api('/api/rules/update', { method: 'POST', body: body }).then(function (d) {
      prog.add({ msg: d && d.meta ? '规则集已更新：' + (d.meta.count || 0) + ' 条' : '规则集已更新', done: true, at: Date.now() / 1000 });
      MM.ui.toast('规则集已更新', 'ok');
      return MM.data.loadRuleSet().catch(function () { return null; });
    }).then(function () {
      var host = refs.sub && refs.sub.querySelector('[data-r="meta"]');
      if (host) host.innerHTML = rulesetMetaRows();
      updateOptionLabels(refs.sub);
    }).catch(function (e) {
      prog.add({ msg: (e && e.message) || '更新规则集失败', err: true, at: Date.now() / 1000 });
      MM.ui.toast((e && e.message) || '更新规则集失败', 'err');
    }).finally(function () {
      pageBusy = false;
      if (btn) MM.ui.setBusy(btn, false);
      if (active && currentSub === 'ruleset') updateSaveUI();
      prog.finish();
      setTimeout(function () { prog.close(); }, 900);
    });
  }

  /* ---------- 4.9.5 autoselect ---------- */
  function renderAutoselect(container) {
    container.innerHTML =
      groupCard('自动选择',
        inputRow('autoIntervalSec', '检查间隔（秒）', { type: 'number', min: 30, max: 3600 }) +
        inputRow('autoToleranceMs', '容差（毫秒）', { type: 'number', min: 0, max: 1000 })) +
      groupCard('说明',
        noteBlock('间隔越短，故障切换越快，但会消耗更多流量；容差用于避免节点之间来回抖动。'));
    bindPage(container, {});
  }

  /* ---------- 4.9.6 schedule ---------- */
  function renderSchedule(container) {
    var eng = MM.store.engine();
    var last = eng.lastRestartAt ? MM.fmt.time(eng.lastRestartAt) : '—';
    var next = eng.nextRestartAt ? MM.fmt.time(eng.nextRestartAt) : '—';
    container.innerHTML =
      groupCard('定时重启',
        switchRow('autoRestart', '启用定时重启', '按间隔或每天固定时刻重启内核') +
        inputRow('restartEveryHours', '每 N 小时重启', { type: 'number', min: 1, max: 168 }) +
        inputRow('restartAt', '每天固定时刻', {
          placeholder: 'HH:MM', inputmode: 'numeric', hint: '留空表示不按固定时刻重启'
        })) +
      groupCard('状态',
        readonlyRow('上次重启', last) +
        readonlyRow('下次重启', next)) +
      groupCard('说明',
        noteBlock('固定时刻与按小时间隔同时填写时，两个条件都会触发重启。'));
    bindPage(container, {});
  }

  /* ---------- 4.9.11 security ---------- */
  function renderSecurity(container) {
    var s = state().settings || {};
    container.innerHTML =
      groupCard('启动',
        switchRow('autoStart', '开机自启', '面板守护 + 引擎跟随启用状态')) +
      groupCard('访问',
        stackRow('panelPassword', '面板密码', {
          type: 'password', placeholder: '留空 = 免密', autocomplete: 'new-password',
          hint: '仅建议在可信 LAN 内使用；密码会明文保存在路由器数据目录'
        }) +
        readonlyRow('当前状态', s.panelPassword ? '已设置密码' : '免密')) +
      groupCard('说明',
        noteBlock('修改密码后，已登录的浏览器会在下次请求时重新要求登录。'));
    bindPage(container, {});
  }

  /* ---------- shared lifecycle helpers ---------- */
  function stopDiagnostics() {
    diag.active = false;
    diag.token++;
    if (diag.timer) { clearInterval(diag.timer); diag.timer = null; }
  }
  function loadPageData(ctx) {
    if (!ctx) return;
    if (!ctx.sub) {
      MM.data.loadStatus().catch(function () {});
      (MM.data.loadService ? MM.data.loadService() : MM.data.loadStatus()).catch(function () {});
      MM.data.loadKernel().catch(function () {});
      MM.data.loadStorage().catch(function () {});
      return;
    }
    var sub = ctx.sub;
    if (sub === 'diagnostics') { startDiagnostics(); return; }
    if (sub === 'kernel') {
      MM.data.loadKernel().catch(function () {}).then(function () {
        if (active && currentSub === 'kernel') renderKernelInfoOnly();
        updateSummaries();
      });
      MM.data.loadStatus().catch(function () {});
      return;
    }
    if (sub === 'storage') {
      MM.data.loadStorage().catch(function () {}).then(function () {
        if (active && currentSub === 'storage' && !pageBusy) renderSub(currentCtx);
        updateSummaries();
      });
      return;
    }
    if (sub === 'ruleset') {
      MM.data.loadRuleSet().catch(function () {}).then(function () {
        if (!active || currentSub !== 'ruleset') return;
        var host = refs.sub && refs.sub.querySelector('[data-r="meta"]');
        if (host) host.innerHTML = rulesetMetaRows();
        updateOptionLabels(refs.sub);
        updateSummaries();
      });
      MM.data.loadSettings().catch(function () {});
      return;
    }
    if (sub === 'backup' || sub === 'appearance') return;
    MM.data.loadSettings().catch(function () {}).then(function () {
      if (!active || currentSub !== sub) return;
      if (pageBusy || (isForm(sub) && dirty)) return;
      renderSub(currentCtx);
      updateSummaries();
    });
    if (sub === 'schedule') MM.data.loadStatus().catch(function () {});
  }

  function activate(ctx, sec) {
    if (!rootEl) rootEl = sec;
    if (!refs) return;
    active = true;
    currentCtx = ctx;
    if (!storeUn) {
      storeUn = MM.store.sub(function () {
        if (!active) return;
        updateSummaries();
        if (refs && isForm(currentSub)) setPaneHead(currentSub);
      });
    }
    var sub = (ctx && ctx.sub) || '';
    var sameDirty = sub === currentSub && sub && isForm(sub) && dirty;
    if (restoreSnap && restoreSnap.sub === sub) {
      draft = clone(restoreSnap.draft);
      baseline = clone(restoreSnap.baseline);
      draftSub = sub;
      dirty = true;
      restoreSnap = null;
      validateAll();
      renderSub(ctx);
    } else if (sameDirty) {
      setPaneHead(sub);
      updateSaveUI();
      updateSummaries();
    } else {
      if (sub !== currentSub) { clearError(); fieldErrors = {}; }
      renderSub(ctx);
    }
    loadPageData(ctx);
  }

  function deactivate(ctx) {
    active = false;
    if (storeUn) { storeUn(); storeUn = null; }
    stopDiagnostics();
    if (dirty && draftSub && isForm(draftSub)) {
      var snap = {
        sub: draftSub, draft: clone(draft), baseline: clone(baseline),
        path: (ctx && ctx.path) || ('/settings/' + draftSub)
      };
      dirty = false;
      restoreSnap = snap;
      MM.ui.confirm({
        title: '放弃未保存的更改？',
        body: '离开「' + (SUB_TITLES[snap.sub] || '设置') + '」后，未保存的修改将丢失。',
        confirm: '放弃', destructive: true
      }).then(function (ok) {
        if (restoreSnap !== snap) return;
        if (ok) restoreSnap = null;
        else MM.router.go(snap.path);
      });
    }
  }

  /* ---------- 4.9.10 appearance ---------- */
  function renderAppearance(container) {
    var theme = MM.theme.prefTheme();
    var accent = MM.theme.prefAccent();
    var mono = MM.theme.prefMono();
    var low = MM.theme.prefLowEnd();
    var dark = MM.theme.resolved() === 'dark';
    var swatches = '';
    MM.theme.ACCENTS.forEach(function (a) {
      var selected = a === accent;
      swatches += '<button class="settings-swatch' + (selected ? ' selected' : '') + '" type="button" data-swatch="' +
        esc(a) + '" data-accent="' + esc(a) + '" aria-label="' + esc(accentName(a)) + '" aria-pressed="' +
        (selected ? 'true' : 'false') + '">' +
        '<span class="swatch-dot">' + MM.icon('check') + '</span>' +
        '<span class="swatch-name">' + esc(accentName(a)) + '</span></button>';
    });
    container.innerHTML =
      groupCard('主题',
        segBlock('主题', 'appearanceTheme', [
          { label: '浅色', value: 'light' }, { label: '深色', value: 'dark' }, { label: '跟随系统', value: 'system' }
        ])) +
      groupCard('主色',
        '<div class="setting-block"><div class="settings-swatch-grid">' + swatches + '</div>' +
        '<p class="field-hint">主色只影响交互元素；表面与状态色保持不变。</p></div>') +
      groupCard('纯黑',
        switchRow('appearanceMono', '纯黑背景（仅深色）', dark ? '深色主题下使用纯黑页面背景' : '浅色主题下不生效', !dark)) +
      groupCard('动效 / 性能',
        segBlock('低端模式', 'appearanceLow', [
          { label: '自动', value: 'auto' }, { label: '开启', value: 'on' }, { label: '关闭', value: 'off' }
        ], false, '自动：根据 CPU 核心数、内存与系统减少动效设置判断')) +
      groupCard('预览',
        '<div class="setting-block settings-preview">' +
          '<div class="preview-row"><button class="btn btn-filled btn-small" type="button">主按钮</button>' +
          '<span class="chip chip-accent">选中</span><span class="chip chip-ok"><i></i>正常</span></div>' +
          '<input class="input" value="预览输入框" readonly aria-label="预览输入框">' +
          '<div class="preview-row"><span class="lat lat-ok">86 ms</span>' +
          '<span class="tiny muted">延迟只染文字</span></div>' +
        '</div>');

    var themeSeg = container.querySelector('[data-seg="appearanceTheme"]');
    if (themeSeg) {
      MM.$$('button', themeSeg).forEach(function (b) {
        b.classList.toggle('active', b.getAttribute('data-v') === theme);
        b.addEventListener('click', function () {
          MM.theme.setTheme(b.getAttribute('data-v'));
          renderSub(currentCtx);
          updateSummaries();
        });
      });
    }
    MM.$$('[data-swatch]', container).forEach(function (b) {
      b.addEventListener('click', function () {
        MM.theme.setAccent(b.getAttribute('data-swatch'));
        renderSub(currentCtx);
        updateSummaries();
      });
    });
    var monoInput = container.querySelector('[data-f="appearanceMono"]');
    if (monoInput) {
      monoInput.checked = mono;
      monoInput.addEventListener('change', function () {
        MM.theme.setMono(monoInput.checked);
        updateSummaries();
      });
    }
    var lowSeg = container.querySelector('[data-seg="appearanceLow"]');
    if (lowSeg) {
      MM.$$('button', lowSeg).forEach(function (b) {
        b.classList.toggle('active', b.getAttribute('data-v') === low);
        b.addEventListener('click', function () {
          MM.theme.setLowEnd(b.getAttribute('data-v'));
          renderSub(currentCtx);
        });
      });
    }
  }

  /* ---------- 4.9.8 backup ---------- */
  function backupTimeLabel(name) {
    return name ? name : '—';
  }
  function renderBackup(container) {
    var ui = state().ui || {};
    container.innerHTML =
      '<div class="error-slot" data-b="notice"></div>' +
      groupCard('备份',
        actionRow('backupExport', '导出备份', '下载 JSON，包含订阅地址与令牌，请妥善保管', 'tonal') +
        readonlyRow('上次导出', backupTimeLabel(ui.lastExport))) +
      groupCard('恢复',
        '<div class="setting-block"><label class="dropzone" data-b="drop">' +
          MM.icon('upload') + '<span>选择备份文件（.json，上限 16MB）</span>' +
          '<input type="file" accept=".json,application/json" data-b="file" aria-label="选择备份文件"></label>' +
          '<p class="field-hint">导入会覆盖当前设置、订阅与节点文件。</p></div>' +
        readonlyRow('上次导入', backupTimeLabel(ui.lastImport))) +
      groupCard('说明',
        noteBlock('导出文件包含订阅地址和令牌，任何拿到文件的人都能读取你的订阅。恢复前建议先导出一份当前配置。'));

    var notice = container.querySelector('[data-b="notice"]');
    var file = container.querySelector('[data-b="file"]');
    var drop = container.querySelector('[data-b="drop"]');
    function show(kind, msg) {
      notice.textContent = '';
      var n = document.createElement('div');
      n.className = 'notice ' + (kind || '');
      n.innerHTML = MM.icon(kind === 'err' ? 'warning' : kind === 'ok' ? 'check' : 'info') + '<span></span>';
      n.querySelector('span').textContent = msg;
      notice.appendChild(n);
    }
    var exportBtn = container.querySelector('[data-act="backupExport"]');
    if (exportBtn) exportBtn.addEventListener('click', function () { doExport(exportBtn, show); });
    if (drop) {
      ['dragenter', 'dragover'].forEach(function (ev) {
        drop.addEventListener(ev, function (e) { e.preventDefault(); drop.classList.add('drag-over'); });
      });
      ['dragleave', 'drop'].forEach(function (ev) {
        drop.addEventListener(ev, function (e) { e.preventDefault(); drop.classList.remove('drag-over'); });
      });
      drop.addEventListener('drop', function (e) {
        var f = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
        if (f) importBackup(f, show);
      });
    }
    if (file) {
      file.addEventListener('change', function () {
        if (file.files && file.files[0]) importBackup(file.files[0], show);
        file.value = '';
      });
    }
  }
  function doExport(btn, show) {
    MM.ui.setBusy(btn, true);
    MM.api('/api/backup').then(function (data) {
      var name = 'mihomo-merlin-backup-' + MM.fmt.date(Date.now() / 1000).replace(/-/g, '') + '-' +
        MM.fmt.clock().replace(/:/g, '') + '.json';
      var blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      var url = URL.createObjectURL(blob);
      var a = document.createElement('a');
      a.href = url;
      a.download = name;
      document.body.appendChild(a);
      a.click();
      setTimeout(function () { URL.revokeObjectURL(url); if (a.parentNode) a.parentNode.removeChild(a); }, 1000);
      MM.store.patchUI({ lastExport: name });
      MM.ui.toast('备份已导出', 'ok');
      if (show) show('ok', '备份已导出：' + name);
      if (refs && currentSub === 'backup') renderSub(currentCtx);
      updateSummaries();
    }).catch(function (e) {
      if (show) show('err', (e && e.message) || '导出备份失败');
      MM.ui.toast((e && e.message) || '导出备份失败', 'err');
    }).finally(function () { MM.ui.setBusy(btn, false); });
  }
  function readTextFile(file, cb) {
    var reader = new FileReader();
    reader.onload = function () { cb(null, String(reader.result || '')); };
    reader.onerror = function () { cb(new Error('读取文件失败')); };
    reader.readAsText(file);
  }
  function importBackup(file, show) {
    if (file.size > 16 * 1024 * 1024) {
      show('err', '备份文件超过 16MB');
      return;
    }
    readTextFile(file, function (err, text) {
      if (err) { show('err', err.message); return; }
      var bundle;
      try { bundle = JSON.parse(text); }
      catch (e) { show('err', '备份文件不是合法 JSON'); return; }
      MM.ui.confirm({
        title: '恢复备份？',
        body: '将用「' + (file.name || '备份文件') + '」覆盖当前设置、订阅与节点文件，内核运行中会自动重载。',
        confirm: '恢复', destructive: true
      }).then(function (ok) {
        if (!ok) return;
        show('', '正在恢复…');
        MM.api('/api/restore', { method: 'POST', body: bundle }).then(function (d) {
          MM.store.patchUI({ lastImport: file.name || '备份文件' });
          MM.ui.toast((d && d.message) || '备份已恢复', 'ok');
          show('ok', (d && d.message) || '备份已恢复');
          MM.sched.markDue('settings');
          MM.sched.markDue('subs');
          MM.sched.markDue('status');
          return Promise.allSettled([
            MM.data.loadSettings(), MM.data.loadSubs(), MM.data.loadStatus()
          ]);
        }).then(function () {
          if (refs && currentSub === 'backup') renderSub(currentCtx);
          updateSummaries();
        }).catch(function (e) {
          show('err', (e && e.message) || '恢复备份失败');
          MM.ui.toast((e && e.message) || '恢复备份失败', 'err');
        });
      });
    });
  }

  /* ---------- 4.9.9 storage ---------- */
  function storageRows(rep) {
    if (!rep) {
      return '<div class="setting-note">正在读取存储占用…</div>';
    }
    var wanted = ['节点文件', '规则集', '原始订阅缓存', '内核备份', '运行日志'];
    var map = {};
    (rep.items || []).forEach(function (it) {
      if (wanted.indexOf(it.name) >= 0) map[it.name] = it;
      else {
        if (!map['配置/设置文件']) map['配置/设置文件'] = { name: '配置/设置文件', bytes: 0, files: 0 };
        map['配置/设置文件'].bytes += Number(it.bytes) || 0;
        map['配置/设置文件'].files += Number(it.files) || 0;
      }
    });
    var rows = '';
    wanted.concat(['配置/设置文件']).forEach(function (name) {
      var it = map[name] || { bytes: 0, files: 0 };
      rows += readonlyRow(name, MM.fmt.bytes(it.bytes) + ' · ' + (it.files || 0) + ' 个文件');
    });
    return rows;
  }
  function renderStorage(container) {
    var rep = state().storage;
    var total = rep ? MM.fmt.bytes(rep.total) : '—';
    var free = rep ? MM.fmt.bytes(rep.free) : '—';
    container.innerHTML =
      '<div class="error-slot" data-st="notice"></div>' +
      groupCard('占用',
        '<div class="setting-block storage-figure">' +
          '<div class="storage-figure-cell"><span class="k">总占用</span><b>' + esc(total) + '</b></div>' +
          '<div class="storage-figure-cell"><span class="k">可用空间</span><b>' + esc(free) + '</b></div>' +
          (rep && rep.dataDir ? '<div class="storage-figure-cell storage-dir"><span class="k">数据目录</span><b class="mono">' +
            esc(rep.dataDir) + '</b></div>' : '') +
        '</div>') +
      groupCard('明细', '<div data-st="items">' + storageRows(rep) + '</div>') +
      groupCard('清理',
        actionRow('storageClean', '立即清理', '删除无主节点文件、原始缓存、临时文件与旧内核备份', 'tonal')) +
      groupCard('说明',
        noteBlock('清理不会删除现有订阅与设置；内核备份只保留最新一份，释放的空间取决于无主文件数量。'));

    var notice = container.querySelector('[data-st="notice"]');
    function show(kind, msg) {
      notice.textContent = '';
      var n = document.createElement('div');
      n.className = 'notice ' + (kind || '');
      n.innerHTML = MM.icon(kind === 'err' ? 'warning' : kind === 'ok' ? 'check' : 'info') + '<span></span>';
      n.querySelector('span').textContent = msg;
      notice.appendChild(n);
    }
    var btn = container.querySelector('[data-act="storageClean"]');
    if (btn) btn.addEventListener('click', function () {
      MM.ui.confirm({
        title: '清理无用文件？',
        body: '将删除无主节点文件、原始订阅缓存、临时文件与旧内核备份，不影响现有订阅与设置。',
        confirm: '清理', destructive: false
      }).then(function (ok) {
        if (!ok) return;
        MM.ui.setBusy(btn, true);
        MM.api('/api/storage/clean', { method: 'POST' }).then(function (d) {
          var n = (d && d.steps && d.steps.length) || 0;
          var freed = MM.fmt.bytes((d && d.freed) || 0);
          MM.ui.toast('已清理 ' + n + ' 项，释放 ' + freed, 'ok');
          show('ok', '已清理 ' + n + ' 项，释放 ' + freed);
          return MM.data.loadStorage();
        }).then(function () {
          if (refs && currentSub === 'storage') renderSub(currentCtx);
          updateSummaries();
        }).catch(function (e) {
          show('err', (e && e.message) || '清理失败');
          MM.ui.toast((e && e.message) || '清理失败', 'err');
        }).finally(function () { MM.ui.setBusy(btn, false); });
      });
    });
  }

  /* ---------- 4.9.7 kernel ---------- */
  function kernelInfoRows() {
    var k = state().kernel;
    if (!k) {
      return readonlyRow('版本', '读取中…') + readonlyRow('大小', '—') +
        readonlyRow('安装时间', '—') + readonlyRow('SHA-256', '—', 'mono') +
        readonlyRow('可回滚版本', '—');
    }
    return readonlyRow('版本', k.version || '—') +
      readonlyRow('大小', k.size != null ? MM.fmt.bytes(k.size) : '—') +
      readonlyRow('安装时间', k.modTime ? MM.fmt.time(k.modTime) : '—') +
      copyRow('SHA-256', k.sha256 || '', true) +
      readonlyRow('可回滚版本', k.backupVersion ? (shortVersion(k.backupVersion) || '可回滚') : '无') +
      (k.backupVersion && k.backupTime ? readonlyRow('备份时间', MM.fmt.time(k.backupTime)) : '') +
      (k.path ? readonlyRow('路径', k.path, 'mono') : '');
  }
  function kernelUpdateInfoRows() {
    var c = kernel.check;
    if (!c) return '<div class="setting-note">点击「检查更新」获取最新 slim 内核版本。</div>';
    if (!c.hasUpdate) {
      return '<div class="setting-note">已是最新版本' +
        (shortVersion(c.current) ? '（' + esc(shortVersion(c.current)) + '）' : '') + '。</div>';
    }
    return readonlyRow('当前版本', shortVersion(c.current) || c.current || '—') +
      readonlyRow('最新版本', shortVersion(c.latest) || c.latest || '—') +
      readonlyRow('文件名', c.asset || '—') +
      readonlyRow('大小', c.size != null && c.size >= 0 ? MM.fmt.bytes(c.size) : '—') +
      (c.notes ? '<div class="setting-note">' + esc(c.notes) + '</div>' : '');
  }
  function renderKernel(container) {
    var hasBackup = !!(state().kernel && state().kernel.backupVersion);
    container.innerHTML =
      '<div class="error-slot" data-k="notice"></div>' +
      groupCard('当前内核', '<div data-k="info">' + kernelInfoRows() + '</div>') +
      groupCard('手动更换',
        '<div class="setting-block">' +
          '<label class="dropzone" data-k="drop">' + MM.icon('upload') +
            '<span>拖入 mihomo 二进制，或点击选择</span>' +
            '<small>支持原始 linux-armv7 可执行文件或 .gz，上限 96MB</small>' +
            '<input type="file" data-k="file" aria-label="选择内核文件"></label>' +
          '<div class="bar hidden" data-k="bar"><span></span></div>' +
          '<p class="field-hint" data-k="progress"></p>' +
        '</div>') +
      groupCard('在线更新',
        '<div class="setting-block setting-kernel-update" data-k="updateInfo">' + kernelUpdateInfoRows() + '</div>' +
        actionRow('kcheck', '检查更新', '读取 GitHub release 的最新 slim 内核', 'tonal') +
        actionRow('kupdate', '立即更新', '下载并替换内核，期间代理会短暂中断', 'tonal')) +
      groupCard('回滚',
        actionRow('krollback', '回滚上一版本', hasBackup ? '切换到上一个备份内核' : '当前没有可回滚的备份', 'destructive')) +
      groupCard('说明',
        noteBlock('上传、在线更新与回滚都会重启内核。校验失败不会改动当前内核，服务端会返回具体原因。'));

    bindKernel(container);
    updateKernelButtons();
  }
  function bindKernel(container) {
    var file = container.querySelector('[data-k="file"]');
    var drop = container.querySelector('[data-k="drop"]');
    if (drop) {
      ['dragenter', 'dragover'].forEach(function (ev) {
        drop.addEventListener(ev, function (e) { e.preventDefault(); drop.classList.add('drag-over'); });
      });
      ['dragleave', 'drop'].forEach(function (ev) {
        drop.addEventListener(ev, function (e) { e.preventDefault(); drop.classList.remove('drag-over'); });
      });
      drop.addEventListener('drop', function (e) {
        var f = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
        if (f) confirmUploadKernel(f);
      });
    }
    if (file) {
      file.addEventListener('change', function () {
        if (file.files && file.files[0]) confirmUploadKernel(file.files[0]);
        file.value = '';
      });
    }
    bindActions(container, {
      kcheck: function (btn) { checkKernelUpdate(btn); },
      kupdate: function (btn) { updateKernel(btn); },
      krollback: function (btn) { rollbackKernel(btn); }
    });
    renderKernelUpdateInfo();
  }
  function kernelEl(sel) { return refs && refs.sub ? refs.sub.querySelector(sel) : null; }
  function setKernelProgress(pct, msg, show) {
    var bar = kernelEl('[data-k="bar"]');
    var text = kernelEl('[data-k="progress"]');
    if (bar) {
      bar.classList.toggle('hidden', !show);
      var span = bar.querySelector('span');
      if (span && pct != null) span.style.width = MM.clamp(pct, 0, 100) + '%';
    }
    if (text && msg != null) text.textContent = msg;
  }
  function resetKernelProgress() {
    setKernelProgress(0, '', false);
    var bar = kernelEl('[data-k="bar"]');
    if (bar) { bar.querySelector('span').style.width = '0%'; bar.classList.remove('done'); }
  }
  function showKernelNotice(kind, msg) {
    var host = kernelEl('[data-k="notice"]');
    if (!host) return;
    host.textContent = '';
    var n = document.createElement('div');
    n.className = 'notice ' + (kind || '');
    n.innerHTML = MM.icon(kind === 'err' ? 'warning' : kind === 'ok' ? 'check' : 'info') + '<span></span>';
    n.querySelector('span').textContent = msg;
    host.appendChild(n);
  }
  function renderKernelUpdateInfo() {
    var host = kernelEl('[data-k="updateInfo"]');
    if (host) host.innerHTML = kernelUpdateInfoRows();
  }
  function updateKernelButtons() {
    if (!refs || !refs.sub) return;
    var check = refs.sub.querySelector('[data-act="kcheck"]');
    var upd = refs.sub.querySelector('[data-act="kupdate"]');
    var rb = refs.sub.querySelector('[data-act="krollback"]');
    if (check) check.disabled = kernel.busy;
    if (upd) upd.disabled = kernel.busy || !(kernel.check && kernel.check.hasUpdate);
    if (rb) rb.disabled = kernel.busy || !(state().kernel && state().kernel.backupVersion);
  }
  function renderKernelInfoOnly() {
    if (!refs || currentSub !== 'kernel') return;
    var host = kernelEl('[data-k="info"]');
    if (host) host.innerHTML = kernelInfoRows();
    updateKernelButtons();
  }
  function checkKernelUpdate(btn) {
    if (kernel.busy) return;
    kernel.busy = true;
    if (btn) MM.ui.setBusy(btn, true);
    updateKernelButtons();
    MM.router.refreshActions();
    MM.api('/api/kernel/check').then(function (d) {
      kernel.check = d || null;
      renderKernelUpdateInfo();
      updateKernelButtons();
      if (d && d.hasUpdate) MM.ui.toast('发现新版本 ' + (shortVersion(d.latest) || d.latest), 'ok');
      else MM.ui.toast('已是最新版本', 'ok');
    }).catch(function (e) {
      kernel.check = null;
      renderKernelUpdateInfo();
      updateKernelButtons();
      showKernelNotice('err', (e && e.message) || '检查更新失败');
    }).finally(function () {
      kernel.busy = false;
      if (btn) MM.ui.setBusy(btn, false);
      updateKernelButtons();
      MM.router.refreshActions();
    });
  }
  function updateKernel(btn) {
    if (kernel.busy) return;
    MM.ui.confirm({
      title: '在线更新内核？',
      body: '将下载最新 slim 内核并替换当前版本，期间代理会短暂中断。',
      confirm: '更新', destructive: true
    }).then(function (ok) {
      if (!ok) return;
      kernel.busy = true;
      if (btn) MM.ui.setBusy(btn, true);
      MM.router.refreshActions();
      resetKernelProgress();
      setKernelProgress(0, '开始下载内核…', true);
      var unsub = MM.progressBus.sub(function (ev) {
        if (!ev || ev.op !== 'kernel') return;
        setKernelProgress(ev.percent, ev.msg || '正在更新内核…', true);
      });
      MM.api('/api/kernel/update', { method: 'POST' }).then(function (d) {
        setKernelProgress(100, '内核已更新', true);
        var msg = (d && d.message) || '内核已更新';
        if (d && d.version) msg += '（' + shortVersion(d.version) + '）';
        showKernelNotice('ok', msg + (d && d.rolledBack ? '；新内核启动失败，已自动回滚' : ''));
        MM.ui.toast(msg, 'ok');
        kernel.check = null;
        renderKernelUpdateInfo();
        return MM.data.loadKernel().catch(function () { return null; });
      }).catch(function (e) {
        showKernelNotice('err', ((e && e.message) || '更新失败') + '\n当前内核未改变。');
        MM.ui.toast((e && e.message) || '更新内核失败', 'err');
      }).finally(function () {
        unsub();
        kernel.busy = false;
        if (btn) MM.ui.setBusy(btn, false);
        renderKernelInfoOnly();
        updateKernelButtons();
        MM.router.refreshActions();
        MM.sched.markDue('status');
        MM.data.loadStatus().catch(function () {});
      });
    });
  }
  function confirmUploadKernel(file) {
    if (kernel.busy) return;
    MM.ui.confirm({
      title: '上传并替换内核？',
      body: '将校验并替换当前内核（' + file.name + '），期间代理会短暂中断。',
      confirm: '上传', destructive: true
    }).then(function (ok) {
      if (!ok) return;
      uploadKernel(file);
    });
  }
  function uploadKernel(file) {
    kernel.busy = true;
    kernel.uploading = true;
    MM.router.refreshActions();
    resetKernelProgress();
    setKernelProgress(0, '正在上传 ' + file.name + '…', true);
    var xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/kernel/upload', true);
    xhr.upload.onprogress = function (e) {
      if (e.lengthComputable) {
        var pct = Math.round(e.loaded * 100 / e.total);
        setKernelProgress(pct, '正在上传 ' + MM.fmt.bytes(e.loaded) + ' / ' + MM.fmt.bytes(e.total), true);
      }
    };
    xhr.onerror = function () {
      showKernelNotice('err', '上传失败：无法连接面板服务。\n当前内核未改变。');
      MM.ui.toast('上传内核失败', 'err');
      finishUpload();
    };
    xhr.onload = function () {
      var data = {};
      try { data = JSON.parse(xhr.responseText || '{}'); } catch (e) { data = {}; }
      if (xhr.status === 401) MM.ui.login();
      if (xhr.status >= 200 && xhr.status < 300 && data.ok !== false) {
        setKernelProgress(100, '内核已更新', true);
        var msg = data.message || '内核已更新';
        if (data.version) msg += '（' + shortVersion(data.version) + '）';
        showKernelNotice('ok', msg);
        MM.ui.toast(msg, 'ok');
        kernel.check = null;
        MM.data.loadKernel().catch(function () { return null; }).then(function () { renderKernelInfoOnly(); });
      } else {
        var err = data.error || data.err || ('HTTP ' + xhr.status);
        showKernelNotice('err', err + '\n当前内核未改变。');
        MM.ui.toast(err, 'err');
      }
      finishUpload();
    };
    var fd = new FormData();
    fd.append('file', file);
    xhr.send(fd);

    function finishUpload() {
      kernel.busy = false;
      kernel.uploading = false;
      updateKernelButtons();
      MM.router.refreshActions();
      MM.sched.markDue('status');
      MM.data.loadStatus().catch(function () {});
    }
  }
  function rollbackKernel(btn) {
    if (kernel.busy) return;
    MM.ui.confirm({
      title: '回滚内核？',
      body: '将把当前内核与上一个备份对调，随后重启内核；期间代理会短暂中断。',
      confirm: '回滚', destructive: true
    }).then(function (ok) {
      if (!ok) return;
      kernel.busy = true;
      if (btn) MM.ui.setBusy(btn, true);
      updateKernelButtons();
      MM.router.refreshActions();
      MM.api('/api/kernel/rollback', { method: 'POST' }).then(function (d) {
        var msg = (d && d.message) || '内核已回滚';
        showKernelNotice('ok', msg);
        MM.ui.toast(msg, 'ok');
        kernel.check = null;
        renderKernelUpdateInfo();
        return MM.data.loadKernel().catch(function () { return null; });
      }).catch(function (e) {
        showKernelNotice('err', (e && e.message) || '回滚失败');
        MM.ui.toast((e && e.message) || '回滚失败', 'err');
      }).finally(function () {
        kernel.busy = false;
        if (btn) MM.ui.setBusy(btn, false);
        renderKernelInfoOnly();
        updateKernelButtons();
        MM.router.refreshActions();
        MM.sched.markDue('status');
        MM.data.loadStatus().catch(function () {});
      });
    });
  }

  /* ---------- 4.9.12 diagnostics ---------- */
  function diagServiceErrRows(msg) {
    return readonlyRow('内核可达性', '—') + readonlyRow('API 延迟', '—') +
      readonlyRow('iptables', '—') + readonlyRow('端口', '—') +
      readonlyRow('实际模式', '—') +
      '<div class="setting-note">读取服务状态失败：' + esc(msg) + '。页面可见时会每 5 秒重试。</div>';
  }
  function diagServiceRows() {
    var svc = state().service;
    if (!svc) {
      return readonlyRow('内核可达性', '—') + readonlyRow('API 延迟', '—') +
        readonlyRow('iptables', '—') + readonlyRow('端口', '—') +
        readonlyRow('实际模式', '—') +
        '<div class="setting-note">正在读取服务状态…</div>';
    }
    var core = svc.core || {};
    var ips = svc.iptables || {};
    var ports = svc.ports || {};
    var reachable = !!core.reachable;
    var mode = svc.mode ? modeLabel(svc.mode) : '—';
    var actual = svc.modeActual ? modeLabel(svc.modeActual) : '—';
    var mismatch = svc.modeActual && svc.mode && svc.modeActual !== svc.mode;
    var html = readonlyRow('内核可达性', reachable ? '可达' : '不可达', reachable ? 'ok' : 'err') +
      readonlyRow('API 延迟', reachable && core.apiLatencyMs != null ? core.apiLatencyMs + ' ms' : '—') +
      readonlyRow('iptables', ips.applied ? '已应用' : '未应用', ips.applied ? 'ok' : 'warn') +
      readonlyRow('端口', (ports.redir != null ? ports.redir : '—') + ' / ' + (ports.mixed != null ? ports.mixed : '—')) +
      readonlyRow('实际模式', mode + (actual !== '—' ? ' → ' + actual : ''), mismatch ? 'warn' : '');
    if (mismatch) html += '<div class="setting-note">设置里的模式尚未应用到内核。</div>';
    if (svc.err) html += '<div class="setting-note">' + esc(svc.err) + '</div>';
    return html;
  }
  function diagNetworkRows() {
    var n = state().intranet;
    if (!n) {
      return readonlyRow('LAN IP', '—') + readonlyRow('接口', '—') +
        readonlyRow('面板地址', '—') + readonlyRow('主机名', '—') +
        '<div class="setting-note">正在读取网络身份…</div>';
    }
    var html = copyRow('LAN IP', n.lanIp || '', true) +
      readonlyRow('接口', n.iface || '—') +
      copyRow('面板地址', n.panelUrl || '', true) +
      readonlyRow('主机名', n.hostname || '—');
    if (n.err) html += '<div class="setting-note">' + esc(n.err) + '</div>';
    return html;
  }
  function ipqTypeLabel(v) {
    var m = {
      hosting: '机房 / 云', mobile: '移动网络', residential: '住宅',
      isp: '运营商', business: '商业', unknown: '未知'
    };
    return m[v] || v || '未知';
  }
  function ipqLevelLabel(v) {
    var m = { good: '良好', normal: '普通', risky: '有风险', unknown: '未知' };
    return m[v] || v || '未知';
  }
  function ipqLevelClass(v) {
    if (v === 'risky') return 'err';
    if (v === 'good') return 'ok';
    if (v === 'normal') return 'warn';
    return '';
  }
  function ipqRows() {
    var q = state().ipq;
    if (!q) {
      return readonlyRow('出口 IP', '—') + readonlyRow('类型', '—') +
        readonlyRow('等级', '—') + readonlyRow('代理 / VPN / Tor', '—') +
        readonlyRow('来源', '—') + readonlyRow('更新时间', '—') +
        '<div class="setting-note">尚未检测 IP 质量。</div>';
    }
    var c = q.checks || {};
    var unknown = q.level === 'unknown' || !q.level;
    var html = readonlyRow('出口 IP', q.ip || '—', 'mono') +
      readonlyRow('类型', ipqTypeLabel(q.type)) +
      readonlyRow('等级', unknown ? '未知' : ipqLevelLabel(q.level), ipqLevelClass(q.level)) +
      readonlyRow('代理 / VPN / Tor',
        (c.proxy ? '是' : '否') + ' / ' + (c.vpn ? '是' : '否') + ' / ' + (c.tor ? '是' : '否')) +
      readonlyRow('来源', q.source || '—') +
      readonlyRow('更新时间', diag.ipqAt ? MM.fmt.clock(diag.ipqAt) : '—');
    if (q.err) {
      html += '<div class="setting-note">检测失败：' + esc(q.err) + '</div>';
    }
    return html;
  }
  function diagAboutRows() {
    var st = state();
    var p = (st.status && st.status.panel) || {};
    var eng = MM.store.engine();
    var ver = shortVersion(eng.version) || shortVersion((st.kernel && st.kernel.version)) || '—';
    return readonlyRow('面板版本', p.version ? 'v' + p.version : MM.VERSION) +
      readonlyRow('平台', p.platform || '—') +
      readonlyRow('数据目录', p.dataDir || '—', 'mono') +
      readonlyRow('运行时长', p.uptime ? MM.fmt.dur(p.uptime) : '—') +
      '<button class="list-row setting-row" type="button" data-d-nav="kernel">' +
        '<span class="row-main"><span class="row-title">内核版本</span></span>' +
        '<span class="row-value">' + esc(ver) + '</span>' +
        '<svg class="chev"><use href="#i-chevron"/></svg></button>';
  }
  function updateDiagHost(name, html) {
    if (!refs || !refs.sub) return;
    var host = refs.sub.querySelector('[data-d="' + name + '"]');
    if (host) host.innerHTML = html;
    if (name === 'about') bindDiagAbout();
  }
  function bindDiagAbout() {
    if (!refs || !refs.sub) return;
    var nav = refs.sub.querySelector('[data-d-nav="kernel"]');
    if (nav && !nav.__bound) {
      nav.__bound = true;
      nav.addEventListener('click', function () { MM.router.go('/settings/kernel'); });
    }
  }
  function renderDiagnostics(container) {
    container.innerHTML =
      groupCard('服务状态',
        '<div data-d="service">' + diagServiceRows() + '</div>' +
        actionRow('diagRefresh', '刷新', '重新读取服务状态、网络身份与 IP 质量', 'tonal')) +
      groupCard('网络身份', '<div data-d="network">' + diagNetworkRows() + '</div>') +
      groupCard('IP 质量',
        '<div data-d="ipq">' + ipqRows() + '</div>' +
        actionRow('diagRecheck', '重新检测', '通过当前出站重新探测 IP 质量', 'tonal')) +
      groupCard('关于', '<div data-d="about">' + diagAboutRows() + '</div>') +
      groupCard('操作',
        actionRow('diagCopy', '复制诊断信息', '复制为纯文本，便于反馈问题', 'tonal'));
    bindDiagAbout();
    bindActions(container, {
      diagRefresh: function (btn) { diagRefresh(btn); },
      diagRecheck: function (btn) { recheckIPQ(btn); },
      diagCopy: function () { copyDiagnostics(); }
    });
  }
  function loadDiagService(token) {
    return MM.api('/api/service').then(function (d) {
      if (!diag.active || token !== diag.token) return;
      MM.store.set({ service: d || null });
      updateDiagHost('service', diagServiceRows());
    }).catch(function (e) {
      if (!diag.active || token !== diag.token) return;
      MM.store.set({ service: null });
      updateDiagHost('service', diagServiceErrRows((e && e.message) || '请求失败'));
    });
  }
  function loadDiagNetwork(token) {
    return MM.api('/api/intranet').then(function (d) {
      if (!diag.active || token !== diag.token) return;
      MM.store.set({ intranet: d || null });
      updateDiagHost('network', diagNetworkRows());
    }).catch(function (e) {
      if (!diag.active || token !== diag.token) return;
      updateDiagHost('network', readonlyRow('LAN IP', '—') + readonlyRow('接口', '—') +
        readonlyRow('面板地址', '—') + readonlyRow('主机名', '—') +
        '<div class="setting-note">读取网络身份失败：' +
        esc((e && e.message) || '请求失败') + '</div>');
    });
  }
  function loadDiagAbout(token) {
    return MM.data.loadStatus().then(function () {
      if (!diag.active || token !== diag.token) return;
      updateDiagHost('about', diagAboutRows());
    }).catch(function () {});
  }
  function loadDiagIPQ(token, force) {
    if (diag.ipqBusy) return Promise.resolve();
    diag.ipqBusy = true;
    if (force) updateDiagHost('ipq', readonlyRow('出口 IP', '检测中…') +
      '<div class="setting-note">正在通过当前出站重新检测…</div>');
    return MM.api('/api/ipquality' + (force ? '?refresh=1' : '')).then(function (d) {
      if (!diag.active || token !== diag.token) return;
      if (d && d.quality) {
        MM.store.set({ ipq: d.quality });
        diag.ipqAt = Math.floor(Date.now() / 1000);
      }
      updateDiagHost('ipq', ipqRows());
    }).catch(function (e) {
      if (!diag.active || token !== diag.token) return;
      MM.store.set({ ipq: { level: 'unknown', type: 'unknown', checks: {}, err: (e && e.message) || '检测失败' } });
      updateDiagHost('ipq', ipqRows());
    }).finally(function () { diag.ipqBusy = false; });
  }
  function startDiagnostics() {
    stopDiagnostics();
    diag.active = true;
    var token = ++diag.token;
    loadDiagService(token);
    loadDiagNetwork(token);
    loadDiagAbout(token);
    loadDiagIPQ(token, false);
    diag.timer = setInterval(function () {
      if (!diag.active || document.hidden) return;
      loadDiagService(token);
    }, 5000);
  }
  function diagRefresh(btn) {
    if (diag.refreshing || !diag.active) return;
    diag.refreshing = true;
    if (btn) MM.ui.setBusy(btn, true);
    var token = diag.token;
    Promise.allSettled([
      loadDiagService(token), loadDiagNetwork(token), loadDiagAbout(token), loadDiagIPQ(token, false)
    ]).then(function () {
      if (active && currentSub === 'diagnostics') MM.ui.toast('诊断信息已刷新', 'ok');
    }).finally(function () {
      diag.refreshing = false;
      if (btn) MM.ui.setBusy(btn, false);
      MM.router.refreshActions();
    });
  }
  function recheckIPQ(btn) {
    if (diag.ipqBusy || !diag.active) return;
    if (btn) MM.ui.setBusy(btn, true);
    loadDiagIPQ(diag.token, true).finally(function () {
      if (btn) MM.ui.setBusy(btn, false);
    });
  }
  function copyDiagnostics() {
    var st = state();
    var svc = st.service || {};
    var core = svc.core || {};
    var ips = svc.iptables || {};
    var ports = svc.ports || {};
    var net = st.intranet || {};
    var q = st.ipq || {};
    var p = (st.status && st.status.panel) || {};
    var c = q.checks || {};
    var lines = [
      'mihomo-merlin 诊断信息',
      '生成时间：' + MM.fmt.time(Date.now() / 1000),
      '面板版本：' + (p.version ? 'v' + p.version : MM.VERSION),
      '平台：' + (p.platform || '—'),
      '数据目录：' + (p.dataDir || '—'),
      '运行时长：' + (p.uptime ? MM.fmt.dur(p.uptime) : '—'),
      '内核可达：' + (core.reachable ? '是' : '否'),
      'API 延迟：' + (core.apiLatencyMs != null && core.reachable ? core.apiLatencyMs + ' ms' : '—'),
      'iptables：' + (ips.applied ? '已应用' : '未应用'),
      '端口：redir ' + (ports.redir != null ? ports.redir : '—') + ' / mixed ' + (ports.mixed != null ? ports.mixed : '—'),
      '模式：' + (svc.mode ? modeLabel(svc.mode) : '—') + ' → ' + (svc.modeActual ? modeLabel(svc.modeActual) : '—'),
      'LAN IP：' + (net.lanIp || '—'),
      '接口：' + (net.iface || '—'),
      '面板地址：' + (net.panelUrl || '—'),
      '主机名：' + (net.hostname || '—'),
      'IP 质量：' + (q.ip || '—') + ' / ' + ipqTypeLabel(q.type) + ' / ' + ipqLevelLabel(q.level),
      '代理标记：proxy=' + (c.proxy ? 'yes' : 'no') + ' vpn=' + (c.vpn ? 'yes' : 'no') + ' tor=' + (c.tor ? 'yes' : 'no'),
      '检测来源：' + (q.source || '—')
    ];
    if (q.err) lines.push('检测错误：' + q.err);
    MM.copy(lines.join('\n')).then(function () {
      MM.ui.toast('诊断信息已复制', 'ok');
    }, function () {
      MM.ui.toast('复制失败，请手动选择文本', 'err');
    });
  }

  /* ---------- actions ---------- */
  function actions(ctx) {
    var sub = (ctx && ctx.sub) || '';
    if (sub === 'diagnostics') {
      return [{
        label: '刷新', icon: 'refresh', showLabel: true,
        onClick: function () { diagRefresh(); }
      }];
    }
    if (sub === 'kernel') {
      return [{
        label: '检查更新', icon: 'refresh', showLabel: true, disabled: kernel.busy,
        onClick: function () { checkKernelUpdate(); }
      }];
    }
    if (isForm(sub)) {
      return [{
        label: dirty ? '保存 •' : '保存',
        showLabel: true, primary: true, mobileOnly: true,
        disabled: !canSave(),
        onClick: function () { saveCurrent(); }
      }];
    }
    return [];
  }

  MM.views.settings = {
    title: function (ctx) {
      if (ctx && ctx.sub && MM.router.isMobile()) return SUB_TITLES[ctx.sub] || '设置';
      return '设置';
    },
    mount: function (root) { mount(root); },
    activate: function (ctx, sec) { activate(ctx, sec); },
    deactivate: function (ctx) {
      deactivate(ctx);
      if (currentSub === 'kernel') kernel.check = null;
    },
    actions: actions
  };

})();
