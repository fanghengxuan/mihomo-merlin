#!/bin/sh
# 「不带内核」安装包的测试：装完应当只剩面板，并提示去面板里下载/上传内核
set -eu

PROJ=$(cd "$(dirname "$0")/.." && pwd)
PKG=$(ls "$PROJ"/dist/mihomo_*_nokernel.tar.gz 2>/dev/null | head -1)
[ -n "$PKG" ] || { echo "先跑 NOKERNEL=1 ./build.sh"; exit 1; }

ROOT=$(mktemp -d /tmp/mm-nk.XXXXXX)
STUB="$ROOT/stub"; KS="$ROOT/koolshare"
mkdir -p "$STUB" "$KS"/bin "$KS"/webs "$KS"/res "$KS"/scripts "$KS"/init.d

cat > "$STUB/uname" <<'EOS'
#!/bin/sh
case "$1" in -m) echo armv7l ;; *) echo Linux ;; esac
EOS
cat > "$STUB/nvram" <<'EOS'
#!/bin/sh
case "$1" in get) case "$2" in productid) echo TUF-AX3000 ;; buildno) echo 388 ;; *) echo x ;; esac ;; esac
EOS
cat > "$STUB/df" <<EOS
#!/bin/sh
echo "Filesystem 1024-blocks Used Available Capacity Mounted on"
echo "/dev/mtdblock 65536 0 \${MM_FAKE_FREE_KB:-17548} 1% /jffs"
EOS
cat > "$STUB/iptables" <<'EOS'
#!/bin/sh
exit 0
EOS
cat > "$STUB/dbus" <<'EOS'
#!/bin/sh
exit 0
EOS
cat > "$STUB/killall" <<'EOS'
#!/bin/sh
exit 0
EOS
cat > "$STUB/pidof" <<'EOS'
#!/bin/sh
exit 1
EOS
cat > "$STUB/mihomo-panel" <<'EOS'
#!/bin/sh
case "$1" in -v) echo "mihomo-merlin panel 1.7.9-stub"; exit 0 ;; esac
exit 0
EOS
chmod +x "$STUB"/*; cp "$STUB"/* "$KS/bin/" 2>/dev/null || true

EXT="$ROOT/extract"; mkdir -p "$EXT"; tar xzf "$PKG" -C "$EXT"
sed "s#/koolshare#$KS#g" "$EXT/mihomo/install.sh" > "$EXT/mihomo/install2.sh"
sed "s#/koolshare#$KS#g" "$EXT/mihomo/scripts/setup.sh" > "$EXT/mihomo/scripts/setup2.sh"
cp "$EXT/mihomo/scripts/setup2.sh" "$EXT/mihomo/scripts/setup.sh"
sed "s#/koolshare#$KS#g" "$EXT/mihomo/scripts/mihomo_ctl.sh" > "$EXT/mihomo/scripts/ctl2.sh"
cp "$EXT/mihomo/scripts/ctl2.sh" "$EXT/mihomo/scripts/mihomo_ctl.sh"
cp -f "$STUB/mihomo-panel" "$EXT/mihomo/bin/mihomo-panel"
chmod +x "$EXT/mihomo/install2.sh" "$EXT/mihomo/scripts/setup.sh" "$EXT/mihomo/bin/mihomo-panel"

fail() { echo "❌ $*"; rm -rf "$ROOT"; exit 1; }
ok() { echo "✅ $*"; }

[ ! -f "$EXT/mihomo/bin/mihomo" ] || fail "包里不应包含内核"
ok "包内确认不含内核"

MM_PAYLOAD_KB=2052 PATH="$STUB:$KS/bin:$PATH" sh "$EXT/mihomo/install2.sh" > "$ROOT/install.log" 2>&1 || { cat "$ROOT/install.log"; fail "无内核安装失败"; }
cat "$ROOT/install.log"

[ -x "$KS/bin/mihomo-panel" ] || fail "面板未安装"
[ ! -e "$KS/bin/mihomo" ] || fail "不该凭空出现内核文件"
grep -q "本安装包不含内核" "$ROOT/install.log" || fail "缺少「不含内核」提示"
grep -q "检查更新" "$ROOT/install.log" || fail "缺少下载内核的引导提示"
ok "面板安装成功，并提示到面板下载/上传内核"

run_uninstall() { sed "s#/koolshare#$KS#g" "$EXT/mihomo/uninstall.sh" > "$EXT/mihomo/uninstall2.sh"; PATH="$STUB:$KS/bin:$PATH" sh "$EXT/mihomo/uninstall2.sh" >/dev/null 2>&1 || true; }
run_uninstall
[ ! -d "$KS/mihomo" ] || fail "卸载未清理数据目录"
ok "卸载正常"

rm -rf "$ROOT"
echo
echo "全部断言通过 🎉"
