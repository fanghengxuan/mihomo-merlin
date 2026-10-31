/* overview view — spec 4.2 */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  MM.views = MM.views || {};

  var rootEl = null;
  var refs = null;
  var storeUn = null;
  var trafficUn = null;
  var active = false;
  var busy = false;
  var modeBusy = false;
  var ipqBusy = false;
  var reachBusy = false;
  var drawTimer = null;
  var lastDrawAt = 0;
  var modeButtons = [];
  var skeletonTimer = null;

  var MODE_LABEL = { rule: '规则', global: '全局', direct: '直连' };

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

  // 后台加载失败由视图的空/错误态承担，避免未处理的 rejection。
  function quiet(p) {
    if (p && typeof p.then === 'function') p.then(function () {}, function () {});
    return p;
  }

  function panelAddress(st) {
    var net = st.intranet || {};
    if (net.panelUrl) return String(net.panelUrl).replace(/^https?:\/\//, '');
    if (net.lanIp) return net.lanIp + (net.mixedPort ? ':' + net.mixedPort : '');
    return '';
  }

  function exitText(e) {
    if (!e || !e.ip) return '—';
    var s = e.ip;
    if (e.countryCode) s += ' ' + e.countryCode;
    if (e.kind) s += ' · ' + e.kind;
    return s;
  }

  // 统计 tile 宽度有限：超过 24 小时用「天+时」，两位小时省略分钟，避免移动端省略号。
  function uptimeText(sec) {
    var s = Math.max(0, Math.floor(Number(sec) || 0));
    var d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
    if (d > 0) return d + '天' + (h ? h + '时' : '');
    if (h >= 10) return h + '时';
    if (h > 0) return h + '时' + (m ? m + '分' : '');
    if (m > 0) return m + '分';
    return s + '秒';
  }

  function qualityText(q) {
    if (!q || !q.level || q.level === 'unknown') return { text: '未检测', cls: '' };
    var labels = { good: '良好', normal: '一般', risky: '有风险' };
    var checks = q.checks || {};
    var risk = !!(checks.proxy || checks.vpn || checks.tor);
    var text = (labels[q.level] || '未知');
    if (q.level === 'good' || q.level === 'risky' || q.level === 'normal') {
      text += ' · 代理/VPN ' + (checks.proxy || checks.vpn ? '是' : '否');
    }
    return { text: text, cls: q.level === 'risky' || risk ? 'warn' : '' };
  }

  function reachInfo(t) {
    var s = t && t.status;
    if (s === 'ok') return { text: (Number(t.latencyMs) || 0) + ' ms', cls: 'lat-ok' };
    if (s === 'restricted') return { text: '受限', cls: 'lat-warn' };
    if (s === 'timeout') return { text: '超时', cls: 'lat-none' };
    if (s === 'fail') return { text: '失败', cls: 'lat-err' };
    return { text: '未知', cls: 'lat-none' };
  }

  function hexToRgb(hex) {
    var h = String(hex || '').trim().replace('#', '');
    if (h.length === 3) h = h.charAt(0) + h.charAt(0) + h.charAt(1) + h.charAt(1) + h.charAt(2) + h.charAt(2);
    var n = parseInt(h, 16);
    if (!isFinite(n)) return { r: 0, g: 122, b: 255 };
    return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
  }

  function colors() {
    var cs = getComputedStyle(document.documentElement);
    var rgb = hexToRgb(cs.getPropertyValue('--accent'));
    return {
      accent: 'rgb(' + rgb.r + ',' + rgb.g + ',' + rgb.b + ')',
      second: 'rgba(' + rgb.r + ',' + rgb.g + ',' + rgb.b + ',.45)',
      fill: 'rgba(' + rgb.r + ',' + rgb.g + ',' + rgb.b + ',.12)',
      grid: cs.getPropertyValue('--outline-variant') || '#3A3E45'
    };
  }

  /* ---------- mount ---------- */
  function mount(root) {
    rootEl = root;
    root.innerHTML =
      '<div class="dash-grid ov-grid">' +
        '<section class="dash-card span2 ov-hero">' +
          '<div class="card-head"><h2>连接状态</h2><span class="spacer"></span><span data-ov="heroChip"></span></div>' +
          '<div class="hero-main">' +
            '<div class="hero-info">' +
              '<div class="hero-node" data-ov="heroNode">—</div>' +
              '<div class="hero-addr" data-ov="heroAddr">—</div>' +
              '<div class="hero-hint" data-ov="heroHint"></div>' +
            '</div>' +
            '<div class="hero-controls">' +
              '<button class="power" type="button" data-ov="power" aria-label="启动">' + MM.icon('power') + '<span data-ov="powerLabel">启动</span></button>' +
              '<button class="btn btn-text btn-small hidden" type="button" data-ov="restart">重启</button>' +
            '</div>' +
          '</div>' +
        '</section>' +
        '<section class="dash-card span2 ov-traffic">' +
          '<div class="card-head"><h2>实时流量</h2></div>' +
          '<div class="chart-wrap"><canvas data-ov="canvas" aria-label="实时流量图" role="img"></canvas><div class="chart-empty" data-ov="chartEmpty">等待流量</div></div>' +
          '<div class="traffic-row">' +
            '<div class="traffic-cell"><span class="lbl">下行</span><b data-ov="downSpeed">0 B/s</b></div>' +
            '<div class="traffic-cell"><span class="lbl">上行</span><b data-ov="upSpeed">0 B/s</b></div>' +
          '</div>' +
          '<div class="traffic-total" data-ov="total">累计 —</div>' +
        '</section>' +
        '<section class="card grid-span4 ov-mode">' +
          '<div class="card-head"><h2>出站模式</h2><span class="spacer"></span><span class="muted" data-ov="modeState"></span></div>' +
          '<div class="ov-mode-inline">' +
            '<div class="segmented" data-ov="modeSeg" role="group" aria-label="出站模式">' +
              '<button type="button" data-mode="rule">规则</button>' +
              '<button type="button" data-mode="global">全局</button>' +
              '<button type="button" data-mode="direct">直连</button>' +
            '</div>' +
            '<p class="mode-note" data-ov="modeNote"></p>' +
          '</div>' +
        '</section>' +
        '<div class="stat-grid ov-stats grid-span4">' +
          '<div class="stat-tile"><div class="stat-label">连接数</div><div class="stat-value" data-ov="connVal">0</div><div class="stat-sub" data-ov="connSub">活动连接</div></div>' +
          '<div class="stat-tile"><div class="stat-label">内存</div><div class="stat-value" data-ov="memVal">—</div><div class="stat-sub" data-ov="memSub">内核进程</div></div>' +
          '<div class="stat-tile"><div class="stat-label">节点数</div><div class="stat-value" data-ov="nodeVal">0</div><div class="stat-sub" data-ov="nodeSub">订阅节点</div></div>' +
          '<div class="stat-tile"><div class="stat-label">运行时长</div><div class="stat-value" data-ov="uptimeVal">—</div><div class="stat-sub" data-ov="uptimeSub">内核运行时长</div></div>' +
        '</div>' +
        '<section class="card grid-span2 ov-subs">' +
          '<div class="card-head"><h2>订阅</h2><span class="spacer"></span><button class="link-btn" type="button" data-ov="manage">管理 →</button></div>' +
          '<div class="sub-summary" data-ov="subList"></div>' +
        '</section>' +
        '<section class="card grid-span2 ov-net" data-ov="netCard" title="查看诊断">' +
          '<div class="card-head"><h2>网络身份</h2><span class="spacer"></span><button class="link-btn" type="button" data-ov="ipqCheck">检测质量</button></div>' +
          '<div class="kv-list">' +
            '<div class="kv-row"><span class="k">局域网</span><span class="v mono" data-ov="vLan">—</span></div>' +
            '<div class="kv-row"><span class="k">出口</span><span class="v mono" data-ov="vProxy">—</span></div>' +
            '<div class="kv-row"><span class="k">直连</span><span class="v mono" data-ov="vDirect">—</span></div>' +
            '<div class="kv-row"><span class="k">质量</span><span class="v" data-ov="vQuality">未检测</span></div>' +
          '</div>' +
        '</section>' +
        '<section class="card grid-span4 ov-reach">' +
          '<div class="card-head"><h2>服务连通性</h2><span class="spacer"></span><button class="icon-btn" type="button" data-ov="reachRefresh" aria-label="刷新连通性" title="刷新连通性">' + MM.icon('refresh') + '</button></div>' +
          '<div class="reach-grid" data-ov="reachList"></div>' +
          '<div class="reach-updated" data-ov="reachUpdated"></div>' +
        '</section>' +
      '</div>' +
      '<div class="hidden" data-ov="skeleton"></div>';

    refs = {
      grid: root.querySelector('.dash-grid'),
      skeleton: root.querySelector('[data-ov="skeleton"]'),
      heroChip: root.querySelector('[data-ov="heroChip"]'),
      heroNode: root.querySelector('[data-ov="heroNode"]'),
      heroAddr: root.querySelector('[data-ov="heroAddr"]'),
      heroHint: root.querySelector('[data-ov="heroHint"]'),
      power: root.querySelector('[data-ov="power"]'),
      powerLabel: root.querySelector('[data-ov="powerLabel"]'),
      restart: root.querySelector('[data-ov="restart"]'),
      canvas: root.querySelector('[data-ov="canvas"]'),
      chartEmpty: root.querySelector('[data-ov="chartEmpty"]'),
      downSpeed: root.querySelector('[data-ov="downSpeed"]'),
      upSpeed: root.querySelector('[data-ov="upSpeed"]'),
      total: root.querySelector('[data-ov="total"]'),
      modeSeg: root.querySelector('[data-ov="modeSeg"]'),
      modeState: root.querySelector('[data-ov="modeState"]'),
      modeNote: root.querySelector('[data-ov="modeNote"]'),
      connVal: root.querySelector('[data-ov="connVal"]'),
      connSub: root.querySelector('[data-ov="connSub"]'),
      memVal: root.querySelector('[data-ov="memVal"]'),
      memSub: root.querySelector('[data-ov="memSub"]'),
      nodeVal: root.querySelector('[data-ov="nodeVal"]'),
      nodeSub: root.querySelector('[data-ov="nodeSub"]'),
      uptimeVal: root.querySelector('[data-ov="uptimeVal"]'),
      uptimeSub: root.querySelector('[data-ov="uptimeSub"]'),
      subList: root.querySelector('[data-ov="subList"]'),
      manage: root.querySelector('[data-ov="manage"]'),
      netCard: root.querySelector('[data-ov="netCard"]'),
      ipqCheck: root.querySelector('[data-ov="ipqCheck"]'),
      vLan: root.querySelector('[data-ov="vLan"]'),
      vProxy: root.querySelector('[data-ov="vProxy"]'),
      vDirect: root.querySelector('[data-ov="vDirect"]'),
      vQuality: root.querySelector('[data-ov="vQuality"]'),
      reachList: root.querySelector('[data-ov="reachList"]'),
      reachRefresh: root.querySelector('[data-ov="reachRefresh"]'),
      reachUpdated: root.querySelector('[data-ov="reachUpdated"]')
    };
    modeButtons = MM.$$('button[data-mode]', refs.modeSeg);

    refs.power.addEventListener('click', onPower);
    refs.restart.addEventListener('click', function () { engineOp('/api/engine/restart', '已重启'); });
    refs.manage.addEventListener('click', function () { MM.router.go('/subs'); });
    refs.ipqCheck.addEventListener('click', checkQuality);
    refs.reachRefresh.addEventListener('click', function () { refreshReach(); });
    refs.netCard.addEventListener('click', function (e) {
      if (e.target.closest('button')) return;
      MM.router.go('/settings/diagnostics');
    });
    modeButtons.forEach(function (b) {
      b.addEventListener('click', function () { setMode(b.getAttribute('data-mode')); });
    });

    if (window.addEventListener) window.addEventListener('resize', function () { scheduleDraw(true); });
    if (typeof ResizeObserver !== 'undefined' && refs.canvas.parentNode) {
      try { new ResizeObserver(function () { scheduleDraw(true); }).observe(refs.canvas.parentNode); } catch (e) { /* ignore */ }
    }
  }

  /* ---------- engine ---------- */
  function reloadStatusService() {
    return Promise.all([
      MM.api('/api/status').then(function (d) { MM.store.set({ status: d }); }, function () { /* ignore */ }),
      MM.api('/api/service').then(function (d) { MM.store.set({ service: d }); }, function () { /* ignore */ })
    ]);
  }

  function engineOp(path, okMsg) {
    if (busy) return;
    busy = true;
    render();
    MM.api(path, { method: 'POST' }).then(function (d) {
      MM.ui.toast((d && d.message) || okMsg, 'ok');
      return reloadStatusService();
    }).then(function () {
      MM.sched.markDue('netinfo');
      quiet(MM.data.loadNetInfo(true));
    }).catch(function (e) {
      MM.ui.toast(e.message || '操作失败', 'err');
    }).finally(function () {
      busy = false;
      render();
    });
  }

  function onPower() {
    if (busy) return;
    if (MM.store.running()) {
      MM.ui.confirm({
        title: '停止内核？',
        body: '停止后路由器流量不再经代理转发，可随时在概览重新启动。',
        confirm: '停止',
        destructive: true
      }).then(function (ok) {
        if (ok) engineOp('/api/engine/stop', '已停止');
      });
    } else {
      engineOp('/api/engine/start', '已启动');
    }
  }

  function setMode(mode) {
    if (!mode || modeBusy) return;
    var cur = (MM.store.state.settings && MM.store.state.settings.mode) || 'rule';
    if (mode === cur) { render(); return; }
    modeBusy = true;
    render();
    MM.api('/api/settings', { method: 'PUT', body: { mode: mode } }).then(function (d) {
      var merged = Object.assign({}, MM.store.state.settings || {}, (d && d.settings) || {});
      merged.mode = mode;
      MM.store.set({ settings: merged });
      return reloadStatusService();
    }).then(function () {
      MM.ui.toast('出站模式已切换', 'ok');
    }).catch(function (e) {
      MM.ui.toast(e.message || '切换模式失败', 'err');
    }).finally(function () {
      modeBusy = false;
      render();
    });
  }

  function checkQuality() {
    if (ipqBusy) return;
    ipqBusy = true;
    render();
    MM.sched.once('ipq', function () {
      return MM.api('/api/ipquality?refresh=1');
    }).then(function (d) {
      if (d && d.quality) MM.store.set({ ipq: d.quality });
    }).catch(function () {
      /* 静默：概览不因 IP 质量失败弹错误 */
    }).finally(function () {
      ipqBusy = false;
      render();
    });
  }

  function refreshReach() {
    if (reachBusy) return;
    reachBusy = true;
    render();
    MM.sched.markDue('reach');
    MM.data.loadReach(true).catch(function () { /* 错误由空态/旧数据承担 */ }).finally(function () {
      reachBusy = false;
      render();
    });
  }

  function refreshAll() {
    MM.sched.markDue('status');
    MM.sched.markDue('netinfo');
    MM.sched.markDue('reach');
    return Promise.allSettled([
      MM.data.loadStatus(),
      MM.data.loadSubs(),
      MM.data.loadNetInfo(true),
      MM.data.loadReach(true),
      MM.data.loadIntranet(),
      MM.data.loadService()
    ]).then(function () {
      MM.ui.toast('已刷新', 'ok');
    });
  }

  /* ---------- skeleton ---------- */
  function hideSkeleton() {
    if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
    if (!refs) return;
    refs.skeleton.classList.add('hidden');
    refs.skeleton.textContent = '';
    refs.grid.classList.remove('hidden');
  }

  function scheduleSkeleton() {
    if (!refs || skeletonTimer) return;
    var st = MM.store.state;
    if (st.status || st.booted) return;
    skeletonTimer = setTimeout(function () {
      skeletonTimer = null;
      if (!active) return;
      var cur = MM.store.state;
      if (cur.status || cur.booted) return;
      refs.skeleton.textContent = '';
      refs.skeleton.appendChild(MM.ui.skeleton('cards'));
      refs.skeleton.classList.remove('hidden');
      refs.grid.classList.add('hidden');
    }, 400);
  }

  /* ---------- render ---------- */
  function render() {
    if (!refs) return;
    var st = MM.store.state;
    if (st.status || st.booted) hideSkeleton();
    else scheduleSkeleton();
    var status = st.status || {};
    var eng = status.engine || {};
    var net = status.net || {};
    var subsMeta = status.subs || {};
    var running = !!eng.running;
    var auto = eng.autoRecover || {};
    var now = Math.floor(Date.now() / 1000);
    var recovering = !running && Number(auto.attempts) > 0 && Number(auto.nextAt) > now;

    refs.heroChip.innerHTML = MM.ui.chip(
      recovering ? '恢复中' : (running ? '运行中' : '未运行'),
      running ? 'ok' : (recovering ? 'warn' : 'neutral')
    );

    var info = st.netinfo || {};
    if (running) {
      var node = info.node || '尚未选择节点';
      var d = Number(info.nodeDelay) || 0;
      setText(refs.heroNode, node + (d > 0 ? ' · ' + d + ' ms' : ' · 未测'));
    } else {
      setText(refs.heroNode, '内核未运行');
    }
    setText(refs.heroAddr, panelAddress(st) || '—');

    var hint = '';
    var hintWarn = false;
    if (!running && recovering) {
      hint = '自动恢复第 ' + auto.attempts + ' 次尝试' + (auto.nextAt ? '，下次 ' + MM.fmt.clock(auto.nextAt) : '');
      hintWarn = true;
    } else if (!running) {
      hint = '内核未启动，点右侧开始代理';
    }
    setText(refs.heroHint, hint);
    refs.heroHint.classList.toggle('warn', hintWarn);
    refs.heroHint.classList.toggle('hidden', !hint);

    refs.power.classList.toggle('on', !running);
    refs.power.classList.toggle('loading', busy);
    refs.power.disabled = busy;
    setText(refs.powerLabel, busy ? '处理中…' : (running ? '停止' : '启动'));
    refs.power.setAttribute('aria-label', busy ? '处理中' : (running ? '停止内核' : '启动内核'));
    refs.restart.classList.toggle('hidden', !running || busy);
    refs.restart.disabled = busy;

    /* mode */
    var settingsMode = (st.settings && st.settings.mode) || 'rule';
    var actual = '';
    if (running) actual = (st.service && st.service.modeActual) || eng.modeActual || '';
    var selected = (running && actual) ? actual : settingsMode;
    modeButtons.forEach(function (b) {
      var on = b.getAttribute('data-mode') === selected;
      b.classList.toggle('active', on);
      b.setAttribute('aria-pressed', on ? 'true' : 'false');
      b.disabled = modeBusy;
    });
    var modeNote = '';
    var modeWarn = false;
    if (!running) {
      modeNote = '内核未运行，模式将在启动后生效';
    } else if (actual && settingsMode !== actual) {
      modeNote = '设置里是' + (MODE_LABEL[settingsMode] || settingsMode) + '，尚未生效（内核：' + (MODE_LABEL[actual] || actual) + '）';
      modeWarn = true;
    } else if (actual) {
      modeNote = '设置与内核实际模式一致';
    } else {
      modeNote = '模式设置已提交';
    }
    setText(refs.modeNote, modeNote);
    refs.modeNote.classList.toggle('warn', modeWarn);
    setText(refs.modeState, MODE_LABEL[selected] || selected);

    /* stats */
    setText(refs.connVal, String(Number(net.connections) || 0));
    setText(refs.connSub, '活动连接');
    setText(refs.memVal, running && eng.memory != null ? Number(eng.memory).toFixed(1) + ' MB' : '—');
    setText(refs.memSub, running ? '内核进程' : '内核未运行');
    setText(refs.nodeVal, String(Number(subsMeta.nodeCount) || 0));
    setText(refs.nodeSub, '订阅节点');
    setText(refs.uptimeVal, running && eng.uptime != null ? uptimeText(eng.uptime) : '—');
    setText(refs.uptimeSub, running ? '内核运行时长' : '内核未运行');

    renderSubs(st);
    renderNet(st, running);
    renderReach(st, running);
    updateTrafficTexts();
    drawChart();
  }

  function renderSubs(st) {
    var list = (st.subs || []).slice(0, 3);
    var host = refs.subList;
    if (!list.length) {
      clearKeyed(host);
      host.innerHTML = '<p class="ov-empty">还没有订阅，点「管理」添加</p>';
      return;
    }
    MM.keyedList(host, list, function (s) { return s.id; }, createSubRow, updateSubRow, { tag: 'ov-subs' });
  }

  function createSubRow() {
    var row = document.createElement('div');
    row.className = 'sum-row';
    row.innerHTML = '<span class="sum-name"></span><span class="sum-meta"></span>' +
      '<div class="bar"><span></span></div><span class="sum-meta pct"></span>' +
      '<span class="sum-detail"></span>';
    row.__r = {
      name: row.querySelector('.sum-name'),
      meta: row.querySelector('.sum-meta'),
      bar: row.querySelector('.bar > span'),
      pct: row.querySelector('.pct'),
      detail: row.querySelector('.sum-detail')
    };
    return row;
  }

  function updateSubRow(row, sub) {
    var r = row.__r;
    var t = sub.traffic || {};
    var used = (Number(t.upload) || 0) + (Number(t.download) || 0);
    var total = Number(t.total) || 0;
    var pct = total > 0 ? Math.min(100, used / total * 100) : 0;
    var over = pct > 85;
    setText(r.name, sub.name || '未命名订阅');
    setText(r.meta, (Number(sub.nodeCount) || 0) + ' 节点');
    r.bar.style.width = pct.toFixed(1) + '%';
    r.bar.className = over ? 'warn' : '';
    setText(r.pct, total > 0 ? Math.round(pct) + '%' : '—');
    r.detail.textContent = '';
    var usedSpan = document.createElement('span');
    usedSpan.textContent = '已用 ' + MM.fmt.bytes(used) + (total > 0 ? ' / ' + MM.fmt.bytes(total) : '');
    r.detail.appendChild(usedSpan);
    var expire = Number(t.expire) || 0;
    if (expire > 0) {
      var exp = document.createElement('span');
      var expired = expire * 1000 < Date.now();
      exp.textContent = '到期 ' + MM.fmt.date(expire) + (expired ? '（已过期）' : '');
      if (expired) exp.className = 'err';
      r.detail.appendChild(exp);
    }
  }

  function renderNet(st, running) {
    var intranet = st.intranet || {};
    var lan = panelAddress(st) || '—';
    setText(refs.vLan, lan);
    refs.vLan.title = lan;

    var info = st.netinfo || {};
    var proxy = running ? exitText(info.proxy) : '—';
    var direct = running ? exitText(info.direct) : '—';
    setText(refs.vProxy, proxy);
    refs.vProxy.title = proxy;
    setText(refs.vDirect, direct);
    refs.vDirect.title = direct;

    var q = qualityText(running ? st.ipq : null);
    setText(refs.vQuality, q.text);
    refs.vQuality.className = 'v' + (q.cls ? ' ' + q.cls : '');
    refs.vQuality.title = q.text;
    refs.ipqCheck.disabled = ipqBusy;
    setText(refs.ipqCheck, ipqBusy ? '检测中…' : '检测质量');
  }

  function createReachItem() {
    var el = document.createElement('span');
    el.className = 'reach-item';
    el.innerHTML = '<span class="reach-dot"></span><span class="reach-name"></span><span class="lat"></span>';
    el.__r = { name: el.querySelector('.reach-name'), lat: el.querySelector('.lat') };
    return el;
  }

  function updateReachItem(el, t) {
    var info = reachInfo(t);
    el.className = 'reach-item ' + (t.status || 'unknown');
    setText(el.__r.name, t.name || t.id || '目标');
    el.__r.lat.className = 'lat ' + info.cls;
    setText(el.__r.lat, info.text);
    el.title = (t.url || '') + (t.err ? '\n' + t.err : '');
  }

  function renderReach(st, running) {
    var host = refs.reachList;
    if (!running) {
      clearKeyed(host);
      host.innerHTML = '<span class="ov-empty">内核未运行</span>';
      setText(refs.reachUpdated, '');
      refs.reachRefresh.disabled = reachBusy;
      return;
    }
    var list = (st.reach && st.reach.targets) || [];
    if (!list.length) {
      clearKeyed(host);
      host.innerHTML = '<span class="ov-empty">暂无连通性数据</span>';
    } else {
      MM.keyedList(host, list, function (t) { return t.id; }, createReachItem, updateReachItem, { tag: 'ov-reach' });
    }
    var at = st.reach && st.reach.updatedAt;
    setText(refs.reachUpdated, at ? '更新于 ' + MM.fmt.time(at) : '');
    refs.reachRefresh.disabled = reachBusy;
  }

  /* ---------- traffic ---------- */
  function updateTrafficTexts() {
    if (!refs) return;
    var running = MM.store.running();
    var last = MM.traffic.last || {};
    var net = (MM.store.state.status && MM.store.state.status.net) || {};
    var up = Number(last.up) || Number(net.uploadSpeed) || 0;
    var down = Number(last.down) || Number(net.downloadSpeed) || 0;
    if (!running) { up = 0; down = 0; }
    setText(refs.downSpeed, MM.fmt.speed(down));
    setText(refs.upSpeed, MM.fmt.speed(up));
    var totalUp = Number(last.upTotal) || Number(net.upTotal) || 0;
    var totalDown = Number(last.downTotal) || Number(net.downTotal) || 0;
    setText(refs.total, totalUp + totalDown > 0 ? '累计 ' + MM.fmt.bytes(totalUp + totalDown) : '累计 —');
  }

  function drawChart() {
    var cv = refs && refs.canvas;
    if (!cv) return;
    var w = cv.clientWidth || (cv.parentNode && cv.parentNode.clientWidth) || 0;
    var h = cv.clientHeight || 96;
    if (!w || !h) return;
    var dpr = Math.min(window.devicePixelRatio || 1, 2);
    var pw = Math.round(w * dpr), ph = Math.round(h * dpr);
    if (cv.width !== pw || cv.height !== ph) { cv.width = pw; cv.height = ph; }
    var ctx = cv.getContext('2d');
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);

    var col = colors();
    ctx.strokeStyle = col.grid;
    ctx.lineWidth = 1;
    for (var i = 0; i < 2; i++) {
      var y = Math.round(h * 0.33 + i * h * 0.34) + 0.5;
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(w, y);
      ctx.stroke();
    }

    var running = MM.store.running();
    var hist = MM.traffic.history.slice(-MM.traffic.max());
    var empty = !running || hist.length < 2;
    refs.chartEmpty.classList.toggle('hidden', !empty);
    setText(refs.chartEmpty, running ? '等待流量' : '内核未运行，等待流量');

    var max = 0;
    hist.forEach(function (p) {
      if (p.down > max) max = p.down;
      if (p.up > max) max = p.up;
    });
    var pad = 4;

    function series(key, color, fill) {
      ctx.beginPath();
      if (empty) {
        ctx.moveTo(0, h - pad);
        ctx.lineTo(w, h - pad);
      } else {
        hist.forEach(function (p, idx) {
          var x = hist.length === 1 ? 0 : idx / (hist.length - 1) * w;
          var y = h - pad - (max > 0 ? (p[key] / max) * (h - pad * 2) : 0);
          if (idx === 0) ctx.moveTo(x, y);
          else ctx.lineTo(x, y);
        });
      }
      ctx.strokeStyle = color;
      ctx.lineWidth = 2;
      ctx.lineJoin = 'round';
      ctx.stroke();
      if (fill && !empty && !MM.theme.isLowEnd()) {
        ctx.lineTo(w, h - pad);
        ctx.lineTo(0, h - pad);
        ctx.closePath();
        ctx.fillStyle = fill;
        ctx.fill();
      }
    }

    series('down', col.accent, col.fill);
    series('up', col.second, null);
  }

  function scheduleDraw(force) {
    if (!active) return;
    if (force) {
      lastDrawAt = Date.now();
      drawChart();
      return;
    }
    var interval = MM.theme.isLowEnd() ? 5000 : 2000;
    var now = Date.now();
    if (now - lastDrawAt < interval || drawTimer) return;
    drawTimer = setTimeout(function () {
      drawTimer = null;
      if (!active) return;
      lastDrawAt = Date.now();
      drawChart();
    }, 0);
  }

  function onTraffic() {
    if (!active) return;
    updateTrafficTexts();
    scheduleDraw(false);
  }

  /* ---------- lifecycle ---------- */
  function subscribe() {
    active = true;
    if (!storeUn) {
      storeUn = MM.store.sub(function () { if (active) render(); });
    }
    if (!trafficUn) {
      trafficUn = MM.traffic.sub(onTraffic);
    }
  }

  function unsubscribe() {
    active = false;
    if (storeUn) { storeUn(); storeUn = null; }
    if (trafficUn) { trafficUn(); trafficUn = null; }
    if (drawTimer) { clearTimeout(drawTimer); drawTimer = null; }
  }

  function activate(ctx, sec) {
    if (!rootEl) rootEl = sec;
    if (!refs) return;
    subscribe();
    render();
    scheduleDraw(true);

    MM.sched.markDue('status');
    quiet(MM.data.loadStatus());
    if (!(MM.store.state.subs || []).length) quiet(MM.data.loadSubs());
    if (!MM.store.state.intranet) quiet(MM.data.loadIntranet());
    quiet(MM.data.loadNetInfo(false));
    quiet(MM.data.loadReach(false));
    quiet(MM.data.loadService());
  }

  function deactivate() {
    unsubscribe();
    if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
  }

  MM.views.overview = {
    title: function () { return MM.router.TITLES.overview; },
    mount: mount,
    activate: activate,
    deactivate: deactivate,
    actions: function () {
      return [{
        id: 'ovRefresh',
        icon: 'refresh',
        label: '刷新',
        desktopOnly: true,
        onClick: refreshAll
      }];
    }
  };
})();
