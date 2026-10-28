#!/bin/sh
# 安装脚本冒烟测试：用假根目录 + 桩命令模拟 koolshare 路由器，
# 验证 install.sh / uninstall.sh 的落盘位置、dbus 登记、开机自启软链、卸载清理。
#
# 用法：sh tests/install_smoke.sh
# 依赖：sh、sed、tar、mktemp；不需要 ARM 环境

set -eu

PROJ=$(cd "$(dirname "$0")/.." && pwd)
PKG_SRC="$PROJ/dist/stage/mihomo"
[ -d "$PKG_SRC" ] || { echo "先跑 ./build.sh 生成 dist/stage/mihomo"; exit 1; }

ROOT=$(mktemp -d /tmp/mm-smoke.XXXXXX)
STUB="$ROOT/stub"
KS="$ROOT/koolshare"
mkdir -p "$STUB" "$KS"/bin "$KS"/webs "$KS"/res "$KS"/scripts "$KS"/init.d
# 安装脚本会自己设 PATH（不再 source base.sh），所以桩命令要放进 $KS/bin
USE_BASH_ALIAS=""
if bash -c 'true' 2>/dev/null && bash -O expand_aliases -c 'true' 2>/dev/null; then
	USE_BASH_ALIAS=1
fi
run_sh() {
	# 桩命令放前面：卸载脚本不自己设 PATH，需要能解析到 iptables/pidof 等桩
	export PATH="$STUB:$KS/bin:$PATH"
	if [ -n "$USE_BASH_ALIAS" ]; then
		bash -O expand_aliases "$@"
	else
		sh "$@"
	fi
}

fail() { echo "❌ $*"; exit 1; }
ok() { echo "✅ $*"; }

# ---------- 桩命令 ----------
cat > "$STUB/uname" <<EOF
#!/bin/sh
case "\$1" in
	-m) echo armv7l ;;
	-r) echo 4.1.52 ;;
	*) echo Linux ;;
esac
EOF

cat > "$STUB/nvram" <<'EOF'
#!/bin/sh
case "$2" in
	productid) echo TUF-AX3000 ;;
	lan_ipaddr) echo 192.168.50.1 ;;
	lan_ifname) echo br0 ;;
	*) echo "" ;;
esac
EOF

cat > "$STUB/dbus" <<EOF
#!/bin/sh
echo "dbus \$*" >> "$ROOT/dbus.log"
case "\$2" in
	softcenter_module_mihomo_install) echo 4 ;;
esac
EOF

cat > "$STUB/iptables" <<EOF
#!/bin/sh
echo "iptables \$*" >> "$ROOT/iptables.log"
exit 0
EOF

# pidof：从 pid 文件读，模拟「进程存在」
cat > "$STUB/pidof" <<EOF
#!/bin/sh
[ -f "$ROOT/panel.pid" ] || exit 1
pid=\$(cat "$ROOT/panel.pid" 2>/dev/null)
[ -n "\$pid" ] || exit 1
kill -0 "\$pid" 2>/dev/null || exit 1
echo "\$pid"
EOF

# 内核桩：-v 打印版本，-t 校验通过
cat > "$STUB/mihomo" <<'EOF'
#!/bin/sh
case "$1" in
	-v) echo "Mihomo Meta v1.19.33 linux arm (stub)"; exit 0 ;;
	-t) exit 0 ;;
	*) exit 0 ;;
esac
EOF

# 面板桩：-v 立刻返回；正常启动时写 pid 后挂起（模拟常驻进程）
cat > "$STUB/mihomo-panel" <<EOF
#!/bin/sh
case "\$1" in
	-v) echo "mihomo-merlin panel 1.0.1-stub"; exit 0 ;;
esac
echo \$\$ > "$ROOT/panel.pid"
echo "mihomo-merlin panel 1.0.1-stub started"
exec sleep 300
EOF

chmod +x "$STUB"/*
# 模拟 koolshare 的 base.sh：里面正是把 echo_date 定义成别名，
# busybox ash 解析函数定义时也会展开别名 —— 这是必须覆盖的回归点
cat > "$KS/scripts/base.sh" <<BASEEOF
#!/bin/sh
# 模拟 koolshare 固件的 base.sh：既有 echo_date 别名，也提供 dbus（shell 函数形式）
alias echo_date='echo -n ""'
dbus() {
	printf 'dbus %s\n' "\$*" >> "$ROOT/dbus.log"
}
BASEEOF

# ---------- 复制并改路径 ----------
mkdir -p "$ROOT/pkg"
cp -R "$PKG_SRC" "$ROOT/pkg/mihomo"
# 真正的 armv7 二进制在 mac 上跑不了，用桩替换掉（本测试只验脚本逻辑）
cp -f "$STUB/mihomo" "$ROOT/pkg/mihomo/bin/mihomo"
cp -f "$STUB/mihomo-panel" "$ROOT/pkg/mihomo/bin/mihomo-panel"
chmod +x "$ROOT/pkg/mihomo/bin/"*
for f in "$ROOT/pkg/mihomo/install.sh" "$ROOT/pkg/mihomo/uninstall.sh" "$ROOT/pkg/mihomo/scripts/mihomo_ctl.sh" "$ROOT/pkg/mihomo/scripts/setup.sh"; do
	sed -i '' -e "s#/koolshare#$KS#g" "$f" 2>/dev/null || sed -i -e "s#/koolshare#$KS#g" "$f"
done

# ---------- 跑安装 ----------
echo "=== 安装 ==="
cp -f "$STUB"/* "$KS/bin/" 2>/dev/null || true
run_sh "$ROOT/pkg/mihomo/install.sh" > "$ROOT/install.log" 2>&1 || {
	if false; then :; fi
}
# 桩环境里最后的「面板是否启动」可能失败，但文件必须落齐；这里只做断言
tail -6 "$ROOT/install.log"

# ---------- 断言 ----------
if grep -v '^[[:space:]]*#' "$ROOT/pkg/mihomo/install.sh" | grep -qE 'echo_date[[:space:]]*\(\)'; then
	fail "install.sh 仍定义 echo_date()，会被 base.sh 的别名展开成语法错误"
fi
grep -q 'mm_log()' "$ROOT/pkg/mihomo/install.sh" || fail "install.sh 缺少 mm_log() 日志函数"
ok "日志函数为 mm_log（避开 base.sh 的 echo_date 别名）"

grep -q '\. /koolshare/scripts/base.sh' "$ROOT/pkg/mihomo/install.sh" \
	|| grep -q 'base\.sh' "$ROOT/pkg/mihomo/install.sh" \
	|| fail "install.sh 没有 source base.sh，拿不到固件的 dbus"
ok "install.sh 会 source base.sh（取 dbus 等固件函数）"

[ -x "$KS/bin/mihomo" ] || fail "内核未安装"
ok "内核已安装到 $KS/bin/mihomo"

[ -x "$KS/bin/mihomo-panel" ] || fail "面板未安装"
ok "面板已安装"

[ -x "$KS/scripts/mihomo_ctl.sh" ] || fail "控制脚本未安装"
ok "控制脚本已安装"

[ -x "$KS/scripts/uninstall_mihomo.sh" ] || fail "软件中心卸载钩子未安装（按钮会失效）"
ok "卸载钩子 /scripts/uninstall_mihomo.sh 已安装"

[ -L "$KS/init.d/S99mihomo.sh" ] || fail "缺少开机自启软链 S99"
[ -L "$KS/init.d/N99mihomo.sh" ] || fail "缺少关机停止软链 N99"
ok "init.d S99/N99 软链已建立"

[ -f "$KS/webs/Module_mihomo.asp" ] || fail "菜单页未安装"
[ -f "$KS/res/icon-mihomo.png" ] || fail "图标未安装"
ok "菜单页与图标已安装"

[ -f "$ROOT/pkg/mihomo/.valid" ] || fail "安装包缺少 .valid 平台标记（软件中心会拒绝安装）"
[ "$(cat "$ROOT/pkg/mihomo/.valid" | tr -d '\n')" = "hnd" ] || fail ".valid 内容不是 hnd"
ok ".valid 平台标记正确：hnd"

grep -q 'PKG_ARCH = "hnd"' "$ROOT/pkg/mihomo/webs/Module_mihomo.asp" || fail "ASP 缺少 PKG_ARCH=hnd 元信息"
ok "ASP 元信息 PKG_ARCH=hnd 存在"

[ -f "$KS/mihomo/rules/chnroute.txt" ] || fail "规则文件未安装"
[ -d "$KS/mihomo/providers" ] || fail "数据目录未创建"
ok "规则与数据目录已就位"

for k in name title description version install; do
	grep -q "softcenter_module_mihomo_${k}=" "$ROOT/dbus.log" || fail "缺少 dbus 键 softcenter_module_mihomo_${k}"
done
grep -q 'softcenter_module_mihomo_install=1' "$ROOT/dbus.log" || fail "install 标记不是 1（MerlinClash Lite 在 388 上用 1）"
ok "软件中心菜单登记完整（name/title/description/version/install=1）"

[ "$(cat "$KS/mihomo/version" 2>/dev/null | tr -d '\n')" = "$(cat "$PROJ/plugin/version" | tr -d '\n')" ] \
	|| fail "版本号未写入数据目录"
ok "版本号已写入：$(cat "$KS/mihomo/version")"

# ---------- 跑卸载 ----------
echo
echo "=== 卸载 ==="
run_sh "$KS/scripts/uninstall_mihomo.sh" > "$ROOT/uninstall.log" 2>&1 || fail "卸载脚本执行失败"
cat "$ROOT/uninstall.log"

[ ! -e "$KS/bin/mihomo" ] || fail "内核未删除"
[ ! -e "$KS/bin/mihomo-panel" ] || fail "面板未删除"
[ ! -e "$KS/scripts/uninstall_mihomo.sh" ] || fail "卸载脚本未自清理"
[ ! -e "$KS/webs/Module_mihomo.asp" ] || fail "菜单页未删除"
[ ! -L "$KS/init.d/S99mihomo.sh" ] || fail "自启软链未删除"
ok "程序文件、菜单页、自启软链均已清理"

[ -d "$KS/mihomo" ] || fail "数据目录被误删（应保留订阅）"
ok "订阅数据目录按设计保留：$KS/mihomo"

grep -q "softcenter_module_mihomo_install" "$ROOT/dbus.log" && grep -q "dbus remove softcenter_module_mihomo_install" "$ROOT/dbus.log" \
	|| fail "dbus 登记未清理"
ok "dbus 菜单登记已清理"

grep -q "iptables -t nat -D PREROUTING" "$ROOT/iptables.log" || fail "卸载时没有清理转发规则（残留会断网）"
ok "卸载时执行了 iptables 清理"

# 收尾
[ -f "$ROOT/panel.pid" ] && kill "$(cat "$ROOT/panel.pid")" 2>/dev/null || true
rm -rf "$ROOT"
echo
echo "全部断言通过 🎉"
