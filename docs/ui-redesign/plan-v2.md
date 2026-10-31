# v2 UI 重构执行计划（剩余工作）

> 基线：分支 `feat/v2-ui-stability`，spec 见 `docs/ui-redesign/spec.md`
> 状态（2026-10-31）：P1 壳与 P2 token/基础组件已完成；7 个视图仍是占位符

## 阶段

### P1 壳与基础（已完成）
- `web/index.html`、`web/css/tokens.css`、`web/app.css`、`web/js/00~50/90` 已落地
- 验证：`node --check web/js/*.js` 通过

### P3 视图（已完成）
- [x] `views/overview.js` — spec 4.2：hero、流量图、出站模式、统计 tile、订阅摘要、网络身份、连通性
- [x] `views/nodes.js` — spec 4.3：代理组、节点列表（100/批）、延迟测试、选中
- [x] `views/subs.js` — spec 4.4：订阅卡、导入、刷新、编辑、删除
- [x] `views/rules.js` — spec 4.5：规则列表、搜索、规则集更新
- [x] `views/connections.js` — spec 4.6：连接列表（200 行上限）、关闭、全部关闭
- [x] `views/logs.js` — spec 4.7：SSE 日志、级别、暂停、清空、500 行上限
- 验证：`node --check`、`tests/ui_check.py`、Playwright 冒烟/成功路径均通过

### P4 设置与更多（已完成）
- [x] `views/settings.js` — spec 4.8/4.9：分组列表 + 12 个子页（代理/DNS/订阅拉取/国内 IP/自动选择/定时重启/内核/备份/存储/外观/启动与访问/诊断）
- [x] `views/more.js` — spec 4.10：移动端更多 sheet
- 验证：`node --check`、静态检查、Playwright（1440/1024/390/320，保存请求、脏状态、诊断刷新、更多 sheet）通过

### P5 收尾与验收（待做）

已知问题（截图复核发现，CSS/小修已完成大半）：
- [x] 订阅卡名称未渲染（`sub-name` 未赋值）— 已修
- [x] `.node-panel` / `.rule-table` / `.conn-table` 的 `overflow:hidden` 破坏 sticky 组头 — 已修为 `overflow:visible` + 表头顶部圆角
- [x] 移动端节点工具栏排序按钮被挤到下一行 — 已修
- [x] 规则页移动端页内「全部策略」与 AppBar 筛选重复 — 移动端隐藏页内筛选按钮
- [x] 桌面设置页 AppBar 标题显示子页名（应与右栏标题重复）— 桌面固定显示「设置」
- [x] 诊断页「实际模式」显示英文 key — 改为中文标签
- [x] JS 原始体积 246 KiB（gzip 58 KiB），超过 spec 150 KiB 目标；CSS 55 KiB、web 总 332 KiB 也超 300 KiB — 已按用户决策接受，记录在 spec §10.3-7 与 §8 体积例外
- [ ] 桌面设置左栏的「存储 / 诊断」摘要首次进入显示 —（root 才预加载）— 可选优化

- [ ] `tests/ui_check.py` 改为扫描 `web/**`（标签、id、脚本顺序/版本戳、禁 `window.confirm`/`color-mix`/`backdrop-filter`/外部 URL、关键类名、路由注册、`node --check` 全部 js）
- [ ] 删除不再引用的 `web/app.js`（旧面板）
- [x] visual-qa 第 1 轮：36/38 通过（V-11 minor + V-38 已接受例外），0 blocker / 0 major / 5 minor
- [x] 修复第 1 轮 5 个 minor：概览数字字号、浅色 sheet 输入框对比度、原生搜索清除按钮、手动导入重复文案、320px 诊断行标题截断
- [x] visual-qa 第 2 轮：37/38（V-38 例外），5 项修复全 PASS；新发现移动端统计值截断
- [x] 修复窄屏截断：运行时长紧凑格式、≤379/≤339 统计值降级、≤359 hero 纵向布局
- [x] visual-qa 第 3 轮最终确认：37/38（V-38 例外），37/37 排除例外，blocker/major/minor 全为 0
- [ ] 提交（不 push、不发 Release；提交前征得用户同意）

## 约束
- 只用现有 API（`docs/API.md`、`main.go registerRoutes`），不改后端
- 列表 keyed diff，轮询不整页重建；页面挂载一次
- 无框架/构建/CDN/Web Font；经典脚本 + `window.MM`
- 破坏性操作走 `MM.ui.confirm`
