#!/bin/sh
# mihomo 插件卸载脚本：停进程、清 iptables、删程序与全部数据（彻底释放空间）
#
# 软件中心的「卸载」按钮会以无参方式调用本脚本；这里默认做「彻底卸载」：
# 连同 /koolshare/mihomo 下的订阅、节点文件、规则、内核备份、日志一起删除。
# 想保留数据再装回来，请先在面板「设置 → 备份与恢复」导出备份。

module=mihomo
DATA_DIR="/koolshare/mihomo"

if [ -x /koolshare/bin/dbus ]; then
	DBUS=/koolshare/bin/dbus
elif command -v dbus >/dev/null 2>&1; then
	DBUS=dbus
else
	DBUS=""
fi

unalias mm_log >/dev/null 2>&1
unalias echo >/dev/null 2>&1

mm_log() {
	echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】: $*"
}

# 目录大小（KB），用于告诉用户释放了多少
dir_kb() {
	[ -d "$1" ] || { echo 0; return; }
	du -sk "$1" 2>/dev/null | awk '{print $1}' | head -1
}

BEFORE_KB=$(dir_kb "$DATA_DIR")

mm_log "停止面板与内核"
if [ -x /koolshare/scripts/mihomo_ctl.sh ]; then
	sh /koolshare/scripts/mihomo_ctl.sh stop >/dev/null 2>&1
fi
# 兜底：ctl 脚本不在或没杀干净时直接按进程名收尾
killall -q mihomo-panel 2>/dev/null
killall -q mihomo 2>/dev/null
sleep 1
killall -q -9 mihomo-panel 2>/dev/null
killall -q -9 mihomo 2>/dev/null

mm_log "清理 iptables 规则（含 DNS/QUIC/MSS）"
# nat：透明代理链（多次删除，防止重复插入的残留）
i=0
while [ $i -lt 5 ]; do
	iptables -t nat -D PREROUTING -i br0 -j MIHOMO 2>/dev/null || break
	i=$((i + 1))
done
iptables -t nat -F MIHOMO 2>/dev/null
iptables -t nat -X MIHOMO 2>/dev/null

i=0
while [ $i -lt 5 ]; do
	iptables -t nat -D OUTPUT -j MIHOMO_OUT 2>/dev/null || break
	i=$((i + 1))
done
iptables -t nat -F MIHOMO_OUT 2>/dev/null
iptables -t nat -X MIHOMO_OUT 2>/dev/null

# filter：阻断 QUIC 的规则
i=0
while [ $i -lt 5 ]; do
	iptables -D FORWARD -p udp --dport 443 -j REJECT 2>/dev/null || break
	i=$((i + 1))
done

# mangle：1.7.2 起加的 TCP MSS 钳制
i=0
while [ $i -lt 5 ]; do
	iptables -t mangle -D FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu 2>/dev/null || break
	i=$((i + 1))
done

mm_log "删除程序文件"
rm -f /koolshare/bin/mihomo
rm -f /koolshare/bin/mihomo-panel
rm -f /koolshare/scripts/mihomo_ctl.sh
rm -f /koolshare/webs/Module_mihomo.asp
rm -f /koolshare/res/icon-mihomo.png
rm -f /koolshare/init.d/S99mihomo.sh
rm -f /koolshare/init.d/N99mihomo.sh

mm_log "删除数据目录（订阅、节点文件、规则、内核备份、日志）"
# 只删本插件自己的目录，路径写死，避免误删
if [ -d "$DATA_DIR" ]; then
	rm -rf "$DATA_DIR"
fi

# 老版本可能把日志写到 /tmp，一并清掉
rm -f /tmp/mihomo-panel.log /tmp/mihomo.log

if [ -n "$DBUS" ]; then
	for k in name title description version install; do
		"$DBUS" remove "softcenter_module_${module}_${k}" 2>/dev/null
	done
fi

# 清理自身（软件中心卸载脚本），避免留下孤儿
rm -f "/koolshare/scripts/uninstall_${module}.sh"

AFTER_KB=$(dir_kb "$DATA_DIR")
if [ "${BEFORE_KB:-0}" -gt 0 ]; then
	if [ "$BEFORE_KB" -ge 1024 ]; then
		mm_log "已释放约 $((BEFORE_KB / 1024)) MB（数据目录 ${BEFORE_KB} KB，剩余 ${AFTER_KB} KB）"
	else
		mm_log "已释放 ${BEFORE_KB} KB（数据目录已清空，剩余 ${AFTER_KB} KB）"
	fi
fi

if [ -d "$DATA_DIR" ]; then
	mm_log "警告：数据目录仍在：$DATA_DIR"
else
	mm_log "卸载完成，程序与数据已全部删除"
fi
