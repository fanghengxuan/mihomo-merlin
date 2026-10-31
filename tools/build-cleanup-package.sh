#!/bin/sh
# 打包「mihomo 残留清理工具」：一个几十 KB 的插件包，
# 在软件中心离线安装后自动清理旧版本残留并释放 jffs，然后自卸载。
set -eu

DIR=$(cd "$(dirname "$0")" && pwd)
PKG_SRC="$DIR/cleanup-pkg/mihomo-cleanup"
ROOT=$(cd "$DIR/.." && pwd)
DIST="$ROOT/dist"
STAGE="$DIST/cleanup-stage"

VER=$(cat "$PKG_SRC/version" 2>/dev/null || echo 1.0.0)
OUT="$DIST/mihomo-cleanup_${VER}_armv7.tar.gz"

[ -d "$PKG_SRC" ] || { echo "缺少 $PKG_SRC"; exit 1; }
mkdir -p "$DIST"
rm -rf "$STAGE"
mkdir -p "$STAGE/mihomo-cleanup"
cp -R "$PKG_SRC/." "$STAGE/mihomo-cleanup/"
chmod 755 "$STAGE/mihomo-cleanup/install.sh" "$STAGE/mihomo-cleanup/scripts/clean.sh"

# 用 tar 打成软件中心认识的格式：模块目录在包根，.valid 标记平台
( cd "$STAGE" && tar czf "$OUT" mihomo-cleanup )
rm -rf "$STAGE"

SIZE=$(wc -c < "$OUT" | tr -d ' ')
MD5=$(md5 -q "$OUT" 2>/dev/null || md5sum "$OUT" | awk '{print $1}')
echo "已生成：$OUT"
echo "大小：${SIZE} 字节"
echo "md5：${MD5}"
echo "用法：软件中心 → 离线安装 → 选择该文件（装完会自动清理并消失）"
