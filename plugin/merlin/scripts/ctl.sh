#!/bin/sh
# mihomo-merlin 控制脚本（Asuswrt-Merlin / gnuton，无软件中心）
# 用法：ctl.sh {start|stop|restart|status}
#
# 面板自己负责内核启停与 iptables；本脚本只做进程托管和兜底清理。
# MM_ROOT 仅用于测试（假根目录），路由器上不要设置。

ROOT=${MM_ROOT:-}
BASE=$ROOT/jffs/mihomo-merlin
BIN=$BASE/bin
DATA=$BASE/data
LOG=$DATA/logs/panel.log
PID=$DATA/panel.pid
WATCH=$DATA/panel-watchdog.pid
PORT=${MM_PORT:-9380}
PANEL=$BIN/mihomo-panel
CORE=$BIN/mihomo

mm_log() { echo "【$(date '+%Y-%m-%d %H:%M:%S')】: $*"; }

panel_running() {
	[ -f "$PID" ] || return 1
	p=$(cat "$PID" 2>/dev/null)
	[ -n "$p" ] || return 1
	kill -0 "$p" 2>/dev/null
}

find_pid_by_path() {
	want=$1
	for d in /proc/[0-9]*; do
		[ -r "$d/cmdline" ] || continue
		cmd=$(tr '\0' ' ' <"$d/cmdline" 2>/dev/null)
		case "$cmd" in
			*"$want"*) echo "${d#/proc/}"; return 0 ;;
		esac
	done
	return 1
}

start_panel() {
	mkdir -p "$DATA/rules" "$DATA/logs" "$DATA/providers"
	if panel_running; then
		return 0
	fi
	rm -f "$PID"
	if command -v nohup >/dev/null 2>&1; then
		MM_MIHOMO="$CORE" nohup "$PANEL" -listen ":$PORT" -data "$DATA" >>"$LOG" 2>&1 &
	else
		MM_MIHOMO="$CORE" "$PANEL" -listen ":$PORT" -data "$DATA" >>"$LOG" 2>&1 &
	fi
	echo $! >"$PID"
	sleep 1
	panel_running
}

start_watchdog() {
	if [ -f "$WATCH" ] && kill -0 "$(cat "$WATCH" 2>/dev/null)" 2>/dev/null; then
		return 0
	fi
	(
		while true; do
			sleep 60
			panel_running || start_panel
		done
	) &
	echo $! >"$WATCH"
}

stop_watchdog() {
	if [ -f "$WATCH" ]; then
		wp=$(cat "$WATCH" 2>/dev/null)
		[ -n "$wp" ] && kill "$wp" 2>/dev/null
		rm -f "$WATCH"
	fi
}

clear_iptables() {
	ipt=$(command -v iptables 2>/dev/null)
	[ -n "$ipt" ] || return 0
	lanif=$(nvram get lan_ifname 2>/dev/null)
	[ -n "$lanif" ] || lanif=br0
	i=0
	while [ "$i" -lt 5 ]; do
		"$ipt" -t nat -D PREROUTING -i "$lanif" -j MIHOMO 2>/dev/null || break
		i=$((i + 1))
	done
	i=0
	while [ "$i" -lt 5 ]; do
		"$ipt" -t nat -D OUTPUT -j MIHOMO_OUT 2>/dev/null || break
		i=$((i + 1))
	done
	"$ipt" -t nat -F MIHOMO 2>/dev/null
	"$ipt" -t nat -X MIHOMO 2>/dev/null
	"$ipt" -t nat -F MIHOMO_OUT 2>/dev/null
	"$ipt" -t nat -X MIHOMO_OUT 2>/dev/null
	"$ipt" -D FORWARD -p udp --dport 443 -j REJECT 2>/dev/null
	i=0
	while [ "$i" -lt 3 ]; do
		"$ipt" -t mangle -D FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu 2>/dev/null || break
		i=$((i + 1))
	done
}

stop_all() {
	stop_watchdog
	if panel_running; then
		p=$(cat "$PID" 2>/dev/null)
		kill -TERM "$p" 2>/dev/null
		i=0
		while [ "$i" -lt 10 ] && kill -0 "$p" 2>/dev/null; do
			sleep 1
			i=$((i + 1))
		done
		kill -9 "$p" 2>/dev/null
	fi
	# 面板异常退出时可能留下内核进程，按安装路径精确回收
	i=0
	while [ "$i" -lt 3 ]; do
		cp=$(find_pid_by_path "$CORE")
		[ -n "$cp" ] || break
		kill -TERM "$cp" 2>/dev/null
		sleep 1
		i=$((i + 1))
	done
	rm -f "$PID"
	clear_iptables
}

case "${1:-}" in
	start)
		[ -x "$PANEL" ] || { mm_log "找不到面板：$PANEL"; exit 1; }
		if start_panel; then
			start_watchdog
			mm_log "面板已启动：http://<路由器IP>:$PORT"
		else
			mm_log "面板启动失败，查看 $LOG"
			exit 1
		fi
		;;
	stop)
		stop_all
		mm_log "已停止"
		;;
	restart)
		stop_all
		sleep 1
		"$0" start
		;;
	status)
		if panel_running; then
			echo "panel: running (pid $(cat "$PID"))"
		else
			echo "panel: stopped"
		fi
		cp=$(find_pid_by_path "$CORE")
		[ -n "$cp" ] && echo "core:  running (pid $cp)" || echo "core:  stopped"
		[ -f "$DATA/enabled" ] && echo "enabled: yes" || echo "enabled: no"
		;;
	*)
		echo "用法：$0 {start|stop|restart|status}"
		exit 2
		;;
esac
