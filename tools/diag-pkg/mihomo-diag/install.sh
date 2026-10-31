#!/bin/sh
DIR=$(cd "$(dirname "$0")" && pwd)
PATH=/koolshare/bin:/bin:/usr/bin:/sbin:/usr/sbin
export PATH
echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】: Mihomo 诊断工具 v1.0.0（只读，不改任何配置）"
/bin/sh "${DIR}/scripts/diag.sh" 2>&1
# 自身不落盘，装完即走
rm -rf /koolshare/mihomo-diag 2>/dev/null
rm -f /koolshare/webs/Module_mihomo-diag.asp 2>/dev/null
exit 0
