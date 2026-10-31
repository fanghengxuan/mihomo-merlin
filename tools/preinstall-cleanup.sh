#!/bin/sh
# mihomo-merlin 安装前清理 / 空间自检
#
# 用途：软件中心在「解压前」就会检查 jffs 剩余空间，空间不够时它会直接退出，
# 插件的 install.sh 根本没有机会运行，所以清理由这个独立脚本完成。
#
# 用法（路由器上，用 ssh）：
#   sh mihomo-cleanup.sh            # 只检查并列出可清理的东西（不动手）
#   sh mihomo-cleanup.sh --yes      # 真的删除
#   sh mihomo-cleanup.sh --yes --need 16692    # 删完顺便判断够不够装（KB）
#
# 安全：只处理本插件自己的路径，绝不删别的目录。

set -u

APPLY=0
NEED_KB=0
while [ $# -gt 0 ]; do
	case "$1" in
		--yes|-y) APPLY=1 ;;
		--need)
			shift
			NEED_KB="${1:-0}"
			;;
		--need=*) NEED_KB="${1#--need=}" ;;
		--help|-h)
			sed -n '2,16p' "$0"
			exit 0
			;;
	esac
	shift
done

say() { echo "$*"; }
kb_of() { [ -e "$1" ] && du -sk "$1" 2>/dev/null | awk '{print $1}' || echo 0; }

FREE_KB=$(df -k /jffs 2>/dev/null | awk 'NR==2 {print $4}')
[ -n "$FREE_KB" ] || FREE_KB=0

DATA=/koolshare/mihomo
BIN1=/koolshare/bin/mihomo
BIN2=/koolshare/bin/mihomo-panel
WEB=/koolshare/webs/Module_mihomo.asp
ICON=/koolshare/res/icon-mihomo.png
CTL=/koolshare/scripts/mihomo_ctl.sh
UNINST=/koolshare/scripts/uninstall_mihomo.sh
S99=/koolshare/init.d/S99mihomo.sh
N99=/koolshare/init.d/N99mihomo.sh

say "==== mihomo-merlin 安装前清理 ===="
say "jffs 剩余：${FREE_KB} KB"

TOTAL=0
FOUND=0

report() { # name path
	name="$1"; path="$2"
	kb=$(kb_of "$path")
	[ "$kb" -gt 0 ] || return 0
	FOUND=$((FOUND + 1))
	TOTAL=$((TOTAL + kb))
	if [ -d "$path" ]; then
		say "  [目录] ${name}  ${kb} KB  ${path}"
	else
		say "  [文件] ${name}  ${kb} KB  ${path}"
	fi
	[ "$APPLY" = "1" ] && rm -rf "$path" 2>/dev/null
	return 0
}

say ""
say "检查本插件的旧版本残留："
report "数据目录（订阅/节点/规则/内核备份/日志）" "$DATA"
report "内核" "$BIN1"
report "面板" "$BIN2"
report "菜单页" "$WEB"
report "图标" "$ICON"
report "控制脚本" "$CTL"
report "卸载脚本" "$UNINST"
report "自启软链(S99)" "$S99"
report "自启软链(N99)" "$N99"
# 老版本写在 /tmp 的日志（重启会自己没，但省空间也算）
for f in /tmp/mihomo-panel.log /tmp/mihomo.log /tmp/mihomo_*.tar.gz /tmp/mihomo; do
	report "临时文件" "$f"
done

# 内核备份是老版本最容易堆积的地方（每份约 7.9MB）
if [ -d "$DATA/kernel" ]; then
	say ""
	say "内核备份明细："
	ls -l "$DATA/kernel" 2>/dev/null | awk 'NR>1 {printf "  %s  %s 字节\n", $9, $5}'
fi

say ""
if [ "$FOUND" = "0" ]; then
	say "没有发现残留，空间已经是干净的。"
else
	if [ "$APPLY" = "1" ]; then
		say "已删除 ${FOUND} 项，合计约 $((TOTAL / 1024)) MB（${TOTAL} KB）"
	else
		say "发现 ${FOUND} 项可清理，合计约 $((TOTAL / 1024)) MB（${TOTAL} KB）"
		say "这只是预览，没有删除任何东西。确认无误后加 --yes 再跑一次。"
	fi
fi

FREE_AFTER=$(df -k /jffs 2>/dev/null | awk 'NR==2 {print $4}')
[ -n "$FREE_AFTER" ] || FREE_AFTER=$FREE_KB
say "jffs 现在剩余：${FREE_AFTER} KB"

if [ "$NEED_KB" -gt 0 ]; then
	if [ "$FREE_AFTER" -ge "$NEED_KB" ]; then
		say "✅ 空间足够：需要 ${NEED_KB} KB（含 2MB 预留），当前 ${FREE_AFTER} KB"
		exit 0
	else
		say "❌ 空间仍然不够：需要 ${NEED_KB} KB，当前只有 ${FREE_AFTER} KB"
		say "   建议：改用 mihomo_1.7.5_armv7_lite.tar.gz（解压约 9740KB），或清理其它插件/启用 USB2JFFS 扩容"
		exit 1
	fi
fi

exit 0
