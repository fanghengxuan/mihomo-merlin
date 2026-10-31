#!/bin/sh
# mihomo 插件安装脚本（软件中心 / 手动安装通用）
# 目标平台：华硕 AX 系列 32 位 ARM 机型（axhnd.675x / 4908 等，内核 4.1/4.19）

DIR=$(cd "$(dirname "$0")" && pwd)
module=mihomo
PLVER=$(cat "${DIR}/version" 2>/dev/null)
[ -n "$PLVER" ] || PLVER=1.1.3
VERSION=${PLVER}

# source base.sh 以拿到固件提供的 dbus（388 上它可能是 shell 函数，不在 PATH 里）。
# 但 base.sh 把 echo_date 定义成了别名，而 ash 解析 `echo_date() {` 时会展开别名 → 语法错误，
# 所以本脚本的日志函数换名为 mm_log，彻底避开这个坑（unalias 仅作兜底）。
[ -f /koolshare/scripts/base.sh ] && . /koolshare/scripts/base.sh 2>/dev/null

# 注意顺序：/bin 必须排在 /sbin、/usr/sbin 之前。
# 这台固件里 /sbin 或 /usr/sbin 下有个同名 `sh`（Broadcom 的内存读写工具，
# 用法是 dw/dh/db/sw/sh/sb），排在前面会导致 `sh 脚本.sh` 被当成地址参数。
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

MODEL=$(nvram get productid 2>/dev/null)
ARCH=$(uname -m)
KERN=$(uname -r | cut -d. -f1-2)
LANIP=$(nvram get lan_ipaddr 2>/dev/null)
[ -n "$LANIP" ] || LANIP="192.168.50.1"

mm_log "================================================"
mm_log " Mihomo 魔方 v$(cat "${DIR}/version" 2>/dev/null) 安装程序"
mm_log " 机型：${MODEL:-未知}  架构：${ARCH}  内核：$(uname -r)"
mm_log "================================================"

[ -d /koolshare ] || fail "未检测到 /koolshare 目录。此安装包需要带软件中心的固件（koolshare / SWRT 等梅林改版）。"

# dbus 位置随固件版本不同，逐个探测
DBUS=""
for c in /koolshare/bin/dbus /usr/bin/dbus /usr/sbin/dbus /bin/dbus /jffs/koolshare/bin/dbus; do
	if [ -x "$c" ]; then DBUS=$c; break; fi
done
if [ -z "$DBUS" ]; then
	DBUS=$(command -v dbus 2>/dev/null)
fi

case "$ARCH" in
	armv7l) : ;;
	aarch64)
		fail "当前是 aarch64 架构，本包内置的是 32 位 ARM 程序。请改用 arm64 版本的安装包。"
		;;
	*)
		fail "不支持的架构：${ARCH}（本包仅支持 armv7l）"
		;;
esac

case "$KERN" in
	4.1|4.19) mm_log "内核版本 ${KERN} 在支持范围内" ;;
	*) mm_log "提示：内核 ${KERN} 未经验证，安装后会继续，如异常请反馈日志" ;;
esac

# 磁盘空间检查：按「本包实际解压大小」判断，而不是写死 20MB
# （写死 20MB 会把 jffs 只有 17MB 级别的机型全部拦掉，连精简包也装不上）
# MM_PAYLOAD_KB 仅供测试注入（冒烟测试里二进制被换成桩，du 量不出来）
PAYLOAD_KB=${MM_PAYLOAD_KB:-$(du -sk "$DIR" 2>/dev/null | awk '{print $1}')}
[ -n "$PAYLOAD_KB" ] || PAYLOAD_KB=0
NEED_KB=$(( PAYLOAD_KB + 1024 ))   # 留 1MB 余量给配置/日志
[ "$NEED_KB" -ge 2048 ] || NEED_KB=2048
FREE_KB=$(df -k /koolshare 2>/dev/null | awk 'NR==2 {print $4}')
if [ -n "$FREE_KB" ] && [ "$FREE_KB" -lt "$NEED_KB" ] 2>/dev/null; then
	fail "剩余空间不足：本包需要 ${NEED_KB}KB（约 $((NEED_KB / 1024))MB），当前仅 ${FREE_KB}KB。请清理 /jffs 后重试，或改用 lite 包"
fi
mm_log "剩余空间：${FREE_KB:-未知}KB（本包需要 ${NEED_KB}KB）"

# 停止旧版本
if [ -x /koolshare/scripts/mihomo_ctl.sh ]; then
	mm_log "检测到已安装的旧版本，先停止运行"
	"${SH:-/bin/sh}" /koolshare/scripts/mihomo_ctl.sh stop >/dev/null 2>&1
fi

mkdir -p /koolshare/bin /koolshare/webs /koolshare/res /koolshare/scripts /koolshare/init.d
mkdir -p /koolshare/mihomo/providers /koolshare/mihomo/rules /koolshare/mihomo/logs

mm_log "开始安装文件"
/bin/sh "${DIR}/scripts/setup.sh" || fail "安装文件阶段失败"
