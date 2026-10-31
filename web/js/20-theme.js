/* 20-theme — light/dark/system, six accents, pure black, low-end */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  var root = document.documentElement;
  var mqDark = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
  var mqMotion = window.matchMedia ? window.matchMedia('(prefers-reduced-motion: reduce)') : null;
  var listeners = [];
  var ACCENTS = ['blue', 'violet', 'teal', 'green', 'orange', 'rose'];
  var ACCENT_NAMES = { blue: '海湾蓝', violet: '暮光紫', teal: '青瓷', green: '苔绿', orange: '琥珀', rose: '玫瑰' };

  function ls(k, d) { try { var v = localStorage.getItem(k); return v == null ? d : v; } catch (e) { return d; } }
  function save(k, v) { try { localStorage.setItem(k, String(v)); } catch (e) { /* private mode */ } }

  function prefTheme() { var v = ls('mm-theme', 'system'); return v === 'light' || v === 'dark' ? v : 'system'; }
  function prefAccent() { var v = ls('mm-accent', 'blue'); return ACCENTS.indexOf(v) >= 0 ? v : 'blue'; }
  function prefMono() { return ls('mm-mono', '0') === '1'; }
  function prefLow() { var v = ls('mm-lowend', 'auto'); return v === 'on' || v === 'off' ? v : 'auto'; }

  function systemDark() { return mqDark ? mqDark.matches : true; }
  function resolved() { var p = prefTheme(); return p === 'system' ? (systemDark() ? 'dark' : 'light') : p; }

  function detectLow() {
    var pref = prefLow();
    if (pref === 'on') return true;
    if (pref === 'off') return false;
    var cores = navigator.hardwareConcurrency || 8;
    var mem = navigator.deviceMemory;
    return cores <= 4 || (mem && mem <= 4) || (mqMotion && mqMotion.matches);
  }

  function metaColor(theme, mono) {
    if (theme === 'dark') return mono ? '#000000' : '#0F1013';
    return '#EDEEF1';
  }

  function apply() {
    var theme = resolved();
    var accent = prefAccent();
    var mono = prefMono();
    var low = detectLow();
    root.setAttribute('data-theme', theme);
    root.setAttribute('data-accent', accent);
    root.setAttribute('data-mono', mono ? '1' : '0');
    root.setAttribute('data-low-end', low ? '1' : '0');
    root.style.colorScheme = theme;
    var meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute('content', metaColor(theme, mono));
    listeners.slice().forEach(function (fn) { try { fn(theme, accent, mono, low); } catch (e) { /* ignore */ } });
    return { theme: theme, accent: accent, mono: mono, low: low };
  }

  function setTheme(v) { save('mm-theme', v === 'light' || v === 'dark' ? v : 'system'); apply(); }
  function setAccent(v) { save('mm-accent', ACCENTS.indexOf(v) >= 0 ? v : 'blue'); apply(); }
  function setMono(on) { save('mm-mono', on ? '1' : '0'); apply(); }
  function setLow(pref) { save('mm-lowend', pref === 'on' || pref === 'off' ? pref : 'auto'); apply(); }

  MM.theme = {
    ACCENTS: ACCENTS,
    ACCENT_NAMES: ACCENT_NAMES,
    init: apply,
    apply: apply,
    setTheme: setTheme,
    setAccent: setAccent,
    setMono: setMono,
    setLowEnd: setLow,
    prefTheme: prefTheme,
    prefAccent: prefAccent,
    prefMono: prefMono,
    prefLowEnd: prefLow,
    resolved: resolved,
    isDark: function () { return resolved() === 'dark'; },
    isMono: function () { return prefMono() && resolved() === 'dark'; },
    isLowEnd: function () { return root.getAttribute('data-low-end') === '1'; },
    onChange: function (fn) { listeners.push(fn); return function () { var i = listeners.indexOf(fn); if (i >= 0) listeners.splice(i, 1); }; },
    summary: function () {
      var t = prefTheme();
      var themeText = t === 'system' ? '跟随系统' : (t === 'dark' ? '深色' : '浅色');
      return themeText + ' · ' + ACCENT_NAMES[prefAccent()] + (prefMono() && resolved() === 'dark' ? ' · 纯黑' : '');
    }
  };

  function onSystem() {
    if (prefTheme() === 'system') apply();
  }
  if (mqDark) {
    if (mqDark.addEventListener) mqDark.addEventListener('change', onSystem);
    else if (mqDark.addListener) mqDark.addListener(onSystem);
  }
  if (mqMotion) {
    var onMotion = function () { apply(); };
    if (mqMotion.addEventListener) mqMotion.addEventListener('change', onMotion);
    else if (mqMotion.addListener) mqMotion.addListener(onMotion);
  }
})();
