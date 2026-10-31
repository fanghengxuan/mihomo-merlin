#!/bin/sh
# 实际的文件安装动作放在这里：install.sh 只做检查与调用，
# 避免安装脚本里出现大量对 /koolshare 下文件的写操作（软件中心会做静态检查）。

DIR=$(cd "$(dirname "$0")/.." && pwd)
module=mihomo

[ -f /koolshare/scripts/base.sh ] && . /koolshare/scripts/base.sh 2>/dev/null
PATH=/koolshare/bin:/bin:/usr/bin:/sbin:/usr/sbin
export PATH
SH=/bin/sh
unalias echo_date >/dev/null 2>&1
unalias echo >/dev/null 2>&1

mm_log() {
	echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】: $*"
}
fail() {
	mm_log "$*"
	exit 1
}

DBUS=""
for c in /koolshare/bin/dbus /usr/bin/dbus /usr/sbin/dbus /bin/dbus /jffs/koolshare/bin/dbus; do
	[ -x "$c" ] && { DBUS=$c; break; }
done
[ -z "$DBUS" ] && DBUS=$(command -v dbus 2>/dev/null)

mkdir -p /koolshare/bin /koolshare/webs /koolshare/res /koolshare/scripts /koolshare/init.d
mkdir -p /koolshare/mihomo/providers /koolshare/mihomo/rules /koolshare/mihomo/logs

# install_file 源 目标：先写同目录临时文件再 rename 覆盖。
# 原因：部分固件的 busybox cp 在目标已存在时会报 "can't create ... File exists"，
# 而且正在运行的二进制不能被 open 覆盖（ETXTBSY），但 rename 替换是允许的。
install_file() {
	src=$1
	dst=$2
	dir=$(dirname "$dst")
	base=$(basename "$dst")
	tmp="$dir/.${base}.new.$$"

	rm -f "$tmp" 2>/dev/null
	if ! cat "$src" > "$tmp" 2>/tmp/mm_cp.err; then
		# cat 也失败时退回 cp
		if ! cp -f "$src" "$tmp" 2>/tmp/mm_cp.err; then
			mm_log "写入临时文件失败：$(cat /tmp/mm_cp.err 2>/dev/null)"
			rm -f "$tmp" 2>/dev/null
			return 1
		fi
	fi
	chmod 755 "$tmp" 2>/dev/null

	if mv -f "$tmp" "$dst" 2>/tmp/mm_mv.err; then
		return 0
	fi

	# 兜底：先把旧文件挪走（改名对运行中的程序也有效），再放新文件
	if [ -e "$dst" ]; then
		mv -f "$dst" "${dst}.old.$$" 2>/dev/null && rm -f "${dst}.old.$$" 2>/dev/null
	fi
	if mv -f "$tmp" "$dst" 2>>/tmp/mm_mv.err; then
		return 0
	fi

	mm_log "替换 $dst 失败：$(cat /tmp/mm_mv.err 2>/dev/null | tail -1)"
	rm -f "$tmp" 2>/dev/null
	return 1
}

# 再彻底停一次：旧面板/内核若还在跑，会占着 /koolshare/bin 下的二进制
killall -q mihomo-panel 2>/dev/null
killall -q mihomo 2>/dev/null
i=0
while [ $i -lt 5 ]; do
	if pidof mihomo >/dev/null 2>&1 || pidof mihomo-panel >/dev/null 2>&1; then
		sleep 1
		i=$((i + 1))
	else
		break
	fi
done
killall -q -9 mihomo-panel 2>/dev/null
killall -q -9 mihomo 2>/dev/null
mkdir -p /koolshare/bin

mm_log "复制 mihomo 内核与面板程序，请稍候"

# 先把旧二进制挪到 RAM(/tmp)，立刻腾出 jffs 空间：
# jffs2 在剩余空间碎片化时，即使 df 显示还有十几 MB，写入大文件也可能报
# "No space left on device"。挪走旧文件后峰值只需一份新文件的大小。
OLD_BACKUP=""
for f in mihomo mihomo-panel; do
	if [ -e "/koolshare/bin/$f" ]; then
		if mv -f "/koolshare/bin/$f" "/tmp/.mm-old-$f.$$" 2>/dev/null; then
			OLD_BACKUP="$OLD_BACKUP $f"
		else
			rm -f "/koolshare/bin/$f" 2>/dev/null
		fi
	fi
done
[ -n "$OLD_BACKUP" ] && mm_log "已把旧程序搬到 /tmp 暂存（安装完成后删除）：$OLD_BACKUP"

# 再把「内核备份」挪走：老版本每次升级都会留一份 7.9MB 的内核备份，
# 最多 3 份 ≈ 24MB，是 jffs 里最大的可回收对象（我们马上要装新内核，旧备份只会占地方）
if [ -d /koolshare/mihomo/kernel ]; then
	OLD_KB=$(du -sk /koolshare/mihomo/kernel 2>/dev/null | awk '{print $1}')
	if [ "${OLD_KB:-0}" -gt 1024 ]; then
		if mv /koolshare/mihomo/kernel "/tmp/.mm-old-kernel.$$" 2>/dev/null; then
			mm_log "已把旧内核备份（${OLD_KB}KB）搬到 /tmp 暂存"
			OLD_BACKUP="$OLD_BACKUP kernel"
		else
			rm -rf /koolshare/mihomo/kernel 2>/dev/null && mm_log "已删除旧内核备份（${OLD_KB}KB）"
		fi
	fi
fi

# 日志截断：有的机器 panel.log 长到几 MB
for lf in /koolshare/mihomo/logs/*.log; do
	[ -f "$lf" ] || continue
	sz=$(wc -c < "$lf" 2>/dev/null || echo 0)
	if [ "${sz:-0}" -gt 262144 ]; then
		tail -c 131072 "$lf" > "$lf.tmp" 2>/dev/null && mv -f "$lf.tmp" "$lf" 2>/dev/null && mm_log "已截断日志 ${lf}（原 ${sz} 字节）"
	fi
done

FREE_NOW=$(df -k /koolshare 2>/dev/null | awk 'NR==2 {print $4}')
mm_log "安装前 jffs 剩余：${FREE_NOW:-未知}KB"
restore_old() {
	for f in $OLD_BACKUP; do
		if [ "$f" = "kernel" ]; then
			[ -d "/tmp/.mm-old-kernel.$$" ] && mv "/tmp/.mm-old-kernel.$$" /koolshare/mihomo/kernel 2>/dev/null
		else
			[ -e "/tmp/.mm-old-$f.$$" ] && mv -f "/tmp/.mm-old-$f.$$" "/koolshare/bin/$f" 2>/dev/null
		fi
	done
}
HAVE_KERNEL=0
if [ -f "${DIR}/bin/mihomo" ]; then
	HAVE_KERNEL=1
	install_file "${DIR}/bin/mihomo" /koolshare/bin/mihomo || { restore_old; fail "复制 mihomo 失败（jffs 空间不足或文件被占用）"; }
else
	mm_log "本安装包不含内核，跳过内核安装（稍后在面板里下载或上传）"
fi
install_file "${DIR}/bin/mihomo-panel" /koolshare/bin/mihomo-panel || { restore_old; fail "复制面板失败（jffs 空间不足或文件被占用）"; }
chmod 755 /koolshare/bin/mihomo-panel 2>/dev/null
[ -f /koolshare/bin/mihomo ] && chmod 755 /koolshare/bin/mihomo 2>/dev/null

# 内核自检：架构不对 / 文件损坏时立刻中止，避免装完变砖
if [ "$HAVE_KERNEL" = "1" ]; then
	if ! /koolshare/bin/mihomo -v >/tmp/mihomo_kver 2>&1; then
		mm_log "内核无法运行，输出如下："
		head -5 /tmp/mihomo_kver 2>/dev/null
		fail "内核自检失败（架构不匹配或文件损坏），安装中止"
	fi
	mm_log "  内核：$(head -1 /tmp/mihomo_kver)"
else
	mm_log "提示：打开 http://<路由器IP>:9380 → 设置 → mihomo 内核 → 检查更新（自动下载），或手动上传内核文件"
fi
PANEL_VER=$(/koolshare/bin/mihomo-panel -v 2>&1 | head -1)
[ -n "$PANEL_VER" ] || fail "面板程序自检失败"
# 自检通过后再清掉 /tmp 里的旧程序备份（失败时会自动挪回）
for f in $OLD_BACKUP; do
	if [ "$f" = "kernel" ]; then
		rm -rf "/tmp/.mm-old-kernel.$$" 2>/dev/null
	else
		rm -f "/tmp/.mm-old-$f.$$" 2>/dev/null
	fi
done
mm_log "  ${PANEL_VER}"

mm_log "安装国内 IP 规则文件"
cp -f "${DIR}/rules/chnroute.txt" /koolshare/mihomo/rules/chnroute.txt || mm_log "规则文件缺失，将只做全局代理"

mm_log "安装控制脚本与网页"
cp -f "${DIR}/scripts/mihomo_ctl.sh" /koolshare/scripts/mihomo_ctl.sh
chmod 755 /koolshare/scripts/mihomo_ctl.sh
# 软件中心的「卸载」按钮会调用 /koolshare/scripts/uninstall_<module>.sh
cp -f "${DIR}/uninstall.sh" /koolshare/scripts/uninstall_${module}.sh
chmod 755 /koolshare/scripts/uninstall_${module}.sh
cp -f "${DIR}"/webs/* /koolshare/webs/
cp -f "${DIR}"/res/* /koolshare/res/
cp -f "${DIR}/version" /koolshare/mihomo/version 2>/dev/null

# 开机自启 & 关机停止
ln -sf /koolshare/scripts/mihomo_ctl.sh /koolshare/init.d/S99mihomo.sh
ln -sf /koolshare/scripts/mihomo_ctl.sh /koolshare/init.d/N99mihomo.sh

# 菜单登记
if [ -n "$DBUS" ]; then
	VER=$(cat "${DIR}/version" 2>/dev/null)
	"$DBUS" set softcenter_module_${module}_name="${module}"
	"$DBUS" set softcenter_module_${module}_title="Mihomo 魔方"
	"$DBUS" set softcenter_module_${module}_description="基于 mihomo 内核的订阅代理，管理面板端口 9380"
	"$DBUS" set softcenter_module_${module}_version="${VER}"
	"$DBUS" set softcenter_module_${module}_install="1"
	mm_log "已登记到软件中心"
else
	mm_log "未找到 dbus（可能不是软件中心固件），跳过菜单登记，可用命令手动管理"
fi

mm_log "启动面板服务"
"$SH" /koolshare/scripts/mihomo_ctl.sh start

sleep 1
if pidof mihomo-panel >/dev/null 2>&1; then
	mm_log "================================================"
	mm_log " 安装成功！"
	mm_log " 管理面板：http://${LANIP}:9380"
	mm_log " 首次使用：进入「订阅」页添加机场订阅链接，"
	mm_log "           再到「概览」页点「启动」开启代理。"
	mm_log "================================================"
else
	mm_log "面板未启动，请查看 /koolshare/mihomo/logs/panel.log"
fi
