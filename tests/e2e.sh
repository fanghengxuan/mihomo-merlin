#!/bin/sh
# 端到端自检：真内核跑一遍「加订阅→启动→节点→切节点→测速→流量→规则→设置→原文→批量测速→改名→删除」
# 用法：sh tests/e2e.sh            （默认用 .cache 里的 darwin 内核做测试）
set -e
cd "$(dirname "$0")/.."
GO=${GO:-/tmp/toolchain/go/bin/go}
MIHOMO=${MM_MIHOMO:-/tmp/toolchain/mihomo/mihomo-darwin-arm64}

echo ">>> 前端一致性检查"
python3 tests/ui_check.py

echo ">>> 单元测试"
MM_MIHOMO="$MIHOMO" MM_NO_IPTABLES=1 "$GO" test ./... 2>&1 | grep -vE "TLS handshake error" | tail -2

echo ">>> 端到端（真内核）"
MM_MIHOMO="$MIHOMO" MM_NO_IPTABLES=1 "$GO" test -run 'TestEndToEndWithRealCore|TestSubNameDuplicate|TestFetchProgressStages' -v ./... 2>&1 \
	| grep -E "✓|FAIL|PASS|ok " | sed 's/^ *//'

echo ">>> 安装脚本冒烟"
sh tests/install_smoke.sh 2>&1 | tail -1
