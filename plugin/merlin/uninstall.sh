#!/bin/sh
# mihomo-merlin 卸载脚本（Asuswrt-Merlin / gnuton，无软件中心）
#
# 用法：uninstall.sh [--keep-data]
#   默认删除 /jffs/mihomo-merlin 全部内容（含订阅/节点/规则/日志）
#   --keep-data 只删程序，保留 data/ 下的订阅与设置

ROOT=${MM_ROOT:-}
JFFS=$ROOT/jffs
BASE=$JFFS/mihomo-merlin
SCRIPTS=$JFFS/scripts
KEEP=0
[ "${1:-}" = "--keep-data" ] && KEEP=1

mm_log() { echo "【$(date '+%Y-%m-%d %H:%M:%S')】: $*"; }

if [ -x "$BASE/ctl.sh" ]; then
	"$BASE/ctl.sh" stop >/dev/null 2>&1
fi

remove_hook() {
	file=$1
	[ -f "$file" ] || return 0
	tmp="$file.mm.$$"
	awk '
		/^# >>> mihomo-merlin >>>/{skip=1; next}
		/^# <<< mihomo-merlin <<</{skip=0; next}
		skip!=1{print}
	' "$file" >"$tmp" || { rm -f "$tmp"; mm_log "更新 $file 失败"; return 1; }
	cat "$tmp" >"$file"
	rm -f "$tmp"
}

mm_log "清理开机自启与防火墙钩子"
remove_hook "$SCRIPTS/services-start"
remove_hook "$SCRIPTS/services-stop"
remove_hook "$SCRIPTS/nat-start"

# 兜底清理 iptables（正常情况下 ctl stop / 面板退出时已经清过）
if command -v iptables >/dev/null 2>&1; then
	lanif=$(nvram get lan_ifname 2>/dev/null)
	[ -n "$lanif" ] || lanif=br0
	i=0
	while [ "$i" -lt 5 ]; do
		iptables -t nat -D PREROUTING -i "$lanif" -j MIHOMO 2>/dev/null || break
		i=$((i + 1))
	done
	i=0
	while [ "$i" -lt 5 ]; do
		iptables -t nat -D OUTPUT -j MIHOMO_OUT 2>/dev/null || break
		i=$((i + 1))
	done
	iptables -t nat -F MIHOMO 2>/dev/null
	iptables -t nat -X MIHOMO 2>/dev/null
	iptables -t nat -F MIHOMO_OUT 2>/dev/null
	iptables -t nat -X MIHOMO_OUT 2>/dev/null
	iptables -D FORWARD -p udp --dport 443 -j REJECT 2>/dev/null
	i=0
	while [ "$i" -lt 3 ]; do
		iptables -t mangle -D FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu 2>/dev/null || break
		i=$((i + 1))
	done
fi

if [ "$KEEP" = "1" ]; then
	mm_log "保留数据目录：$BASE/data"
	rm -rf "$BASE/bin" "$BASE/scripts" "$BASE/ctl.sh" "$BASE/uninstall.sh" "$BASE/version" 2>/dev/null
else
	mm_log "删除 $BASE"
	rm -rf "$BASE"
fi
rm -rf /tmp/mihomo 2>/dev/null

mm_log "卸载完成"
