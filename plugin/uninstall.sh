#!/bin/sh
# mihomo 插件卸载脚本：停止服务、清理规则、删除文件，保留订阅数据（可手动删除）

module=mihomo
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

mm_log "停止面板与内核"
if [ -x /koolshare/scripts/mihomo_ctl.sh ]; then
	sh /koolshare/scripts/mihomo_ctl.sh stop >/dev/null 2>&1
fi

# 兜底清理 iptables
iptables -t nat -D PREROUTING -i br0 -j MIHOMO 2>/dev/null
iptables -t nat -F MIHOMO 2>/dev/null
iptables -t nat -X MIHOMO 2>/dev/null
iptables -t nat -D OUTPUT -j MIHOMO_OUT 2>/dev/null
iptables -t nat -F MIHOMO_OUT 2>/dev/null
iptables -t nat -X MIHOMO_OUT 2>/dev/null
iptables -D FORWARD -p udp --dport 443 -j REJECT 2>/dev/null

mm_log "删除程序文件"
rm -f /koolshare/bin/mihomo
rm -f /koolshare/bin/mihomo-panel
rm -f /koolshare/scripts/mihomo_ctl.sh
rm -f /koolshare/webs/Module_mihomo.asp
rm -f /koolshare/res/icon-mihomo.png
rm -f /koolshare/init.d/S99mihomo.sh
rm -f /koolshare/init.d/N99mihomo.sh

mm_log "保留订阅与配置数据：/koolshare/mihomo（如需彻底清除：rm -rf /koolshare/mihomo）"

if [ -n "$DBUS" ]; then
	"$DBUS" remove softcenter_module_${module}_name 2>/dev/null
	"$DBUS" remove softcenter_module_${module}_title 2>/dev/null
	"$DBUS" remove softcenter_module_${module}_description 2>/dev/null
	"$DBUS" remove softcenter_module_${module}_version 2>/dev/null
	"$DBUS" remove softcenter_module_${module}_install 2>/dev/null
fi

# 清理自身（软件中心卸载脚本），避免留下孤儿
rm -f /koolshare/scripts/uninstall_${module}.sh
rm -f /koolshare/mihomo/.config.stamp

mm_log "卸载完成"
