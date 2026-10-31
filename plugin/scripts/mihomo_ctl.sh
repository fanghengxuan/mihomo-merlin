#!/bin/sh
# mihomo 面板 / 内核控制脚本（兼容 asuswrt busybox ash）
# 用法：mihomo_ctl.sh {start|stop|restart|status}

# 自己设 PATH：/bin 在 /sbin 之前，避免把固件里的同名工具当成 shell
PATH=/koolshare/bin:/bin:/usr/bin:/sbin:/usr/sbin
export PATH

PANEL_BIN=/koolshare/bin/mihomo-panel
MIHOMO_BIN=/koolshare/bin/mihomo
DATA_DIR=/koolshare/mihomo
LOG_FILE=$DATA_DIR/logs/panel.log
PORT=9380

unalias echo_date >/dev/null 2>&1

ensure_dirs() {
	mkdir -p "$DATA_DIR/providers" "$DATA_DIR/rules" "$DATA_DIR/logs"
}

# 优先 pidof；部分 busybox 精简了 pidof，退回扫描 /proc
find_pid_by_name() {
	name="$1"
	pid=$(pidof "$name" 2>/dev/null | awk '{print $1}')
	if [ -n "$pid" ]; then
		echo "$pid"
		return 0
	fi
	for d in /proc/[0-9]*; do
		[ -r "$d/cmdline" ] || continue
		cmd=$(tr '\0' ' ' < "$d/cmdline" 2>/dev/null)
		case "$cmd" in
			*"$name"*)
				echo "${d#/proc/}"
				return 0
				;;
		esac
	done
	return 1
}

panel_pid() { find_pid_by_name mihomo-panel; }
WATCH_PID="$DATA_DIR/panel-watchdog.pid"

# 守护进程：面板挂了就拉起来（面板自己会按 enabled 标记决定是否启动内核）
start_watchdog() {
	wp=$(cat "$WATCH_PID" 2>/dev/null)
	if [ -n "$wp" ] && kill -0 "$wp" 2>/dev/null; then
		return 0
	fi
	(
		while true; do
			sleep 60
			# 面板被显式停止（enabled 标记没了）就不再拉起，避免“停不掉”
			[ -f "$DATA_DIR/enabled" ] || exit 0
			if [ -z "$(panel_pid)" ]; then
				echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】看门狗：面板已退出，正在重新拉起" >>"$LOG_FILE"
				if [ -x /sbin/start-stop-daemon ]; then
					/sbin/start-stop-daemon -S -b -x "$PANEL_BIN" -- -listen ":$PORT" -data "$DATA_DIR" >>"$LOG_FILE" 2>&1
				else
					"$PANEL_BIN" -listen ":$PORT" -data "$DATA_DIR" >>"$LOG_FILE" 2>&1 &
				fi
			fi
		done
	) &
	echo $! > "$WATCH_PID"
}

stop_watchdog() {
	wp=$(cat "$WATCH_PID" 2>/dev/null)
	[ -n "$wp" ] && kill "$wp" 2>/dev/null
	rm -f "$WATCH_PID"
}
core_pid() { find_pid_by_name "/koolshare/bin/mihomo "; }

start() {
	ensure_dirs
	[ -x "$PANEL_BIN" ] || { echo "找不到 $PANEL_BIN"; return 1; }
	pid=$(panel_pid)
	if [ -n "$pid" ]; then
		echo "面板已在运行（pid ${pid}）"
		return 0
	fi

	if [ -x /sbin/start-stop-daemon ]; then
		/sbin/start-stop-daemon -S -b -x "$PANEL_BIN" -- -listen ":$PORT" -data "$DATA_DIR" >>"$LOG_FILE" 2>&1
	elif [ -x /usr/sbin/start-stop-daemon ]; then
		/usr/sbin/start-stop-daemon -S -b -x "$PANEL_BIN" -- -listen ":$PORT" -data "$DATA_DIR" >>"$LOG_FILE" 2>&1
	else
		"$PANEL_BIN" -listen ":$PORT" -data "$DATA_DIR" >>"$LOG_FILE" 2>&1 &
	fi

	i=0
	while [ $i -lt 15 ]; do
		sleep 0.2
		pid=$(panel_pid)
		[ -n "$pid" ] && break
		i=$((i + 1))
	done
	if [ -n "$pid" ]; then
		start_watchdog
		echo "面板已启动（pid ${pid}），端口 ${PORT}（已启用 60 秒守护）"
	else
		echo "面板启动失败，请查看 $LOG_FILE"
		return 1
	fi
}

cleanup_iptables() {
	LANIF=$(nvram get lan_ifname 2>/dev/null)
	[ -n "$LANIF" ] || LANIF=br0
	i=0
	while [ $i -lt 5 ]; do
		iptables -t nat -D PREROUTING -i "$LANIF" -j MIHOMO 2>/dev/null || break
		i=$((i + 1))
	done
	i=0
	while [ $i -lt 5 ]; do
		iptables -t nat -D OUTPUT -j MIHOMO_OUT 2>/dev/null || break
		i=$((i + 1))
	done
	iptables -t nat -F MIHOMO 2>/dev/null
	iptables -t nat -X MIHOMO 2>/dev/null
	iptables -t nat -F MIHOMO_OUT 2>/dev/null
	iptables -t nat -X MIHOMO_OUT 2>/dev/null
	iptables -D FORWARD -p udp --dport 443 -j REJECT 2>/dev/null
}

stop() {
	stop_watchdog
	pid=$(panel_pid)
	if [ -n "$pid" ]; then
		kill "$pid" 2>/dev/null
		# 给面板时间：它会停掉内核并清理规则
		i=0
		while [ $i -lt 25 ]; do
			sleep 0.2
			[ -z "$(panel_pid)" ] && break
			i=$((i + 1))
		done
	fi
	pid=$(panel_pid)
	[ -n "$pid" ] && kill -9 "$pid" 2>/dev/null

	for p in $(pidof mihomo 2>/dev/null); do
		kill "$p" 2>/dev/null
	done
	pid=$(core_pid)
	[ -n "$pid" ] && kill "$pid" 2>/dev/null

	# 兜底：面板异常退出时也要恢复直连
	cleanup_iptables
	echo "已停止"
}

status() {
	if [ -n "$(panel_pid)" ]; then
		echo "panel: running (pid $(panel_pid))"
	else
		echo "panel: stopped"
	fi
	if [ -n "$(pidof mihomo 2>/dev/null)" ] || [ -n "$(core_pid)" ]; then
		echo "core:  running"
	else
		echo "core:  stopped"
	fi
}

case "$1" in
	start) start ;;
	stop) stop ;;
	restart) stop; sleep 0.5; start ;;
	status) status ;;
	*)
		echo "用法: $0 {start|stop|restart|status}"
		exit 1
		;;
esac
