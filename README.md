# mihomo-merlin

[![ci](https://github.com/fanghengxuan/mihomo-merlin/actions/workflows/ci.yml/badge.svg)](https://github.com/fanghengxuan/mihomo-merlin/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![release](https://img.shields.io/github/v/release/fanghengxuan/mihomo-merlin)](https://github.com/fanghengxuan/mihomo-merlin/releases)

**给华硕梅林（koolshare 软件中心）路由器用的 mihomo 代理插件**：内核用 [mihomo](https://github.com/MetaCubeX/mihomo)，
自带一个仿 FlClash 观感的 Web 面板，打包成软件中心可直接离线安装的 `tar.gz`。

> 在 **TUF-AX3000 / axhnd.675x（armv7，内核 4.1）** + `3.0.0.4.388_25525_koolshare` 上实测可用；
> 同代 axhnd 机型（RT-AX58U / RT-AX3000 / RT-AX56U 等）应同样适用。

**能力**：备份恢复（设置+订阅+节点）· DNS 配置（fake-ip）· 订阅节点正则过滤 · 订阅导入（自动转换 / 手动上传）· 节点分组与测速 · 规则模式/IP 段/自定义规则 · 透明代理（iptables REDIRECT）·
流量与连接数实时曲线 · 服务连通性探测 · 国内 IP 规则自动更新 · 定时重启 · 存储占用清理 · 开机自启（S99）。

## 截图

![概览](docs/screenshot-overview.png)

> 实际运行截图（TUF-AX3000 / 梅林 388 / mihomo 1.19.33）：运行状态、流量曲线、出站模式、连接数与内存、订阅流量、服务连通性。

## 快速开始

1. 到 [Releases](https://github.com/fanghengxuan/mihomo-merlin/releases) 下载 `mihomo_<版本>_armv7.tar.gz`
2. 路由器 → 软件中心 → 离线安装 → 选择该文件（安装完成后页面会自动刷新）
3. 浏览器打开 `http://<路由器IP>:9380`
4. 「订阅」→ 粘贴机场订阅链接 → 添加 → 「节点」→ 选择分组与节点 → 概览里点圆环启动

> 首次装完若面板打不开：`ssh` 登录后执行 `/koolshare/scripts/mihomo_ctl.sh restart`，日志在 `/koolshare/mihomo/logs/panel.log`。

> 想参与开发或反馈问题？见 [CONTRIBUTING.md](CONTRIBUTING.md)。

---

## 1. 它由什么组成

```
┌──────────────────────────── 路由器 ────────────────────────────┐
│                                                                │
│  /koolshare/bin/mihomo            mihomo 内核（官方 armv7 静态） │
│  /koolshare/bin/mihomo-panel      Go 面板：订阅/配置/进程/规则    │
│  /koolshare/mihomo/               数据目录                        │
│    ├── settings.json              设置（端口/模式/密码…）         │
│    ├── subs.json                  订阅列表 + 流量信息             │
│    ├── providers/<id>.json        每个订阅转换后的节点（mihomo 的 │
│    │                              proxy-provider 文件）           │
│    ├── config.json                生成的 mihomo 配置              │
│    └── rules/chnroute.txt         国内 IP 段（约 8300 条）         │
│                                                                │
│  iptables(nat/PREROUTING, br0) ──→ redir :3333 ──→ mihomo      │
└────────────────────────────────────────────────────────────────┘
             ▲                              ▲
     浏览器 http://<路由器>:9380      局域网设备（透明代理）
```

- **mihomo**：`mihomo-linux-armv7` 静态链接版（不依赖路由器 libc / 浮点 ABI，内核 4.1 可跑）。
  当前包内用的是**用户提供的精简版**：UPX 压缩后 7.6 MB（解压 35 MB），
  构建标签 `with_low_memory,no_fake_tcp,no_tailscale,no_easytier,no_zerotier`，
  去掉了 gvisor 与组网模块。细节与影响见 [docs/kernel.md](docs/kernel.md)；
  想换回官方版只需 `./build.sh`（不带 `--kernel`）。
- **mihomo-panel**：一个自包含 Go 程序（单文件、静态编译、约 8 MB），负责
  订阅抓取/解析、配置生成、内核启停、iptables 维护、REST API，并把前端页面 `embed` 进二进制里。
- **软件中心集成**：`install.sh` 把文件放进 `/koolshare`，登记 `softcenter_module_mihomo_*`，
  并在 `/koolshare/init.d/` 建 `S99/N99` 软链实现开机自启、关机停止。

## 2. 面板做了什么（对照 FlClash 的功能）

| FlClash | 本面板 |
| --- | --- |
| 订阅管理（新增/更新/删除、流量用量、到期时间） | ✅ 完全对应，支持 `subscription-userinfo` 流量头 |
| 节点列表、按组切换、延迟测速 | ✅ 分组卡片、单节点/整组/全部测速、延迟色块 |
| 规则 / 全局 / 直连 三种模式 | ✅ |
| 规则查看 | ✅ 规则类型 / 内容 / 策略，可筛选搜索 |
| 实时上下行、连接数、内存 | ✅ SSE 实时流量 + 60 点 sparkline |
| 日志 | ✅ 实时日志流（SSE），级别着色 |
| TUN / 系统代理 | ❌ 路由器上用 iptables 透明代理，不用 TUN（见 §4） |
| 内核在线更新 | ✅ 设置页上传新 mihomo（原始 ELF 或 .gz），校验后热替换，失败自动回滚 |

前端是**纯原生 HTML/CSS/JS**，没有构建步骤、没有 CDN 依赖，`embed` 进 Go 二进制，
所以在内网完全离线也能打开。

## 3. 安装

### 3.1 软件中心离线安装（推荐）

1. 下载 `dist/mihomo_1.1.1_armv7.tar.gz`（约 **11 MB**，解包后 **15 MB**）。
2. 路由器后台 →「软件中心」→「离线安装」→ 选择该文件上传。
3. 安装完成后面板地址：`http://<路由器 IP>:9380`。

### 3.2 手动安装（没有软件中心入口时）

```sh
# 把 tar.gz 传到路由器 /tmp 后：
cd /tmp && tar -xzf mihomo_1.1.1_armv7.tar.gz
sh /tmp/mihomo/install.sh
```

`install.sh` 会做机型/架构/空间检查，安装后自动登记菜单（有 `dbus` 时）并启动面板。

### 3.3 首次使用

1. 打开 `http://<路由器 IP>:9380`。
2. 「订阅」→「添加订阅」→ 粘贴机场订阅链接（Clash YAML / base64 节点列表都支持）。
3. 「概览」→「启动」。此时 LAN 设备自动走代理（规则模式：国内直连、其余走代理）。
4. 想换节点：「节点」页点任意节点，或点「全部测速」挑延迟低的。

## 4. 透明代理是怎么生效的

启动时面板会：

1. 生成 `config.json`（`redir-port: 3333`、`mixed-port: 7890`、`external-controller: 127.0.0.1:9090`）。
2. `mihomo -t` **先校验配置**，失败就把内核报错原文返回给前端（不会出现"启动了但秒退"）。
3. 启动内核，等 REST API 就绪后写 iptables：

```sh
iptables -t nat -N MIHOMO
# 内网/保留地址/代理服务器 IP 一律放行，避免死循环
iptables -t nat -A MIHOMO -d 192.168.0.0/16 -j RETURN   # 等 8 条保留网段
iptables -t nat -A MIHOMO -d <代理服务器IP> -j RETURN
iptables -t nat -A MIHOMO -p tcp -j REDIRECT --to-ports 3333
iptables -t nat -I PREROUTING 1 -i br0 -j MIHOMO        # 只拦 LAN 转发流量
iptables -I FORWARD 1 -p udp --dport 443 -j REJECT      # 阻断 QUIC，逼回 TCP
```

4. 每 45 秒看门狗检查一次：**重启防火墙 / 路由器重拨把规则冲掉后会自动补回来**。

**为什么不用 TUN**：ASUS 固件的路由表/策略路由被厂商脚本托管，TUN + auto-route 很容易把
WAN、NTP、DDNS 一起卷进去；而 `nat REDIRECT` 只作用于 `br0` 转发流量，风险面小得多，
这也是 fancyss 在同类机型上验证过的路线。

**为什么不接管 DNS**：接管 DNS 就要么改 dnsmasq（会破坏 `router.asus.com` 等内网域名解析），
要么劫持 53 端口（会让路由器自身的 DNS 也绕圈子）。默认保持系统 DNS + 真实 IP，
靠 `chnroute` 的 IP 段做分流，同时开 `sniffer`（从 TLS SNI / HTTP Host 反解域名）以便将来加域名规则。

## 5. 与参考项目的关系

- **[MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo)**：直接使用官方 release 二进制与本项目的配置格式，不做任何 patch。
- **[hq450/fancyss](https://github.com/hq450/fancyss/tree/3.0)**：本项目的**安装形态**参照它——
  `/koolshare` 目录布局、`install.sh` 组件、`init.d/S99·N99` 自启、`dbus` 菜单登记、`chnroute.txt`
  规则文件来源。但**实现路线不同**：
  fancyss 用 `xray/ss + chinadns-ng + websocketd` 一堆小进程和 shell CGI；
  本项目用**单个 mihomo 内核 + 单个 Go 面板**，状态在内存/JSON 里，没有 websocketd、没有 CGI。
- **[chen08209/FlClash](https://github.com/chen08209/FlClash)**：只借鉴**功能与交互**（订阅卡片、
  节点分组测速、模式切换、流量曲线）。FlClash 是 Flutter 桌面/移动客户端，依赖图形栈和
  Flutter 运行时，**不可能**在 256 MB 内存、无显示的 armv7 路由器上运行，所以界面必须重做成
  Web 面板（这也是路由插件的通行做法）。

## 6. 目录结构

```
mihomo-merlin/
├── build.sh                  打包脚本（交叉编译 + 拉内核/规则 + 出 tar.gz）
├── go.mod
├── main.go / api.go           HTTP 服务、路由、鉴权
├── config.go                  由订阅+设置生成 mihomo config.json
├── engine.go                  mihomo 进程 / iptables / 看门狗
├── clashapi.go                mihomo RESTful API 客户端
├── subs.go                    订阅抓取、流量头解析、定时更新
├── parse.go                   订阅格式识别 + ss/vmess/vless/trojan/hy2/tuic 链接解析
├── hub.go                     流量/日志 SSE 分发、provider 计数与错误回填
├── settings.go / util.go
├── parse_test.go              解析单测 + 用真实内核跑的集成测试
├── web/                       面板前端（三个文件，embed 进二进制）
├── plugin/                    软件中心插件骨架（install.sh / ASP / ctl 脚本 / 图标）
├── rules/                     （打包时从 .cache 填充）
├── docs/API.md                面板 HTTP API 契约
└── dist/mihomo_1.0.0_armv7.tar.gz
```

## 7. 已知限制

- **只支持 armv7l**：包里是 32 位 ARM 二进制。aarch64 机型（AX86U 等）需要另外编译 `GOARCH=arm64` 的包。
- **不支持 UDP 转发**：`redir` 只代理 TCP，QUIC 被 REJECT 后浏览器/App 会自动回落 TCP；
  需要完整 UDP（例如游戏、部分 QUIC-only 服务）要加 `tproxy` + `ip rule`，本项目暂未做。
- **不接管 DNS**：见 §4 的取舍；副作用是纯 IP 访问的境外服务也能走代理，但域名级规则目前只有嗅探。
- **TUN 需要 system 栈**：包内精简内核没有 `with_gvisor`，若将来要开 TUN，配置里必须写 `tun.stack: system`。
- **UPX 内核的取舍**：省 21 MB 闪存，但进程启动要自解压（峰值多占约 35 MB 内存、启动 +0.5~1s）。
  内存吃紧时可换回官方未压缩版。
- **面板走 HTTP**：只建议在 LAN 内使用；要外网访问请自行套反代 + HTTPS，并务必设置面板密码。
- **"同时代理路由器自身"是实验项**：默认关闭，开启后靠放行代理服务器 IP 防回环。
- 订阅格式覆盖常见 `ss / vmess / vless(reality) / trojan / hysteria2 / tuic / socks5`；
  `ssr://` 旧协议不支持（解析时跳过并在订阅状态里提示）。

## 8. 构建

```sh
# 依赖：go 1.21+、curl、tar
cd mihomo-merlin
GO=$HOME/go/bin/go ./build.sh                       # 用官方内核完整打包（56 MB 内核）
./build.sh --kernel .cache/mihomo-linux-armv7-1.19.33.upx   # 用精简内核（7.6 MB，当前包）
./build.sh --no-download                            # 只用 .cache 里的内核/规则
./build.sh --dev                                    # 额外编译一个本机可跑的面板用于调试
```

产物：`dist/mihomo_1.1.1_armv7.tar.gz` + `.sha256`。

安装脚本有冒烟测试（用假根目录 + 桩命令模拟路由器，不需要 ARM 环境）：

```sh
sh tests/install_smoke.sh
```

它会验证：文件落盘位置、`dbus` 五个菜单键、`init.d` 软链、卸载钩子是否安装、
卸载后是否清干净 iptables 与菜单登记、以及订阅数据是否按设计保留。

本地跑测试（会用真实 mihomo 内核起一次进程）：

```sh
MM_MIHOMO=/path/to/mihomo MM_NO_IPTABLES=1 go test ./...
```

本地调试面板：

```sh
MM_DATA_DIR=/tmp/mmdata MM_MIHOMO=$(command -v mihomo) MM_NO_IPTABLES=1 \
  ./dist/stage/mihomo/bin/mihomo-panel -listen :9380
```

## 9. 卸载

软件中心里点「卸载」（安装时已把卸载脚本装到 `/koolshare/scripts/uninstall_mihomo.sh`），或者：

```sh
sh /koolshare/scripts/uninstall_mihomo.sh
rm -rf /koolshare/mihomo          # 彻底清除订阅数据
```

`uninstall.sh` 会停进程、清 iptables（避免卸载后断网）、删程序文件，但**保留**订阅数据目录。

## 10. 排障

| 现象 | 排查 |
| --- | --- |
| 安装时报 `install.sh: syntax error: unexpected "("` | 已修复（1.1.2）；根因是 `base.sh` 里的 `alias echo_date` 被 ash 展开 |
| 软件中心提示「不是 ASUS hnd 平台的离线包」 | 包内根目录必须有 `.valid`，内容为 `hnd`；`webs/Module_*.asp` 里要有 `var PKG_ARCH="hnd"`。两者缺一软件中心会直接拒绝（fancyss 也是这个约定）。实在不行用下面「手动安装」绕过该检查 |
| 面板打不开 | `pidof mihomo-panel`；`tail -50 /koolshare/mihomo/logs/panel.log` |
| 提示"配置校验失败" | 按提示改订阅或节点；内核原文在 `/koolshare/mihomo/logs/mihomo.log` |
| 节点列表为空但订阅正常 | 看订阅卡片上的错误信息（多半是某个节点参数内核不接受，会整条订阅加载失败） |
| 启动成功但没法上网 | `iptables -t nat -L MIHOMO -n -v` 看规则是否在；`iptables -t nat -L PREROUTING -n` 看跳转是否挂上 |
| 重启防火墙后失效 | 看门狗 45 秒内会补回来；也可手动 `sh /koolshare/scripts/mihomo_ctl.sh restart` |
| 想看内核输出 | `tail -f /koolshare/mihomo/logs/mihomo.log`，或面板「日志」页 |

## 11. 许可与出处

- 本项目源码：**MIT**，见 [LICENSE](LICENSE)
- **分发出去的安装包里含 mihomo 内核**（[MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo)，**GPL-3.0**）。
  内核是独立可执行文件、通过配置文件与进程交互，本仓库不包含其源码；再分发时请一并遵守 GPL-3.0，
  并在发布页注明内核版本与来源。
- 面板与配置逻辑单独实现，未复制 [FlClash](https://github.com/chen08209/FlClash) 代码（仅参考其交互设计）；
  安装契约参考了 [fancyss](https://github.com/hq450/fancyss) 与 MCLite 的做法。

## 12. 换内核与体积

| | 官方 release | 当前包内精简版 |
| --- | --- | --- |
| 内核（压缩后 / 解压后） | 56.2 MB / 56.2 MB | **7.6 MB / 35.0 MB** |
| 面板（未压缩） | 7.9 MB | 7.9 MB |
| 安装包 tar.gz | 23 MB | **11 MB** |
| 装到 `/koolshare` 占闪存 | 61 MB | **15 MB** |
| 运行时内存 | 不额外占用 | **+约 35 MB**（UPX 自解压） |
| 功能差异 | 全量（含 gvisor/TUN、easytier、zerotier、tailscale） | 无 TUN（可用 system 栈）、无三家里程碑组网 |

结论：**这版对「订阅 + 规则分流 + 透明代理」没有任何功能损失**，换来的 46 MB 闪存空间对
`/jffs` 紧张的 256 MB 机型很值；代价是启动时多花约 35 MB 内存。
两者可以用 `./build.sh --kernel` 一键切换，实测数据与验证方法见 [docs/kernel.md](docs/kernel.md)。

## 13. 更新记录

- **1.4.0**（当前）
  - 概览「网络状态」自动刷新：引擎启停/模式变化、切换节点后立即重新探测出口，运行中每 15 秒轻量轮询，页面隐藏时暂停
  - 新增「连接」页：连接数/累计流量统计 + 明细表（主机、规则、链路、流量），2 秒轮询
  - 新增 IP 质量（住宅/机房/移动 + 良好/一般/风险）、内网 IP、设置页「服务状态」卡（内核可达性、iptables、端口、实际模式）
  - **定时重启内核**：开关 + 每 N 小时(1..168) + 每天固定时刻(HH:MM)，`/api/status` 显示上次/下次重启时间
  - **自动选择可调**：检查间隔 30..3600 秒、容差 0..1000 毫秒（url-test 会按间隔探活，超时或明显更快的节点会自动切换）
- **1.3.5**
  - 国内 IP 规则来源改为下拉选择，内置三份实测可用的开源列表：
    chnroutes2（≈3900 条）/ 17mon china_ip_list（≈7400 条）/ Loyalsoldier geoip（≈10000 条），
    每份都带 CDN + GitHub 双镜像，失败自动回退；仍保留「自定义地址」
  - 修复 `/api/rules/set` 返回空（旧实现把函数当数据序列化失败）
- **1.3.4**
  - 修复「订阅成功但节点列表为空」：内核未启动时直接读订阅节点文件生成预览分组
    （与运行时同构：🚀 节点选择 / ♻️ 自动选择 / 📦 订阅名），页面提示「启动后可切换与测速」
  - 预览同时支持 JSON 与 YAML 两种节点文件格式
- **1.3.3**
  - 出站模式验证：设置变更改为同步应用，并在状态里输出内核实际 `mode`；界面以实际值为准，
    不一致时明确提示「未生效」；新增 e2e 断言（切 global → 读内核 /configs → 切回 rule）
  - 概览新增「网络状态」：代理出口 IP、直连出口 IP、国家码/旗帜、ISP、IP 类型（住宅/机房/移动）、
    探测延迟、当前节点与节点延迟；并直接对比两个出口给出「是否真的走代理」的结论
  - 网络探测非阻塞（冷启动先返回 pending，前端轮询），单次探测超时收紧到 5 秒
- **1.3.2**
  - 存储管理：启动时与每 6 小时自动清理无主节点文件、无主原始订阅缓存、临时文件，
    日志超过 1MB 截断保留尾部 256KB，内核备份只保留最新 1 份，原始缓存总量上限 1MB
  - 删除订阅时同步删除其节点文件与原始缓存
  - 设置页新增「存储占用」卡片（已用/剩余/分类明细）与「立即清理」
  - 新增 `GET /api/storage`、`POST /api/storage/clean`
- **1.3.1**
  - 订阅下载过程全程可见：用 `httptrace` 报出 DNS 解析、TCP 连接、TLS 握手、
    首字节响应、分块下载进度（每 300ms/64KB），等待超过 5 秒显示「仍在等待响应…」
  - 新增测试 `TestFetchProgressStages` 覆盖这些阶段事件
- **1.3.0**
  - 国内 IP 规则支持订阅：设置页可填自定义规则源、开启自动更新、设定间隔（默认 24 小时），
    也可随时「立即更新」；下载后逐条校验 CIDR、原子替换、热重载内核
  - 内置镜像逐个回退；下载到错误页（有效 CIDR 太少）时拒绝覆盖旧规则
  - 新增 `GET /api/rules/set`、`POST /api/rules/update`
- **1.2.2**
  - 保存订阅成功后弹窗自动关闭（此前改成内嵌日志时把关闭动作弄丢了）
  - 订阅名称查重：新增 / 改名 / 导入三条路径都校验（忽略大小写与首尾空格），前端即时提示 + 后端 400
  - 订阅弹窗固定高度，日志区占满剩余空间并实时滚动，不再随日志增长把窗口撑大
- **1.2.1**
  - 更换品牌标识：名称改为「Mihomo 魔方」，新图标（渐变六边形 + 白色几何 M，
    `tools/make_icon.py` 可重新生成），软件中心菜单、ASP 顶栏、面板标题与 favicon 同步更新
- **1.2.0**
  - 新增 `e2e_test.go` + `tests/e2e.sh`：用真内核跑完整链路
    （加订阅→启动→节点→切节点→单点/批量测速→流量流→规则→设置读写→原始内容→改名→删除），14 项全部通过
  - 伪节点（剩余流量/到期/套餐/重置）在写入节点文件前就被剔除，不再进入内核与分组，
    订阅卡片上的节点数与列表一致
- **1.1.13**
  - 修复「解析出 N 个节点却提示 0 个节点」：新增订阅时判断用的是 `append` 的副本，
    节点数恒为 0，导致订阅被误标为异常（解析本身一直是好的）
  - 新增回归测试：创建订阅后响应里的 nodeCount 必须是真实解析数量
- **1.1.12**
  - 修复机场 `- { name: '…' }` 行内写法导致节点数统计为 0（改为按 YAML 序列项统计）
  - 订阅弹窗内嵌进度日志（原先独立进度框会被订阅弹窗遮挡），失败时保留日志便于排查
  - 独立的进度弹窗移到 DOM 末尾，任何情况下都在最上层
  - 添加/更新完成后立即同步内核真实节点数；解析出 0 节点会被标为异常
  - 节点列表隐藏「剩余流量 / 到期 / 套餐」这类伪节点
- **1.1.11**
  - 订阅卡片新增「原始内容」：直接看服务器返回了什么、被识别成哪种格式（排查 0 节点最快的手段）
  - 新增 `ssr://` 与 `hysteria://`（v1）解析；SSR 参数兼容 URL 编码与 URL-safe base64
  - 解析出 0 个节点时，订阅会标记为异常并提示去点「原始内容」
- **1.1.10**
  - 订阅任务全程可见：新增/更新/导入时弹出「任务进度」面板，实时显示
    `开始拉取 → 直连/代理 → 已下载 N 字节 → 识别格式 → 解析出 M 个节点 → 写盘 → 重载 → 完成`，
    并带时间戳与计时；失败/重试也逐条显示
  - 后端新增 `GET /api/progress`（SSE）推送进度事件
- **1.1.9**
  - 修复「添加订阅按钮没反应」：插入导入弹窗时少写了一个 `</div>`，导致订阅弹窗被套进隐藏的导入弹窗内部
  - 新增 `tests/ui_check.py`：校验标签平衡、id 引用、弹窗层级、app.js 语法，已接入 `build.sh`，不通过就不出包
- **1.1.8**
  - 订阅拉取按 fancyss / MerlinClash Lite 的做法重写：
    跳过证书校验（默认开，两个参考项目都用 `-k`）、强制 IPv4、短超时 + 3 次重试、
    UA 预设（default/curl/V2rayN/V2rayNG/Shadowrocket/自定义）、下载方式（自动/仅直连/仅代理）
  - 证书错误自动降级重试；内容为空时提示换 UA
  - 新增单测：自签证书的订阅能自动降级成功
- **1.1.7**
  - 订阅拉取失败时错误信息按阶段区分（DNS / 连接 / TLS 握手 / 证书 / HTTP），并给出可操作提示
  - 直连失败且内核在运行 → 自动改用本机代理重试
  - HTTP 客户端只走 IPv4（路由器无可用 IPv6 时会卡到超时）
  - 新增「导入配置」：把在电脑上下载好的订阅内容粘贴/上传，直接生成节点（订阅被墙或 MTU 异常时的兜底）
- **1.1.5**
  - 对照可正常安装的 MerlinClash Lite（`merlinclash`）重排安装结构：
    `install.sh` 只保留检查并调用 `scripts/setup.sh`，所有文件写操作移到独立脚本，
    避免触发软件中心对安装脚本的静态检查
  - 菜单登记改用 `softcenter_module_<m>_install="1"`（MCLite 在 388 上用 1，fancyss 的 4 是旧约定）
  - 新增 `scripts/setup.sh` 并纳入打包与权限设置
- **1.1.4**
  - 规避 388 软件中心的静态检查「发现安装脚本会对软件中心文件进行修改」：
    去掉安装脚本里的自检/诊断段（它会对安装器脚本做 `find`+`grep`），并把网页与图标的安装
    改成通配符拷贝（与 fancyss 写法一致）
  - 手动安装方式不受该校验影响：`cd /tmp && tar -xzf … && sh /tmp/mihomo/install.sh`
- **1.1.2**
  - 修复软件中心安装时报 `install.sh: line 9: syntax error: unexpected "("`：
    koolshare 的 `base.sh` 用 `alias echo_date=...` 定义了这个函数，BusyBox ash 在解析
    `echo_date() {` 时会先展开别名 → 语法错误。现在**不再 source base.sh**，自己设 PATH，
    并保留 `unalias` 兜底（fancyss 靠 `unalias echo_date` 规避同一问题）
  - 冒烟测试升级：桩命令注入、模拟带别名的 base.sh、用 `bash -O expand_aliases` 复现 ash 行为
- **1.1.1**
  - 修复软件中心离线安装被拒：补上 `.valid`（内容 `hnd`）与 ASP 里的 `PKG_NAME/PKG_ARCH/PKG_TYPE/PKG_EXTA` 元信息，
    对齐 fancyss_hnd 的打包约定
  - 冒烟测试新增两条断言（`.valid` 内容、`PKG_ARCH=hnd`）
- **1.1.0**
  - 界面按 Material 3 / FlClash 重做：导航栏、圆角卡片、分段按钮、Snackbar、面积渐变流量图
  - 设置页新增「mihomo 内核」卡片：上传 / 校验 / 热替换 / 自动回滚 / 手动回滚
  - 静态资源加 `Cache-Control`，升级面板后不会读到旧 CSS/JS
  - 状态色不再用 transition（避免后台标签页首帧颜色停留）
  - 新增内核校验单测（gzip / ELF 架构 / 错误日志提取）
- **1.0.1**
  - 换用精简内核：UPX 后 7.6 MB（解压 35 MB，去掉 gvisor 与三家里程碑组网），安装包 23 MB → 11 MB
  - 新增内核/面板自检：`mihomo -v` 跑不起来就中止安装，不会留下半残状态
  - 新增 `/koolshare/scripts/uninstall_mihomo.sh` 钩子，软件中心「卸载」按钮可用
  - `dbus` 改用绝对路径 `/koolshare/bin/dbus`（PATH 里不一定有）
  - 配置校验结果缓存：配置和内核没变时不再重复跑 `mihomo -t`（对 UPX 内核省一次解压）
  - 控制脚本：`pidof` 缺失时退回扫 `/proc`；`start-stop-daemon` 两个路径都试
  - 新增 `tests/install_smoke.sh` 安装/卸载冒烟测试
- **1.0.0**：首个版本，官方 mihomo 内核，软件中心可安装

## 14. 开发与测试

```bash
# 单元测试（不需要内核；e2e 会在有 MM_MIHOMO 时跑真实内核）
go test ./...

# 前端静态检查：标签配对 / id 引用 / 依赖函数是否存在
python3 tests/ui_check.py

# 一键：静态检查 + 单元测试 + 真实内核 e2e + 安装冒烟
sh tests/e2e.sh

# 打包
./build.sh --kernel .cache/mihomo-linux-armv7-<版本>.upx
```

## 15. 界面

![服务连通性](docs/connectivity-preview.png)

面板按 **Material 3 / FlClash** 的语言实现：

- 左侧导航栏（移动端自动变底部 Tab），24px 圆角卡片，M3 色板（深色下主色是浅蓝 `#A8C7FA`）
- 仪表盘：大状态卡 + 圆形电源键 + 实时流量曲线（canvas 手绘，含面积渐变）
- 节点：分组为**横向 tab**（可左右滑动，带节点数），下方只显示当前分组；每行带协议角标（SS/VM/VL/TR/HY2/TU…）、延迟药丸、单点测速
- 分段按钮切换规则/全局/直连；Snackbar 提示；深浅色主题切换并记忆
- 前端三件套（HTML/CSS/JS）无构建、无 CDN，`embed` 进二进制；静态资源带 `no-cache`，升级面板后不会读到旧样式

> 截图取自路由器上的实际运行界面；手机浏览器访问同一地址会自动切成底部 Tab 布局。

## 16. 内核在线更新

「设置 → mihomo 内核」：

- 显示当前内核的版本、大小、安装时间、SHA-256、可回滚版本
- **检查更新 / 立即更新**：直接查 [fanghengxuan/mihomo](https://github.com/fanghengxuan/mihomo) 的 `slim-*` Release，
  匹配 `mihomo-linux-armv7-slim-*` 资产；下载后同样做 ELF / `-v` / `-t` 校验，失败自动回滚
- **拖入**或点击选择新的内核文件（`linux-armv7` 原始 ELF，或 GitHub release 的 `.gz`，上限 96 MB）
- 服务端依次校验：ELF 架构（32 位 ARM）→ 能否 `-v` → 能否用**当前配置** `-t` → 备份旧版本 → 替换并重启
- 任一步失败都不写入磁盘；若替换后内核起不来，**自动回滚**并提示
- 「回滚上一版本」可随时切回（来回切换都保留备份，最多 3 个）

对应接口见 [docs/API.md](docs/API.md) §7。
