#!/bin/sh
# mihomo-merlin 构建脚本：交叉编译面板 + 拉取内核/规则 + 打包成软件中心可安装的 tar.gz
#
# 用法：
#   ./build.sh                        # 完整打包（armv7，自动下载 fork 的 slim 内核）
#   ./build.sh --kernel /path/to/mihomo   # 用自己提供的内核（例如 UPX 精简版）
#   ./build.sh --no-download          # 只用本地缓存的内核与规则
#   ./build.sh --dev                  # 额外编译一个本机（darwin/arm64）可跑的 panel，便于本地调试
#
# 依赖：go（1.21+）、curl、tar、gzip

set -eu

VERSION=$(cat "$(dirname "$0")/plugin/version" 2>/dev/null || echo 1.0.0)
MIHOMO_VERSION="${MIHOMO_VERSION:-v1.19.33}"
KERNEL_SRC="${MIHOMO_BIN:-}"      # 外部内核路径（--kernel 或 MIHOMO_BIN 环境变量）
KERNEL_BIN=""                     # 最终打进包里的内核
CHNROUTE_URLS="https://cdn.jsdelivr.net/gh/hq450/fancyss@3.0/rules_ng/chnroute.txt
https://raw.githubusercontent.com/hq450/fancyss/3.0/rules_ng/chnroute.txt
https://gitee.com/raw/hq450/fancyss/3.0/rules_ng/chnroute.txt"
# 内核默认取本仓库 fork 的 slim 构建（体积小、适配低内存路由器）
# 也可用 --kernel 指定本地文件，或用 MIHOMO_URL 覆盖
KERNEL_REPO="${KERNEL_REPO:-fanghengxuan/mihomo}"
MIHOMO_URL="${MIHOMO_URL:-https://github.com/${KERNEL_REPO}/releases/download/slim-${MIHOMO_VERSION}/mihomo-linux-armv7-slim-${MIHOMO_VERSION}}"

ROOT=$(cd "$(dirname "$0")" && pwd)
CACHE="$ROOT/.cache"
DIST="$ROOT/dist"
STAGE="$DIST/stage"
PKG="mihomo_${VERSION}_armv7"
GO=${GO:-go}

need() { command -v "$1" >/dev/null 2>&1 || { echo "缺少命令：$1"; exit 1; }; }
need "$GO"; need curl; need tar; need gzip

mkdir -p "$CACHE" "$DIST"

fetch_mihomo() {
	out="$CACHE/mihomo-linux-armv7-${MIHOMO_VERSION}-slim"
	if [ -s "$out" ]; then
		echo ">>> 使用缓存内核：$out"
		return 0
	fi
	echo ">>> 下载 mihomo ${MIHOMO_VERSION} slim 构建（${KERNEL_REPO}）"
	curl -fL --retry 3 -o "$out.part" "$MIHOMO_URL"
	# 兼容 .gz 资产与原始 ELF 资产
	if gzip -t "$out.part" 2>/dev/null; then
		mv "$out.part" "$out.gz"
		gunzip -f "$out.gz"
	else
		mv "$out.part" "$out"
	fi
	chmod +x "$out"
}

fetch_rules() {
	out="$CACHE/chnroute.txt"
	if [ -s "$out" ]; then
		echo ">>> 使用缓存规则：$out"
		return 0
	fi
	echo ">>> 下载国内 IP 列表（fancyss rules_ng，多镜像回退）"
	echo "$CHNROUTE_URLS" | while read -r u; do
		[ -n "$u" ] || continue
		echo "    - $u"
		if curl -fL --retry 2 --connect-timeout 10 -m 120 -o "$out.part" "$u" && [ -s "$out.part" ]; then
			mv "$out.part" "$out"
			echo "    下载成功（$(wc -c < "$out") 字节）"
			break
		fi
		rm -f "$out.part"
	done
	[ -s "$out" ] || { echo "规则下载失败，可手动放入 $out"; exit 1; }
}

build_panel() {
	echo ">>> 交叉编译面板 (linux/arm GOARM=7)"
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
		"$GO" build -trimpath -ldflags "-s -w" -o "$CACHE/mihomo-panel-linux-armv7" .
	if command -v file >/dev/null 2>&1; then
		file "$CACHE/mihomo-panel-linux-armv7"
	fi
}

build_dev() {
	echo ">>> 编译本机调试版本"
	"$GO" build -o "$CACHE/mihomo-panel-local" .
}

stage_package() {
	rm -rf "$STAGE"
	mkdir -p "$STAGE/mihomo/bin" "$STAGE/mihomo/rules" "$STAGE/mihomo/scripts" "$STAGE/mihomo/webs" "$STAGE/mihomo/res"
	cp -f "$ROOT/plugin/install.sh" "$STAGE/mihomo/"
	cp -f "$ROOT/plugin/uninstall.sh" "$STAGE/mihomo/"
	cp -f "$ROOT/plugin/version" "$STAGE/mihomo/"
	# 软件中心靠这个标记判断离线包是不是 hnd 平台，缺了会直接拒绝安装
	printf 'hnd\n' > "$STAGE/mihomo/.valid"
	cp -f "$ROOT"/plugin/scripts/* "$STAGE/mihomo/scripts/"
	cp -f "$ROOT/plugin/webs/Module_mihomo.asp" "$STAGE/mihomo/webs/"
	cp -f "$ROOT/plugin/res/icon-mihomo.png" "$STAGE/mihomo/res/"
	cp -f "$KERNEL_BIN" "$STAGE/mihomo/bin/mihomo"
	cp -f "$CACHE/mihomo-panel-linux-armv7" "$STAGE/mihomo/bin/mihomo-panel"
	cp -f "$CACHE/chnroute.txt" "$STAGE/mihomo/rules/chnroute.txt"
	chmod 755 "$STAGE/mihomo/install.sh" "$STAGE/mihomo/uninstall.sh" "$STAGE/mihomo/scripts/"*.sh
	chmod 644 "$STAGE/mihomo/version" "$STAGE/mihomo/.valid" "$STAGE/mihomo/webs/Module_mihomo.asp" "$STAGE/mihomo/res/icon-mihomo.png"
	chmod 755 "$STAGE/mihomo/bin/mihomo" "$STAGE/mihomo/bin/mihomo-panel"
}

pack() {
	echo ">>> 打包 $PKG.tar.gz"
	( cd "$STAGE" && tar czf "$DIST/$PKG.tar.gz" mihomo )
	( cd "$DIST" && shasum -a 256 "$PKG.tar.gz" > "$PKG.tar.gz.sha256" 2>/dev/null || sha256sum "$PKG.tar.gz" > "$PKG.tar.gz.sha256" )
	echo
	echo "===================================================="
	ls -lh "$DIST/$PKG.tar.gz"
	cat "$DIST/$PKG.tar.gz.sha256"
	echo "包内内核：$(wc -c < "$STAGE/mihomo/bin/mihomo") 字节  面板：$(wc -c < "$STAGE/mihomo/bin/mihomo-panel") 字节"
	echo "安装：软件中心 → 离线安装 → 选择上面的 tar.gz"
	echo "===================================================="
}

MODE="full"
while [ $# -gt 0 ]; do
	case "$1" in
		--no-download) MODE="offline" ;;
		--dev) build_dev ;;
		--kernel)
			shift
			[ $# -gt 0 ] || { echo "--kernel 需要一个文件路径"; exit 1; }
			KERNEL_SRC="$1"
			;;
		--kernel=*) KERNEL_SRC="${1#--kernel=}" ;;
		*) echo "未知参数：$1"; exit 1 ;;
	esac
	shift
done

if [ -n "$KERNEL_SRC" ]; then
	[ -f "$KERNEL_SRC" ] || { echo "找不到指定的内核：$KERNEL_SRC"; exit 1; }
	KERNEL_BIN="$KERNEL_SRC"
	echo ">>> 使用外部内核：${KERNEL_BIN}（$(wc -c < "$KERNEL_BIN") 字节）"
	if command -v file >/dev/null 2>&1; then
		file "$KERNEL_BIN"
	fi
else
	case "$MODE" in
		full)
			fetch_mihomo
			fetch_rules
			;;
		offline)
			[ -s "$CACHE/mihomo-linux-armv7-${MIHOMO_VERSION}-slim" ] || { echo "缓存里没有内核"; exit 1; }
			[ -s "$CACHE/chnroute.txt" ] || { echo "缓存里没有规则"; exit 1; }
			;;
	esac
	KERNEL_BIN="$CACHE/mihomo-linux-armv7-${MIHOMO_VERSION}-slim"
fi
[ -s "$CACHE/chnroute.txt" ] || { [ -s "$CACHE/chnroute.txt" ] || fetch_rules; }

build_panel

# 前端一致性检查：标签平衡 / id 引用 / 弹窗位置 / app.js 语法
if command -v python3 >/dev/null 2>&1; then
	echo ">>> 前端一致性检查"
	python3 "$ROOT/tests/ui_check.py" || { echo "!!! 前端检查未通过，停止打包"; exit 1; }
fi

stage_package
pack
