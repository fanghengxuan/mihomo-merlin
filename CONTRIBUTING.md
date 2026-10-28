# 贡献指南

## 提 issue

请带上这些信息，定位会快很多：

- 路由器型号、固件版本（`nvram get buildno` / 软件中心显示的版本）
- 插件版本（面板「设置 → 关于」）
- 现象 + 复现步骤 + 报错截图
- 相关日志：`/tmp/mihomo-panel.log`、`/tmp/mihomo.log`

## 提 PR

1. 从 `main` 切分支，改动尽量聚焦单一问题
2. 提交前必须跑通：

```bash
python3 tests/ui_check.py     # 前端静态检查
go test ./...                 # 单元测试
sh tests/e2e.sh               # 有内核时跑真实 e2e
```

3. 改前端时注意：`web/` 通过 `go:embed` 打进面板，**改完必须重新编译**，否则路由器上看到的还是旧资源
4. 版本号需同步四处：`main.go` 的 `panelVersion`、`plugin/version`、`plugin/webs/Module_mihomo.asp` 的 `PKG_VERSION`、`web/index.html` 里的资源版本号（用于打破浏览器缓存）

## 代码风格

- Go：`gofmt`，标准库优先，不引第三方依赖（面板要能单文件嵌进路由器）
- 前端：原生三件套，无构建、无 CDN；新增交互请同时在 `tests/ui_check.py` 里补静态检查
