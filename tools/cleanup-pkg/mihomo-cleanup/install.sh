#!/bin/sh
# mihomo 残留清理工具：装一次即可，清理完自动从软件中心消失
DIR=$(cd "$(dirname "$0")" && pwd)
PATH=/koolshare/bin:/bin:/usr/bin:/sbin:/usr/sbin
export PATH

echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】: ================================================"
echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】:  Mihomo 清理工具 v1.0.0（安装后自动执行）"
echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】: ================================================"

/bin/sh "${DIR}/scripts/clean.sh"

# 登记一下让软件中心认为安装成功（随后立刻撤掉，菜单里不会长期留着）
if [ -x /koolshare/bin/dbus ]; then
	/koolshare/bin/dbus set softcenter_module_mihomo-cleanup_name=mihomo-cleanup 2>/dev/null
	/koolshare/bin/dbus set softcenter_module_mihomo-cleanup_title=Mihomo清理工具 2>/dev/null
	/koolshare/bin/dbus set softcenter_module_mihomo-cleanup_description=清理旧版本残留并释放jffs空间 2>/dev/null
	/koolshare/bin/dbus set softcenter_module_mihomo-cleanup_version=1.0.0 2>/dev/null
	/koolshare/bin/dbus set softcenter_module_mihomo-cleanup_install=1 2>/dev/null
fi

# 自清理：删掉刚装进来的文件与登记，装完就消失
rm -rf /koolshare/mihomo-cleanup 2>/dev/null
rm -f /koolshare/webs/Module_mihomo-cleanup.asp 2>/dev/null
rm -f /koolshare/res/icon-mihomo-cleanup.png 2>/dev/null
rm -f /koolshare/scripts/uninstall_mihomo-cleanup.sh 2>/dev/null
rm -f /koolshare/init.d/S99mihomo-cleanup.sh /koolshare/init.d/N99mihomo-cleanup.sh 2>/dev/null
if [ -x /koolshare/bin/dbus ]; then
	for k in name title description version install; do
		/koolshare/bin/dbus remove "softcenter_module_mihomo-cleanup_${k}" 2>/dev/null
	done
fi

echo "【$(TZ=UTC-8 date '+%Y%m%d %X')】: 清理工具已自动卸载，请刷新软件中心页面后安装 mihomo 插件"
exit 0
