# mihomo-merlin v2 面板视觉验收报告（最终结论 · 第 3 轮定向确认）

- 验收日期：2026-10-31
- 分支：`feat/v2-ui-stability`
- 面板版本：v2.0.0
- 证据目录：`docs/ui-redesign/acceptance/`（137 张截图）
- 验收依据：`docs/ui-redesign/spec.md` 第 8 节 V-01..V-38，辅以第 2/4/6 节的可量测 token

---

## 1. 结论摘要

| 指标 | 结果 |
|---|---|
| 严格通过率 | **37 / 38 = 97.4%** |
| 排除已接受例外后的通过率 | **37 / 37 = 100%** |
| blocker | **0** |
| major | **0** |
| minor | **0** |
| 已接受例外 | 1（V-38 体积项，按 spec §10.3-7 / §8） |

第 1 轮的 5 个 minor 与第 2 轮发现的窄屏截断已全部修复并复测通过；V-11 满足 40（D）/34（M）、w700、tabular 的 spec 要求，判定 PASS。390×844、375×844、320×700 概览的统计值与 hero 节点名均完整显示，无横向溢出、无 page error。**minor 已清零**；V-38 体积项仍为已接受例外。核心布局、sticky 表头与页面脚本均无回归。

---

## 2. 验收环境与方法

- 第 2 轮使用 `/Users/zhangxuefang/sdk/go/bin/go build -o /tmp/mm-qa-panel-r2 .` 重新构建当前分支；第 3 轮使用 `/Users/zhangxuefang/sdk/go/bin/go build -o /tmp/mm-qa-panel-r3 .` 重新构建，真实面板分别以 `/tmp/mm-qa-data-r2`、`/tmp/mm-qa-data-r3` 启动，截图均由真实面板二进制的静态资源提供。
- 真实端点返回形状用于校准 fixture：`/api/status`、`/api/settings`、`/api/subs`、`/api/intranet`、`/api/netinfo`、`/api/reachability`、`/api/kernel`、`/api/service`、`/api/storage`、`/api/rules`、`/api/rules/set`、`/api/connections?legacy=0`。
- 通过 Playwright `context.route('**/api/**')` 注入运行中/停止/空/错误/130 节点等状态；通过注入假 `EventSource` 覆盖 traffic / logs / progress 三个流。
- 引擎未实际启动（本机无 mihomo 内核），真实内核 API、真实订阅下载、真实内核更新不在本轮端到端范围内；对应 UI 状态已用 fixture 覆盖。
- 第 2 轮定向复测：D/M 概览、M 浅色添加订阅 sheet、M 节点搜索、M 手动导入订阅卡、S 320 诊断页；另复核 sticky 表头、横向溢出、page error 与 768 流量字号。
- 第 3 轮只做窄屏最终确认：390×844、375×844、320×700 概览，验证统计值/hero 不截断、字号断点（34/28/24）、320 hero 纵向布局、无横向溢出与 page error。
- 量测方式：截图 + `getComputedStyle`/`getBoundingClientRect`/canvas 像素采样/键盘事件序列。所有数值均来自本轮运行结果。

---

## 3. 问题列表（按严重度）

### blocker

无。

### major

无。

### minor

无。第 2 轮发现的窄屏统计值截断已在第 3 轮修复并复测通过。

### 第 1/2 轮 minor 修复复测（全部通过）

| 问题 | 最终结果 | 证据 / 实测 |
|---|---|---|
| 浅色 sheet 输入框与 sheet 同色 | **PASS** | sheet `#EDEEF1` vs input/textarea `#E3E5E9`，边界可见；`M-light-subs-add-sheet.png`、`M-light-subs-add-paste.png` |
| 搜索框双清除图标 | **PASS** | 原生 `::-webkit-search-cancel-button` 已隐藏，仅保留自定义 ×；`M-light-nodes-search-empty.png` |
| 手动导入订阅重复文案 | **PASS** | 空 URL 行隐藏，卡内“手动导入（不自动更新）”只出现 1 次；`M-light-subs.png` |
| 320 诊断页标题截断 | **PASS** | “面板地址” `clientWidth = scrollWidth = 64`，完整显示；`S-dark-settings-diagnostics-320.png` |
| V-11 关键数字字号 | **PASS** | 桌面 40 / 移动 34；流量 34（D）/28（768）/24（M）；hero 34（D）/28（M）；w700 且 “111/888” 等宽；`M-dark-overview-stats.png`、`D-dark-overview-running.png` |
| 移动端统计值/hero 截断 | **PASS** | 390：`1天18时`、`42.7 MB` 均 141/141；375：28px，134/134；320：24px，106/106；hero 320 纵向布局，节点名 248/248；`M-dark-overview-stats.png`、`M-light-overview-375.png`、`S-light-overview-320.png`、`S-light-overview-320-stats.png` |

### 已接受例外（不计为缺陷）

- **V-38 体积项 FAIL（已接受例外）**：JS 原始 246,462 B（>150KB）、`web/` 原始 332,815 B（>300KB）、CSS 55,170 B（≤60KB）；gzip 后 JS 58,512 B。按 spec §10.3-7 的 2026-10-31 决策接受该偏差，不引入构建/压缩产物分离；不记为 blocker。V-38 其余静态检查均通过。

---

## 4. 重点复核项

### 4.1 第 1 轮近期修复（仍通过）

| 复核项 | 结果 | 证据 / 实测 |
|---|---|---|
| 1. 订阅卡名称渲染 | **PASS** | “机场A · 主力节点 / 手动导入 · 测试”正常；`M-light-subs.png`、`M-dark-subs.png`、`D-dark-subs.png` |
| 2. 节点/规则/连接桌面 sticky 表头 | **PASS** | 规则 head 72–108、首行 top=108；连接 head 72–112、首行 top=112；gap=0、未压首行、无 72px 空白；`D-dark-rules-sticky-top.png`、`D-light-connections-sticky-top.png` |
| 3. 移动端节点工具栏 | **PASS** | 搜索框与排序图标同一行；`M-dark-nodes-130.png` |
| 4. 桌面设置标题 | **PASS** | AppBar 标题“设置”，右栏为子页标题；`C-dark-settings-1024.png`、`D-dark-settings-proxy.png` |
| 5. 诊断页实际模式中文 | **PASS** | “规则 → 全局”，无 rule/global/direct 裸值；`D-dark-settings-diagnostics-mode-cn.png` |

### 4.2 第 2/3 轮回归检查

| 检查项 | 结果 | 实测 |
|---|---|---|
| 横向溢出 | **PASS** | 所有定向页面 `scrollWidth - clientWidth = 0`（390、375、320、768、1440） |
| page error | **PASS** | 定向复测期间 `pageerror` 计数 0 |
| sticky 表头 | **PASS** | 规则 gap 0、连接 gap 0，表头 72–108/112 |
| 768 流量字号 | **PASS** | `28px`（符合“34/28/24”方案） |
| 窄屏字号断点 | **PASS** | 390 统计 34；375 统计 28；320 统计 24；hero 320 为 column 布局 |
| 窄屏内容完整性 | **PASS** | 390 `1天18时`/`42.7 MB`、375 同、320 `1天18时`/`42.7 MB`，scrollWidth=clientWidth |
| 纯黑/浅色表面 | **PASS** | 字号改动未影响 token 与四层表面 |

---

## 5. 逐条判定表（V-01..V-38）

> 证据路径均相对 `docs/ui-redesign/`。PASS 行的“严重度”为 —；FAIL 行标注严重度。

| ID | 视口/主题 | 判定 | 证据 | 期望 / 实际 | 严重度 |
|---|---|---|---|---|---|
| V-01 | D light/dark | **PASS** | `acceptance/D-light-overview-running.png`、`acceptance/D-dark-overview-running.png` | 7 项齐全、顺序正确；选中底 `rgba(242,243,245,.08)`，指示条 3×16 accent，非整行 primary；切换后标题/视图同步 | — |
| V-02 | M light/dark | **PASS** | `acceptance/M-dark-overview-running.png`、`acceptance/M-light-overview-stopped.png` | dock 恰 5 项，左右/底部 21px，高 62，圆角 999；选中 accent 图标/文字 + 中性 platter；无第 6 项 | — |
| V-03 | M light/dark | **PASS** | `acceptance/M-light-more-sheet.png`、`acceptance/M-dark-more-sheet.png`、`acceptance/M-light-rules-from-more.png` | 更多 sheet 含规则/日志/外观/诊断 4 行，顶部圆角 28；点规则后 sheet 关闭、推入规则页、返回回原页 | — |
| V-04 | M+D light/dark | **PASS** | `acceptance/M-light-appearance-after-reload.png`、`acceptance/M-dark-pb-appearance.png` | system 跟随系统，light/dark 不跟随；刷新后 data-theme/meta 正确；meta 深色 `#0F1013`、浅色 `#EDEEF1`、纯黑 `#000` | — |
| V-05 | M light | **PASS** | `acceptance/M-light-appearance-accent-*.png`、`acceptance/D-dark-overview-accent-violet-chart.png` | 6 套主色改变分段/色板/焦点环/dock/图表首序列；表面四层与 ok/warn/err 不变 | — |
| V-06 | M dark PB | **PASS** | `acceptance/M-dark-pb-overview-running.png`、`acceptance/M-dark-pb-appearance.png`、`acceptance/M-light-appearance-mono-disabled.png` | 纯黑 page #000 → card #121316 → block #1B1D21 → inset #26282D；浅色开关 disabled 且不生效；持久化 | — |
| V-07 | D/M dark | **PASS** | `acceptance/D-dark-overview-running-traffic.png`、`acceptance/M-dark-settings.png` | 四层可辨；深色只变亮；列表卡片 border 0、shadow none；仪表盘卡 e1 | — |
| V-08 | D/M light | **PASS** | `acceptance/D-light-overview-running-traffic.png`、`acceptance/M-light-subs.png` | page #EDEEF1、card #FBFBFC（非纯白）、block #EDEEF1、inset #E3E5E9；无纯白页面；列表卡无边框 | — |
| V-09 | D/M light/dark | **PASS** | `acceptance/D-light-connections.png`、`acceptance/M-light-subs.png`、`acceptance/M-light-confirm-stop-engine.png` | card/row/dialog/sub-card 的 borderTopWidth 均为 0；仅分隔线、输入聚焦环、焦点环、chrome 边出现线 | — |
| V-10 | D/M light | **PASS** | `acceptance/D-light-overview-running-traffic.png`、`acceptance/M-dark-more-sheet.png`、`acceptance/M-light-confirm-stop-engine.png` | 仪表盘 32（D）/34（M）；常规卡 24；sheet 顶部 28；alert 34；胶囊 999 | — |
| V-11 | D/M dark | **PASS** | `acceptance/D-dark-overview-running.png`、`acceptance/M-dark-overview-running.png`、`acceptance/M-dark-overview-stats.png`、`acceptance/M-light-overview-375.png`、`acceptance/S-light-overview-320-stats.png` | 统计值 40（D）/34（M）/28（375）/24（320）、w700、tabular（111/888 等宽）；流量 34/28/24；hero 34/28；运行时长 `1天18时`，统计值与 hero 节点名在 390/375/320 均完整显示 | — |
| V-12 | D/M light/dark | **PASS** | `acceptance/D-dark-overview-running.png`、`acceptance/M-dark-settings.png` | 标题/值 on-surface；标签/说明 on-surface-variant；无 outline 文字色；禁用 opacity | — |
| V-13 | M light/dark | **PASS** | `acceptance/M-light-overview-stopped.png`、`acceptance/M-dark-overview-stopped.png`、`acceptance/M-light-overview-stopped-reach.png` | 未运行中性 chip + 启动按钮；流量 0；图表平线；连通性“内核未运行”；无整卡红 | — |
| V-14 | D dark | **PASS** | `acceptance/D-dark-overview-mismatch.png` | 当前节点+延迟；分段以实际“全局”选中；说明“设置里是规则，尚未生效（内核：全局）”warn | — |
| V-15 | D/M light/dark | **PASS** | `acceptance/D-dark-nodes-selected-delay.png`、`acceptance/M-dark-nodes-selected-delay.png` | 行保持 card 填充，仅 leading check 为 accent；无整行 primary；组头显示当前节点 | — |
| V-16 | D/M light/dark | **PASS** | `acceptance/D-dark-nodes-selected-delay.png`、`acceptance/M-dark-nodes-testing.png` | <600 ok、≥600 warn、超时 err、未测 variant；只染文字；测试中“测试…”+ spinner | — |
| V-17 | M light | **PASS** | `acceptance/M-light-subs-empty.png`、`acceptance/M-light-nodes-empty.png`、`acceptance/M-light-nodes-search-empty.png` | 空状态为代码几何图标+标题+说明+1 CTA；搜索无结果与无节点文案不同；无位图 | — |
| V-18 | D light/dark | **PASS** | `acceptance/M-light-subs.png`、`acceptance/M-dark-subs.png`、`acceptance/M-dark-subs-error.png`、`acceptance/M-dark-subs-expired.png` | ok/err/中性 chip 正确；错误条 inline 且文字 err；卡片不整卡染红；流量条/到期/过滤 tag 正常 | — |
| V-19 | M light | **PASS** | `acceptance/M-light-subs-add-sheet.png`、`acceptance/M-light-subs-add-paste.png`、`acceptance/M-light-subs-save-progress.png` | 添加 sheet 双 tab；切 tab 内容保留；保存进度可见；成功后关闭并刷新。第 2 轮复核输入框已改为 inset，边界清晰 | — |
| V-20 | D/M dark | **PASS** | `acceptance/D-dark-rules.png`、`acceptance/D-dark-rules-filter-sheet.png`、`acceptance/D-dark-rules-source-jump.png`、`acceptance/M-dark-rules.png` | 规则数 800/800；类型/内容/策略可读；规则源入口跳 `#/settings/ruleset`；策略筛选 134/800；移动 overflowX=0 | — |
| V-21 | D light | **PASS** | `acceptance/D-light-connections-200-scrolled.png`、`acceptance/D-dark-connections-210-note.png`、`acceptance/D-light-connections-closeall-confirm.png` | 表头 sticky；列宽完整；210 行提示“仅显示前 200 条 / 共 210 条”；单条关闭 toast；全部关闭自定义 confirm | — |
| V-22 | M light/dark | **PASS** | `acceptance/M-light-connections.png`、`acceptance/M-dark-connections.png` | 每条连接一张卡；无横向滚动；关闭按钮 44×44；3 个统计 tile 不溢出 | — |
| V-23 | M light | **PASS** | `acceptance/M-light-connections-stopped.png`、`acceptance/M-light-connections-error.png`、`acceptance/M-light-connections-stale.png` | 停止态“内核未运行+去启动”；500 保留旧数据并显示“显示上次数据”+重试；不白屏 | — |
| V-24 | D/M dark | **PASS** | `acceptance/M-dark-logs-paused.png`、`acceptance/M-dark-logs-500-lines.png`、`acceptance/M-dark-logs-cleared.png`、`acceptance/M-dark-logs-level-sheet.png` | 上滚不被拉底，“回到最新”；暂停计数；恢复后 500 行；warning/error 仅文字色 | — |
| V-25 | M light | **PASS** | `acceptance/M-light-logs-connected.png`、`acceptance/M-dark-logs-connected.png` | 视口占满剩余高度；工具栏 sticky；dock 隐藏；返回可用；mono 12/18；可复制 | — |
| V-26 | D light | **PASS** | `acceptance/D-light-settings.png`、`acceptance/C-dark-settings-1024.png` | 双栏；5 组 12 行；左栏选中中性底 .08 + accent check；切换无整页刷新；默认 `#/settings/proxy` | — |
| V-27 | M light | **PASS** | `acceptance/M-light-settings-proxy.png`、`acceptance/M-light-settings-storage.png` | 推入子页、dock 隐藏、AppBar 返回+标题+保存；返回后 scrollY 180 恢复为 180 | — |
| V-28 | M light | **PASS** | `acceptance/M-light-settings-proxy-dirty.png`、`acceptance/M-light-settings-proxy-saved.png`、`acceptance/M-light-settings-proxy-save-error.png`、`acceptance/M-light-settings-after-mode-change.png` | 脏时“保存 •”点亮；成功 toast；失败保留草稿+错误条；模式改为全局后设置摘要显示“全局 · 3333/7890” | — |
| V-29 | M light/dark PB | **PASS** | `acceptance/M-light-appearance-accent-*.png`、`acceptance/M-dark-pb-appearance.png`、`acceptance/M-light-appearance-mono-disabled.png` | 即时生效且不发 PUT；6 色板 100×72、圆点 44、选中环+check；纯黑仅 dark；持久化 | — |
| V-30 | M light/dark | **PASS** | `acceptance/M-light-confirm-stop-engine.png`、`acceptance/D-dark-confirm-delete-sub.png`、`acceptance/D-dark-confirm-kernel-rollback.png`、`acceptance/D-light-connections-closeall-confirm.png` | 五类操作均走自定义 confirm；Esc 取消；焦点返回触发按钮；无 `window.confirm` | — |
| V-31 | D/M light/dark | **PASS** | 运行量测 | Tab 顺序与视觉一致；2px accent 焦点环；Shift+Tab 反向；Esc 关闭并返回焦点；dialog 末项后 document.body 落点为 Chromium headless 基线（最小原生 dialog A/B 复现） | — |
| V-32 | D/M light | **PASS** | `acceptance/M-light-skeleton.png` | >400ms 显示匹配布局骨架、无 spinner-only；<400ms（320ms 实测）无骨架且 booted=true | — |
| V-33 | M light | **PASS** | `acceptance/M-light-connections-stale.png`、`acceptance/M-light-connections-stale-recovered.png` | 错误条含可读原因+重试；旧 30 行仍在；重试成功后错误条消失；不整页空白 | — |
| V-34 | M light/dark | **PASS** | `acceptance/M-light-toast-stack.png` | 最多 3 条；位于 dock 上方；圆角 16；e2 阴影；error 仅图标红；`aria-live=polite` | — |
| V-35 | M dark | **PASS** | `acceptance/M-dark-lowend-overview.png` | `data-low-end=1`；e1/e2 none；dock 顶边 1px；动效 0ms；骨架静态；图表仅折线；轮询 15s/5s/隐藏 0 | — |
| V-36 | D 1024 / C 768 / M 390 | **PASS** | `acceptance/C-light-overview-1024.png`、`acceptance/C-dark-settings-1024.png`、`acceptance/T-light-overview-768.png`、`acceptance/S-light-overview-599.png`、`acceptance/S-light-overview-320.png`、`acceptance/S-light-overview-320-stats.png` | 1024 侧栏、dock 隐藏；<1024 dock；768 两列概览；599 单列；320 overflowX=0；dock 21px；320 统计值与 hero 完整显示 | — |
| V-37 | M light | **PASS** | `acceptance/M-light-login.png`、`acceptance/M-light-login-error.png`、`acceptance/M-light-login-success.png` | 401 触发“需要登录”；错误密码 inline；Enter 提交；成功后重新 boot；无循环 401 | — |
| V-38 | — | **FAIL（已接受例外）** | 静态检查 | 无 `color-mix`/`window.confirm`/`backdrop-filter`/外部字体/CDN/装饰性位图；CSS 55,170 B ≤60KB；JS `node --check` 全通过。体积：JS 246,462 B >150KB、web 332,815 B >300KB；gzip JS 58,512 B。按 spec §10.3-7 例外接受，非 blocker | 已接受例外 |

---

## 6. 未覆盖项与限制

1. **C=1024 仅覆盖概览/节点/连接/设置列表/代理子页**：规则、日志、订阅未单独出 1024 截图；≥1024 使用同一桌面布局，差异已由断点量测覆盖。
2. **纯黑仅覆盖 V-06/V-29 指定场景（概览、外观）**：spec 只对标注 PB 的条目要求纯黑。
3. **安全区未做刘海/手势条真机模拟**：headless Chromium 的 `env(safe-area-inset-*)` 解析为 0；CSS 已正确使用 env()。
4. **“刷新无主题闪烁”未做逐帧录屏**：以 head 内联脚本先于样式表执行、刷新后 data-theme/meta 正确作为结构证据。
5. **V-30 恢复备份的确认**未用真实 JSON 文件走到弹窗：复用同一 `MM.ui.confirm`，其余四入口已实际触发。
6. **V-10 嵌套内圆 = 外圆 − 内边距**未逐组件数值审计：本轮已量测各主容器圆角，未发现嵌套卡片反例。
7. **V-35 轮询变慢**由源码核对（status 15s、connections 5s、隐藏页 0），未做长时间计时。
8. **真实内核/真实订阅网络操作**未执行：本机无 mihomo 内核；UI 状态覆盖完整，真实链路性能/错误文案未验证。
9. **第 3 轮仅做窄屏定向确认**：未重拍全部 137 张截图；未涉及的页面沿用第 1/2 轮证据。代码改动仅限 CSS 字号/窄屏断点/hero 纵向布局/运行时长格式/输入框底色/搜索清除/订阅 URL 行/320 行宽，回归风险已由定向项与 sticky/overflow/pageerror 复核覆盖。

---

## 7. 最终结论

- **V-11 PASS**：字号、字重、tabular 三项均达到 spec；`.ov-stats .stat-value` 40/34（≤379px 28、≤339px 24）、`.traffic-cell b` 34/28/24、`.hero-node` 34/28。运行时长使用紧凑格式（42时55分 → 1天18时），390/375/320 均完整显示。
- **minor 0**：第 1 轮 5 个 minor 与第 2 轮窄屏截断已全部修复并复测通过；390/375/320 统计值与 hero 节点名 `scrollWidth = clientWidth`，无横向溢出，page error 0。
- **V-36 PASS**：1024 侧栏、<1024 dock、768 两列、599 单列、320 单列且无横向滚动；320 hero 改为纵向布局，节点名获得整行宽度。
- **V-38 仍为已接受例外**：体积按 spec §10.3-7 记录，不记 blocker；其余静态检查通过。
- 最终通过率：严格 **37 / 38 = 97.4%**；排除 V-38 已接受例外为 **37 / 37 = 100%**。blocker 0、major 0、minor 0。
- 本轮未修改 `web/**` 或 Go 代码，未提交 git。验收结束。
