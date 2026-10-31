/* more sheet (mobile) — spec 4.10 */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  MM.views = MM.views || {};

  function row(icon, title, sub, path, subKey) {
    return '<button class="list-row" type="button" data-path="' + path + '" aria-label="' + MM.esc(title) + '">' +
      '<span class="icon-well">' + MM.icon(icon) + '</span>' +
      '<span class="row-main"><span class="row-title">' + MM.esc(title) + '</span>' +
      '<span class="row-sub"' + (subKey ? ' data-sub="' + subKey + '"' : '') + '>' + MM.esc(sub) + '</span></span>' +
      '<svg class="chev"><use href="#i-chevron"/></svg></button>';
  }

  function ruleSummary(st) {
    var n = (st.rules || []).length;
    return n ? n + ' 条' : '—';
  }

  function logSummary(st) {
    return (st.settings && st.settings.logLevel) || 'info';
  }

  function diagSummary(st) {
    var svc = st.service;
    if (!svc) return '—';
    return svc.core && svc.core.reachable ? '服务可达' : '服务不可达';
  }

  MM.views.more = {
    open: function () {
      if (MM.router.isMobile && !MM.router.isMobile()) return;
      var st = MM.store.state;
      var body = document.createElement('div');
      body.className = 'group-card more-list';
      body.innerHTML =
        row('rules', '规则', ruleSummary(st), '/rules', 'rules') +
        row('logs', '日志', logSummary(st), '/logs') +
        row('palette', '外观', MM.theme.summary(), '/settings/appearance') +
        row('info', '诊断', diagSummary(st), '/settings/diagnostics');

      var d = MM.ui.sheet({ title: '更多', body: body, width: 'sheet' });
      if (!(st.rules || []).length) {
        MM.api('/api/rules').then(function (data) {
          var rules = (data && data.rules) || [];
          MM.store.set({ rules: rules });
          var sub = body.querySelector('[data-sub="rules"]');
          if (sub) sub.textContent = rules.length ? rules.length + ' 条' : '—';
        }).catch(function () { /* 摘要保持 —，不打断 sheet */ });
      }
      body.querySelectorAll('[data-path]').forEach(function (b) {
        b.addEventListener('click', function () {
          var path = b.getAttribute('data-path');
          d.close();
          if (path) MM.router.go(path);
        });
      });
    }
  };
})();
