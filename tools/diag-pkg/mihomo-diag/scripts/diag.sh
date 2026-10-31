#!/bin/sh
# 只读诊断：不改任何东西，把关键信息打到安装日志里
L() { echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】: $*"; }
L "================ mihomo 插件诊断 ================"
L "机型：$(nvram get productid 2>/dev/null)  固件：$(nvram get buildno 2>/dev/null) / $(nvram get extendno 2>/dev/null)"
L "内核：$(uname -a 2>/dev/null)"
echo
L "--- 挂载点（只列 jffs / koolshare / tmp / 根） ---"
mount 2>/dev/null | grep -E " / | /jffs | /koolshare | /tmp " | sed 's/^/    /'
echo
L "--- df -h ---"
df -h 2>/dev/null | sed 's/^/    /'
echo
L "--- df -i（inode） ---"
df -i 2>/dev/null | sed 's/^/    /'
echo
L "--- /koolshare 各目录占用（KB，从大到小） ---"
du -sk /koolshare/* 2>/dev/null | sort -n | tail -25 | sed 's/^/    /'
echo
L "--- /koolshare/mihomo 明细 ---"
du -sk /koolshare/mihomo/* 2>/dev/null | sort -n | sed 's/^/    /'
ls -la /koolshare/mihomo/kernel 2>/dev/null | sed 's/^/    /'
echo
L "--- /koolshare/bin 内容 ---"
ls -la /koolshare/bin 2>/dev/null | sed 's/^/    /'
echo
L "--- mihomo 相关文件 ---"
for f in /koolshare/bin/mihomo /koolshare/bin/mihomo-panel /koolshare/webs/Module_mihomo.asp /koolshare/res/icon-mihomo.png /koolshare/scripts/mihomo_ctl.sh /koolshare/scripts/uninstall_mihomo.sh /koolshare/init.d/S99mihomo.sh /koolshare/init.d/N99mihomo.sh; do
	if [ -e "$f" ] || [ -L "$f" ]; then L "  存在 ${f}（$(wc -c < "$f" 2>/dev/null || echo '?') 字节）"; else L "  缺失 ${f}"; fi
done
echo
L "--- 进程 ---"
pidof mihomo 2>/dev/null | sed 's/^/    mihomo pid: /'
pidof mihomo-panel 2>/dev/null | sed 's/^/    panel pid: /'
ps w 2>/dev/null | grep -E "mihomo" | grep -v grep | sed 's/^/    /'
echo
L "--- iptables 残留 ---"
iptables -t nat -L PREROUTING -n 2>/dev/null | head -8 | sed 's/^/    /'
iptables -t nat -L MIHOMO -n 2>/dev/null | head -5 | sed 's/^/    /'
echo
L "--- 试写测试（会立刻删掉） ---"
for d in /koolshare /koolshare/bin /jffs /tmp; do
	if dd if=/dev/zero of="$d/.mm-write-test" bs=1024 count=64 2>/dev/null; then
		L "  写入 $d 成功（64KB）"
		rm -f "$d/.mm-write-test" 2>/dev/null
	else
		L "  ❌ 写入 $d 失败"
	fi
done
echo
L "--- 大文件写入测试（4MB，验证是否真的没空间） ---"
if dd if=/dev/zero of=/koolshare/.mm-write-big bs=1024 count=4096 2>/dev/null; then
	L "  /koolshare 写入 4MB 成功"
	rm -f /koolshare/.mm-write-big 2>/dev/null
else
	L "  ❌ /koolshare 写入 4MB 失败（说明 df 显示的空间并不真实可用）"
	rm -f /koolshare/.mm-write-big 2>/dev/null
fi
L "================ 诊断结束 ================"
