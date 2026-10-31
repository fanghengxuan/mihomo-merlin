#!/bin/sh
set -eu
DIR=$(cd "$(dirname "$0")" && pwd); ROOT=$(cd "$DIR/.." && pwd)
SRC="$DIR/diag-pkg/mihomo-diag"; DIST="$ROOT/dist"; STAGE="$DIST/diag-stage"
VER=$(cat "$SRC/version"); OUT="$DIST/mihomo-diag_${VER}_armv7.tar.gz"
mkdir -p "$DIST"; rm -rf "$STAGE"; mkdir -p "$STAGE/mihomo-diag"
cp -R "$SRC/." "$STAGE/mihomo-diag/"
chmod 755 "$STAGE/mihomo-diag/install.sh" "$STAGE/mihomo-diag/scripts/diag.sh"
( cd "$STAGE" && tar czf "$OUT" mihomo-diag )
rm -rf "$STAGE"
echo "已生成：$OUT"; echo "大小：$(wc -c < "$OUT" | tr -d ' ') 字节"; echo "md5：$(md5 -q "$OUT" 2>/dev/null || md5sum "$OUT" | awk '{print $1}')"
