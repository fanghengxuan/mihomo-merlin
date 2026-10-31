#!/bin/sh
# mihomo-merlin 安装脚本（Asuswrt-Merlin / gnuton，无软件中心）
#
# 安装位置：/jffs/mihomo-merlin
# 自启动：/jffs/scripts/services-start / services-stop（托管块，可重复安装）
# 卸载：sh /jffs/mihomo-merlin/uninstall.sh [--keep-data]
#
# MM_ROOT 仅用于测试（假根目录），路由器上不要设置。

DIR=$(cd "$(dirname "$0")" && pwd)
ROOT=${MM_ROOT:-}
JFFS=$ROOT/jffs
BASE=$JFFS/mihomo-merlin
BIN=$BASE/bin
DATA=$BASE/data
SCRIPTS=$JFFS/scripts
PLVER=$(cat "$DIR/version" 2>/dev/null || echo unknown)

mm_log() { echo "【$(date '+%Y-%m-%d %H:%M:%S')】: $*"; }
fail() { mm_log "$*"; exit 1; }

mm_log "================================================"
mm_log " Mihomo 魔方 v${PLVER}（Asuswrt-Merlin 安装程序）"
mm_log " 机型：$(nvram get productid 2>/dev/null || echo 未知)  架构：$(uname -m)  内核：$(uname -r)"
mm_log "================================================"

[ -d "$JFFS" ] || fail "未检测到 /jffs。Merlin 固件需要在「系统管理 → 系统设置」里启用 JFFS。"
[ -x "$DIR/bin/mihomo-panel" ] || fail "安装包缺少 bin/mihomo-panel"

ARCH=$(uname -m)
PKG_ARCH=$(cat "$DIR/arch" 2>/dev/null)
[ -n "$PKG_ARCH" ] || PKG_ARCH=armv7l
[ "$ARCH" = "$PKG_ARCH" ] || fail "本安装包是 ${PKG_ARCH} 架构，当前设备是 ${ARCH}。请换对应架构的安装包。"

SCRIPTS_ON=$(nvram get jffs2_scripts 2>/dev/null || echo "")
if [ "$SCRIPTS_ON" != "1" ]; then
	if [ "${MM_ALLOW_NO_SCRIPTS:-0}" = "1" ]; then
		mm_log "警告：JFFS 自定义脚本未开启，开机自启不会生效（继续安装）"
	else
		fail "请先在「系统管理 → 系统设置」勾选 Enable JFFS custom scripts and configs（或执行 nvram set jffs2_scripts=1 && nvram commit），再重新安装。"
	fi
fi

PAYLOAD_KB=$(du -sk "$DIR" 2>/dev/null | awk '{print $1}')
[ -n "$PAYLOAD_KB" ] || PAYLOAD_KB=0
NEED_KB=$((PAYLOAD_KB + 1024))
[ "$NEED_KB" -ge 2048 ] || NEED_KB=2048
# 自检：在写入前确认二进制能在这台机器上跑
"$DIR/bin/mihomo-panel" -v >/dev/null 2>&1 || fail "面板程序无法运行（架构不匹配或文件损坏）"
if [ -f "$DIR/bin/mihomo" ]; then
	"$DIR/bin/mihomo" -v >/dev/null 2>&1 || fail "内核无法运行（架构不匹配或文件损坏）"
fi

# 停止旧版本
if [ -x "$BASE/ctl.sh" ]; then
	mm_log "检测到已安装版本，先停止"
	"$BASE/ctl.sh" stop >/dev/null 2>&1
fi

mkdir -p "$BIN" "$DATA/rules" "$DATA/providers" "$SCRIPTS"

# 先更新控制脚本，再用它把 logs/kernel 迁到 /tmp（旧版本没有 prepare 也能执行新脚本）
cp -f "$DIR/scripts/ctl.sh" "$BASE/ctl.sh" || fail "复制控制脚本失败"
chmod 755 "$BASE/ctl.sh"
"$BASE/ctl.sh" prepare >/dev/null 2>&1

# 把旧二进制挪到 /tmp 暂存：jffs2 碎片化时，峰值只保留一份新文件
OLD_BACKUP=""
for f in mihomo mihomo-panel; do
	if [ -e "$BIN/$f" ]; then
		if mv -f "$BIN/$f" "/tmp/.mm-merlin-old-$f.$$" 2>/dev/null; then
			OLD_BACKUP="$OLD_BACKUP $f"
		else
			rm -f "$BIN/$f" 2>/dev/null
		fi
	fi
done

restore_old() {
	for f in $OLD_BACKUP; do
		[ -e "/tmp/.mm-merlin-old-$f.$$" ] && mv -f "/tmp/.mm-merlin-old-$f.$$" "$BIN/$f" 2>/dev/null
	done
}

FREE_KB=$(df -k "$JFFS" 2>/dev/null | awk 'NR==2 {print $4}')
if [ -n "$FREE_KB" ] && [ "$FREE_KB" -lt "$NEED_KB" ] 2>/dev/null; then
	restore_old
	fail "剩余空间不足：本包需要 ${NEED_KB}KB，当前只有 ${FREE_KB}KB。请清理 /jffs 后重试。"
fi
mm_log "安装前剩余空间：${FREE_KB:-未知}KB（本包需要 ${NEED_KB}KB）"

install_file() {
	src=$1
	dst=$2
	if ! cp -f "$src" "$dst.tmp"; then
		rm -f "$dst.tmp" 2>/dev/null
		restore_old
		fail "复制 $src 失败（空间不足或文件被占用）"
	fi
	chmod 755 "$dst.tmp" 2>/dev/null
	if ! mv -f "$dst.tmp" "$dst"; then
		rm -f "$dst.tmp" 2>/dev/null
		restore_old
		fail "写入 $dst 失败"
	fi
}

mm_log "安装面板与内核"
install_file "$DIR/bin/mihomo-panel" "$BIN/mihomo-panel"
if [ -f "$DIR/bin/mihomo" ]; then
	install_file "$DIR/bin/mihomo" "$BIN/mihomo"
else
	mm_log "本包不含内核，稍后可在面板「设置 → 内核 → 检查更新」下载，或手动上传"
fi
for f in $OLD_BACKUP; do rm -f "/tmp/.mm-merlin-old-$f.$$" 2>/dev/null; done

[ -f "$DIR/rules/chnroute.txt" ] && cp -f "$DIR/rules/chnroute.txt" "$DATA/rules/chnroute.txt" 2>/dev/null
cp -f "$DIR/uninstall.sh" "$BASE/uninstall.sh" || fail "复制卸载脚本失败"
cp -f "$DIR/version" "$BASE/version" 2>/dev/null
chmod 755 "$BASE/uninstall.sh"

# 在 services-start / services-stop 中维护托管块（重复安装不会叠加）
install_hook() {
	file=$1
	action=$2
	[ -f "$file" ] || printf '#!/bin/sh\n' >"$file"
	tmp="$file.mm.$$"
	awk '
		/^# >>> mihomo-merlin >>>/{skip=1; next}
		/^# <<< mihomo-merlin <<</{skip=0; next}
		skip!=1{print}
	' "$file" >"$tmp" || { rm -f "$tmp"; fail "更新 $file 失败"; }
	cat "$tmp" >"$file"
	rm -f "$tmp"
	{
		echo ""
		echo "# >>> mihomo-merlin >>>"
		echo "$BASE/ctl.sh $action"
		echo "# <<< mihomo-merlin <<<"
	} >>"$file"
	chmod a+rx "$file"
}

mm_log "配置开机自启与防火墙钩子"
install_hook "$SCRIPTS/services-start" start
install_hook "$SCRIPTS/services-stop" stop
install_hook "$SCRIPTS/nat-start" reapply

mm_log "启动面板"
"$BASE/ctl.sh" start || fail "面板启动失败，查看 $DATA/logs/panel.log"

LANIP=$(nvram get lan_ipaddr 2>/dev/null)
[ -n "$LANIP" ] || LANIP="路由器IP"
mm_log "================================================"
mm_log " 安装完成"
mm_log " 管理面板：http://${LANIP}:9380"
mm_log " 卸载：sh $BASE/uninstall.sh [--keep-data]"
mm_log " 注意：首次使用请在「概览」点一次启动，之后开机会自动拉起内核"
mm_log "================================================"
