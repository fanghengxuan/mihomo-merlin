# mihomo-merlin 面板 API 契约 v1

面板由一个静态前端（`web/`，被 Go `embed` 打包）和一个 Go HTTP 服务组成。
前端只与下面这些 HTTP 接口通信，**不得**直接访问 mihomo 的 9090 API（浏览器在 LAN 侧，`external-controller` 只监听 127.0.0.1）。

- 基础路径：`/api`
- 编码：请求/响应均为 `application/json`（SSE 除外）
- 时间：Unix 秒
- 出错统一返回 `{"ok":false,"error":"<可读中文原因>"}` + 合适的 HTTP 状态码
- 成功通常返回 `{"ok":true, ...}`

认证（可选）：当设置里配置了 `panelPassword` 时，除 `/api/login`、`/api/status`、`/` 静态资源外，
所有接口要求 `Cookie: mmp_token=<token>`。未登录返回 `401 {"ok":false,"error":"未登录"}`。

---

## 1. 状态

### `GET /api/status`

```json
{
  "ok": true,
  "panel": { "version": "1.0.0", "uptime": 812, "host": "192.168.50.1", "platform": "linux/arm" },
  "engine": {
    "running": true,
    "version": "1.19.33",
    "pid": 30211,
    "uptime": 720,
    "mode": "rule",
    "redirPort": 3333,
    "mixedPort": 7890,
    "memory": 42.7
  },
  "net": { "uploadSpeed": 12543, "downloadSpeed": 861233, "connections": 87 },
  "iptables": { "applied": true, "lastApply": 1792942000, "blockQuic": true },
  "subs": { "count": 2, "nodeCount": 84, "lastRefresh": 1792941900 }
}
```

`engine.running=false` 时 `engine` 其余字段可为 `null`/省略；`net` 全为 0。

### `GET /api/settings` / `PUT /api/settings`

```json
{
  "ok": true,
  "settings": {
    "mode": "rule",              // rule | global | direct
    "allowLan": true,
    "redirPort": 3333,
    "mixedPort": 7890,
    "logLevel": "info",          // silent|error|warning|info|debug
    "autoStart": true,           // 开机自启（面板守护 + 引擎跟随启用状态）
    "blockQuic": true,           // 阻断 UDP/443 逼回 TCP
    "routerSelf": false,         // 是否也代理路由器自身流量（默认关，避免回环）
    "subInterval": 6,            // 订阅自动更新间隔（小时，0=关闭）
    "panelPassword": "",         // 空 = 免密
    "rulePreset": "cn-direct",   // cn-direct | proxy-all
    "sniffer": true
  }
}
```

`PUT` 接受上述字段的任意子集，返回完整 `settings`。`mode` 变更会触发热重载。

---

## 2. 引擎控制

### `POST /api/engine/start` / `POST /api/engine/stop` / `POST /api/engine/restart`

```json
{ "ok": true, "message": "已启动" }
```

启动成功后需已应用 iptables 规则；失败返回 `{"ok":false,"error":"配置校验失败：..."}`。

### `POST /api/engine/reload`

重新生成配置并热重载（不中断已建立的连接）。

---

## 3. 订阅

### `GET /api/subs`

```json
{
  "ok": true,
  "subs": [
    {
      "id": "s_9f2c1a",
      "name": "机场A",
      "url": "https://example.com/api/v1/client/subscribe?token=***",
      "interval": 6,
      "enabled": true,
      "nodeCount": 42,
      "format": "clash",           // clash | base64 | uri-list | unknown
      "updatedAt": 1792941900,
      "status": "ok",              // ok | error | never
      "error": "",
      "traffic": { "upload": 10737418240, "download": 107374182400, "total": 536870912000, "expire": 1795000000 }
    }
  ]
}
```

`url` 在响应里对 token 部分打码；前端编辑时通过 `GET /api/subs/{id}?reveal=1` 取原文。

### `POST /api/subs`

Body：`{"name":"机场A","url":"https://...","interval":6}`
成功：`{"ok":true,"id":"s_9f2c1a"}`（会立即拉取一次；拉取失败仍创建并用 `status=error` 反馈）

### `PUT /api/subs/{id}`、`DELETE /api/subs/{id}`

删除后若引擎在运行则自动重载。

### `POST /api/subs/{id}/refresh`、`POST /api/subs/refresh`

重新拉取并解析；返回更新后的订阅对象 / `{"ok":true,"refreshed":2,"failed":0}`。

---

## 4. 节点与选择

### `GET /api/nodes`

由 mihomo `/proxies` 聚合而来：

```json
{
  "ok": true,
  "groups": [
    { "name": "🚀 节点选择", "type": "Selector", "now": "香港01", "nodes": [
        { "name": "香港01", "type": "Shadowsocks", "sub": "机场A", "delay": 86, "delayAt": 1792942000 },
        { "name": "日本02", "type": "Vless", "sub": "机场B", "delay": 132, "delayAt": 1792942000 }
    ]}
  ]
}
```

### `PUT /api/select`

Body：`{"group":"🚀 节点选择","node":"香港01"}`

### `GET /api/delay?name=香港01&url=http://www.gstatic.com/generate_204`

单节点测速，返回 `{"ok":true,"name":"香港01","delay":86}`；超时 `delay:0`。

### `POST /api/delay`

Body：`{"names":["香港01","日本02"],"group":"🚀 节点选择"}`
批量测速（服务端并发 ≤ 8，单节点超时 3s），返回 `{"ok":true,"results":{"香港01":86,"日本02":0}}`。

---

## 5. 规则 / 连接 / 日志

- `GET /api/rules` → `{"ok":true,"rules":[{"type":"RuleSet","payload":"cn-ip","proxy":"DIRECT"}]}`
- `GET /api/connections` → `{"ok":true,"uploadTotal":0,"downloadTotal":0,"connections":[{"id":"...","host":"www.google.com","rule":"Match","chains":["🚀 节点选择","香港01"],"upload":123,"download":456,"start":"..."}]}`
- `GET /api/logs/stream`（SSE，事件 `data: {"type":"info","payload":"..."}`）── 由服务端拉起 mihomo `/logs?level=...` 转发
- `GET /api/traffic/stream`（SSE，`data: {"up":123,"down":456}`，每秒一条）

SSE 断线由前端自动重连（EventSource 自带）。

---

## 6. 前端约定

- 纯静态：`index.html` + `css/tokens.css` + `app.css` + `js/**`（经典脚本 + `window.MM`），**不引入任何 CDN / npm 依赖**，可离线使用（路由器场景）。
- 风格参考 FlClash：四级明度表面、圆角卡片、桌面左侧栏 / 移动底部 dock、延迟只染文字（<600ms 成功 / ≥600ms 警告 / 超时错误 / 未测中性）。设计规范见 `docs/ui-redesign/spec.md`。
- 所有文案中文；错误 toast 显示服务端 `error` 原文。
- 轮询：`/api/status` 每 10s（低端 15s，页面可见时），`/api/nodes` 每 10s，连接页 2s，日志与流量走 SSE。
- 写操作按钮需有 loading 与二次确认（删除订阅、停止引擎）。

---

## 7. 内核管理

### `GET /api/kernel`

```json
{
  "ok": true,
  "kernel": {
    "path": "/koolshare/bin/mihomo",
    "version": "Mihomo Meta v1.19.33 linux arm with go1.26.10",
    "size": 7973176,
    "sha256": "6bc1f3ecc6c8…",
    "modTime": 1793036000,
    "backupVersion": "Mihomo Meta v1.19.33 …",
    "backupTime": 1793030000
  }
}
```

`backupVersion` 为空表示没有可回滚的旧版本。

### `POST /api/kernel/upload`（multipart/form-data，字段名 `file`）

上传新的 mihomo 内核，支持两种格式：

- 原始可执行文件（linux-armv7）
- GitHub release 的 `.gz`（服务端自动解压）

服务端校验顺序（任一步失败都不会动现有内核）：

1. 大小 ≤ 96 MB
2. ELF 魔数 + 32 位 + `e_machine=40`（ARM）；UPX 压缩过的内核同样接受
3. 跑 `mihomo -v`，必须能执行
4. 用**当前配置**跑 `mihomo -t`，必须接受本插件的配置
5. 备份旧内核到 `<dataDir>/kernel/`（保留最近 3 个）
6. 替换 → 若引擎在运行则重启；**启动失败会自动回滚到旧内核**

```json
{ "ok": true, "message": "内核已更新并重启", "version": "Mihomo Meta v1.19.33 …",
  "restarted": true, "kernel": { "…": "同上" } }
```

校验失败返回 400，例如：

```json
{ "ok": false, "error": "不是 32 位 ELF（当前是 64 位，请下载 linux-armv7 而不是 linux-arm64）" }
```

### `POST /api/kernel/rollback`

把当前内核与上一个备份对调（并把当前版本也存为新的备份，可来回切换），必要时重启引擎。

### `POST /api/subs/import`

手动导入配置（订阅地址在路由器上拉不动时的兜底）。Body：

```json
{ "name": "机场A", "content": "proxies:\n  - {name: HK01, type: ss, ... }" }
```

`content` 支持 Clash YAML / base64 节点列表 / 明文节点链接，上限 16 MB。
成功返回 `{"ok":true,"id":"s_xxx","sub":{…}}`；这类订阅 `url` 为空，不参与自动更新。

### `GET /api/progress`（SSE）

订阅任务进度流。添加/更新/导入订阅时推送，用来在前端展示“现在在干什么”。

```json
{ "op":"fetch", "sub":"机场A", "step":"download", "msg":"已下载 20480 字节，开始识别格式",
  "at":1793108000, "done":false, "err":false }
```

`step` 取值：`start` / `connect` / `proxy` / `retry` / `download` / `parse` / `write` / `reload` / `done` / `error`。
`done=true` 或 `err=true` 表示该任务结束。

### `GET /api/rules/set` 与 `POST /api/rules/update`

国内 IP 规则集（用于 `RULE-SET,cn-ip,DIRECT`）的订阅与更新。

`GET /api/rules/set` → `{"meta":{"count":8123,"bytes":180422,"updatedAt":…,"source":…},"auto":true,"hours":24,"preset":[…]}`

`GET /api/rules/set` 还会返回可选预设：

```json
{"preset":"chnroutes2","presets":[
  {"id":"chnroutes2","name":"chnroutes2（推荐）","note":"…","lines":"≈3900"},
  {"id":"17mon","name":"17mon china_ip_list","lines":"≈7400"},
  {"id":"loyalsoldier","name":"Loyalsoldier/geoip","lines":"≈10000"}]}
```

`POST /api/rules/update` 支持 `{"preset":"17mon"}` 或 `{"url":"…"}`（显式 url 优先）→ 下载 → 校验 CIDR → 原子替换 → 热重载内核。

### `GET /api/netinfo`

网络状态（出口 IP 对比）。首次调用或缓存过期时返回 `{"pending":true}`，前端 2 秒后重试；
`?refresh=1` 强制同步探测。

```json
{"ok":true,"info":{
  "direct":{"ip":"1.2.3.4","countryCode":"US","country":"美国","isp":"…","kind":"机房/云","latencyMs":320},
  "proxy":{"ip":"5.6.7.8","countryCode":"JP","country":"日本","isp":"…","kind":"住宅/商业","latencyMs":95},
  "sameIp":false,"proxied":true,"node":"[机场A] 日本 01","nodeDelay":88,
  "connections":12,"memoryMb":41.2,"upTotal":1234567,"downTotal":98765432,"updatedAt":…}}
```

### 1.4.0 新增接口

#### `GET /api/connections`（扩展）
保留旧字段（`connections`/`uploadTotal`/`downloadTotal`），新增扁平化列表：

```json
{"ok":true,"count":2,"up":1235,"down":5680,"rows":[
  {"id":"conn-1","host":"www.google.com","dest":"142.250.1.1:443","network":"tcp","type":"Redir",
   "rule":"RuleSet","rulePayload":"cn-ip","chains":["🚀 节点选择","[机场A] 香港 01"],
   "upload":1234,"download":5678,"start":"2026-10-28T23:00:00Z"}]}
```
`host` 为空时用 `dest` 兜底；内核未运行时 `rows: []` 且带 `err`。

#### `GET /api/service`
```json
{"ok":true,"core":{"reachable":true,"version":"Mihomo Meta v1.19.33","apiLatencyMs":3},
 "iptables":{"applied":true,"lastApply":1761700000},"ports":{"redir":3333,"mixed":7890},
 "mode":"rule","modeActual":"rule"}
```
`modeActual` 直接读内核 `/configs`，用于判断设置是否真的生效。

#### `GET /api/intranet`
```json
{"ok":true,"lanIp":"192.168.50.1","iface":"br0","ipv4":["192.168.50.1"],"hostname":"TUF-AX3000"}
```
优先 `nvram get lan_ipaddr`（3 秒超时），失败回落到接口地址；取不到时仍 200 并带 `err`。

#### `GET /api/ipquality`
```json
{"ok":true,"quality":{"ip":"1.2.3.4","type":"hosting","level":"normal",
 "checks":{"proxy":false,"vpn":false,"tor":false},"source":"ipapi.is","err":""}}
```
经代理探测（api.ipapi.is → ipapi.co 回退），缓存 120 秒，`?refresh=1` 强刷；失败时 `level=unknown` + `err`，绝不 5xx。

#### 新增设置项（`PUT /api/settings`）
| 字段 | 范围/格式 | 说明 |
|---|---|---|
| `autoRestart` | bool | 定时重启开关 |
| `restartEveryHours` | 1..168 | 每 N 小时重启内核 |
| `restartAt` | `HH:MM` 或空 | 每天固定时刻重启，非法 400 |
| `autoIntervalSec` | 30..3600 | 自动选择（url-test）健康检查间隔 |
| `autoToleranceMs` | 0..1000 | 切换容差，避免节点间抖动 |

`/api/status` 的 `engine` 新增 `lastRestartAt`、`nextRestartAt`（0 表示无）。

#### `GET /api/reachability`

走当前出站实测各站点可达性（并发、单目标 6 秒超时、缓存 45 秒、`?refresh=1` 强刷，永不 5xx）：

```json
{"ok":true,"updatedAt":1761700000,"targets":[
 {"id":"google","name":"Google","url":"https://www.google.com/generate_204",
  "status":"ok","httpCode":204,"latencyMs":95,"err":""},
 {"id":"chatgpt","name":"ChatGPT","url":"https://chat.openai.com/cdn-cgi/trace",
  "status":"restricted","httpCode":403,"latencyMs":210,"err":""}]}
```

`status`：`ok`（2xx）/ `restricted`（401/403/451，能连通但被限制）/ `timeout` / `fail` / `unknown`（内核未运行）。

#### `DELETE /api/connections/{id}` 与 `DELETE /api/connections`

关闭单条连接 / 全部连接。返回 `{"ok":true}` / `{"ok":true,"closed":N}`；内核未运行时 `{"ok":false,"err":"内核未运行"}`。

## 备份与恢复（1.7.0）

### GET /api/backup
导出完整备份（JSON 下载）：`kind`、`format`、`panelVersion`、`exportedAt`、`settings`、
`subs`（含订阅地址与令牌）、`providers`（订阅 id → 节点文件内容）、`rulesMeta`。

### POST /api/restore
请求体为上面的备份 JSON（≤16MB）。校验 `kind == "mihomo-merlin-backup"` 且 `format <= 1`；
设置以默认值起步再覆盖备份里出现的字段（老备份缺字段不会把 allowLan/autoStart 清成 false）；
订阅与节点文件一并写回，内核运行中会热重载。

## DNS 设置（1.7.0）

`settings` 新增：`dnsEnable`、`dnsMode`（redir-host|fake-ip）、`dnsListen`、`dnsNameserver`、
`dnsFallback`、`dnsFakeIpRange`。开启后配置里生成 `dns:` 段，fake-ip 模式自动带 `fake-ip-filter`。

## 订阅节点过滤（1.7.0）

订阅对象新增 `includeFilter` / `excludeFilter`（正则），写到 mihomo `proxy-provider` 的
`filter` / `exclude-filter`，由内核加载节点时过滤。
