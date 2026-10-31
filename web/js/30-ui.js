/* 30-ui — toast, confirm, dialog, sheets, skeleton, empty, error bar */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  var ui = MM.ui = {};

  /* ---------- toast ---------- */
  ui.toast = function (msg, kind) {
    var host = MM.$('#toastHost');
    if (!host) return;
    while (host.children.length >= 3) host.removeChild(host.firstChild);
    var tip = document.createElement('div');
    tip.className = 'toast' + (kind === 'err' ? ' toast-err' : kind === 'ok' ? ' toast-ok' : '');
    tip.setAttribute('role', 'status');
    var icon = kind === 'err' ? 'warning' : kind === 'ok' ? 'check' : 'info';
    tip.innerHTML = '<span class="toast-ico">' + MM.icon(icon) + '</span><span class="toast-msg"></span>';
    tip.querySelector('.toast-msg').textContent = String(msg == null ? '' : msg);
    host.appendChild(tip);
    var life = kind === 'err' ? 5000 : 2400;
    var timer = setTimeout(function () {
      tip.classList.add('toast-out');
      setTimeout(function () { if (tip.parentNode) tip.parentNode.removeChild(tip); }, 220);
    }, life);
    tip.addEventListener('click', function () { clearTimeout(timer); if (tip.parentNode) tip.parentNode.removeChild(tip); });
  };

  /* ---------- dialog core ---------- */
  var openDialogs = [];

  function focusables(root) {
    return MM.$$('a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])', root)
      .filter(function (el) { return el.offsetParent !== null || el === document.activeElement; });
  }

  function trapFallback(dlg) {
    dlg.__trap = function (e) {
      if (e.key !== 'Tab') return;
      var f = focusables(dlg);
      if (!f.length) return;
      var first = f[0], last = f[f.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    };
    dlg.addEventListener('keydown', dlg.__trap);
  }

  ui.dialog = function (opts) {
    opts = opts || {};
    var native = typeof HTMLDialogElement !== 'undefined' && typeof document.createElement('dialog').showModal === 'function';
    var trigger = document.activeElement;
    var dlg = document.createElement('dialog');
    dlg.className = 'dlg' + (opts.width === 'wide' ? ' dlg-wide' : '') +
      (opts.width === 'form' ? ' dlg-form' : '') + (opts.width === 'sheet' ? ' dlg-sheet' : '') +
      (opts.className ? ' ' + opts.className : '');
    dlg.setAttribute('role', 'dialog');
    dlg.setAttribute('aria-modal', 'true');
    if (opts.title) dlg.setAttribute('aria-label', opts.title);
    var head = opts.title ? '<div class="dlg-head"><h2>' + MM.esc(opts.title) + '</h2></div>' : '';
    var body = opts.body == null ? '' : (typeof opts.body === 'string' ? opts.body : '');
    dlg.innerHTML = head + '<div class="dlg-body"></div>' + (opts.actions ? '<div class="dlg-actions"></div>' : '');
    if (typeof opts.body !== 'string' && opts.body) dlg.querySelector('.dlg-body').appendChild(opts.body);
    else dlg.querySelector('.dlg-body').innerHTML = body;

    var closed = false;
    var resolvePromise;
    var promise = new Promise(function (res) { resolvePromise = res; });

    function close(value) {
      if (closed) return;
      closed = true;
      if (opts.onClose) { try { opts.onClose(value); } catch (e) { /* ignore */ } }
      if (native) {
        try { dlg.close(); } catch (e) { /* ignore */ }
        setTimeout(function () { if (dlg.parentNode) dlg.parentNode.removeChild(dlg); }, 0);
      }
      else { dlg.classList.remove('open'); dlg.remove(); }
      var i = openDialogs.indexOf(dlg); if (i >= 0) openDialogs.splice(i, 1);
      if (trigger && trigger.focus) { try { trigger.focus(); } catch (e) { /* ignore */ } }
      resolvePromise(value);
    }

    var actions = opts.actions || [];
    var actionBox = dlg.querySelector('.dlg-actions');
    (actions || []).forEach(function (a) {
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'btn ' + (a.kind === 'filled' ? 'btn-filled' : a.kind === 'destructive' ? 'btn-text btn-destructive' : a.kind === 'text' ? 'btn-text' : 'btn-tonal');
      btn.textContent = a.label;
      if (a.id) btn.id = a.id;
      if (a.disabled) btn.disabled = true;
      btn.addEventListener('click', function () {
        if (a.onClick) { a.onClick(close); return; }
        close(a.value);
      });
      actionBox.appendChild(btn);
    });

    var dismissible = opts.dismissible !== false;
    dlg.addEventListener('cancel', function (e) {
      e.preventDefault();
      if (dismissible && !(opts.beforeClose && opts.beforeClose() === false)) close(false);
    });
    dlg.addEventListener('click', function (e) {
      if (e.target === dlg && dismissible && !(opts.beforeClose && opts.beforeClose() === false)) close(false);
    });

    MM.$('#dialogHost').appendChild(dlg);
    if (native) dlg.showModal();
    else { dlg.classList.add('fallback-open'); dlg.setAttribute('open', ''); trapFallback(dlg); }
    openDialogs.push(dlg);

    var focusTarget = opts.focus ? (typeof opts.focus === 'string' ? dlg.querySelector(opts.focus) : opts.focus) : null;
    if (!focusTarget) {
      var fs = focusables(dlg);
      focusTarget = fs[0] || dlg;
    }
    setTimeout(function () { try { focusTarget.focus(); } catch (e) { /* ignore */ } }, 30);

    if (opts.onMount) opts.onMount(dlg, close);
    return { el: dlg, close: close, promise: promise };
  };

  ui.sheet = function (opts) {
    opts = opts || {};
    opts.width = 'sheet';
    opts.className = 'dlg-bottom' + (opts.className ? ' ' + opts.className : '');
    return ui.dialog(opts);
  };

  /* ---------- confirm ---------- */
  ui.confirm = function (opts) {
    opts = opts || {};
    var title = opts.title || '确认操作？';
    var text = opts.body || '';
    var confirmText = opts.confirm || '确定';
    var cancelText = opts.cancel || '取消';
    var destructive = opts.destructive !== false;
    var result = false;
    var d = ui.dialog({
      title: title,
      body: '<p class="dlg-text">' + MM.esc(text) + '</p>',
      width: 'alert',
      className: 'dlg-alert',
      actions: [
        { label: cancelText, kind: 'tonal', value: false },
        {
          label: confirmText, kind: destructive ? 'destructive' : 'filled', value: true,
          onClick: function (close) { result = true; close(true); }
        }
      ],
      onClose: function () { d = null; }
    });
    // 破坏性操作默认聚焦「取消」
    setTimeout(function () {
      var btns = MM.$$('.dlg-actions .btn', d.el);
      if (btns.length) btns[0].focus();
    }, 40);
    return d.promise.then(function (v) { return v === true && result !== false; });
  };

  /* ---------- login ---------- */
  var loginOpen = false;
  ui.login = function () {
    if (loginOpen) return;
    loginOpen = true;
    var body = document.createElement('div');
    body.className = 'login-body';
    body.innerHTML =
      '<p class="dlg-text">面板设置了访问密码</p>' +
      '<label class="field-label" for="mm-login-pw">密码</label>' +
      '<input class="input" id="mm-login-pw" type="password" autocomplete="current-password" placeholder="输入面板密码">' +
      '<p class="field-error hidden" id="mm-login-err"></p>';
    var d = ui.dialog({
      title: '需要登录', body: body, width: 'alert', className: 'dlg-alert', dismissible: false,
      actions: [{ label: '登录', kind: 'filled', id: 'mm-login-submit', onClick: function () { /* submit bound in onMount */ } }],
      onClose: function () { loginOpen = false; },
      onMount: function (dlg) {
        var pw = dlg.querySelector('#mm-login-pw');
        var err = dlg.querySelector('#mm-login-err');
        var btn = dlg.querySelector('#mm-login-submit');
        setTimeout(function () { pw.focus(); }, 60);
        function submit() {
          var value = pw.value;
          if (!value) { err.textContent = '请输入密码'; err.classList.remove('hidden'); return; }
          btn.disabled = true; btn.textContent = '登录中…';
          fetch('/api/login', {
            method: 'POST', credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ password: value })
          }).then(function (res) {
            return res.json().catch(function () { return {}; }).then(function (data) {
              if (res.ok && data && data.ok !== false) { location.reload(); return; }
              throw new Error((data && (data.error || data.err)) || ('HTTP ' + res.status));
            });
          }).catch(function (e) {
            err.textContent = e.message || '登录失败'; err.classList.remove('hidden');
            btn.disabled = false; btn.textContent = '登录'; pw.focus();
          });
        }
        btn.addEventListener('click', submit);
        pw.addEventListener('keydown', function (e) { if (e.key === 'Enter') { e.preventDefault(); submit(); } });
      }
    });
    return d.promise;
  };

  /* ---------- option sheet ---------- */
  ui.options = function (opts) {
    opts = opts || {};
    var body = document.createElement('div');
    body.className = 'option-list';
    var picked = null;
    (opts.items || []).forEach(function (it) {
      var row = document.createElement('button');
      row.type = 'button';
      row.className = 'option-row' + (String(it.value) === String(opts.value) ? ' selected' : '');
      row.innerHTML = '<span class="option-main"><b></b>' + (it.note ? '<small></small>' : '') + '</span>' +
        '<span class="option-check">' + (String(it.value) === String(opts.value) ? MM.icon('check') : '') + '</span>';
      row.querySelector('b').textContent = it.label;
      if (it.note) row.querySelector('small').textContent = it.note;
      row.addEventListener('click', function () { picked = it.value; d.close(it.value); });
      body.appendChild(row);
    });
    var d = ui.dialog({ title: opts.title || '选择', body: body, width: 'sheet', className: 'dlg-options' });
    return d.promise.then(function (v) { return v === undefined ? picked : v; });
  };

  /* ---------- progress dialog ---------- */
  ui.progress = function (title) {
    var body = document.createElement('div');
    body.className = 'progress-body';
    body.innerHTML = '<div class="progress-meta"><span class="progress-title"></span><span class="progress-elapsed">0.0s</span></div>' +
      '<div class="progress-log" role="log"></div>';
    body.querySelector('.progress-title').textContent = title || '正在处理…';
    var t0 = Date.now();
    var timer = setInterval(function () {
      var el = body.querySelector('.progress-elapsed');
      if (el) el.textContent = ((Date.now() - t0) / 1000).toFixed(1) + 's';
    }, 100);
    var done = false;
    var d = ui.dialog({
      body: body, width: 'wide', className: 'dlg-progress', dismissible: false,
      actions: [{ label: '关闭', kind: 'text', id: 'mm-prog-close', value: true }],
      onMount: function (dlg) {
        var btn = dlg.querySelector('#mm-prog-close');
        btn.classList.add('hidden');
        dlg.__progClose = btn;
      },
      beforeClose: function () { return done; },
      onClose: function () { clearInterval(timer); }
    });
    return {
      el: d.el,
      add: function (ev) {
        var log = body.querySelector('.progress-log');
        if (!log) return;
        var line = document.createElement('div');
        var cls = ev && ev.err ? 'pl-err' : ev && ev.done ? 'pl-ok' : 'pl-run';
        line.className = 'prog-line ' + cls;
        line.innerHTML = '<span class="prog-time"></span><span class="prog-msg"></span>';
        line.querySelector('.prog-time').textContent = MM.fmt.clock((ev && ev.at) || Date.now() / 1000);
        line.querySelector('.prog-msg').textContent = String((ev && (ev.msg || ev.step)) || '');
        log.appendChild(line);
        while (log.children.length > 200) log.removeChild(log.firstChild);
        log.scrollTop = log.scrollHeight;
      },
      finish: function () {
        done = true;
        clearInterval(timer);
        if (d.el && d.el.__progClose) d.el.__progClose.classList.remove('hidden');
      },
      close: function () { done = true; d.close(true); }
    };
  };

  /* ---------- skeleton / empty / error ---------- */
  ui.skeleton = function (kind) {
    var wrap = document.createElement('div');
    wrap.className = 'skeleton-' + (kind || 'cards');
    var n = kind === 'list' ? 6 : 3;
    var html = '';
    for (var i = 0; i < n; i++) {
      html += '<div class="skeleton-card"><div class="sk-line sk-title"></div><div class="sk-line"></div><div class="sk-line short"></div></div>';
    }
    wrap.innerHTML = html;
    return wrap;
  };

  ui.empty = function (opts) {
    opts = opts || {};
    var wrap = document.createElement('div');
    wrap.className = 'empty-state' + (opts.kind ? ' empty-' + opts.kind : '');
    wrap.innerHTML = '<div class="empty-art">' + MM.icon(opts.icon || 'info', 'empty-icon') + '</div>' +
      '<p class="empty-title"></p>' + (opts.text ? '<p class="empty-text"></p>' : '') +
      (opts.cta ? '<button class="btn btn-filled empty-cta" type="button"></button>' : '');
    wrap.querySelector('.empty-title').textContent = opts.title || '暂无数据';
    if (opts.text) wrap.querySelector('.empty-text').textContent = opts.text;
    if (opts.cta) {
      var btn = wrap.querySelector('.empty-cta');
      btn.textContent = opts.cta.label;
      btn.addEventListener('click', opts.cta.onClick);
    }
    return wrap;
  };

  ui.errorBar = function (msg, onRetry) {
    var bar = document.createElement('div');
    bar.className = 'error-bar';
    bar.setAttribute('role', 'alert');
    bar.innerHTML = '<span class="err-ico">' + MM.icon('warning') + '</span><span class="err-msg"></span>' +
      (onRetry ? '<button class="btn btn-text btn-small err-retry" type="button">重试</button>' : '');
    bar.querySelector('.err-msg').textContent = String(msg || '请求失败');
    if (onRetry) {
      var btn = bar.querySelector('.err-retry');
      btn.addEventListener('click', function () {
        btn.disabled = true; btn.textContent = '重试中…';
        Promise.resolve(onRetry()).finally(function () { btn.disabled = false; btn.textContent = '重试'; });
      });
    }
    return bar;
  };

  ui.chip = function (text, kind) {
    return '<span class="chip' + (kind ? ' chip-' + kind : '') + '">' + (kind ? '<i></i>' : '') + MM.esc(text) + '</span>';
  };

  // 给 .btn 加/去 loading（保留原 innerHTML 宽度，不双提交）
  ui.setBusy = function (btn, on) {
    if (!btn) return;
    if (on) {
      if (btn.__mmHTML === undefined) btn.__mmHTML = btn.innerHTML;
      btn.disabled = true;
      btn.classList.add('btn-loading');
    } else {
      btn.disabled = false;
      btn.classList.remove('btn-loading');
      if (btn.__mmHTML !== undefined) { btn.innerHTML = btn.__mmHTML; delete btn.__mmHTML; }
    }
  };

  ui.copyValue = function (text, label) {
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'icon-btn copy-btn';
    btn.setAttribute('aria-label', label || '复制');
    btn.title = label || '复制';
    btn.innerHTML = MM.icon('copy');
    btn.addEventListener('click', function () {
      MM.copy(text).then(function () { ui.toast('已复制', 'ok'); })
        .catch(function () { ui.toast('复制失败，请手动选择文本', 'err'); });
    });
    return btn;
  };

  // 关闭所有弹窗（路由切换时使用）
  ui.closeAll = function () {
    openDialogs.slice().forEach(function (dlg) {
      try { if (typeof dlg.close === 'function') dlg.close(); else dlg.remove(); } catch (e) { dlg.remove(); }
    });
    openDialogs = [];
    MM.$('#dialogHost').innerHTML = '';
  };
})();
