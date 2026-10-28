# 内核档案：当前打进安装包的 mihomo

## 文件事实（本地实测）

| 项目 | 值 |
| --- | --- |
| 文件 | `mihomo-linux-armv7-1.19.33`（用户提供） |
| 压缩 | UPX 4.22，压缩后 **7,973,176 字节** |
| 解压后 | **34,996,350 字节**（`upx -l` 报 22.78%） |
| ELF | `ELF 32-bit LSB executable, ARM, EABI5, statically linked` |
| 工具链 | go1.26.10，`CGO_ENABLED=0 GOARCH=arm GOARM=7` |
| mihomo 提交 | `96396ba59771ed9da4f8bd79184293b1e38ad926`（2026-10-22，vcs.modified=true） |
| 版本号 | 1.19.33（文件名 + 二进制内字符串一致） |
| 构建标签 | `with_low_memory,no_fake_tcp,no_tailscale,no_easytier,no_zerotier` |

## 与官方 release 的差异

官方 `mihomo-linux-armv7-v1.19.33`：

- 标签 `with_gvisor`，**无 UPX**，未压缩 **56,230,014 字节**
- 含 gvisor 用户态网络栈、easytier / zerotier / tailscale 组网

用户这版：

- 去掉了 gvisor 与三个组网模块，加上 `with_low_memory`（低内存匹配器，正适合 256 MB 机型）
- 再用 UPX 压到 7.6 MB
- 两者相差 21 MB，主要就是 gvisor 的体积

## 对本插件的影响

在解压后的二进制里逐项做过字符串命中检查，下面这些**全部仍在**：

```
proxy-providers  redir-port  tproxy-port  external-controller
rule-providers   RULE-SET    url-test     proxy-groups
sniffer          unified-delay  tcp-concurrent  find-process-mode  store-selected
hysteria2  tuic  vless  reality-opts
```

结论：本插件的配置生成逻辑（provider 挂订阅、redir 透明代理、sniffer、REST 控制）**不需要改动**。

唯一的功能缺口是 **TUN**：没有 `with_gvisor` 就用不了 gvisor 栈，只能用 `tun.stack: system`。
本项目默认走 `redir-port` + iptables，本来就不用 TUN，因此无影响。

## UPX 的代价（需要在路由器上实测确认）

- **启动时自解压**：exec 时把 35 MB 镜像解到匿名内存，峰值 RSS 会多出这部分；
  Go 运行时随后会触碰大部分页，所以这部分基本是常驻的。
- **启动慢 0.5~1 秒**：Cortex-A7 上解压 35 MB 大致这个量级。
- 面板已加「配置校验缓存」：只有 `config.json` 内容或内核文件变化时才跑 `mihomo -t`，
  平时启动/重启不会重复解压校验。

## 在路由器上验证

```sh
# 1) 内核能不能跑、版本对不对
/koolshare/bin/mihomo -v

# 2) 装了之后的内存占用（重点看 MemAvailable 和 mihomo 的 RSS）
free
cat /proc/$(pidof mihomo)/status | grep -E "VmRSS|VmSize"

# 3) 面板启动一次要多久（对比 UPX 前后的体感）
time sh /koolshare/scripts/mihomo_ctl.sh restart
```

如果 `VmSize` 明显超过 100 MB、或者 8 小时运行后 `MemAvailable` 低于 20 MB，
建议换回官方未压缩版（`./build.sh` 不带 `--kernel` 即为官方版）。
