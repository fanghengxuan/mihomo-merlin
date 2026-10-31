#!/bin/sh
# 安装前清理脚本的测试：用假 /koolshare、假 /jffs 验证
#   1) 默认只预览不删除
#   2) --yes 真的删干净
#   3) --need 能判断空间是否足够并给出退出码
set -eu

PROJ=$(cd "$(dirname "$0")/.." && pwd)
SRC="$PROJ/tools/preinstall-cleanup.sh"
[ -f "$SRC" ] || { echo "缺少 $SRC"; exit 1; }

ROOT=$(mktemp -d /tmp/mm-cleanup.XXXXXX)
KS="$ROOT/koolshare"
JFFS="$ROOT/jffs"
mkdir -p "$KS"/bin "$KS"/webs "$KS"/res "$KS"/scripts "$KS"/init.d "$KS"/mihomo/kernel "$JFFS"

# 造残留：数据目录 + 内核备份 + 两个二进制 + 菜单页
head -c 400000 /dev/urandom > "$KS/mihomo/kernel/backup-1.bin"
head -c 300000 /dev/urandom > "$KS/mihomo/kernel/backup-2.bin"
head -c 200000 /dev/urandom > "$KS/bin/mihomo"
head -c 200000 /dev/urandom > "$KS/bin/mihomo-panel"
echo menu > "$KS/webs/Module_mihomo.asp"
echo icon > "$KS/res/icon-mihomo.png"
ln -s "$KS/scripts/mihomo_ctl.sh" "$KS/init.d/S99mihomo.sh" 2>/dev/null || true
echo ctl > "$KS/scripts/mihomo_ctl.sh"

# 生成一份把 /koolshare、/jffs 换成临时目录的副本
TEST_SH="$ROOT/cleanup.sh"
sed "s#/koolshare#$KS#g; s#/jffs#$JFFS#g" "$SRC" > "$TEST_SH"

fail() { echo "❌ $*"; rm -rf "$ROOT"; exit 1; }
ok() { echo "✅ $*"; }

echo "=== 1) 预览模式不应删除 ==="
sh "$TEST_SH" > "$ROOT/preview.log" 2>&1 || fail "预览模式异常退出"
[ -d "$KS/mihomo" ] || fail "预览模式把数据目录删了"
[ -f "$KS/bin/mihomo" ] || fail "预览模式把内核删了"
grep -q "没有删除任何东西" "$ROOT/preview.log" || fail "预览模式没提示"
grep -q "内核备份明细" "$ROOT/preview.log" || fail "没列出内核备份明细"
ok "预览模式只报告不删除"

echo "=== 2) --yes 应删干净 ==="
sh "$TEST_SH" --yes > "$ROOT/apply.log" 2>&1 || fail "--yes 执行失败"
[ ! -d "$KS/mihomo" ] || fail "数据目录未删除"
[ ! -f "$KS/bin/mihomo" ] || fail "内核未删除"
[ ! -f "$KS/bin/mihomo-panel" ] || fail "面板未删除"
[ ! -f "$KS/webs/Module_mihomo.asp" ] || fail "菜单页未删除"
grep -q "已删除" "$ROOT/apply.log" || fail "没报告删除了多少"
ok "--yes 删除干净并报告释放量"

echo "=== 3) --need 空间判断 ==="
sh "$TEST_SH" --need 16692 > "$ROOT/need.log" 2>&1 && ok "空间足够时退出码 0" || fail "空间足够却报失败"
sh "$TEST_SH" --need 999999999 > "$ROOT/need2.log" 2>&1 && fail "空间不足时不该返回成功" || true
grep -q "空间仍然不够" "$ROOT/need2.log" || fail "空间不足没有给提示"
grep -q "lite" "$ROOT/need2.log" || fail "空间不足没有建议精简版"
ok "空间不足时返回非 0 并给出建议"

rm -rf "$ROOT"
echo
echo "全部断言通过 🎉"
