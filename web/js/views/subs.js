/* subs view — spec 4.4 */
(function () {
  'use strict';
  var MM = window.MM = window.MM || {};
  MM.views = MM.views || {};

  var rootEl = null;
  var refs = null;
  var active = false;
  var storeUn = null;
  var loading = false;
  var loadingPromise = null;
  var loadedAt = 0;
  var errorMsg = '';
  var lastErrKey = null;
  var skeletonTimer = null;
  var showSkel = false;
  var refreshing = {};
  var deleting = {};
  var allBusy = false;

  function state() { return MM.store.state; }

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

  function formatLabel(f) {
    if (f === 'clash') return 'Clash';
    if (f === 'base64') return 'Base64';
    if (f === 'uri-list') return '链接列表';
    if (f === 'unknown') return '未知';
    return f ? String(f) : '未知';
  }

  /* ---------- mount ---------- */
  function mount(root) {
    rootEl = root;
    root.innerHTML =
      '<div class="toolbar">' +
        '<span class="toolbar-note">订阅会在后台按间隔自动更新</span>' +
        '<div class="toolbar-actions">' +
          '<button class="btn btn-tonal btn-small" type="button" data-s="import">导入配置</button>' +
          '<button class="btn btn-tonal btn-small" type="button" data-s="refreshAll">全部更新</button>' +
        '</div>' +
      '</div>' +
      '<div class="error-slot" data-s="errors"></div>' +
      '<div class="sub-list" data-s="list"></div>' +
      '<div data-s="empty"></div>';

    refs = {
      importBtn: root.querySelector('[data-s="import"]'),
      refreshAll: root.querySelector('[data-s="refreshAll"]'),
      errors: root.querySelector('[data-s="errors"]'),
      list: root.querySelector('[data-s="list"]'),
      empty: root.querySelector('[data-s="empty"]')
    };

    refs.importBtn.addEventListener('click', function () { openForm('add', null, 'paste'); });
    refs.refreshAll.addEventListener('click', refreshAll);
  }

  /* ---------- data ---------- */
  function refresh(force) {
    if (loadingPromise) return loadingPromise;
    if (!force && Date.now() - loadedAt < 8000 && (state().subs || []).length) {
      render();
      return Promise.resolve();
    }
    loading = true;
    startSkeleton();
    loadingPromise = MM.data.loadSubs().then(function () {
      loadedAt = Date.now();
      errorMsg = '';
    }).catch(function (e) {
      errorMsg = (e && e.message) || '读取订阅失败';
    }).finally(function () {
      loading = false;
      loadingPromise = null;
      showSkel = false;
      if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
      if (active) render();
    });
    return loadingPromise;
  }

  // 写操作后直接取列表，避免 sched.once 去重返回旧请求的响应。
  function reloadSubs() {
    return MM.api('/api/subs').then(function (d) {
      MM.store.set({ subs: (d && d.subs) || [] });
      return d;
    });
  }

  // 写操作成功后的静默刷新：刷新失败不应被误报成写操作失败。
  function reloadAfterWrite() {
    reloadSubs().then(function () {
      loadedAt = Date.now();
      errorMsg = '';
      if (active) render();
    }, function () { /* 下一轮轮询会再取 */ });
  }

  function startSkeleton() {
    if ((state().subs || []).length || loadedAt) return;
    if (skeletonTimer) clearTimeout(skeletonTimer);
    skeletonTimer = setTimeout(function () {
      skeletonTimer = null;
      if (loading && !(state().subs || []).length && !loadedAt) {
        showSkel = true;
        if (active) render();
      }
    }, 400);
  }

  /* ---------- rendering ---------- */
  function render() {
    if (!refs) return;
    renderError();
    var subs = state().subs || [];
    if (!subs.length) {
      clearKeyed(refs.list);
      refs.list.classList.add('hidden');
      refs.empty.classList.remove('hidden');
      refs.empty.textContent = '';
      if (showSkel) {
        refs.empty.appendChild(MM.ui.skeleton('cards'));
      } else if (errorMsg) {
        refs.empty.appendChild(MM.ui.empty({
          kind: 'error', icon: 'warning', title: '暂时读不到订阅数据',
          text: errorMsg,
          cta: { label: '重试', onClick: function () { errorMsg = ''; lastErrKey = null; refresh(true); } }
        }));
      } else {
        refs.empty.appendChild(MM.ui.empty({
          icon: 'subs', title: '还没有订阅',
          text: '添加订阅链接，或粘贴 Clash 配置导入',
          cta: { label: '添加订阅', onClick: openAdd }
        }));
      }
      return;
    }
    refs.empty.classList.add('hidden');
    refs.empty.textContent = '';
    refs.list.classList.remove('hidden');
    MM.keyedList(refs.list, subs, function (s) { return s.id; }, createCard, updateCard, { tag: 'subs' });
  }

  function renderError() {
    var key = errorMsg + '|' + loadedAt;
    if (key === lastErrKey) return;
    lastErrKey = key;
    refs.errors.textContent = '';
    if (!errorMsg) return;
    if (!(state().subs || []).length) return;
    var msg = errorMsg;
    if (loadedAt) msg += ' · 显示上次数据 ' + MM.fmt.clock(loadedAt / 1000);
    refs.errors.appendChild(MM.ui.errorBar(msg, function () {
      errorMsg = '';
      lastErrKey = null;
      refs.errors.textContent = '';
      return refresh(true);
    }));
  }

  function addTag(host, text) {
    var tag = document.createElement('span');
    tag.className = 'tag';
    tag.textContent = text;
    tag.title = text;
    host.appendChild(tag);
  }

  function createCard() {
    var card = document.createElement('article');
    card.className = 'sub-card';
    card.innerHTML =
      '<div class="sub-head">' +
        '<span class="sub-name"></span>' +
        '<span data-r="status"></span>' +
        '<span class="chip chip-neutral" data-r="format"></span>' +
        '<span class="tag" data-r="nodes"></span>' +
      '</div>' +
      '<div class="sub-tags" data-r="tags"></div>' +
      '<div class="sub-url" data-r="url"></div>' +
      '<div class="sub-error hidden" data-r="error"></div>' +
      '<div class="sub-auto" data-r="auto"></div>' +
      '<div class="sub-traffic">' +
        '<div class="sub-traffic-line"><span data-r="used"></span><span data-r="pct"></span></div>' +
        '<div class="bar"><span data-r="bar"></span></div>' +
        '<div class="sub-traffic-line"><span data-r="expire"></span><span></span></div>' +
      '</div>' +
      '<div class="sub-actions">' +
        '<button class="btn btn-text btn-small" type="button" data-act="refresh">更新</button>' +
        '<button class="btn btn-text btn-small" type="button" data-act="edit">编辑</button>' +
        '<button class="btn btn-text btn-small" type="button" data-act="copy">复制链接</button>' +
        '<button class="btn btn-text btn-small" type="button" data-act="raw">原始内容</button>' +
        '<span class="spacer"></span>' +
        '<button class="btn btn-text btn-small btn-destructive" type="button" data-act="delete">删除</button>' +
      '</div>';
    card.__r = {
      name: card.querySelector('.sub-name'),
      status: card.querySelector('[data-r="status"]'),
      format: card.querySelector('[data-r="format"]'),
      nodes: card.querySelector('[data-r="nodes"]'),
      tags: card.querySelector('[data-r="tags"]'),
      url: card.querySelector('[data-r="url"]'),
      error: card.querySelector('[data-r="error"]'),
      auto: card.querySelector('[data-r="auto"]'),
      used: card.querySelector('[data-r="used"]'),
      pct: card.querySelector('[data-r="pct"]'),
      bar: card.querySelector('[data-r="bar"]'),
      expire: card.querySelector('[data-r="expire"]'),
      refreshBtn: card.querySelector('[data-act="refresh"]'),
      deleteBtn: card.querySelector('[data-act="delete"]')
    };
    card.addEventListener('click', function (e) {
      var btn = e.target.closest('[data-act]');
      if (!btn) return;
      var sub = card.__sub;
      if (!sub) return;
      var act = btn.getAttribute('data-act');
      if (act === 'refresh') refreshOne(sub);
      else if (act === 'edit') openEdit(sub);
      else if (act === 'copy') copyLink(sub);
      else if (act === 'raw') openRaw(sub);
      else if (act === 'delete') removeSub(sub);
    });
    return card;
  }

  function updateCard(card, sub) {
    card.__sub = sub;
    var r = card.__r;
    var status = sub.status || 'never';
    var map = {
      ok: { text: '正常', kind: 'ok' },
      error: { text: '异常', kind: 'err' },
      never: { text: '未更新', kind: 'neutral' }
    };
    var s = map[status] || map.never;
    setText(r.name, sub.name || '未命名订阅');
    r.status.innerHTML = MM.ui.chip(s.text, s.kind);
    r.format.textContent = formatLabel(sub.format);
    setText(r.nodes, (Number(sub.nodeCount) || 0) + ' 节点');

    r.tags.textContent = '';
    if (sub.includeFilter) addTag(r.tags, '保留 ' + sub.includeFilter);
    if (sub.excludeFilter) addTag(r.tags, '排除 ' + sub.excludeFilter);
    if (!r.tags.children.length) r.tags.classList.add('hidden');
    else r.tags.classList.remove('hidden');

    var url = sub.url || '';
    setText(r.url, url);
    r.url.title = url;
    r.url.classList.toggle('hidden', !url);

    var err = sub.error || '';
    if (status === 'error' && !err) err = '订阅更新失败';
    if (err) {
      r.error.textContent = '拉取失败：' + err;
      r.error.classList.remove('hidden');
    } else {
      r.error.classList.add('hidden');
    }

    var au = sub.autoUpdate || {};
    var autoText;
    if (au.manual) autoText = '手动导入（不自动更新）';
    else if (au.enabled) {
      autoText = '自动更新 · 每 ' + (Number(au.interval) || 0) + ' 小时' +
        (au.nextAt ? ' · 下次 ' + MM.fmt.left(au.nextAt) : '');
    } else autoText = '自动更新已关闭';
    setText(r.auto, autoText);

    var t = sub.traffic || {};
    var used = (Number(t.upload) || 0) + (Number(t.download) || 0);
    var total = Number(t.total) || 0;
    var pct = total > 0 ? Math.min(100, used / total * 100) : 0;
    r.bar.style.width = pct.toFixed(1) + '%';
    r.bar.className = pct > 85 ? 'warn' : '';
    setText(r.used, '已用 ' + MM.fmt.bytes(used) + (total > 0 ? ' / ' + MM.fmt.bytes(total) : ''));
    setText(r.pct, total > 0 ? Math.round(pct) + '%' : '—');

    var expire = Number(t.expire) || 0;
    if (expire > 0) {
      var expired = expire * 1000 < Date.now();
      setText(r.expire, '到期 ' + MM.fmt.date(expire) + (expired ? '（已过期）' : ''));
      r.expire.className = 'expire' + (expired ? ' err' : '');
    } else {
      setText(r.expire, '到期 —');
      r.expire.className = 'expire';
    }

    var rb = !!refreshing[sub.id];
    r.refreshBtn.disabled = rb;
    setText(r.refreshBtn, rb ? '更新中…' : '更新');
    var del = !!deleting[sub.id];
    r.deleteBtn.disabled = del;
    setText(r.deleteBtn, del ? '删除中…' : '删除');
    card.setAttribute('aria-busy', rb ? 'true' : 'false');
  }

  /* ---------- progress helper ---------- */
  function runWithProgress(title, fn) {
    var prog = MM.ui.progress(title);
    var unsub = MM.progressBus.sub(function (ev) { if (ev) prog.add(ev); });
    return Promise.resolve().then(fn).then(function (r) {
      prog.finish();
      setTimeout(function () { prog.close(); }, 450);
      return r;
    }, function (e) {
      prog.finish();
      prog.add({ err: true, msg: (e && e.message) || '操作失败' });
      setTimeout(function () { prog.close(); }, 1600);
      throw e;
    }).finally(unsub);
  }

  /* ---------- actions ---------- */
  function refreshOne(sub) {
    if (refreshing[sub.id]) return;
    refreshing[sub.id] = true;
    render();
    MM.api('/api/subs/' + encodeURIComponent(sub.id) + '/refresh', { method: 'POST' }).then(function (d) {
      if (d && d.error) MM.ui.toast('更新失败：' + d.error, 'err');
      else MM.ui.toast('订阅已更新', 'ok');
      reloadAfterWrite();
    }).catch(function (e) {
      MM.ui.toast(e.message || '更新订阅失败', 'err');
    }).finally(function () {
      delete refreshing[sub.id];
      if (active) render();
    });
  }

  function refreshAll() {
    if (allBusy) return;
    allBusy = true;
    MM.ui.setBusy(refs.refreshAll, true);
    runWithProgress('正在更新全部订阅…', function () {
      return MM.api('/api/subs/refresh', { method: 'POST' });
    }).then(function (d) {
      var failed = Number(d && d.failed) || 0;
      var done = Number(d && d.refreshed) || 0;
      if (failed > 0) MM.ui.toast('已更新 ' + done + ' 个，失败 ' + failed + ' 个', 'err');
      else MM.ui.toast('已更新 ' + done + ' 个订阅', 'ok');
      reloadAfterWrite();
    }).catch(function (e) {
      MM.ui.toast(e.message || '更新订阅失败', 'err');
    }).finally(function () {
      allBusy = false;
      MM.ui.setBusy(refs.refreshAll, false);
      if (active) render();
    });
  }

  function copyLink(sub) {
    if (!sub.url) { MM.ui.toast('手动导入的订阅没有链接', 'err'); return; }
    MM.api('/api/subs/' + encodeURIComponent(sub.id) + '?reveal=1').then(function (d) {
      var url = (d && d.sub && d.sub.url) || sub.url;
      return MM.copy(url);
    }).then(function () {
      MM.ui.toast('链接已复制', 'ok');
    }).catch(function (e) {
      MM.ui.toast(e.message || '复制失败', 'err');
    });
  }

  function openRaw(sub) {
    MM.api('/api/subs/' + encodeURIComponent(sub.id) + '/raw').then(function (d) {
      var wrap = document.createElement('div');
      var meta = document.createElement('p');
      meta.className = 'field-hint';
      meta.textContent = (d.guess || '未知格式') + ' · ' + MM.fmt.bytes(d.bytes || 0);
      var pre = document.createElement('pre');
      pre.className = 'raw-pre';
      pre.textContent = d.preview || '';
      wrap.appendChild(meta);
      wrap.appendChild(pre);
      MM.ui.dialog({
        title: '原始内容 · ' + (sub.name || ''),
        body: wrap,
        width: 'wide',
        actions: [
          {
            label: '复制', kind: 'text', onClick: function () {
              MM.copy(d.preview || '').then(function () { MM.ui.toast('已复制', 'ok'); },
                function () { MM.ui.toast('复制失败，请手动选择文本', 'err'); });
            }
          },
          { label: '关闭', kind: 'filled', value: true }
        ]
      });
    }).catch(function (e) {
      MM.ui.toast(e.message || '读取原始内容失败', 'err');
    });
  }

  function removeSub(sub) {
    MM.ui.confirm({
      title: '删除订阅？',
      body: '将删除「' + (sub.name || '') + '」及其节点，内核运行中会自动重载。',
      confirm: '删除',
      destructive: true
    }).then(function (ok) {
      if (!ok) return;
      deleting[sub.id] = true;
      render();
      MM.api('/api/subs/' + encodeURIComponent(sub.id), { method: 'DELETE' }).then(function () {
        MM.ui.toast('订阅已删除', 'ok');
        reloadAfterWrite();
      }).catch(function (e) {
        MM.ui.toast(e.message || '删除订阅失败', 'err');
      }).finally(function () {
        delete deleting[sub.id];
        if (active) render();
      });
    });
  }

  function openAdd() { openForm('add', null, 'link'); }

  function openEdit(sub) {
    MM.api('/api/subs/' + encodeURIComponent(sub.id) + '?reveal=1').then(function (d) {
      var full = (d && d.sub) || sub;
      openForm('edit', full, 'link');
    }).catch(function (e) {
      MM.ui.toast(e.message || '读取订阅失败', 'err');
    });
  }

  /* ---------- add / edit sheet ---------- */
  function openForm(mode, sub, initialTab) {
    var isEdit = mode === 'edit';
    var body = document.createElement('div');
    body.className = 'sheet-form';
    body.innerHTML =
      (isEdit ? '' :
        '<div class="segmented form-tabs" data-tabs>' +
          '<button type="button" data-tab="link"' + (initialTab === 'paste' ? '' : ' class="active"') + '>订阅链接</button>' +
          '<button type="button" data-tab="paste"' + (initialTab === 'paste' ? ' class="active"' : '') + '>粘贴配置</button>' +
        '</div>') +
      '<div data-panel="link"' + (initialTab === 'paste' && !isEdit ? ' class="hidden"' : '') + '>' +
        '<label class="field"><span class="field-label">名称</span>' +
          '<input class="input" data-f="name" placeholder="例如：机场A" autocomplete="off"></label>' +
        '<label class="field"><span class="field-label">订阅链接</span>' +
          '<input class="input" data-f="url" placeholder="粘贴 http(s) 订阅链接" autocomplete="off" spellcheck="false"></label>' +
        '<div class="form-grid">' +
          '<label class="field"><span class="field-label">自动更新间隔（小时，0=全局）</span>' +
            '<input class="input" data-f="interval" type="number" min="0" max="168" inputmode="numeric" placeholder="0"></label>' +
          '<label class="field"><span class="field-label">包含过滤（正则，可选）</span>' +
            '<input class="input" data-f="includeFilter" placeholder="/HK|JP/" autocomplete="off"></label>' +
          '<label class="field"><span class="field-label">排除过滤（正则，可选）</span>' +
            '<input class="input" data-f="excludeFilter" placeholder="/过期|剩余/" autocomplete="off"></label>' +
        '</div>' +
      '</div>' +
      '<div data-panel="paste"' + (initialTab === 'paste' && !isEdit ? '' : ' class="hidden"') + '>' +
        '<label class="field"><span class="field-label">名称</span>' +
          '<input class="input" data-f="pasteName" placeholder="例如：手动导入" autocomplete="off"></label>' +
        '<label class="field"><span class="field-label">配置内容</span>' +
          '<textarea class="input" data-f="content" rows="8" placeholder="粘贴 Clash YAML、base64 节点列表或明文节点链接"></textarea></label>' +
        '<label class="dropzone" data-drop>' + MM.icon('upload') +
          '<span data-drop-text>从文件读取（.yaml / .txt，上限 16 MB）</span>' +
          '<input type="file" accept=".yaml,.yml,.txt,.conf" data-file></label>' +
      '</div>' +
      '<p class="field-error hidden" data-form-error></p>';

    var nameInput = body.querySelector('[data-f="name"]');
    var urlInput = body.querySelector('[data-f="url"]');
    var intervalInput = body.querySelector('[data-f="interval"]');
    var includeInput = body.querySelector('[data-f="includeFilter"]');
    var excludeInput = body.querySelector('[data-f="excludeFilter"]');
    var pasteName = body.querySelector('[data-f="pasteName"]');
    var contentInput = body.querySelector('[data-f="content"]');
    var errEl = body.querySelector('[data-form-error]');
    var tab = initialTab === 'paste' && !isEdit ? 'paste' : 'link';

    if (isEdit) {
      nameInput.value = sub.name || '';
      urlInput.value = sub.url || '';
      intervalInput.value = sub.interval != null ? sub.interval : '';
      includeInput.value = sub.includeFilter || '';
      excludeInput.value = sub.excludeFilter || '';
    } else if (sub) {
      nameInput.value = sub.name || '';
    }

    if (!isEdit) {
      var tabs = body.querySelector('[data-tabs]');
      tabs.addEventListener('click', function (e) {
        var b = e.target.closest('[data-tab]');
        if (!b) return;
        tab = b.getAttribute('data-tab');
        MM.$$('[data-tab]', tabs).forEach(function (x) { x.classList.toggle('active', x === b); });
        body.querySelector('[data-panel="link"]').classList.toggle('hidden', tab !== 'link');
        body.querySelector('[data-panel="paste"]').classList.toggle('hidden', tab !== 'paste');
        errEl.classList.add('hidden');
      });
    }

    function readFile(file) {
      if (!file) return;
      if (file.size > 16 * 1024 * 1024) {
        errEl.textContent = '文件超过 16 MB，无法导入';
        errEl.classList.remove('hidden');
        return;
      }
      var reader = new FileReader();
      reader.onload = function () {
        contentInput.value = String(reader.result || '');
        if (!pasteName.value) pasteName.value = file.name.replace(/\.[^.]+$/, '');
        var dt = body.querySelector('[data-drop-text]');
        if (dt) dt.textContent = '已读取：' + file.name + '（' + MM.fmt.bytes(file.size) + '）';
      };
      reader.onerror = function () {
        errEl.textContent = '读取文件失败';
        errEl.classList.remove('hidden');
      };
      reader.readAsText(file, 'utf-8');
    }

    var fileInput = body.querySelector('[data-file]');
    fileInput.addEventListener('change', function () { readFile(fileInput.files && fileInput.files[0]); });
    var drop = body.querySelector('[data-drop]');
    drop.addEventListener('dragover', function (e) { e.preventDefault(); drop.classList.add('drag-over'); });
    drop.addEventListener('dragleave', function () { drop.classList.remove('drag-over'); });
    drop.addEventListener('drop', function (e) {
      e.preventDefault();
      drop.classList.remove('drag-over');
      readFile(e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0]);
    });

    function showError(msg) {
      errEl.textContent = msg;
      errEl.classList.remove('hidden');
    }

    var dlg;
    dlg = MM.ui.sheet({
      title: isEdit ? '编辑订阅' : '添加订阅',
      body: body,
      actions: [
        { label: '取消', kind: 'tonal', value: false },
        {
          label: isEdit ? '保存' : '保存', kind: 'filled',
          onClick: function (close) { submit(close); }
        }
      ]
    });

    function submit(close) {
      errEl.classList.add('hidden');
      var saveBtn = dlg.el.querySelector('.dlg-actions .btn-filled');
      var task;
      if (isEdit) {
        var payload = {
          name: nameInput.value.trim(),
          interval: Math.max(0, parseInt(intervalInput.value, 10) || 0),
          includeFilter: includeInput.value.trim(),
          excludeFilter: excludeInput.value.trim()
        };
        if (!payload.name) { showError('请填写名称'); return; }
        var u = urlInput.value.trim();
        if (!u || u.indexOf('***') >= 0) {
          // URL 为空或仍是打码值：不提交，避免把订阅地址写坏
        } else {
          if (!/^https?:\/\//i.test(u)) { showError('订阅链接必须是 http/https 地址'); return; }
          payload.url = u;
        }
        task = function () {
          return MM.api('/api/subs/' + encodeURIComponent(sub.id), { method: 'PUT', body: payload });
        };
      } else if (tab === 'paste') {
        var content = contentInput.value.trim();
        if (!content) { showError('请粘贴配置或选择文件'); return; }
        var pname = pasteName.value.trim();
        task = function () {
          return MM.api('/api/subs/import', { method: 'POST', body: { name: pname || '导入配置', content: content } });
        };
      } else {
        var name = nameInput.value.trim();
        var url = urlInput.value.trim();
        if (!url) { showError('请填写订阅链接'); return; }
        if (!/^https?:\/\//i.test(url)) { showError('订阅链接必须是 http/https 地址'); return; }
        var bodyPayload = {
          name: name,
          url: url,
          interval: Math.max(0, parseInt(intervalInput.value, 10) || 0),
          includeFilter: includeInput.value.trim(),
          excludeFilter: excludeInput.value.trim()
        };
        task = function () {
          return MM.api('/api/subs', { method: 'POST', body: bodyPayload });
        };
      }

      if (saveBtn) { saveBtn.disabled = true; saveBtn.textContent = '保存中…'; }
      runWithProgress(isEdit ? '正在保存订阅…' : '正在添加订阅…', task).then(function (d) {
        if (d && d.error) MM.ui.toast('订阅已保存，但拉取失败：' + d.error, 'err');
        else MM.ui.toast(isEdit ? '订阅已保存' : '订阅已添加', 'ok');
        close(true);
        reloadAfterWrite();
      }).catch(function (e) {
        showError(e.message || '保存失败');
        if (saveBtn) { saveBtn.disabled = false; saveBtn.textContent = '保存'; }
      });
    }
  }

  /* ---------- lifecycle ---------- */
  function activate(ctx, sec) {
    if (!rootEl) rootEl = sec;
    if (!refs) return;
    active = true;
    if (!storeUn) {
      storeUn = MM.store.sub(function () { if (active) render(); });
    }
    render();
    refresh(false);
  }

  function deactivate() {
    active = false;
    if (storeUn) { storeUn(); storeUn = null; }
    if (skeletonTimer) { clearTimeout(skeletonTimer); skeletonTimer = null; }
  }

  MM.views.subs = {
    title: function () { return MM.router.TITLES.subs; },
    mount: mount,
    activate: activate,
    deactivate: deactivate,
    refresh: refresh,
    actions: function () {
      return [
        {
          icon: 'upload', label: '导入', showLabel: true, desktopOnly: true,
          onClick: function () { openForm('add', null, 'paste'); }
        },
        {
          icon: 'refresh', label: '全部更新', showLabel: true, desktopOnly: true,
          onClick: refreshAll
        },
        {
          icon: 'plus', label: '添加订阅', primary: true, showLabel: true,
          onClick: openAdd
        }
      ];
    }
  };
})();
