#!/bin/sh
# Merlin 原生安装脚本冒烟测试：假根目录 + 桩命令，验证落盘、hooks 幂等、
# 启动/停止、卸载与数据保留。不需要 ARM 环境。
#
# 用法：sh tests/merlin_install_smoke.sh

set -u

PROJ=$(cd "$(dirname "$0")/.." && pwd)
ROOT=$(mktemp -d /tmp/mm-merlin-smoke.XXXXXX)
JFFS="$ROOT/jffs"
STUB="$ROOT/stub"
PKG="$ROOT/pkg/mihomo-merlin"
BASE="$JFFS/mihomo-merlin"

mkdir -p "$JFFS/scripts" "$STUB" "$PKG/bin" "$PKG/rules" "$PKG/scripts"

fail() { echo "❌ $*"; kill_all; rm -rf "$ROOT"; exit 1; }
ok() { echo "✅ $*"; }
kill_all() {
	for f in "$BASE/data/panel.pid" "$BASE/data/panel-watchdog.pid"; do
		[ -f "$f" ] && kill "$(cat "$f" 2>/dev/null)" 2>/dev/null
	done
}

# ---------- 桩命令 ----------
cat >"$STUB/uname" <<'EOF'
#!/bin/sh
case "$1" in
	-m) echo armv7l ;;
	-r) echo 4.1.52 ;;
	*) echo Linux ;;
esac
EOF

cat >"$STUB/nvram" <<'EOF'
#!/bin/sh
case "$2" in
	productid) echo TUF-AX3000 ;;
	lan_ipaddr) echo 192.168.50.1 ;;
	lan_ifname) echo br0 ;;
	jffs2_scripts) echo "${MM_FAKE_SCRIPTS:-1}" ;;
	*) echo "" ;;
esac
EOF

cat >"$STUB/df" <<'EOF'
#!/bin/sh
echo "Filesystem 1024-blocks Used Available Capacity Mounted on"
echo "/dev/mtdblock 65536 0 ${MM_FAKE_FREE_KB:-65536} 1% /jffs"
EOF

cat >"$STUB/iptables" <<EOF
#!/bin/sh
echo "iptables \$*" >> "$ROOT/iptables.log"
exit 0
EOF

cat >"$STUB/mihomo" <<'EOF'
#!/bin/sh
case "$1" in
	-v) echo "Mihomo Meta v1.19.33 linux arm (stub)"; exit 0 ;;
	-t) exit 0 ;;
	*) exit 0 ;;
esac
EOF

cat >"$STUB/mihomo-panel" <<'EOF'
#!/bin/sh
case "$1" in
	-v) echo "mihomo-merlin panel 2.0.1-stub"; exit 0 ;;
esac
trap 'exit 0' TERM INT
# 保持 shell 进程，让 cmdline 里保留 mihomo-panel 路径（panel_running 会校验）
while :; do sleep 1; done
EOF

chmod +x "$STUB"/*

# ---------- 组装假安装包 ----------
cp "$PROJ/plugin/merlin/install.sh" "$PKG/install.sh"
cp "$PROJ/plugin/merlin/uninstall.sh" "$PKG/uninstall.sh"
cp "$PROJ/plugin/merlin/scripts/ctl.sh" "$PKG/scripts/ctl.sh"
cp "$PROJ/plugin/version" "$PKG/version"
cp "$STUB/mihomo" "$STUB/mihomo-panel" "$PKG/bin/"
echo "0.0.0.0/0" >"$PKG/rules/chnroute.txt"
chmod +x "$PKG/install.sh" "$PKG/uninstall.sh" "$PKG/scripts/ctl.sh" "$PKG/bin/"*

run() {
	MM_ROOT="$ROOT" PATH="$STUB:$PATH" sh "$@"
}

# 模拟老版本把 logs/kernel 留在 jffs 上
mkdir -p "$BASE/data/kernel" "$BASE/data/logs"
echo "OLD-KERNEL" >"$BASE/data/kernel/old.bin"
echo "OLD-LOG" >"$BASE/data/logs/panel.log"

# ---------- 安装 ----------
echo "=== 安装 ==="
if ! run "$PKG/install.sh" >"$ROOT/install.log" 2>&1; then
	cat "$ROOT/install.log"
	fail "安装脚本执行失败"
fi
tail -6 "$ROOT/install.log"

[ -x "$BASE/bin/mihomo-panel" ] || fail "面板未安装"
[ -x "$BASE/bin/mihomo" ] || fail "内核未安装"
[ -x "$BASE/ctl.sh" ] || fail "控制脚本未安装"
[ -x "$BASE/uninstall.sh" ] || fail "卸载脚本未安装"
[ -f "$BASE/data/rules/chnroute.txt" ] || fail "规则未安装"
[ -L "$BASE/data/logs" ] || fail "logs 未迁移到 /tmp"
[ -L "$BASE/data/kernel" ] || fail "kernel 未迁移到 /tmp"
[ -f /tmp/mihomo/kernel/old.bin ] || fail "旧内核备份未迁移到 /tmp"
[ -f /tmp/mihomo/logs/panel.log ] || fail "旧日志未迁移到 /tmp"
ok "程序、数据目录与规则已落盘；logs/kernel 已迁移并链接到 /tmp"

grep -q "# >>> mihomo-merlin >>>" "$JFFS/scripts/services-start" || fail "services-start 缺少托管块"
grep -q "$BASE/ctl.sh start" "$JFFS/scripts/services-start" || fail "services-start 未调用 ctl start"
grep -q "$BASE/ctl.sh stop" "$JFFS/scripts/services-stop" || fail "services-stop 未调用 ctl stop"
grep -q "$BASE/ctl.sh reapply" "$JFFS/scripts/nat-start" || fail "nat-start 未调用 ctl reapply"
ok "services-start / services-stop / nat-start 钩子已写入"

[ -f "$BASE/data/panel.pid" ] || fail "面板未启动（缺少 pid 文件）"
PID=$(cat "$BASE/data/panel.pid")
kill -0 "$PID" 2>/dev/null || fail "面板进程未运行"
ok "面板已启动（pid ${PID}）"

# ---------- 重复安装幂等 ----------
echo
echo "=== 重复安装 ==="
if ! run "$PKG/install.sh" >"$ROOT/install2.log" 2>&1; then
	cat "$ROOT/install2.log"
	fail "重复安装失败"
fi
[ "$(grep -c '# >>> mihomo-merlin >>>' "$JFFS/scripts/services-start")" = "1" ] \
	|| fail "services-start 托管块重复"
[ "$(grep -c '# >>> mihomo-merlin >>>' "$JFFS/scripts/services-stop")" = "1" ] \
	|| fail "services-stop 托管块重复"
[ "$(grep -c '# >>> mihomo-merlin >>>' "$JFFS/scripts/nat-start")" = "1" ] \
	|| fail "nat-start 托管块重复"
ok "重复安装不会叠加钩子"

# ---------- 停止 ----------
echo
echo "=== 停止 ==="
run "$BASE/ctl.sh" stop >"$ROOT/stop.log" 2>&1 || fail "ctl stop 失败"
if [ -n "$(cat "$BASE/data/panel.pid" 2>/dev/null)" ]; then
	kill -0 "$(cat "$BASE/data/panel.pid")" 2>/dev/null && fail "面板仍在运行"
fi
grep -q "nat -F MIHOMO" "$ROOT/iptables.log" || fail "停止时未清理 iptables"
grep -q "nat -D OUTPUT -j MIHOMO_OUT" "$ROOT/iptables.log" || fail "停止时未清理 MIHOMO_OUT 跳转"
ok "面板已停止并清理 iptables（含 MIHOMO_OUT）"

# ---------- 卸载 ----------
echo
echo "=== 卸载 ==="
run "$BASE/uninstall.sh" >"$ROOT/uninstall.log" 2>&1 || fail "卸载失败"
[ ! -d "$BASE" ] || fail "数据目录未删除"
grep -q "# >>> mihomo-merlin >>>" "$JFFS/scripts/services-start" && fail "services-start 钩子未清理"
grep -q "# >>> mihomo-merlin >>>" "$JFFS/scripts/services-stop" && fail "services-stop 钩子未清理"
grep -q "# >>> mihomo-merlin >>>" "$JFFS/scripts/nat-start" && fail "nat-start 钩子未清理"
ok "程序、数据与 hooks 已清理"

# ---------- 保留数据卸载 ----------
echo
echo "=== 保留数据卸载 ==="
run "$PKG/install.sh" >/dev/null 2>&1 || fail "重新安装失败"
echo '{"mode":"rule"}' >"$BASE/data/settings.json"
run "$BASE/uninstall.sh" --keep-data >"$ROOT/uninstall-keep.log" 2>&1 || fail "保留数据卸载失败"
[ -f "$BASE/data/settings.json" ] || fail "--keep-data 未保留设置"
[ ! -e "$BASE/bin/mihomo-panel" ] || fail "--keep-data 未删除程序"
ok "--keep-data 保留数据、删除程序"

# ---------- 未开启 JFFS 脚本时应拒绝 ----------
echo
echo "=== JFFS 脚本未开启 ==="
if MM_FAKE_SCRIPTS=0 run "$PKG/install.sh" >"$ROOT/install-noscripts.log" 2>&1; then
	fail "未开启 JFFS 脚本时不应安装成功"
fi
grep -q "jffs2_scripts" "$ROOT/install-noscripts.log" || fail "缺少 jffs2_scripts 提示"
ok "未开启 JFFS 脚本时明确拒绝"

kill_all
rm -rf "$ROOT"
echo
echo "全部断言通过 🎉"
