#!/bin/sh
# 「mihomo 清理工具」插件包的测试：用假 /koolshare 模拟软件中心离线安装
#   1) 装完旧版本残留（数据目录/二进制/菜单页/自启软链）全部消失
#   2) 释放空间有明确输出
#   3) 清理工具自身也会卸载（不长期占菜单）
set -eu

PROJ=$(cd "$(dirname "$0")/.." && pwd)
PKG=$(ls "$PROJ"/dist/mihomo-cleanup_*_armv7.tar.gz 2>/dev/null | head -1)
[ -n "$PKG" ] || { echo "先跑 sh tools/build-cleanup-package.sh"; exit 1; }

ROOT=$(mktemp -d /tmp/mm-cleanpkg.XXXXXX)
STUB="$ROOT/stub"
KS="$ROOT/koolshare"
mkdir -p "$STUB" "$KS"/bin "$KS"/webs "$KS"/res "$KS"/scripts "$KS"/init.d

# 桩：iptables / dbus / killall / df
cat > "$STUB/iptables" <<EOS
#!/bin/sh
echo "iptables \$*" >> "$ROOT/iptables.log"
exit 0
EOS
cat > "$STUB/dbus" <<EOS
#!/bin/sh
echo "dbus \$*" >> "$ROOT/dbus.log"
exit 0
EOS
cat > "$STUB/killall" <<'EOS'
#!/bin/sh
exit 0
EOS
cat > "$STUB/df" <<'EOS'
#!/bin/sh
echo "Filesystem 1024-blocks Used Available Capacity Mounted on"
echo "/dev/mtdblock 65536 0 24000 1% /jffs"
EOS
chmod +x "$STUB"/*
# 注意：install.sh 会自己设 PATH=/koolshare/bin:...，所以桩命令要同时放进 KS/bin 才能被解析
cp "$STUB"/* "$KS/bin/" 2>/dev/null || true

# 造旧版本残留：数据目录（含内核备份）+ 二进制 + 菜单页 + 软链
mkdir -p "$KS/mihomo/kernel" "$KS/mihomo/providers"
head -c 1500000 /dev/urandom > "$KS/mihomo/kernel/backup-1.bin"
head -c 1500000 /dev/urandom > "$KS/mihomo/kernel/backup-2.bin"
head -c 400000 /dev/urandom > "$KS/mihomo/providers/s_1.json"
head -c 500000 /dev/urandom > "$KS/bin/mihomo"
head -c 500000 /dev/urandom > "$KS/bin/mihomo-panel"
echo menu > "$KS/webs/Module_mihomo.asp"
echo icon > "$KS/res/icon-mihomo.png"
echo ctl > "$KS/scripts/mihomo_ctl.sh"
ln -s "$KS/scripts/mihomo_ctl.sh" "$KS/init.d/S99mihomo.sh"
ln -s "$KS/scripts/mihomo_ctl.sh" "$KS/init.d/N99mihomo.sh"

# 模拟软件中心：解压 + 拷到 /tmp 后执行 install.sh（这里把 /koolshare 换成假目录）
EXT="$ROOT/extract"
mkdir -p "$EXT"
tar xzf "$PKG" -C "$EXT"
sed "s#/koolshare#$KS#g" "$EXT/mihomo-cleanup/install.sh" > "$EXT/mihomo-cleanup/install.test.sh"
sed "s#/koolshare#$KS#g" "$EXT/mihomo-cleanup/scripts/clean.sh" > "$EXT/mihomo-cleanup/scripts/clean.test.sh"
cp "$EXT/mihomo-cleanup/scripts/clean.test.sh" "$EXT/mihomo-cleanup/scripts/clean.sh"
chmod +x "$EXT/mihomo-cleanup/install.test.sh" "$EXT/mihomo-cleanup/scripts/clean.sh"

# 模拟软件中心先落盘（装到 KS 里）
mkdir -p "$KS/mihomo-cleanup"
cp -R "$EXT/mihomo-cleanup/." "$KS/mihomo-cleanup/"
cp "$EXT/mihomo-cleanup/webs/Module_mihomo-cleanup.asp" "$KS/webs/" 2>/dev/null || true
cp "$EXT/mihomo-cleanup/res/icon-mihomo-cleanup.png" "$KS/res/" 2>/dev/null || true

fail() { echo "❌ $*"; rm -rf "$ROOT"; exit 1; }
ok() { echo "✅ $*"; }

PATH="$STUB:$KS/bin:$PATH" sh "$EXT/mihomo-cleanup/install.test.sh" > "$ROOT/run.log" 2>&1 || fail "清理脚本执行失败"
sed -n '1,30p' "$ROOT/run.log"

[ ! -d "$KS/mihomo" ] || fail "数据目录未删除"
[ ! -f "$KS/bin/mihomo" ] || fail "内核未删除"
[ ! -f "$KS/bin/mihomo-panel" ] || fail "面板未删除"
[ ! -f "$KS/webs/Module_mihomo.asp" ] || fail "旧菜单页未删除"
[ ! -L "$KS/init.d/S99mihomo.sh" ] || fail "S99 软链未删除"
[ ! -L "$KS/init.d/N99mihomo.sh" ] || fail "N99 软链未删除"
ok "旧版本残留已清理干净"

grep -q "释放约" "$ROOT/run.log" || fail "没有报告释放空间"
ok "报告了释放量：$(grep -o '释放约[^，]*' "$ROOT/run.log" | head -1)"

grep -q "iptables -t nat -D PREROUTING" "$ROOT/iptables.log" || fail "没有清理转发规则"
grep -q "iptables -t mangle -D FORWARD" "$ROOT/iptables.log" || fail "没有清理 MSS 规则"
ok "iptables 残留已清理（nat / filter / mangle）"

grep -q "softcenter_module_mihomo_install" "$ROOT/dbus.log" || fail "没有清理旧版本软件中心登记"
ok "旧版本 dbus 登记已清理"

[ ! -d "$KS/mihomo-cleanup" ] || fail "清理工具自身未卸载"
[ ! -f "$KS/webs/Module_mihomo-cleanup.asp" ] || fail "清理工具菜单页未删除"
ok "清理工具装完即自卸载"

rm -rf "$ROOT"
echo
echo "全部断言通过 🎉"
