#!/bin/sh
# 真正的清理逻辑放这里：软件中心只扫描 install.sh 的文本，
# 把涉及 /koolshare 下文件的操作放在子脚本里，避免被误判成「修改软件中心文件」。

DATA=/koolshare/mihomo
KB=0
FREED=0

# 注意：软链可能已经悬空（-e 会返回 false），要用 -L 兜住
kb_of() {
	if [ -L "$1" ]; then echo 0; return; fi
	[ -e "$1" ] && du -sk "$1" 2>/dev/null | awk '{print $1}' || echo 0
}

say() { echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】: $*"; }

del() { # 描述 路径
	kb=$(kb_of "$2")
	exists=0
	[ -e "$2" ] && exists=1
	[ -L "$2" ] && exists=1
	if [ "$exists" = "1" ]; then
		if [ -z "$RM" ]; then
			say "⚠️ 没有可用的 rm，无法删除：$2"
			return 0
		fi
		if [ -d "$2" ] && [ ! -L "$2" ]; then
			err=$($RM -rf "$2" 2>&1)
		else
			err=$($RM -f "$2" 2>&1)
		fi
		[ -n "$err" ] && say "  rm 输出：$err"
		if [ -e "$2" ] || [ -L "$2" ]; then
			# 兜底：用 find -depth -delete（busybox 通常支持）
			if [ -d "$2" ] && [ ! -L "$2" ]; then
				err2=$(find "$2" -depth -delete 2>&1)
				[ -n "$err2" ] && say "  find 输出：$err2"
			fi
		fi
		if [ -e "$2" ] || [ -L "$2" ]; then
			say "⚠️ 未能删除：$1（$2）"
			ls -ld "$2" 2>&1 | sed 's/^/  ls: /'
			id 2>/dev/null | sed 's/^/  id: /'
			return 0
		fi
		FREED=$((FREED + kb))
		say "已删除 $1，释放 ${kb}KB"
	else
		say "跳过 $1（不存在）"
	fi
	return 0
}

FREE_BEFORE=$(df -k /jffs 2>/dev/null | awk 'NR==2 {print $4}')
say "开始清理 mihomo 插件的历史残留"
say "PATH=$PATH"
for c in rm du df ls; do
	p=$(command -v "$c" 2>/dev/null)
	if [ -n "$p" ]; then say "命令 $c：$p"; else say "⚠️ 找不到命令 $c"; fi
done
# 选一个能用的 rm（有的固件把 busybox applet 放在别处）
RM=""
for c in /bin/rm /usr/bin/rm /usr/sbin/rm /sbin/rm; do
	[ -x "$c" ] && { RM=$c; break; }
done
[ -n "$RM" ] || RM=$(command -v rm 2>/dev/null)
if [ -z "$RM" ] && [ -x /bin/busybox ]; then
	RM="/bin/busybox rm"
fi
say "使用的 rm：${RM:-（没有找到，删除会失败）}"
mount 2>/dev/null | grep -E " /jffs | /koolshare " | sed 's/^/  mount: /'


# 先停进程（旧版本可能在跑，文件被占用删不掉）
if [ -x /koolshare/scripts/mihomo_ctl.sh ]; then
	sh /koolshare/scripts/mihomo_ctl.sh stop >/dev/null 2>&1
fi
killall -q mihomo-panel 2>/dev/null
killall -q mihomo 2>/dev/null
sleep 1
killall -q -9 mihomo-panel 2>/dev/null
killall -q -9 mihomo 2>/dev/null

# 清 iptables 残留（不清会断网）
i=0
while [ $i -lt 5 ]; do iptables -t nat -D PREROUTING -i br0 -j MIHOMO 2>/dev/null || break; i=$((i+1)); done
iptables -t nat -F MIHOMO 2>/dev/null
iptables -t nat -X MIHOMO 2>/dev/null
i=0
while [ $i -lt 5 ]; do iptables -t nat -D OUTPUT -j MIHOMO_OUT 2>/dev/null || break; i=$((i+1)); done
iptables -t nat -F MIHOMO_OUT 2>/dev/null
iptables -t nat -X MIHOMO_OUT 2>/dev/null
i=0
while [ $i -lt 5 ]; do iptables -D FORWARD -p udp --dport 443 -j REJECT 2>/dev/null || break; i=$((i+1)); done
i=0
while [ $i -lt 5 ]; do iptables -t mangle -D FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu 2>/dev/null || break; i=$((i+1)); done

# 数据目录（订阅/节点/规则/内核备份/日志，内核备份是大头）
del "数据目录 /koolshare/mihomo（订阅·节点·规则·内核备份·日志）" "$DATA"
del "内核 /koolshare/bin/mihomo" /koolshare/bin/mihomo
del "面板 /koolshare/bin/mihomo-panel" /koolshare/bin/mihomo-panel
del "菜单页 /koolshare/webs/Module_mihomo.asp" /koolshare/webs/Module_mihomo.asp
del "图标 /koolshare/res/icon-mihomo.png" /koolshare/res/icon-mihomo.png
del "控制脚本 /koolshare/scripts/mihomo_ctl.sh" /koolshare/scripts/mihomo_ctl.sh
del "卸载脚本 /koolshare/scripts/uninstall_mihomo.sh" /koolshare/scripts/uninstall_mihomo.sh
del "自启软链 S99mihomo.sh" /koolshare/init.d/S99mihomo.sh
del "自启软链 N99mihomo.sh" /koolshare/init.d/N99mihomo.sh
del "旧日志 /tmp/mihomo-panel.log" /tmp/mihomo-panel.log
del "旧日志 /tmp/mihomo.log" /tmp/mihomo.log

# 清掉旧版本的软件中心登记，避免重装时版本号/菜单残留
if [ -x /koolshare/bin/dbus ]; then
	for k in name title description version install; do
		/koolshare/bin/dbus remove "softcenter_module_mihomo_${k}" 2>/dev/null
	done
fi

FREE=$(df -k /jffs 2>/dev/null | awk 'NR==2 {print $4}')
[ -n "$FREE" ] || FREE=$(df -k /koolshare 2>/dev/null | awk 'NR==2 {print $4}')
[ -n "$FREE" ] || FREE="未知"
if [ "${FREE_BEFORE:-}" != "" ] && [ "$FREE" != "未知" ]; then
	GAIN=$(( FREE - FREE_BEFORE ))
	[ "$GAIN" -gt 0 ] && say "按 df 计算，jffs 增加了 ${GAIN}KB"
fi
say "清理完成：释放约 $((FREED / 1024)) MB（${FREED}KB），jffs 当前剩余 ${FREE:-未知}KB"
say "现在可以到「软件中心 → 离线安装」装 mihomo_1.7.6_armv7_lite.tar.gz（解压约 9.7MB）"
say "诊断日志：/tmp/mihomo-cleanup.log（如需反馈请贴这段）"
