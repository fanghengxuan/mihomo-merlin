#!/bin/sh
# 构建 Asuswrt-Merlin（gnuton 等，无软件中心）原生安装包
#
# 用法：
#   ./tools/build-merlin-package.sh
#   NOKERNEL=1 ./tools/build-merlin-package.sh       # 不带内核
#   PANEL_UPX=1 ./tools/build-merlin-package.sh      # 面板 UPX 压缩（需要 upx）
#   MM_MIHOMO=/path/to/mihomo MM_CHNROUTE=/path/to/chnroute.txt ./tools/build-merlin-package.sh
#
# 依赖：Go 1.23+、tar、gzip；内核与规则默认复用 .cache（先跑一次 ./build.sh 即可）。

set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
VERSION=$(cat "$ROOT/plugin/version" 2>/dev/null || echo 0.0.0)
MIHOMO_VERSION=${MIHOMO_VERSION:-v1.19.33}
GO=${GO:-go}
CACHE="$ROOT/.cache"
DIST="$ROOT/dist"
KERNEL=${MM_MIHOMO:-"$CACHE/mihomo-linux-armv7-${MIHOMO_VERSION}-slim"}
RULES=${MM_CHNROUTE:-"$CACHE/chnroute.txt"}

NAME=mihomo-merlin
SUFFIX=""
if [ "${NOKERNEL:-0}" = "1" ]; then
	SUFFIX=_nokernel
fi
PKG="mihomo-merlin_${VERSION}_armv7_merlin${SUFFIX}"
STAGE="$DIST/merlin-stage/$NAME"

rm -rf "$DIST/merlin-stage"
mkdir -p "$STAGE/bin" "$STAGE/rules" "$STAGE/scripts"

echo ">>> 交叉编译面板 (linux/arm GOARM=7)"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
	"$GO" build -trimpath -ldflags "-s -w" -o "$STAGE/bin/mihomo-panel" "$ROOT"

if [ "${PANEL_UPX:-0}" = "1" ] && command -v upx >/dev/null 2>&1; then
	if upx --best --lzma "$STAGE/bin/mihomo-panel" >/dev/null 2>&1; then
		echo ">>> 面板已用 UPX 压缩"
	else
		echo ">>> UPX 压缩失败，使用未压缩面板"
	fi
fi

if [ "${NOKERNEL:-0}" = "1" ]; then
	echo ">>> 不包含内核"
else
	[ -s "$KERNEL" ] || {
		echo "找不到内核：$KERNEL"
		echo "先运行 ./build.sh 下载到 .cache，或用 MM_MIHOMO=... 指定"
		exit 1
	}
	cp -f "$KERNEL" "$STAGE/bin/mihomo"
	chmod 755 "$STAGE/bin/mihomo"
fi

[ -s "$RULES" ] || {
	echo "找不到 chnroute.txt：$RULES"
	echo "先运行 ./build.sh 下载到 .cache，或用 MM_CHNROUTE=... 指定"
	exit 1
}
cp -f "$RULES" "$STAGE/rules/chnroute.txt"

cp -f "$ROOT/plugin/merlin/install.sh" "$STAGE/install.sh"
cp -f "$ROOT/plugin/merlin/uninstall.sh" "$STAGE/uninstall.sh"
cp -f "$ROOT/plugin/merlin/scripts/ctl.sh" "$STAGE/scripts/ctl.sh"
cp -f "$ROOT/plugin/version" "$STAGE/version"
printf 'armv7l\n' > "$STAGE/arch"
chmod 755 "$STAGE/install.sh" "$STAGE/uninstall.sh" "$STAGE/scripts/ctl.sh" "$STAGE/bin/mihomo-panel"

echo ">>> 打包 $PKG.tar.gz"
( cd "$DIST/merlin-stage" && tar czf "$DIST/$PKG.tar.gz" "$NAME" )
( cd "$DIST" && shasum -a 256 "$PKG.tar.gz" >"$PKG.tar.gz.sha256" 2>/dev/null || sha256sum "$PKG.tar.gz" >"$PKG.tar.gz.sha256" )

echo
echo "===================================================="
ls -lh "$DIST/$PKG.tar.gz"
cat "$DIST/$PKG.tar.gz.sha256"
if [ -f "$STAGE/bin/mihomo" ]; then
	echo "包内内核：$(wc -c <"$STAGE/bin/mihomo") 字节  面板：$(wc -c <"$STAGE/bin/mihomo-panel") 字节"
else
	echo "包内不含内核；安装后在面板「设置 → 内核」下载或手动上传"
fi
echo "安装：把 tar.gz 传到路由器 /tmp，解包后执行 sh mihomo-merlin/install.sh"
echo "===================================================="
