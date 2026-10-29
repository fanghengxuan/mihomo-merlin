'use strict';

/* =========================================================
 * mihomo-merlin 面板
 * Material 3 / FlClash 风格，纯原生 JS，无第三方依赖
 * 数据全部来自 /api/*
 * =======================================================*/

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

const VIEWS = {
  overview: '概览',
  nodes: '节点',
  subs: '订阅',
  rules: '规则',
  connections: '连接',
  logs: '日志',
  settings: '设置',
};

// 机场常把流量/到期信息伪装成节点，列表里不显示
const PSEUDO_NODE_RE = /(剩余流量|到期|剩余|重置|套餐|官网|订阅|客服|过期)/;

const PROTO_SHORT = {
  Shadowsocks: 'SS', ShadowsocksR: 'SSR', Vmess: 'VM', Vless: 'VL', Trojan: 'TR',
  Hysteria: 'HY', Hysteria2: 'HY2', Tuic: 'TU', Socks5: 'SO', Http: 'HT',
  WireGuard: 'WG', Snell: 'SN', Mieru: 'MI', AnyTLS: 'AT', SSH: 'SH',
};

const state = {
  view: 'overview',
  status: null,
  settings: null,
  kernel: null,
  groups: [],
  subs: [],
  rules: [],
  history: [],
  testing: new Set(),
  nodeFilter: '',
  ruleFilter: '',
  ruleSearch: '',
  editingSub: null,
  logPaused: false,
  logStream: null,
  trafficStream: null,
  progressStream: null,
  progressTimer: null,
  progressOpen: false,
  nodesPreview: false,
  reachTargets: null,
  activeGroup: null,
  connFilter: '',
  logFilter: '',
  nodeSort: 'default',
  netInfo: null,
  intranet: null,
  ipQuality: null,
  reachTimer: null,
  inlineProgress: false,
  uploading: false,
  conns: null,
  connsAt: 0,
  netSig: null,
};

/* ---------------- 格式化 ---------------- */

function fmtSpeed(bps) {
  const v = Number(bps) || 0;
  if (v < 1024) return v.toFixed(0) + ' B/s';
  const units = ['KB/s', 'MB/s', 'GB/s'];
  let n = v / 1024, i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return n.toFixed(n < 10 ? 2 : 1) + ' ' + units[i];
}

function fmtBytes(n) {
  const v = Number(n) || 0;
  if (v < 1024) return v.toFixed(0) + ' B';
  const units = ['KB', 'MB', 'GB', 'TB'];
  let x = v / 1024, i = 0;
  while (x >= 1024 && i < units.length - 1) { x /= 1024; i++; }
  return x.toFixed(x < 10 ? 2 : 1) + ' ' + units[i];
}

function fmtDur(sec) {
  const s = Math.max(0, Math.floor(Number(sec) || 0));
  if (s < 60) return s + ' 秒';
  const m = Math.floor(s / 60);
  if (m < 60) return m + ' 分钟';
  const h = Math.floor(m / 60);
  if (h < 24) return h + ' 时 ' + (m % 60) + ' 分';
  return Math.floor(h / 24) + ' 天 ' + (h % 24) + ' 时';
}

function fmtTime(ts) {
  if (!ts) return '—';
  const d = new Date(Number(ts) * 1000);
  if (isNaN(d.getTime())) return '—';
  const p = (x) => String(x).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function esc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function latClass(d) {
  const v = Number(d) || 0;
  if (!v) return 'none';
  if (v < 200) return 'good';
  if (v < 500) return 'mid';
  return 'bad';
}

function protoShort(type) {
  if (!type) return '·';
  return PROTO_SHORT[type] || String(type).slice(0, 2).toUpperCase();
}

/* ---------------- Snackbar ---------------- */

function toast(msg, kind = '') {
  const wrap = $('#toasts');
  const el = document.createElement('div');
  el.className = 'snackbar ' + kind;
  el.textContent = msg;
  wrap.appendChild(el);
  setTimeout(() => {
    el.style.transition = '.22s';
    el.style.opacity = '0';
    el.style.transform = 'translateY(8px)';
    setTimeout(() => el.remove(), 240);
  }, kind === 'err' ? 5000 : 2400);
}

/* ---------------- HTTP ---------------- */

async function api(path, { method = 'GET', body } = {}) {
  const opt = { method, credentials: 'same-origin', headers: {} };
  if (body !== undefined) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(path, opt);
  } catch (e) {
    throw new Error('无法连接面板服务');
  }
  if (res.status === 401) {
    showLogin();
    throw new Error('未登录');
  }
  let data = null;
  try { data = await res.json(); } catch (e) { throw new Error('响应解析失败（HTTP ' + res.status + '）'); }
  if (!res.ok || (data && data.ok === false)) {
    throw new Error((data && data.error) || ('HTTP ' + res.status));
  }
  return data;
}

/* ---------------- 主题 ---------------- */

function applyTheme(theme) {
  document.documentElement.setAttribute('data-theme', theme);
  if (state.reachTargets && state.reachTargets.length) renderReachability(state.reachTargets);
  $('#themeBtn').innerHTML = '<svg><use href="' + (theme === 'dark' ? '#i-sun' : '#i-moon') + '"/></svg>';
}

function initTheme() {
  const saved = localStorage.getItem('mm-theme');
  const sysLight = window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches;
  applyTheme(saved || (sysLight ? 'light' : 'dark'));
}

$('#themeBtn').addEventListener('click', () => {
  const next = document.documentElement.getAttribute('data-theme') === 'light' ? 'dark' : 'light';
  applyTheme(next);
  localStorage.setItem('mm-theme', next);
  drawSpark();
});

/* ---------------- 视图切换 ---------------- */

function setView(view) {
  if (!VIEWS[view]) view = 'overview';
  state.view = view;
  if (location.hash !== '#' + view) history.replaceState(null, '', '#' + view);

  $$('.view').forEach((v) => v.classList.add('hidden'));
  const el = $('#view-' + view);
  if (el) el.classList.remove('hidden');
  $('#viewTitle').textContent = VIEWS[view];
  $$('#railNav .rail-item, #bottomNav button').forEach((b) => b.classList.toggle('active', b.dataset.view === view));

  if (view === 'nodes') loadNodes();
  if (view === 'subs') loadSubs();
  if (view === 'rules') loadRules();
  if (view === 'connections') loadConnections();
  if (view === 'logs') ensureLogStream();
  if (view === 'settings') { loadSettings(); loadKernel(); loadService(); }
}

document.addEventListener('click', (e) => {
  const nav = e.target.closest('[data-view]');
  if (nav) { setView(nav.dataset.view); return; }
  const go = e.target.closest('[data-goto]');
  if (go) setView(go.dataset.goto);
});

/* ---------------- 状态渲染 ---------------- */

function statusChip(el, running) {
  if (!el) return;
  el.classList.toggle('on', !!running);
  el.innerHTML = '<i></i><span>' + (running ? '运行中' : '未运行') + '</span>';
}

async function loadStatus() {
  try {
    const data = await api('/api/status');
    state.status = data;
    renderStatus(data);
  } catch (e) { /* 保持旧数据 */ }
}

function renderStatus(st) {
  const running = !!(st.engine && st.engine.running);
  const eng = st.engine || {};
  const net = st.net || {};
  const subs = st.subs || {};
  const panel = st.panel || {};

  statusChip($('#enginePill'), running);
  statusChip($('#setEnginePill'), running);

  $('#heroState').textContent = running ? '已连接' : '未连接';
  $('#heroSub').textContent = running
    ? `mihomo ${eng.version || ''} · 已运行 ${fmtDur(eng.uptime)}`
    : '内核未启动，点右侧按钮开始代理';

  $('#btnPowerRing').classList.toggle('on', running);
  const ringLabel = $('#ringLabel');
  if (ringLabel) ringLabel.textContent = running ? '停止' : '启动';
  $('#btnPowerRing').setAttribute('title', running ? '停止代理' : '启动代理');
  $('#btnRestart').classList.toggle('hidden', !running);

  $('#statConns').textContent = net.connections || 0;
  $('#statMem').textContent = running && eng.memory ? eng.memory.toFixed(1) + ' MB' : '—';
  $('#statUptime').textContent = running ? fmtDur(eng.uptime) : '—';
  $('#statNodes').textContent = subs.nodeCount || 0;
  if ($('#statSubs')) $('#statSubs').textContent = subs.count || 0;

  // 以「内核实际生效的模式」为准，避免设置写了但没应用时误显示
  const actualMode = (st.engine && st.engine.modeActual) || (st.engine && st.engine.mode) ||
    (state.settings && state.settings.mode) || 'rule';
  $$('#modeSeg button').forEach((b) => {
    b.classList.toggle('active', b.dataset.mode === actualMode);
  });
  const wantMode = (state.settings && state.settings.mode) || 'rule';
  const modeHint = $('#modeActual');
  if (modeHint) {
    const name = { rule: '规则模式按国内 / 国外分流', global: '全局模式：所有流量走代理', direct: '直连模式：所有流量不走代理' }[actualMode] || actualMode;
    modeHint.textContent = running
      ? (actualMode === wantMode ? name : name + '（设置里是 ' + wantMode + '，未生效）')
      : name;
  }

  const setVer = $('#setEngineVer');
  if (setVer) setVer.textContent = running ? ('mihomo ' + (eng.version || '?')) : '—';
  const setIpt = $('#setIptables');
  if (setIpt) setIpt.textContent = (st.iptables && st.iptables.applied) ? ('已应用 · ' + fmtTime(st.iptables.lastApply)) : '未应用';

  // 定时重启：上次 / 下次（0 或缺失显示「—」）
  const lastRestart = $('#setLastRestart');
  if (lastRestart) lastRestart.textContent = fmtTime(eng.lastRestartAt);
  const nextRestart = $('#setNextRestart');
  if (nextRestart) nextRestart.textContent = fmtTime(eng.nextRestartAt);

  $('#aboutVer').textContent = 'v' + (panel.version || '1.5.7');
  $('#aboutPlatform').textContent = panel.platform || 'linux/arm';
  $('#aboutData').textContent = panel.dataDir || '—';

  // 引擎状态（运行中 / 实际模式）变化后，网络状态卡片自动重新探测一次，
  // 不然启动代理后出口 IP 要手动点刷新才更新。
  const sig = (running ? '1' : '0') + '|' + actualMode;
  const prevSig = state.netSig;
  state.netSig = sig;
  if (prevSig && prevSig !== sig && !document.hidden) loadNetInfo(true);

  renderSubSummary();
}

function renderSubSummary() {
  const box = $('#subSummary');
  if (!state.subs.length) {
    box.innerHTML = '<div class="muted">还没有订阅，去「订阅」页添加一个机场链接。</div>';
    return;
  }
  box.innerHTML = state.subs.slice(0, 4).map((s) => {
    const t = s.traffic || {};
    const used = (t.upload || 0) + (t.download || 0);
    const total = t.total || 0;
    const pct = total ? Math.min(100, (used / total) * 100) : 0;
    return `<div class="sum-row">
      <span class="name">${esc(s.name)}</span>
      <span class="muted">${s.nodeCount || 0} 节点</span>
      <div class="bar" style="flex:1"><span class="${pct > 85 ? 'warn' : ''}" style="width:${pct.toFixed(1)}%"></span></div>
      <span class="muted">${total ? fmtBytes(used) + ' / ' + fmtBytes(total) : '流量未知'}</span>
    </div>`;
  }).join('');
}

/* ---------------- 流量 ---------------- */

function pushTraffic(up, down, upTotal, downTotal) {
  state.history.push({ up: Number(up) || 0, down: Number(down) || 0 });
  if (state.history.length > 60) state.history.shift();

  $('#upSpeed').textContent = fmtSpeed(up);
  $('#downSpeed').textContent = fmtSpeed(down);
  const total = (Number(upTotal) || 0) + (Number(downTotal) || 0);
  $('#totalTraffic').textContent = total ? fmtBytes(total) : '0 B';
  drawSpark();
}

function drawSpark() {
  const cvs = $('#spark');
  if (!cvs) return;
  const dpr = window.devicePixelRatio || 1;
  const w = cvs.clientWidth || 600;
  const h = 90;
  if (cvs.width !== Math.floor(w * dpr) || cvs.height !== Math.floor(h * dpr)) {
    cvs.width = Math.floor(w * dpr);
    cvs.height = Math.floor(h * dpr);
  }
  const ctx = cvs.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, w, h);

  const data = state.history;
  const style = getComputedStyle(document.documentElement);
  const lineColor = style.getPropertyValue('--md-outline-var').trim() || 'rgba(255,255,255,.08)';
  const downColor = style.getPropertyValue('--md-tertiary').trim() || '#7fd3e8';
  const upColor = style.getPropertyValue('--md-warn').trim() || '#ffd18c';

  ctx.strokeStyle = lineColor;
  ctx.lineWidth = 1;
  for (let i = 1; i <= 2; i++) {
    const y = (h / 3) * i;
    ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke();
  }

  const max = Math.max(1024, ...data.map((d) => Math.max(d.up, d.down)));
  const pad = 4;
  const plot = (key, color) => {
    if (data.length < 2) return;
    const pts = data.map((d, i) => [
      pad + (i / (data.length - 1)) * (w - pad * 2),
      h - pad - Math.min(1, d[key] / max) * (h - pad * 2),
    ]);
    // 面积渐变
    const grad = ctx.createLinearGradient(0, 0, 0, h);
    grad.addColorStop(0, color + '55');
    grad.addColorStop(1, color + '00');
    ctx.beginPath();
    ctx.moveTo(pts[0][0], h - pad);
    pts.forEach(([x, y]) => ctx.lineTo(x, y));
    ctx.lineTo(pts[pts.length - 1][0], h - pad);
    ctx.closePath();
    ctx.fillStyle = grad;
    ctx.fill();
    // 折线
    ctx.beginPath();
    pts.forEach(([x, y], i) => (i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y)));
    ctx.strokeStyle = color;
    ctx.lineWidth = 2;
    ctx.lineJoin = 'round';
    ctx.stroke();
  };
  plot('down', downColor);
  plot('up', upColor);
}

setInterval(drawSpark, 2000);
window.addEventListener('resize', drawSpark);

/* ---------------- 引擎操作 ---------------- */

async function engineAction(action, confirmMsg) {
  if (confirmMsg && !window.confirm(confirmMsg)) return;
  try {
    const r = await api('/api/engine/' + action, { method: 'POST' });
    toast(r.message || '操作成功', 'ok');
  } catch (e) {
    toast(e.message, 'err');
  }
  setTimeout(() => { loadStatus(); if (state.view === 'nodes') loadNodes(); }, 700);
}

function toggleEngine() {
  const running = !!(state.status && state.status.engine && state.status.engine.running);
  if (running) engineAction('stop', '确定停止代理？局域网将恢复直连。');
  else engineAction('start');
}

$('#btnPowerRing').addEventListener('click', toggleEngine);
$('#btnRestart').addEventListener('click', () => engineAction('restart'));
$('#btnRestart2').addEventListener('click', () => engineAction('restart'));
$('#btnReloadCfg').addEventListener('click', async () => {
  try {
    const r = await api('/api/engine/reload', { method: 'POST' });
    toast(r.message || '已重载', 'ok');
  } catch (e) { toast(e.message, 'err'); }
});

$('#modeSeg').addEventListener('click', async (e) => {
  const btn = e.target.closest('button[data-mode]');
  if (!btn) return;
  const mode = btn.dataset.mode;
  try {
    await api('/api/settings', { method: 'PUT', body: { mode } });
    toast('已切换：' + ({ rule: '规则模式', global: '全局模式', direct: '直连模式' })[mode], 'ok');
    await loadStatus();
  } catch (err) { toast(err.message, 'err'); }
});

/* ---------------- 节点 ---------------- */

async function loadNodes() {
  try {
    const data = await api('/api/nodes');
    state.groups = data.groups || [];
    state.nodesPreview = !!data.preview;
    renderGroups();
    const hint = $('#nodesPreviewHint');
    if (hint) hint.classList.toggle('hidden', !state.nodesPreview);
  } catch (e) { /* 忽略 */ }
}

function allNodeNames() {
  const set = new Set();
  state.groups.forEach((g) => (g.nodes || []).forEach((n) => set.add(n.name)));
  return Array.from(set);
}

function applyNodeSort(nodes) {
  const list = nodes.slice();
  switch (state.nodeSort) {
    case 'delay':
      list.sort((a, b) => (a.delay > 0 ? a.delay : 1e9) - (b.delay > 0 ? b.delay : 1e9));
      break;
    case 'name':
      list.sort((a, b) => a.name.localeCompare(b.name, 'zh-Hans-CN'));
      break;
    case 'nameDesc':
      list.sort((a, b) => b.name.localeCompare(a.name, 'zh-Hans-CN'));
      break;
    default:
      break;
  }
  return list;
}

function renderGroups() {
  const box = $('#groups');
  const tabs = $('#groupTabs');
  const kw = state.nodeFilter.trim().toLowerCase();

  const groups = state.groups
    .map((g) => ({
      ...g,
      nodes: applyNodeSort(
        (g.nodes || [])
          .filter((n) => !PSEUDO_NODE_RE.test(n.name))
          .filter((n) => !n.isGroup)   // 分组成员（自动选择 / 订阅组）各自是一栏，别重复列
          .filter((n) => !kw || n.name.toLowerCase().includes(kw) || (n.sub || '').toLowerCase().includes(kw))
      ),
    }))
    .filter((g) => (g.nodes || []).length > 0);

  if (!tabs) return;
  if (!groups.length) {
    tabs.innerHTML = '';
    box.innerHTML = '';
    $('#nodesEmpty').classList.remove('hidden');
    return;
  }
  $('#nodesEmpty').classList.add('hidden');

  // 当前选中栏；组被删/改名时回退到第一个
  if (!state.activeGroup || !groups.some((g) => g.name === state.activeGroup)) {
    state.activeGroup = groups[0].name;
  }

  tabs.innerHTML = groups.map((g) => {
    const active = g.name === state.activeGroup ? ' active' : '';
    return `<button class="group-tab${active}" data-tab="${esc(g.name)}">
      <span class="t">${esc(g.name)}</span>
      <span class="c">${g.nodes.length}</span>
    </button>`;
  }).join('');

  const g = groups.find((x) => x.name === state.activeGroup) || groups[0];
  const rows = g.nodes.map((n) => {
    const testing = state.testing.has(n.name);
    const cls = testing ? 'testing' : latClass(n.delay);
    const txt = testing ? '测试…' : (n.delay ? n.delay + ' ms' : '未测');
    return `<div class="node ${g.now === n.name ? 'sel' : ''}" data-group="${esc(g.name)}" data-node="${esc(n.name)}">
      <div class="node-avatar">${esc(protoShort(n.type))}</div>
      <div class="node-main">
        <div class="node-name">${esc(n.name)}</div>
        <div class="node-sub">${esc(n.type || '')}${n.sub ? ' · ' + esc(n.sub) : ''}</div>
      </div>
      <span class="lat ${cls}">${esc(txt)}</span>
      <button class="icon-btn" data-test="${esc(n.name)}" title="测速"><svg><use href="#i-bolt"/></svg></button>
    </div>`;
  }).join('');

  box.innerHTML = `<div class="group active-group">
    <div class="group-head">
      <span class="group-title">${esc(g.name)}</span>
      <span class="group-now">${esc(g.now || '—')}</span>
      <span class="muted">${g.nodes.length} 个节点</span>
      <span style="flex:1"></span>
      <button class="btn text small" data-testgroup="${esc(g.name)}">测速本组</button>
    </div>
    <div class="node-list">${rows}</div>
  </div>`;
}

$('#groupTabs') && $('#groupTabs').addEventListener('click', (e) => {
  const btn = e.target.closest('[data-tab]');
  if (!btn) return;
  state.activeGroup = btn.dataset.tab;
  renderGroups();
});

$('#groups').addEventListener('click', async (e) => {
  const testBtn = e.target.closest('[data-test]');
  if (testBtn) {
    e.stopPropagation();
    await testNodes([testBtn.dataset.test]);
    return;
  }
  const grpBtn = e.target.closest('[data-testgroup]');
  if (grpBtn) {
    const g = state.groups.find((x) => x.name === grpBtn.dataset.testgroup);
    if (g) await testNodes((g.nodes || []).map((n) => n.name));
    return;
  }
  const node = e.target.closest('.node[data-node]');
  if (node) {
    try {
      await api('/api/select', { method: 'PUT', body: { group: node.dataset.group, node: node.dataset.node } });
      toast('已切换到 ' + node.dataset.node, 'ok');
      await loadNodes();
      loadNetInfo(true);   // 换节点后立刻重新探测出口，不用手动点刷新
    } catch (err) { toast(err.message, 'err'); }
  }
});

async function testNodes(names) {
  if (!names.length) return;
  names.forEach((n) => state.testing.add(n));
  renderGroups();
  try {
    const data = await api('/api/delay', { method: 'POST', body: { names } });
    const results = data.results || {};
    state.groups.forEach((g) => (g.nodes || []).forEach((n) => {
      if (Object.prototype.hasOwnProperty.call(results, n.name)) n.delay = results[n.name];
    }));
  } catch (e) {
    toast(e.message, 'err');
  } finally {
    names.forEach((n) => state.testing.delete(n));
    renderGroups();
  }
}

$('#btnTestAll').addEventListener('click', () => testNodes(allNodeNames()));
$('#nodeSort').addEventListener('change', (e) => {
  state.nodeSort = e.target.value || 'default';
  renderGroups();
});

$('#nodeSearch').addEventListener('input', (e) => { state.nodeFilter = e.target.value; renderGroups(); });

/* ---------------- 订阅 ---------------- */

async function loadSubs() {
  try {
    const data = await api('/api/subs');
    state.subs = data.subs || [];
    renderSubs();
    renderSubSummary();
  } catch (e) { /* ignore */ }
}

function renderSubs() {
  const box = $('#subList');
  if (!state.subs.length) {
    box.innerHTML = '';
    $('#subsEmpty').classList.remove('hidden');
    return;
  }
  $('#subsEmpty').classList.add('hidden');

  box.innerHTML = state.subs.map((s) => {
    const t = s.traffic || null;
    const autoUpdateLine = renderAutoUpdateLine(s);
    let trafficHtml = '<div class="traffic"><div class="line"><span>流量信息不可用</span><span>更新于 ' + fmtTime(s.updatedAt) + '</span></div></div>';
    if (t) {
      const used = (t.upload || 0) + (t.download || 0);
      const total = t.total || 0;
      const pct = total ? Math.min(100, (used / total) * 100) : 0;
      trafficHtml = `<div class="traffic">
        <div class="line"><span>已用 ${fmtBytes(used)}</span><span>${total ? '共 ' + fmtBytes(total) : '不限流量'}</span></div>
        <div class="bar"><span class="${pct > 85 ? 'warn' : ''}" style="width:${pct.toFixed(1)}%"></span></div>
        <div class="line" style="margin-top:6px"><span>${t.expire ? '到期 ' + fmtTime(t.expire) : '无到期时间'}</span><span>更新于 ${fmtTime(s.updatedAt)}</span></div>
      </div>`;
    }
    const chip = s.status === 'ok' ? '<span class="chip ok">正常</span>'
      : s.status === 'error' ? '<span class="chip err">异常</span>'
        : '<span class="chip">未更新</span>';
    const fmts = { clash: 'Clash 配置', base64: 'Base64 链接', 'uri-list': '明文链接' };

    return `<div class="sub-card" data-id="${esc(s.id)}">
      <div class="sub-top">
        <span class="sub-name">${esc(s.name)}</span>
        ${chip}
        <span class="chip">${esc(fmts[s.format] || s.format || '未知格式')}</span>
        <span class="chip">${s.nodeCount || 0} 节点</span>
        ${filterTag(s)}
      </div>
      <div class="sub-url">${esc(s.url)}</div>
      ${s.error ? `<div class="sub-err">${esc(s.error)}</div>` : ''}
      ${autoUpdateLine}
      ${trafficHtml}
      <div class="sub-actions">
        <button class="btn text small" data-act="refresh">更新</button>
        <button class="btn text small" data-act="edit">编辑</button>
        <button class="btn text small" data-act="copy">复制链接</button>
        <button class="btn text small" data-act="raw">原始内容</button>
        <button class="btn text small" data-act="del" style="color:var(--md-error)">删除</button>
      </div>
    </div>`;
  }).join('');
}

$('#subList').addEventListener('click', async (e) => {
  const btn = e.target.closest('button[data-act]');
  if (!btn) return;
  const card = btn.closest('.sub-card');
  const id = card.dataset.id;
  const sub = state.subs.find((s) => s.id === id);
  if (!sub) return;
  const act = btn.dataset.act;

  if (act === 'refresh') {
    btn.disabled = true; btn.textContent = '更新中…';
    progressOpen('正在更新「' + sub.name + '」…');
    try {
      await api('/api/subs/' + encodeURIComponent(id) + '/refresh', { method: 'POST' });
      toast('订阅已更新', 'ok');
      setTimeout(progressClose, 1500);
      await loadSubs();
    } catch (err) { progressClose(); toast(err.message, 'err'); }
    btn.disabled = false; btn.textContent = '更新';
  }
  if (act === 'edit') openSubModal(sub);
  if (act === 'raw') {
    try {
      const data = await api('/api/subs/' + encodeURIComponent(id) + '/raw');
      $('#rawTitle').textContent = '订阅原始内容：' + sub.name;
      $('#rawMeta').textContent = data.bytes + ' 字节 · 识别：' + data.guess;
      $('#rawBody').textContent = data.preview || '（空）';
      $('#rawModal').classList.remove('hidden');
    } catch (err) { toast(err.message, 'err'); }
  }
  if (act === 'copy') {
    try {
      const data = await api('/api/subs/' + encodeURIComponent(id) + '?reveal=1');
      await copyText((data.sub && data.sub.url) || sub.url);
      toast('链接已复制', 'ok');
    } catch (err) { toast(err.message, 'err'); }
  }
  if (act === 'del') {
    if (!window.confirm('确定删除订阅「' + sub.name + '」？对应节点会一并移除。')) return;
    try {
      await api('/api/subs/' + encodeURIComponent(id), { method: 'DELETE' });
      toast('已删除', 'ok');
      await loadSubs();
      await loadStatus();
    } catch (err) { toast(err.message, 'err'); }
  }
});

async function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  try { document.execCommand('copy'); } finally { ta.remove(); }
}

function openSubModal(sub) {
  const log = $('#subProgLog');
  if (log) { log.innerHTML = ''; log.classList.remove('hidden'); }
  state.editingSub = sub || null;
  $('#subModalTitle').textContent = sub ? '编辑订阅' : '添加订阅';
  $('#f-subName').value = sub ? sub.name : '';
  $('#f-subUrl').value = sub ? sub.url : '';
  $('#f-subInterval').value = sub ? (sub.interval || 0) : 0;
  if ($('#f-subInclude')) $('#f-subInclude').value = sub ? (sub.includeFilter || '') : '';
  if ($('#f-subExclude')) $('#f-subExclude').value = sub ? (sub.excludeFilter || '') : '';
  $('#subModal').classList.remove('hidden');
  setTimeout(() => $('#f-subUrl').focus(), 60);
}

function closeSubModal() {
  $('#subModal').classList.add('hidden');
  const log = $('#subProgLog');
  if (log) { log.classList.add('hidden'); log.innerHTML = ''; }
  state.editingSub = null;
  state.inlineProgress = false;
}

$('#btnAddSub').addEventListener('click', () => openSubModal(null));
$('#btnAddSub2').addEventListener('click', () => openSubModal(null));
$('#subModalCancel').addEventListener('click', closeSubModal);
$('#subModal').addEventListener('click', (e) => { if (e.target.id === 'subModal') closeSubModal(); });

$('#subModalSave').addEventListener('click', async () => {
  const name = $('#f-subName').value.trim();
  const url = $('#f-subUrl').value.trim();
  const interval = parseInt($('#f-subInterval').value, 10) || 0;
  const includeFilter = $('#f-subInclude').value.trim();
  const excludeFilter = $('#f-subExclude').value.trim();
  if (!url) { toast('请填写订阅链接', 'err'); return; }
  if (!/^https?:\/\//i.test(url)) { toast('订阅链接必须以 http:// 或 https:// 开头', 'err'); return; }

  // 名称查重（后端还会再校验一次）
  const dupId = state.editingSub ? state.editingSub.id : null;
  const dup = state.subs.find((x) => x.id !== dupId && (x.name || '').trim().toLowerCase() === name.toLowerCase());
  if (name && dup) { toast('订阅名称「' + name + '」已存在，请换一个', 'err'); $('#f-subName').focus(); return; }

  const btn = $('#subModalSave');
  btn.disabled = true; btn.textContent = '保存中…';
  const isNew = !state.editingSub;
  state.inlineProgress = true;
  const inlineLog = $('#subProgLog');
  inlineLog.innerHTML = '';
  inlineLog.classList.remove('hidden');
  try {
    if (state.editingSub) {
      await api('/api/subs/' + encodeURIComponent(state.editingSub.id), { method: 'PUT', body: { name, url, interval, includeFilter, excludeFilter } });
      toast('订阅已更新', 'ok');
    } else {
      const r = await api('/api/subs', { method: 'POST', body: { name, url, interval, includeFilter, excludeFilter } });
      if (r.error) toast('已添加，但拉取失败：' + r.error, 'err');
      else toast('订阅已添加', 'ok');
    }
    state.inlineProgress = false;
    closeSubModal();          // 成功后自动关闭
    await loadSubs();
    await loadStatus();
    await loadRuleSetInfo();
    await loadStorage();
    await loadNetInfo(false);
    await loadReachability(false);
    if (state.view === 'nodes') await loadNodes();
  } catch (e) {
    state.inlineProgress = false;
    toast(e.message, 'err');
    // 失败时保留弹窗与日志，方便按提示改 URL / 直接看原文
  } finally {
    btn.disabled = false; btn.textContent = '保存';
  }
});

/* ---------------- 备份 / 恢复 ---------------- */

$('#btnBackupExport').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  btn.disabled = true;
  try {
    const res = await fetch('/api/backup', { credentials: 'same-origin' });
    if (!res.ok) throw new Error('导出失败：HTTP ' + res.status);
    const blob = await res.blob();
    const ts = new Date();
    const pad = (n) => String(n).padStart(2, '0');
    const name = 'mihomo-merlin-backup-' + ts.getFullYear() + pad(ts.getMonth() + 1) + pad(ts.getDate()) +
      '-' + pad(ts.getHours()) + pad(ts.getMinutes()) + '.json';
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = name;
    document.body.appendChild(a);
    a.click();
    setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1000);
    if ($('#backupHint')) $('#backupHint').textContent = '已导出 ' + name;
    toast('备份已导出（含订阅地址，请妥善保管）', 'ok');
  } catch (err) {
    toast(err.message, 'err');
  } finally {
    btn.disabled = false;
  }
});

$('#btnBackupImport').addEventListener('click', () => $('#backupFile').click());

$('#backupFile').addEventListener('change', async (e) => {
  const file = e.target.files && e.target.files[0];
  e.target.value = '';
  if (!file) return;
  if (!confirm('导入会覆盖当前设置与订阅（节点文件一起恢复），继续？')) return;
  const btn = $('#btnBackupImport');
  btn.disabled = true;
  try {
    const text = await file.text();
    const r = await api('/api/restore', { method: 'POST', body: JSON.parse(text) });
    toast((r && r.message) || '已恢复', 'ok');
    if ($('#backupHint')) $('#backupHint').textContent = '已导入 ' + file.name;
    await Promise.all([loadSettings(), loadStatus(), loadSubs()]);
    if (state.view === 'nodes') await loadNodes();
  } catch (err) {
    toast('导入失败：' + (err.message || err), 'err');
  } finally {
    btn.disabled = false;
  }
});

/* 导入配置：订阅在路由器上拉不动时的兜底 */
function openImportModal() {
  $('#f-importName').value = '';
  $('#f-importContent').value = '';
  $('#importModal').classList.remove('hidden');
  setTimeout(() => $('#f-importContent').focus(), 60);
}

function closeImportModal() {
  $('#importModal').classList.add('hidden');
}

$('#btnImportSub').addEventListener('click', openImportModal);
$('#importCancel').addEventListener('click', closeImportModal);
$('#importModal').addEventListener('click', (e) => { if (e.target.id === 'importModal') closeImportModal(); });

$('#btnImportFile').addEventListener('click', () => $('#importFile').click());
$('#importFile').addEventListener('change', (e) => {
  const f = e.target.files && e.target.files[0];
  if (!f) return;
  const reader = new FileReader();
  reader.onload = () => {
    $('#f-importContent').value = String(reader.result || '');
    if (!$('#f-importName').value) $('#f-importName').value = f.name.replace(/\.[^.]+$/, '');
    toast('已读取 ' + f.name, 'ok');
  };
  reader.readAsText(f);
});

$('#importSave').addEventListener('click', async () => {
  const name = $('#f-importName').value.trim();
  const content = $('#f-importContent').value;
  if (!content.trim()) { toast('请先粘贴或读取配置内容', 'err'); return; }
  const btn = $('#importSave');
  btn.disabled = true;
  btn.textContent = '导入中…';
  progressOpen('正在导入配置…');
  try {
    const r = await api('/api/subs/import', { method: 'POST', body: { name, content } });
    toast('已导入 ' + ((r.sub && r.sub.nodeCount) || 0) + ' 个节点', 'ok');
    setTimeout(progressClose, 1500);
    closeImportModal();
    await loadSubs();
    await loadStatus();
    await loadRuleSetInfo();
    await loadStorage();
    await loadNetInfo(false);
    await loadReachability(false);
    if (state.view === 'nodes') await loadNodes();
  } catch (e) {
    progressClose();
    toast(e.message, 'err');
  } finally {
    btn.disabled = false;
    btn.textContent = '导入';
  }
});

$('#btnRefreshAll').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  btn.disabled = true;
  const old = btn.innerHTML;
  btn.innerHTML = '<span>更新中…</span>';
  progressOpen('正在更新全部订阅…');
  try {
    const r = await api('/api/subs/refresh', { method: 'POST' });
    let msg = `已更新 ${r.refreshed} 个订阅`;
    if (r.failed) msg += `，${r.failed} 个失败`;
    setTimeout(progressClose, 1500);
    toast(msg, r.failed ? 'err' : 'ok');
    await loadSubs();
    await loadStatus();
    await loadRuleSetInfo();
    await loadStorage();
    await loadNetInfo(false);
    await loadReachability(false);
  } catch (err) {
    progressClose();
    toast(err.message, 'err');
  } finally {
    btn.disabled = false;
    btn.innerHTML = old;
  }
});

/* ---------------- 规则 ---------------- */

async function loadRules() {
  try {
    const data = await api('/api/rules');
    state.rules = data.rules || [];
    fillRuleFilter();
    renderRules();
  } catch (e) { /* ignore */ }
}

function fillRuleFilter() {
  const sel = $('#ruleFilter');
  const cur = sel.value;
  const proxies = Array.from(new Set(state.rules.map((r) => r.proxy).filter(Boolean))).sort();
  sel.innerHTML = '<option value="">全部策略</option>' + proxies.map((p) => `<option value="${esc(p)}">${esc(p)}</option>`).join('');
  sel.value = proxies.includes(cur) ? cur : '';
}

function renderRules() {
  const kw = state.ruleSearch.trim().toLowerCase();
  const rows = state.rules.filter((r) => {
    if (state.ruleFilter && r.proxy !== state.ruleFilter) return false;
    if (!kw) return true;
    return String(r.type || '').toLowerCase().includes(kw)
      || String(r.payload || '').toLowerCase().includes(kw)
      || String(r.proxy || '').toLowerCase().includes(kw);
  });
  $('#ruleCount').textContent = rows.length + ' / ' + state.rules.length + ' 条';
  $('#ruleList').innerHTML = rows.length
    ? rows.map((r) => `<div class="rule-row">
        <span class="rule-type">${esc(r.type || '')}</span>
        <span class="rule-payload" title="${esc(r.payload || '')}">${esc(r.payload || '—')}</span>
        <span class="rule-proxy">→ ${esc(r.proxy || '')}</span>
      </div>`).join('')
    : '<div class="muted" style="padding:14px">没有匹配的规则</div>';
}

$('#ruleSearch').addEventListener('input', (e) => { state.ruleSearch = e.target.value; renderRules(); });
$('#ruleFilter').addEventListener('change', (e) => { state.ruleFilter = e.target.value; renderRules(); });

/* ---------------- 连接明细 ---------------- */

const CONN_POLL_MS = 2000;
const CONN_MAX_ROWS = 200;

let connTimer = null;

function hhmmss() {
  const d = new Date();
  const p = (x) => String(x).padStart(2, '0');
  return p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
}

function connRowHtml(r) {
  const host = r.host || r.dest || '—';
  const netType = [r.network, r.type ? protoShort(r.type) : ''].filter(Boolean).join(' · ');
  const ruleMain = r.rulePayload || r.rule || '—';
  const ruleSub = r.rulePayload && r.rule ? r.rule : '—';
  const chains = Array.isArray(r.chains) && r.chains.length ? r.chains.join(' → ') : '—';
  const hostTitle = host + (r.start ? ' · 开始于 ' + fmtTime(r.start) : '');
  const dur = r.start ? fmtDur((Date.now() - new Date(r.start).getTime()) / 1000) : '—';
  return `<div class="conn-row" data-id="${esc(r.id || '')}">
    <div class="conn-cell conn-host">
      <b title="${esc(hostTitle)}">${esc(host)}</b>
      <span class="conn-sub">${esc(netType || '—')}</span>
    </div>
    <div class="conn-cell conn-rule">
      <b title="${esc(ruleMain)}">${esc(ruleMain)}</b>
      <span class="conn-sub">${esc(ruleSub)}</span>
    </div>
    <div class="conn-chain" title="${esc(chains)}">${esc(chains)}</div>
    <div class="conn-traffic">
      <span class="dn">↓ ${fmtBytes(r.download || 0)}</span>
      <span class="up">↑ ${fmtBytes(r.upload || 0)}</span>
    </div>
    <div class="conn-dur">${esc(dur)}</div>
    <button class="conn-close" data-close="${esc(r.id || '')}" title="关闭此连接">✕</button>
  </div>`;
}

function renderConnRows(rows) {
  const body = $('#connBody');
  const table = $('#connTable');
  const empty = $('#connEmpty');
  if (!rows.length) {
    body.innerHTML = '';
    if (table) table.classList.add('hidden');
    if (empty) empty.classList.remove('hidden');
    return;
  }
  if (table) table.classList.remove('hidden');
  if (empty) empty.classList.add('hidden');
  const kw = (state.connFilter || '').trim().toLowerCase();
  const shown = kw
    ? rows.filter((r) => JSON.stringify([r.host || r.dest || '', r.rule || '', (r.chains || []).join(' '), r.process || '']).toLowerCase().includes(kw))
    : rows;
  body.innerHTML = shown.slice(0, CONN_MAX_ROWS).map(connRowHtml).join('');
  body.querySelectorAll('.conn-close').forEach((btn) => {
    btn.addEventListener('click', async (e) => {
      e.stopPropagation();
      const id = btn.dataset.close;
      if (!id) return;
      btn.disabled = true;
      try {
        const r = await api('/api/connections/' + encodeURIComponent(id), { method: 'DELETE' });
        if (r && r.ok) toast('已关闭该连接', 'ok'); else toast((r && r.err) || '关闭失败', 'err');
      } catch (err) { toast(err.message, 'err'); }
      await loadConnections();
    });
  });
}

$('#connSearch').addEventListener('input', (e) => {
  state.connFilter = e.target.value;
  if (state.conns) renderConnRows(state.conns);
});

$('#btnCloseAllConns').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  const n = (state.conns || []).length;
  if (!confirm('关闭全部 ' + n + ' 条连接？')) return;
  btn.disabled = true;
  try {
    const r = await api('/api/connections', { method: 'DELETE' });
    toast('已关闭 ' + ((r && r.closed) || n) + ' 条连接', 'ok');
  } catch (err) {
    toast(err.message, 'err');
  } finally {
    if (btn) btn.disabled = false;
  }
  await loadConnections();
});

async function loadConnections() {
  let data;
  try {
    data = await api('/api/connections');
  } catch (e) {
    // 接口不可用：没有历史数据时显示「—」/空态；有数据时保留上次结果，不清屏
    if (!state.conns) {
      $('#connCount').textContent = '—';
      $('#connUp').textContent = '—';
      $('#connDown').textContent = '—';
      $('#connUpdated').textContent = '—';
      renderConnRows([]);
    } else {
      $('#connUpdated').textContent = '更新失败，显示上次数据';
    }
    return;
  }
  const rows = Array.isArray(data.rows) ? data.rows
    : (Array.isArray(data.connections) ? data.connections : []);
  state.conns = rows;

  const count = data.count != null ? data.count : rows.length;
  const up = data.up != null ? data.up : data.uploadTotal;
  const down = data.down != null ? data.down : data.downloadTotal;
  $('#connCount').textContent = String(count || 0);
  $('#connUp').textContent = up != null ? fmtBytes(up) : '—';
  $('#connDown').textContent = down != null ? fmtBytes(down) : '—';

  renderConnRows(rows);
  let note = '更新于 ' + hhmmss();
  if (rows.length > CONN_MAX_ROWS) note += ' · 仅显示前 ' + CONN_MAX_ROWS + ' / 共 ' + rows.length + ' 条';
  else note += ' · 共 ' + (count || rows.length) + ' 条';
  $('#connUpdated').textContent = note;
}

function startConnPoll() {
  if (connTimer) return;
  connTimer = setInterval(() => {
    if (document.hidden) return;               // 页面不可见时暂停轮询
    if (state.view !== 'connections') return;  // 只在连接页轮询
    loadConnections();
  }, CONN_POLL_MS);
}

/* ---------------- 日志 ---------------- */

function ensureLogStream() {
  if (state.logStream) return;
  const es = new EventSource('/api/logs/stream');
  state.logStream = es;
  es.onmessage = (ev) => {
    if (state.logPaused) return;
    let d;
    try { d = JSON.parse(ev.data); } catch (e) { return; }
    appendLog(d);
  };
}

$('#logSearch').addEventListener('input', (e) => {
  state.logFilter = e.target.value;
  const kw = (state.logFilter || '').trim().toLowerCase();
  $('#logBox').querySelectorAll('div').forEach((el) => {
    el.classList.toggle('hidden', !!kw && !el.textContent.toLowerCase().includes(kw));
  });
});

function appendLog(d) {
  const box = $('#logBox');
  const cls = d.type === 'error' ? 'l-error'
    : d.type === 'warning' ? 'l-warning'
      : d.type === 'debug' ? 'l-debug'
        : (String(d.payload || '').startsWith('[面板]') ? 'l-panel' : '');
  const line = document.createElement('div');
  if (cls) line.className = cls;
  line.textContent = String(d.payload == null ? '' : d.payload);
  const kw = (state.logFilter || '').trim().toLowerCase();
  if (kw && !line.textContent.toLowerCase().includes(kw)) line.classList.add('hidden');
  box.appendChild(line);
  while (box.childElementCount > 500) box.removeChild(box.firstChild);
  if ($('#logAuto').checked) box.scrollTop = box.scrollHeight;
}

$('#logPause').addEventListener('change', (e) => { state.logPaused = e.target.checked; });
$('#btnClearLog').addEventListener('click', () => { $('#logBox').innerHTML = ''; });

/* ---------------- 设置 ---------------- */

const SETTING_FIELDS = ['mode', 'rulePreset', 'redirPort', 'mixedPort', 'logLevel', 'subInterval', 'panelPassword',
  'allowLan', 'autoStart', 'blockQuic', 'sniffer', 'routerSelf',
  'subUA', 'subUACustom', 'subInsecure', 'subProxyMode',
  'ruleSourceURL', 'ruleAutoUpdate', 'ruleIntervalH', 'ruleSourcePreset',
  'autoRestart', 'restartEveryHours', 'restartAt',
  'dnsEnable', 'dnsMode', 'dnsListen', 'dnsNameserver', 'dnsFallback', 'dnsFakeIpRange',
  'autoIntervalSec', 'autoToleranceMs'];

async function loadSettings() {
  try {
    const data = await api('/api/settings');
    state.settings = data.settings || {};
    fillSettings(state.settings);
  } catch (e) { /* ignore */ }
}

$('#logLevelSel').addEventListener('change', async (e) => {
  const lv = e.target.value;
  try {
    await api('/api/settings', {
      method: 'PUT',
      body: JSON.stringify({ ...state.settings, logLevel: lv }),
    });
    state.settings.logLevel = lv;
    const set = $('#s-logLevel');
    if (set) set.value = lv;
    toast('日志级别已切换为 ' + lv);
  } catch (err) {
    toast(err.message, 'err');
  }
});

function syncLogLevelSel(level) {
  const sel = $('#logLevelSel');
  if (sel && level) sel.value = level;
}

function fillSettings(s) {
  SETTING_FIELDS.forEach((k) => {
    const el = $('#s-' + k);
    if (!el) return;
    if (el.type === 'checkbox') el.checked = !!s[k];
    else el.value = s[k] == null ? '' : s[k];
  });
  syncLogLevelSel(s.logLevel);
  if ($('#s-ruleSourcePreset') && !$('#s-ruleSourcePreset').value) $('#s-ruleSourcePreset').value = 'chnroutes2';
  if (typeof syncRuleCustomVisibility === 'function') syncRuleCustomVisibility();
}

function collectSettings() {
  const out = {};
  SETTING_FIELDS.forEach((k) => {
    const el = $('#s-' + k);
    if (!el) return;
    if (el.type === 'checkbox') out[k] = el.checked;
    else if (el.type === 'number') out[k] = parseInt(el.value, 10) || 0;
    else out[k] = el.value;
  });
  return out;
}

async function doSaveSettings(silent) {
  try {
    const body = collectSettings();
    delete body.mode;
    const data = await api('/api/settings', { method: 'PUT', body });
    state.settings = data.settings || {};
    fillSettings(state.settings);
    if (!silent) toast('设置已保存', 'ok');
    await loadStatus();
    await loadRuleSetInfo();
    return true;
  } catch (e) {
    toast(e.message, 'err');
    return false;
  }
}

async function onSaveClick(e) {
  const btn = e.currentTarget;
  btn.disabled = true;
  await doSaveSettings(false);
  btn.disabled = false;
}

if ($('#btnSaveSettingsBottom')) $('#btnSaveSettingsBottom').addEventListener('click', onSaveClick);

// 这些参数改完就自动保存，不用再去找按钮
['s-autoRestart', 's-restartEveryHours', 's-restartAt',
 's-autoIntervalSec', 's-autoToleranceMs',
 's-ruleSourcePreset', 's-ruleAutoUpdate', 's-ruleIntervalH', 's-ruleSourceURL'].forEach((id) => {
  const el = $('#' + id);
  if (!el) return;
  el.addEventListener('change', async () => {
    const ok = await doSaveSettings(true);
    if (ok) toast('已保存', 'ok');
  });
});

$('#btnResetSettings').addEventListener('click', async () => {
  if (!window.confirm('恢复默认设置？（不会删除订阅）')) return;
  const defaults = {
    allowLan: true, redirPort: 3333, mixedPort: 7890, logLevel: 'info', autoStart: true,
    blockQuic: true, subInterval: 6, panelPassword: '', rulePreset: 'cn-direct', sniffer: true, routerSelf: false,
  };
  try {
    const data = await api('/api/settings', { method: 'PUT', body: defaults });
    state.settings = data.settings || {};
    fillSettings(state.settings);
    toast('已恢复默认', 'ok');
  } catch (e) { toast(e.message, 'err'); }
});

/* ---------------- 服务状态（设置页） ---------------- */

const SVC_POLL_MS = 5000;
const MODE_LABEL = { rule: '规则', global: '全局', direct: '直连' };

let svcTimer = null;

function svcValue(id, text, kind) {
  const el = $('#' + id);
  if (!el) return;
  el.textContent = text;
  el.classList.remove('svc-ok', 'svc-err');
  if (kind) el.classList.add(kind);
}

function svcFallback() {
  svcValue('svcCore', '—');
  svcValue('svcIptables', '—');
  svcValue('svcPorts', '—');
  svcValue('svcMode', '—');
  const hint = $('#svcHint');
  if (hint) hint.textContent = '暂时读不到服务状态（显示「—」）';
}

function renderService(d) {
  const core = d.core || {};
  const ipt = d.iptables || {};
  const ports = d.ports || {};

  if (core.reachable) {
    const lat = Number(core.apiLatencyMs);
    svcValue('svcCore', '可达' + (lat > 0 ? ' · ' + lat.toFixed(0) + ' ms' : ''), 'svc-ok');
  } else {
    svcValue('svcCore', '不可达', 'svc-err');
  }
  const coreEl = $('#svcCore');
  if (coreEl) coreEl.title = core.version ? 'mihomo ' + core.version : '';

  if (ipt.applied) svcValue('svcIptables', '已应用 · ' + fmtTime(ipt.lastApply), 'svc-ok');
  else svcValue('svcIptables', '未应用', ipt.applied === false ? 'svc-err' : '');

  const portBits = [];
  if (ports.redir) portBits.push('透明 ' + ports.redir);
  if (ports.mixed) portBits.push('混合 ' + ports.mixed);
  svcValue('svcPorts', portBits.join(' · ') || '—');

  let modeText = MODE_LABEL[d.modeActual] || d.modeActual || '—';
  if (d.modeActual && d.mode && d.modeActual !== d.mode) {
    modeText += '（设置里是 ' + (MODE_LABEL[d.mode] || d.mode) + '，未生效）';
  }
  svcValue('svcMode', modeText);

  const hint = $('#svcHint');
  if (hint) hint.textContent = '更新于 ' + hhmmss() + ' · 每 5 秒自动刷新（仅本页可见时）';
}

async function loadService() {
  try {
    const d = await api('/api/service');
    renderService(d || {});
  } catch (e) {
    svcFallback();   // 接口还没实现时静默降级成「—」
  }
}

function startSvcPoll() {
  if (svcTimer) return;
  svcTimer = setInterval(() => {
    if (document.hidden) return;            // 页面不可见时暂停轮询
    if (state.view !== 'settings') return;  // 只在设置页刷新
    loadService();
  }, SVC_POLL_MS);
}

$('#btnSvcRefresh').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  btn.disabled = true;
  await loadService();
  if (btn) btn.disabled = false;
});

/* ---------------- 订阅自动更新展示 ---------------- */

function renderAutoUpdateLine(s) {
  const info = s.autoUpdate || {};
  if (s.url === '' || s.url === '—') return '<div class="line auto-update"><span>手动导入（不自动更新）</span><span></span></div>';
  if (!info.enabled) return '<div class="line auto-update"><span>自动更新已关闭</span><span></span></div>';
  const left = info.nextAt ? Math.max(0, info.nextAt - Math.floor(Date.now() / 1000)) : 0;
  const next = info.nextAt ? (left > 0 ? '下次 ' + fmtDur(left) + '后' : '即将更新') : '下次 —';
  return '<div class="line auto-update"><span>自动更新 · 每 ' + info.interval + ' 小时</span><span>' + next + '</span></div>';
}

/* ---------------- 网络状态 ---------------- */

// 国家码 → emoji 旗帜（用区域指示符拼出来）
function flagOf(cc) {
  if (!cc || cc.length !== 2) return '';
  const A = 0x1f1e6;
  const up = cc.toUpperCase();
  return String.fromCodePoint(A + up.charCodeAt(0) - 65) + String.fromCodePoint(A + up.charCodeAt(1) - 65);
}

// 只显示：旗帜+国家码 · 类型 · 延迟；ISP/ASN 等细节放进 title
const KIND_SHORT = { '住宅/商业': '住宅', '机房/云': '机房', '移动网络': '移动', '代理/VPN': '代理', '未知': '未知' };


let netPollTimer = null;

async function loadIntranet() {
  try {
    const r = await api('/api/intranet');
    const d = (r && r.intranet) || {};
    state.intranet = d;
  } catch (e) { /* 忽略 */ }
}

async function loadNetInfo(force) {
  // 只同步「当前节点 + 延迟」，不再做出口 IP / 质量探测
  try {
    const r = await api('/api/netinfo' + (force ? '?refresh=1' : ''));
    const info = (r && r.info) || {};
    const nodeEl = $('#netNode');
    const delayEl = $('#netNodeDelay');
    if (!nodeEl || !delayEl) return;
    if (info.node) {
      nodeEl.textContent = info.node;
      delayEl.textContent = info.nodeDelay > 0 ? info.nodeDelay + ' ms' : '未测';
    } else {
      nodeEl.textContent = '—';
      delayEl.textContent = '';
    }
  } catch (e) {
    /* 拿不到就保持原样，不打扰用户 */
  }
}

;

/* ---------------- IP 质量 / 内网 IP ---------------- */

const IPQ_TYPE = {
  residential: '住宅', datacenter: '机房', idc: '机房', hosting: '机房',
  mobile: '移动', business: '企业', corporate: '企业', education: '教育',
  isp: '运营商', unknown: '未知',
};
const IPQ_LEVEL = {
  good: { text: '良好', cls: 'q-good' },
  normal: { text: '一般', cls: 'q-normal' },
  risky: { text: '风险', cls: 'q-risky' },
  unknown: { text: '未知', cls: 'q-unknown' },
};

function isDarkTheme() {
  return document.documentElement.getAttribute('data-theme') !== 'light';
}




/* ---------------- 网络状态自动刷新 ---------------- */
// 引擎状态变化时由 renderStatus 触发一次强制刷新；运行中每 15 秒轻量轮询。
const NET_AUTO_MS = 15000;
let netAutoTimer = null;

function engineRunning() {
  return !!(state.status && state.status.engine && state.status.engine.running);
}


/* ---------------- 服务连通性 ---------------- */

const REACH_LOGOS = {"google": {"vb": "0 0 256 262", "inner": "<path fill=\"#4285f4\" d=\"M255.878 133.451c0-10.734-.871-18.567-2.756-26.69H130.55v48.448h71.947c-1.45 12.04-9.283 30.172-26.69 42.356l-.244 1.622l38.755 30.023l2.685.268c24.659-22.774 38.875-56.282 38.875-96.027\"/><path fill=\"#34a853\" d=\"M130.55 261.1c35.248 0 64.839-11.605 86.453-31.622l-41.196-31.913c-11.024 7.688-25.82 13.055-45.257 13.055c-34.523 0-63.824-22.773-74.269-54.25l-1.531.13l-40.298 31.187l-.527 1.465C35.393 231.798 79.49 261.1 130.55 261.1\"/><path fill=\"#fbbc05\" d=\"M56.281 156.37c-2.756-8.123-4.351-16.827-4.351-25.82c0-8.994 1.595-17.697 4.206-25.82l-.073-1.73L15.26 71.312l-1.335.635C5.077 89.644 0 109.517 0 130.55s5.077 40.905 13.925 58.602z\"/><path fill=\"#eb4335\" d=\"M130.55 50.479c24.514 0 41.05 10.589 50.479 19.438l36.844-35.974C195.245 12.91 165.798 0 130.55 0C79.49 0 35.393 29.301 13.925 71.947l42.211 32.783c10.59-31.477 39.891-54.251 74.414-54.251\"/>", "mono": null}, "github": {"vb": "0 0 256 250", "inner": "<path  d=\"M128.001 0C57.317 0 0 57.307 0 128.001c0 56.554 36.676 104.535 87.535 121.46c6.397 1.185 8.746-2.777 8.746-6.158c0-3.052-.12-13.135-.174-23.83c-35.61 7.742-43.124-15.103-43.124-15.103c-5.823-14.795-14.213-18.73-14.213-18.73c-11.613-7.944.876-7.78.876-7.78c12.853.902 19.621 13.19 19.621 13.19c11.417 19.568 29.945 13.911 37.249 10.64c1.149-8.272 4.466-13.92 8.127-17.116c-28.431-3.236-58.318-14.212-58.318-63.258c0-13.975 5-25.394 13.188-34.358c-1.329-3.224-5.71-16.242 1.24-33.874c0 0 10.749-3.44 35.21 13.121c10.21-2.836 21.16-4.258 32.038-4.307c10.878.049 21.837 1.47 32.066 4.307c24.431-16.56 35.165-13.12 35.165-13.12c6.967 17.63 2.584 30.65 1.255 33.873c8.207 8.964 13.173 20.383 13.173 34.358c0 49.163-29.944 59.988-58.447 63.157c4.591 3.972 8.682 11.762 8.682 23.704c0 17.126-.148 30.91-.148 35.126c0 3.407 2.304 7.398 8.792 6.14C219.37 232.5 256 184.537 256 128.002C256 57.307 198.691 0 128.001 0m-80.06 182.34c-.282.636-1.283.827-2.194.39c-.929-.417-1.45-1.284-1.15-1.922c.276-.655 1.279-.838 2.205-.399c.93.418 1.46 1.293 1.139 1.931m6.296 5.618c-.61.566-1.804.303-2.614-.591c-.837-.892-.994-2.086-.375-2.66c.63-.566 1.787-.301 2.626.591c.838.903 1 2.088.363 2.66m4.32 7.188c-.785.545-2.067.034-2.86-1.104c-.784-1.138-.784-2.503.017-3.05c.795-.547 2.058-.055 2.861 1.075c.782 1.157.782 2.522-.019 3.08m7.304 8.325c-.701.774-2.196.566-3.29-.49c-1.119-1.032-1.43-2.496-.726-3.27c.71-.776 2.213-.558 3.315.49c1.11 1.03 1.45 2.505.701 3.27m9.442 2.81c-.31 1.003-1.75 1.459-3.199 1.033c-1.448-.439-2.395-1.613-2.103-2.626c.301-1.01 1.747-1.484 3.207-1.028c1.446.436 2.396 1.602 2.095 2.622m10.744 1.193c.036 1.055-1.193 1.93-2.715 1.95c-1.53.034-2.769-.82-2.786-1.86c0-1.065 1.202-1.932 2.733-1.958c1.522-.03 2.768.818 2.768 1.868m10.555-.405c.182 1.03-.875 2.088-2.387 2.37c-1.485.271-2.861-.365-3.05-1.386c-.184-1.056.893-2.114 2.376-2.387c1.514-.263 2.868.356 3.061 1.403\"/>", "mono": true}, "youtube": {"vb": "0 0 256 180", "inner": "<path fill=\"red\" d=\"M250.346 28.075A32.18 32.18 0 0 0 227.69 5.418C207.824 0 127.87 0 127.87 0S47.912.164 28.046 5.582A32.18 32.18 0 0 0 5.39 28.24c-6.009 35.298-8.34 89.084.165 122.97a32.18 32.18 0 0 0 22.656 22.657c19.866 5.418 99.822 5.418 99.822 5.418s79.955 0 99.82-5.418a32.18 32.18 0 0 0 22.657-22.657c6.338-35.348 8.291-89.1-.164-123.134\"/><path fill=\"#fff\" d=\"m102.421 128.06l66.328-38.418l-66.328-38.418z\"/>", "mono": null}, "openai": {"vb": "0 0 256 260", "inner": "<path d=\"M239.184 106.203a64.72 64.72 0 0 0-5.576-53.103C219.452 28.459 191 15.784 163.213 21.74A65.586 65.586 0 0 0 52.096 45.22a64.72 64.72 0 0 0-43.23 31.36c-14.31 24.602-11.061 55.634 8.033 76.74a64.67 64.67 0 0 0 5.525 53.102c14.174 24.65 42.644 37.324 70.446 31.36a64.72 64.72 0 0 0 48.754 21.744c28.481.025 53.714-18.361 62.414-45.481a64.77 64.77 0 0 0 43.229-31.36c14.137-24.558 10.875-55.423-8.083-76.483m-97.56 136.338a48.4 48.4 0 0 1-31.105-11.255l1.535-.87l51.67-29.825a8.6 8.6 0 0 0 4.247-7.367v-72.85l21.845 12.636c.218.111.37.32.409.563v60.367c-.056 26.818-21.783 48.545-48.601 48.601M37.158 197.93a48.35 48.35 0 0 1-5.781-32.589l1.534.921l51.722 29.826a8.34 8.34 0 0 0 8.441 0l63.181-36.425v25.221a.87.87 0 0 1-.358.665l-52.335 30.184c-23.257 13.398-52.97 5.431-66.404-17.803M23.549 85.38a48.5 48.5 0 0 1 25.58-21.333v61.39a8.29 8.29 0 0 0 4.195 7.316l62.874 36.272l-21.845 12.636a.82.82 0 0 1-.767 0L41.353 151.53c-23.211-13.454-31.171-43.144-17.804-66.405zm179.466 41.695l-63.08-36.63L161.73 77.86a.82.82 0 0 1 .768 0l52.233 30.184a48.6 48.6 0 0 1-7.316 87.635v-61.391a8.54 8.54 0 0 0-4.4-7.213m21.742-32.69l-1.535-.922l-51.619-30.081a8.39 8.39 0 0 0-8.492 0L99.98 99.808V74.587a.72.72 0 0 1 .307-.665l52.233-30.133a48.652 48.652 0 0 1 72.236 50.391zM88.061 139.097l-21.845-12.585a.87.87 0 0 1-.41-.614V65.685a48.652 48.652 0 0 1 79.757-37.346l-1.535.87l-51.67 29.825a8.6 8.6 0 0 0-4.246 7.367zm11.868-25.58L128.067 97.3l28.188 16.218v32.434l-28.086 16.218l-28.188-16.218z\"/>", "mono": true}, "anthropic": {"vb": "0 0 256 257", "inner": "<path fill=\"#d97757\" d=\"m50.228 170.321l50.357-28.257l.843-2.463l-.843-1.361h-2.462l-8.426-.518l-28.775-.778l-24.952-1.037l-24.175-1.296l-6.092-1.297L0 125.796l.583-3.759l5.12-3.434l7.324.648l16.202 1.101l24.304 1.685l17.629 1.037l26.118 2.722h4.148l.583-1.685l-1.426-1.037l-1.101-1.037l-25.147-17.045l-27.22-18.017l-14.258-10.37l-7.713-5.25l-3.888-4.925l-1.685-10.758l7-7.713l9.397.649l2.398.648l9.527 7.323l20.35 15.75L94.817 91.9l3.889 3.24l1.555-1.102l.195-.777l-1.75-2.917l-14.453-26.118l-15.425-26.572l-6.87-11.018l-1.814-6.61c-.648-2.723-1.102-4.991-1.102-7.778l7.972-10.823L71.42 0l10.63 1.426l4.472 3.888l6.61 15.101l10.694 23.786l16.591 32.34l4.861 9.592l2.592 8.879l.973 2.722h1.685v-1.556l1.36-18.211l2.528-22.36l2.463-28.776l.843-8.1l4.018-9.722l7.971-5.25l6.222 2.981l5.12 7.324l-.713 4.73l-3.046 19.768l-5.962 30.98l-3.889 20.739h2.268l2.593-2.593l10.499-13.934l17.628-22.036l7.778-8.749l9.073-9.657l5.833-4.601h11.018l8.1 12.055l-3.628 12.443l-11.342 14.388l-9.398 12.184l-13.48 18.147l-8.426 14.518l.778 1.166l2.01-.194l30.46-6.481l16.462-2.982l19.637-3.37l8.88 4.148l.971 4.213l-3.5 8.62l-20.998 5.184l-24.628 4.926l-36.682 8.685l-.454.324l.519.648l16.526 1.555l7.065.389h17.304l32.21 2.398l8.426 5.574l5.055 6.805l-.843 5.184l-12.962 6.611l-17.498-4.148l-40.83-9.721l-14-3.5h-1.944v1.167l11.666 11.406l21.387 19.314l26.767 24.887l1.36 6.157l-3.434 4.86l-3.63-.518l-23.526-17.693l-9.073-7.972l-20.545-17.304h-1.36v1.814l4.73 6.935l25.017 37.59l1.296 11.536l-1.814 3.76l-6.481 2.268l-7.13-1.297l-14.647-20.544l-15.1-23.138l-12.185-20.739l-1.49.843l-7.194 77.448l-3.37 3.953l-7.778 2.981l-6.48-4.925l-3.436-7.972l3.435-15.749l4.148-20.544l3.37-16.333l3.046-20.285l1.815-6.74l-.13-.454l-1.49.194l-15.295 20.999l-23.267 31.433l-18.406 19.702l-4.407 1.75l-7.648-3.954l.713-7.064l4.277-6.286l25.47-32.405l15.36-20.092l9.917-11.6l-.065-1.686h-.583L44.07 198.125l-12.055 1.555l-5.185-4.86l.648-7.972l2.463-2.593l20.35-13.999z\"/>", "mono": null}, "netflix": {"vb": "0 0 24 24", "inner": "<path fill=\"#e50914\" d=\"m5.398 0l8.348 23.602c2.346.059 4.856.398 4.856.398L10.113 0zm8.489 0v9.172l4.715 13.33V0zM5.398 1.5V24c1.873-.225 2.81-.312 4.715-.398V14.83z\"/>", "mono": null}, "bilibili": {"vb": "0 0 24 24", "inner": "<path fill=\"#00A1D6\" d=\"M17.813 4.653h.854q2.266.08 3.773 1.574Q23.946 7.72 24 9.987v7.36q-.054 2.266-1.56 3.773c-1.506 1.507-2.262 1.524-3.773 1.56H5.333q-2.266-.054-3.773-1.56C.053 19.614.036 18.858 0 17.347v-7.36q.054-2.267 1.56-3.76t3.773-1.574h.774l-1.174-1.12a1.23 1.23 0 0 1-.373-.906q0-.534.373-.907l.027-.027q.4-.373.92-.373t.92.373L9.653 4.44q.107.106.187.213h4.267a.8.8 0 0 1 .16-.213l2.853-2.747q.4-.373.92-.373c.347 0 .662.151.929.4s.391.551.391.907q0 .532-.373.906zM5.333 7.24q-1.12.027-1.88.773q-.76.748-.786 1.894v7.52q.026 1.146.786 1.893t1.88.773h13.334q1.12-.026 1.88-.773t.786-1.893v-7.52q-.026-1.147-.786-1.894t-1.88-.773zM8 11.107q.56 0 .933.373q.375.374.4.96v1.173q-.025.586-.4.96q-.373.375-.933.374c-.56-.001-.684-.125-.933-.374q-.375-.373-.4-.96V12.44q0-.56.386-.947q.387-.386.947-.386m8 0q.56 0 .933.373q.375.374.4.96v1.173q-.025.586-.4.96q-.373.375-.933.374c-.56-.001-.684-.125-.933-.374q-.375-.373-.4-.96V12.44q.025-.586.4-.96q.373-.373.933-.373\"/>", "mono": null}}

const REACH_LABEL = { ok: '可用', restricted: '受限', timeout: '超时', fail: '失败', unknown: '未知' };
const REACH_LOGO = {
  google: 'M12.48 10.92v3.28h7.84c-.24 1.84-.853 3.187-1.787 4.133-1.147 1.147-2.933 2.4-6.053 2.4-4.827 0-8.6-3.893-8.6-8.72s3.773-8.72 8.6-8.72c2.6 0 4.507 1.027 5.907 2.347l2.307-2.307C18.747 1.44 16.133 0 12.48 0 5.867 0 .307 5.387.307 12s5.56 12 12.173 12c3.573 0 6.267-1.173 8.373-3.36 2.16-2.16 2.84-5.213 2.84-7.667 0-.76-.053-1.467-.173-2.053H12.48z',
  github: 'M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12',
  youtube: 'M23.498 6.186a3.016 3.016 0 0 0-2.122-2.136C19.505 3.545 12 3.545 12 3.545s-7.505 0-9.377.505A3.017 3.017 0 0 0 .502 6.186C0 8.07 0 12 0 12s0 3.93.502 5.814a3.016 3.016 0 0 0 2.122 2.136c1.871.505 9.376.505 9.376.505s7.505 0 9.377-.505a3.015 3.015 0 0 0 2.122-2.136C24 15.93 24 12 24 12s0-3.93-.502-5.814zM9.545 15.568V8.432L15.818 12l-6.273 3.568z',
  openai: 'M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z',
  anthropic: 'M17.3041 3.541h-3.6718l6.696 16.918H24Zm-10.6082 0L0 20.459h3.7442l1.3693-3.5527h7.0052l1.3693 3.5528h3.7442L10.5363 3.5409Zm-.3712 10.2232 2.2914-5.9456 2.2914 5.9456Z',
  netflix: 'M5.398 0v.006c3.028 8.556 5.37 15.175 8.348 23.596 2.344.058 4.85.398 4.854.398-2.8-7.924-5.923-16.747-8.487-24zm8.489 0v9.63L18.6 22.951c-.043-7.86-.004-15.913.002-22.95zM5.398 1.05V24c1.873-.225 2.81-.312 4.715-.398v-9.22z',
  bilibili: 'M17.813 4.653h.854c1.51.054 2.769.578 3.773 1.574 1.004.995 1.524 2.249 1.56 3.76v7.36c-.036 1.51-.556 2.769-1.56 3.773s-2.262 1.524-3.773 1.56H5.333c-1.51-.036-2.769-.556-3.773-1.56S.036 18.858 0 17.347v-7.36c.036-1.511.556-2.765 1.56-3.76 1.004-.996 2.262-1.52 3.773-1.574h.774l-1.174-1.12a1.234 1.234 0 0 1-.373-.906c0-.356.124-.658.373-.907l.027-.027c.267-.249.573-.373.92-.373.347 0 .653.124.92.373L9.653 4.44c.071.071.134.142.187.213h4.267a.836.836 0 0 1 .16-.213l2.853-2.747c.267-.249.573-.373.92-.373.347 0 .662.151.929.4.267.249.391.551.391.907 0 .355-.124.657-.373.906zM5.333 7.24c-.746.018-1.373.276-1.88.773-.506.498-.769 1.13-.786 1.894v7.52c.017.764.28 1.395.786 1.893.507.498 1.134.756 1.88.773h13.334c.746-.017 1.373-.275 1.88-.773.506-.498.769-1.129.786-1.893v-7.52c-.017-.765-.28-1.396-.786-1.894-.507-.497-1.134-.755-1.88-.773zM8 11.107c.373 0 .684.124.933.373.25.249.383.569.4.96v1.173c-.017.391-.15.711-.4.96-.249.25-.56.374-.933.374s-.684-.125-.933-.374c-.25-.249-.383-.569-.4-.96V12.44c0-.373.129-.689.386-.947.258-.257.574-.386.947-.386zm8 0c.373 0 .684.124.933.373.25.249.383.569.4.96v1.173c-.017.391-.15.711-.4.96-.249.25-.56.374-.933.374s-.684-.125-.933-.374c-.25-.249-.383-.569-.4-.96V12.44c.017-.391.15-.711.4-.96.249-.249.56-.373.933-.373Z'
};


function renderReachability(targets) {
  const list = $('#reachList');
  if (!list) return;
  if (!targets || !targets.length) { list.innerHTML = '<p class="muted">暂无数据</p>'; return; }
  const dark = isDarkTheme();
  list.innerHTML = targets.map((t) => {
    const logoKey = t.id === 'chatgpt' ? 'openai' : (t.id === 'claude' ? 'anthropic' : t.id);
    const lat = (t.status === 'ok' || t.status === 'restricted')
      ? (t.latencyMs > 0 ? t.latencyMs + 'ms' : '—')
      : (REACH_LABEL[t.status] || '—');
    const def = REACH_LOGOS[logoKey];
    const svg = def
      ? '<svg class="logo" viewBox="' + def.vb + '" aria-hidden="true"' +
        (def.mono === true ? ' style="fill:' + (dark ? '#ffffff' : '#181717') + '"' : '') +
        '>' + def.inner + '</svg>'
      : '';
    return '<div class="reach-item ' + esc(t.status) + '" title="' + esc(t.name + ' · ' + (REACH_LABEL[t.status] || '') +
      (t.err ? ' · ' + t.err : '')) + '">' + svg + '<span class="lat">' + esc(lat) + '</span></div>';
  }).join('');
}

function startReachPolling() {
  clearInterval(state.reachTimer);
  state.reachTimer = setInterval(() => {
    if (!document.hidden && state.view === 'overview') loadReachability(false);
  }, 60000);
}

async function loadReachability(force) {
  try {
    const r = await api('/api/reachability' + (force ? '?refresh=1' : ''));
    state.reachTargets = r.targets || [];
    renderReachability(state.reachTargets);
  } catch (e) {
    const list = $('#reachList');
    if (list) list.innerHTML = '<p class="muted">—</p>';
  }
}

$('#btnReachRefresh').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  btn.disabled = true;
  await loadReachability(true);
  if (btn) btn.disabled = false;
});

/* ---------------- 出站详情 ---------------- */

const QUALITY_TEXT = { good: '良好', normal: '一般', risky: '风险', unknown: '未知' };
function qualityText(q) {
  if (!q) return '—';
  const lv = QUALITY_TEXT[q.level] || q.level || '未知';
  const type = q.type ? ({ residential: '住宅', mobile: '移动', business: '商业', hosting: '机房' }[q.type] || q.type) : '';
  return [type, lv].filter(Boolean).join(' · ');
}

function kvRow(label, value) {
  return '<div class="kv-row"><span>' + esc(label) + '</span><b title="' + esc(String(value)) + '">' +
    esc(String(value == null || value === '' ? '—' : value)) + '</b></div>';
}


;

$('#btnStorageClean').addEventListener('click', async () => {
  const btn = $('#btnStorageClean');
  btn.disabled = true; btn.textContent = '清理中…';
  try {
    const r = await api('/api/storage/clean', { method: 'POST', body: {} });
    const n = (r.steps || []).length;
    toast(n ? '已清理 ' + n + ' 项，释放 ' + fmtBytes(r.freed || 0) : '没有可清理的内容', 'ok');
    await loadStorage();
  } catch (e) {
    toast(e.message, 'err');
  } finally {
    btn.disabled = false; btn.textContent = '立即清理';
  }
});

/* ---------------- 国内 IP 规则 ---------------- */

function syncRuleCustomVisibility() {
  const preset = $('#s-ruleSourcePreset').value;
  $('#ruleCustomWrap').classList.toggle('hidden', preset !== 'custom');
}

$('#s-ruleSourcePreset').addEventListener('change', syncRuleCustomVisibility);

async function loadRuleSetInfo() {
  try {
    const r = await api('/api/rules/set');
    const m = r.meta || {};
    const parts = [];
    if (m.count) parts.push(m.count + ' 条');
    if (m.bytes) parts.push(fmtBytes(m.bytes));
    if (m.updatedAt) parts.push('更新于 ' + fmtTime(m.updatedAt));
    if (m.source) parts.push('来源 ' + (m.source.length > 42 ? m.source.slice(0, 42) + '…' : m.source));
    $('#ruleMeta').textContent = parts.length ? parts.join(' · ') : '尚未下载';
    if (r.preset && $('#s-ruleSourcePreset')) {
      $('#s-ruleSourcePreset').value = r.preset;
      syncRuleCustomVisibility();
    }
  } catch (e) { /* 忽略 */ }
}

$('#btnRuleUpdate').addEventListener('click', async () => {
  const btn = $('#btnRuleUpdate');
  btn.disabled = true; btn.textContent = '更新中…';
  progressOpen('正在更新国内 IP 规则…');
  try {
    const preset = $('#s-ruleSourcePreset').value;
    const payload = preset === 'custom' ? { url: $('#s-ruleSourceURL').value.trim() } : { preset };
    const r = await api('/api/rules/update', { method: 'POST', body: payload });
    toast('规则已更新：' + (r.meta && r.meta.count ? r.meta.count + ' 条' : '完成'), 'ok');
    setTimeout(progressClose, 1200);
    await loadRuleSetInfo();
    await loadStorage();
    await loadNetInfo(false);
    await loadReachability(false);
  } catch (e) {
    progressClose();
    toast(e.message, 'err');
  } finally {
    btn.disabled = false; btn.textContent = '立即更新';
  }
});

/* ---------------- 任务进度弹窗 ---------------- */

function progressOpen(title) {
  state.progressOpen = true;
  $('#progTitle').textContent = title;
  $('#progLog').innerHTML = '';
  $('#progModal').classList.remove('hidden');
  $('#progClose').classList.add('hidden');
  const t0 = Date.now();
  $('#progElapsed').textContent = '0.0s';
  clearInterval(state.progressTimer);
  state.progressTimer = setInterval(() => {
    $('#progElapsed').textContent = ((Date.now() - t0) / 1000).toFixed(1) + 's';
  }, 100);
}

function appendProgLine(box, ev) {
  if (!box) return;
  const el = document.createElement('div');
  let cls = 'run';
  if (ev.err) cls = 'err';
  else if (ev.done) cls = 'ok';
  el.className = 'pl ' + cls;
  const t = new Date((ev.at || Math.floor(Date.now() / 1000)) * 1000);
  const p2 = (x) => String(x).padStart(2, '0');
  el.innerHTML = '<span class="t">' + p2(t.getHours()) + ':' + p2(t.getMinutes()) + ':' + p2(t.getSeconds()) +
    '</span><span class="m"></span>';
  el.querySelector('.m').textContent = ev.msg;
  box.appendChild(el);
  box.scrollTop = box.scrollHeight;
}

function progressLine(ev) {
  // 在订阅弹窗里操作时，日志直接展开在弹窗下方
  if (state.inlineProgress && (ev.op === 'add' || ev.op === 'fetch')) {
    appendProgLine($('#subProgLog'), ev);
    return;
  }
  if (!state.progressOpen) return;
  appendProgLine($('#progLog'), ev);
  if (ev.err || ev.done) {
    clearInterval(state.progressTimer);
    $('#progClose').classList.remove('hidden');
    state.progressOpen = false;
  }
}

function progressClose() {
  $('#progModal').classList.add('hidden');
  state.progressOpen = false;
  clearInterval(state.progressTimer);
}

function startProgressStream() {
  if (state.progressStream) return;
  const es = new EventSource('/api/progress');
  state.progressStream = es;
  es.onmessage = (e) => {
    let ev;
    try { ev = JSON.parse(e.data); } catch (err) { return; }
    progressLine(ev);
  };
}

$('#rawClose').addEventListener('click', () => $('#rawModal').classList.add('hidden'));
$('#rawModal').addEventListener('click', (e) => { if (e.target.id === 'rawModal') $('#rawModal').classList.add('hidden'); });

$('#progClose').addEventListener('click', progressClose);
$('#progModal').addEventListener('click', (e) => {
  if (e.target.id === 'progModal' && !$('#progClose').classList.contains('hidden')) progressClose();
});

/* ---------------- 内核管理 ---------------- */

async function loadKernel() {
  try {
    const data = await api('/api/kernel');
    const k = data.kernel || {};
    state.kernel = k;
    $('#kVer').textContent = k.version || '—';
    $('#kSize').textContent = k.size ? fmtBytes(k.size) : '—';
    $('#kMtime').textContent = fmtTime(k.modTime);
    $('#kSha').textContent = k.sha256 ? k.sha256.slice(0, 16) + '…' : '—';
    $('#kSha').title = k.sha256 || '';
    $('#kBackupHint').textContent = k.backupVersion
      ? '可回滚到：' + String(k.backupVersion).slice(0, 46) + (k.backupTime ? '（' + fmtTime(k.backupTime) + '）' : '')
      : '暂无可回滚的旧版本';
    $('#btnRollbackKernel').disabled = !k.backupVersion;
  } catch (e) { /* ignore */ }
}

function setUploadProgress(pct, text) {
  $('#kProgress').classList.remove('hidden');
  $('#kBar').style.width = Math.max(0, Math.min(100, pct)).toFixed(1) + '%';
  $('#kProgressText').textContent = text;
}

function showKernelResult(msg, kind) {
  const el = $('#kResult');
  el.textContent = msg;
  el.className = 'notice ' + (kind || '');
  el.classList.remove('hidden');
}

function uploadKernel(file) {
  if (!file) return;
  if (state.uploading) { toast('正在上传，请稍候', 'err'); return; }
  if (file.size > 96 * 1024 * 1024) { toast('文件超过 96 MB', 'err'); return; }
  if (!window.confirm('用「' + file.name + '」替换当前 mihomo 内核？\n替换前会自动备份，若新内核启动失败会自动回滚。')) return;

  state.uploading = true;
  $('#kResult').classList.add('hidden');
  setUploadProgress(0, '准备上传：' + file.name + '（' + fmtBytes(file.size) + '）');

  const fd = new FormData();
  fd.append('file', file);
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/kernel/upload');
  xhr.upload.onprogress = (e) => {
    if (!e.lengthComputable) return;
    const pct = (e.loaded / e.total) * 100;
    setUploadProgress(pct * 0.85, '上传中 ' + pct.toFixed(0) + '%（' + fmtBytes(e.loaded) + ' / ' + fmtBytes(e.total) + '）');
  };
  xhr.onload = async () => {
    state.uploading = false;
    let data = null;
    try { data = JSON.parse(xhr.responseText); } catch (e) { /* ignore */ }
    if (xhr.status === 401) { showLogin(); return; }
    if (!data) {
      setUploadProgress(100, '完成');
      showKernelResult('上传失败：响应解析错误（HTTP ' + xhr.status + '）', 'err');
      return;
    }
    if (xhr.status >= 200 && xhr.status < 300 && data.ok) {
      setUploadProgress(100, '校验通过，已替换');
      showKernelResult((data.message || '内核已更新') + (data.version ? '：' + data.version : ''), 'ok');
      toast('内核已更新', 'ok');
      await loadKernel();
      await loadStatus();
      setTimeout(() => $('#kProgress').classList.add('hidden'), 1500);
    } else {
      setUploadProgress(0, '未通过校验');
      showKernelResult(data.error || ('上传失败（HTTP ' + xhr.status + '）'), 'err');
      $('#kProgressText').textContent = '没有写入磁盘，当前内核未改变';
    }
  };
  xhr.onerror = () => {
    state.uploading = false;
    showKernelResult('上传中断（网络错误）', 'err');
    $('#kProgressText').textContent = '当前内核未改变';
  };
  xhr.send(fd);
}

$('#kDrop').addEventListener('click', () => $('#kFile').click());
$('#kFile').addEventListener('change', (e) => {
  const f = e.target.files && e.target.files[0];
  e.target.value = '';
  uploadKernel(f);
});

['dragenter', 'dragover'].forEach((evt) => {
  $('#kDrop').addEventListener(evt, (e) => { e.preventDefault(); $('#kDrop').classList.add('drag'); });
});
['dragleave', 'drop'].forEach((evt) => {
  $('#kDrop').addEventListener(evt, (e) => {
    e.preventDefault();
    $('#kDrop').classList.remove('drag');
    if (evt === 'drop') {
      const f = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
      uploadKernel(f);
    }
  });
});

$('#btnRollbackKernel').addEventListener('click', async () => {
  if (!window.confirm('回滚到上一个内核版本？')) return;
  const btn = $('#btnRollbackKernel');
  btn.disabled = true;
  try {
    const r = await api('/api/kernel/rollback', { method: 'POST' });
    showKernelResult(r.message || '已回滚', 'ok');
    toast('已回滚', 'ok');
    await loadKernel();
    await loadStatus();
  } catch (e) {
    showKernelResult(e.message, 'err');
    toast(e.message, 'err');
  } finally {
    btn.disabled = false;
  }
});

/* ---------------- 登录 ---------------- */

function showLogin() {
  $('#loginModal').classList.remove('hidden');
  setTimeout(() => $('#f-password').focus(), 60);
}

async function doLogin() {
  try {
    await api('/api/login', { method: 'POST', body: { password: $('#f-password').value } });
    $('#loginModal').classList.add('hidden');
    location.reload();
  } catch (e) { toast(e.message, 'err'); }
}

$('#loginBtn').addEventListener('click', doLogin);
$('#f-password').addEventListener('keydown', (e) => { if (e.key === 'Enter') doLogin(); });

/* ---------------- 轮询 ---------------- */

function startTrafficStream() {
  if (state.trafficStream) return;
  const es = new EventSource('/api/traffic/stream');
  state.trafficStream = es;
  es.onmessage = (ev) => {
    let d;
    try { d = JSON.parse(ev.data); } catch (e) { return; }
    pushTraffic(d.up, d.down, d.upTotal, d.downTotal);
  };
}

function poll() {
  if (document.hidden) return;   // 页面不可见时不发请求（切回页面由 visibilitychange 补一次）
  loadStatus();
  if (state.view === 'nodes') loadNodes();
  if (state.view === 'subs') loadSubs();
}

async function boot() {
  initTheme();
  try {
    const data = await api('/api/settings');
    state.settings = data.settings || {};
    fillSettings(state.settings);
  } catch (e) {
    if (e.message !== '未登录') toast(e.message, 'err');
  }
  await loadSubs();
  await loadStatus();
  await loadIntranet();
  startTrafficStream();
  startProgressStream();
  startReachPolling();
  startConnPoll();
  startSvcPoll();
  const initial = (location.hash || '').replace('#', '');
  setView(VIEWS[initial] ? initial : 'overview');
  setInterval(poll, 3000);
}

window.addEventListener('hashchange', () => {
  const v = (location.hash || '').replace('#', '');
  if (VIEWS[v] && v !== state.view) setView(v);
});

document.addEventListener('visibilitychange', () => {
  if (document.hidden) return;
  // 切回页面：立刻补一次当前页的实时数据，不用等下一次轮询
  loadStatus();
  if (state.view === 'overview') {
    loadNetInfo(true);
  }
  if (state.view === 'connections') loadConnections();
  if (state.view === 'settings') loadService();
});

boot();
