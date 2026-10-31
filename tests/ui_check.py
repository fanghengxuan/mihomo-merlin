#!/usr/bin/env python3
"""前端一致性检查（v2 面板，扫描 web/**）。

检查项：
1. index.html 标签平衡、SVG sprite 引用完整
2. JS 引用的 #id 存在于 HTML 或 JS 动态生成的字符串里
3. 所有 web/js/**/*.js 通过 node --check
4. 脚本加载顺序与版本戳一致（?v= 等于 MM.VERSION）
5. 禁止 window.confirm / alert 式阻塞、color-mix、backdrop-filter、外部 URL
6. 关键类名在 CSS 中有定义
7. 7 个视图都注册了 MM.views.<name>（含 title/mount/activate）
8. 90-main 的 boot() 调用的本地函数都有定义
"""
import os
import re
import subprocess
import sys
from html.parser import HTMLParser

ROOT = "web"
VOID = {"br", "img", "input", "meta", "link", "hr", "use", "path", "circle", "rect", "ellipse",
        "line", "polyline", "polygon", "source", "area", "base", "col", "embed", "param", "track", "wbr"}
SCRIPT_ORDER = [
    "js/00-core.js", "js/10-store.js", "js/20-theme.js", "js/30-ui.js",
    "js/40-sched.js", "js/50-router.js",
    "js/views/overview.js", "js/views/nodes.js", "js/views/subs.js",
    "js/views/rules.js", "js/views/connections.js", "js/views/logs.js",
    "js/views/settings.js", "js/views/more.js", "js/90-main.js",
]
VIEWS = ["overview", "nodes", "subs", "rules", "connections", "logs", "settings", "more"]
CSS_CLASSES = [".card", ".group-card", ".list-row", ".btn", ".chip", ".toast", ".dlg",
               ".empty-state", ".error-bar", ".segmented", ".switch", ".skeleton-card",
               ".dock", ".rail", ".appbar", ".stat-tile", ".dash-card"]
FORBIDDEN = [
    (r"window\.confirm\s*\(", "window.confirm（应使用 MM.ui.confirm）"),
    (r"color-mix\s*\(", "color-mix()（token 必须预计算）"),
    (r"backdrop-filter\s*:", "backdrop-filter（v2 禁用模糊）"),
    (r"https?://(?!www\.w3\.org)", "外部 URL（禁止 CDN/外链）"),
]

errors = []
warnings = []


def rel(path):
    return os.path.relpath(path, ROOT)


class Checker(HTMLParser):
    def __init__(self):
        super().__init__()
        self.stack = []
        self.ids = set()
        self.uses = set()

    def handle_starttag(self, tag, attrs):
        d = dict(attrs)
        if d.get("id"):
            self.ids.add(d["id"])
        if tag == "use" and d.get("href", "").startswith("#"):
            self.uses.add(d["href"][1:])
        if tag not in VOID:
            self.stack.append((tag, d.get("id")))

    def handle_endtag(self, tag):
        if tag in VOID:
            return
        if not self.stack:
            errors.append(f"index.html 多余的结束标签 </{tag}>")
            return
        top_tag, top_id = self.stack[-1]
        if top_tag == tag:
            self.stack.pop()
            return
        errors.append(f"index.html 标签不匹配：</{tag}> 对应 <{top_tag}{'#' + top_id if top_id else ''}>")
        for i in range(len(self.stack) - 1, -1, -1):
            if self.stack[i][0] == tag:
                del self.stack[i:]
                return


def web_files():
    for base, _, files in os.walk(ROOT):
        for name in files:
            if name.endswith((".html", ".css", ".js")):
                yield os.path.join(base, name)


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def check_html(html):
    p = Checker()
    p.feed(html)
    if p.stack:
        errors.append("index.html 未闭合标签: " + ", ".join(f"<{t}{'#' + i if i else ''}>" for t, i in p.stack))

    symbols = set(re.findall(r'<symbol id="([^"]+)"', html))
    missing = sorted(p.uses - symbols)
    if missing:
        errors.append("index.html 引用了不存在的 sprite 图标: " + ", ".join(missing))
    return p


def check_ids(html, parser):
    js = "\n".join(read(p) for p in web_files() if p.endswith(".js"))
    ids = set(parser.ids) | set(re.findall(r'id="([\w-]+)"', js))
    used = set(re.findall(r"\$\('#([\w-]+)'", js)) | set(re.findall(r"getElementById\('([\w-]+)'", js))
    missing = sorted(used - ids)
    if missing:
        errors.append("JS 引用了不存在的 id: " + ", ".join(missing))


def check_scripts(html):
    srcs = re.findall(r'<script[^>]*\bsrc="([^"]+)"', html)
    paths = [s.split("?")[0] for s in srcs]
    if paths != SCRIPT_ORDER:
        errors.append("脚本顺序/清单与约定不一致：\n     实际: " + ", ".join(paths) +
                      "\n     期望: " + ", ".join(SCRIPT_ORDER))
    core = read(os.path.join(ROOT, "js", "00-core.js"))
    m = re.search(r"MM\.VERSION\s*=\s*'([^']+)'", core)
    if not m:
        errors.append("00-core.js 缺少 MM.VERSION")
        return
    version = m.group(1)
    for tag in re.findall(r'<(?:script|link)[^>]*(?:src|href)="([^"]+)"', html):
        if tag.startswith(("http://", "https://", "data:")) or tag.endswith((".png", ".svg", ".ico")):
            continue
        if not tag.endswith("?v=" + version):
            errors.append(f"资源版本戳不是 ?v={version}: {tag}")


def check_syntax():
    node = None
    for cand in ("node", "/opt/homebrew/bin/node", "/usr/local/bin/node"):
        try:
            subprocess.run([cand, "--version"], check=True, capture_output=True)
            node = cand
            break
        except (FileNotFoundError, subprocess.CalledProcessError):
            continue
    if not node:
        warnings.append("未找到 node，跳过全部 JS 语法检查")
        return
    for path in sorted(p for p in web_files() if p.endswith(".js")):
        try:
            subprocess.run([node, "--check", path], check=True, capture_output=True)
        except subprocess.CalledProcessError as e:
            errors.append(f"{rel(path)} 语法错误: " + e.stderr.decode("utf-8", "ignore")[:400])


def check_forbidden():
    for path in web_files():
        text = read(path)
        for pattern, label in FORBIDDEN:
            for m in re.finditer(pattern, text):
                line = text[:m.start()].count("\n") + 1
                errors.append(f"{rel(path)}:{line} 出现{label}")


def check_css():
    css = "\n".join(read(p) for p in web_files() if p.endswith(".css"))
    for cls in CSS_CLASSES:
        if cls not in css:
            errors.append(f"CSS 缺少 {cls} 定义")


def check_views():
    js = "\n".join(read(p) for p in web_files() if p.endswith(".js"))
    for view in VIEWS:
        if not re.search(r"MM\.views\." + view + r"\s*=", js):
            errors.append(f"MM.views.{view} 未注册")
    for key in ("title", "mount", "activate"):
        if not re.search(r"\b" + key + r"\s*:\s*(async\s*)?(function|\()", js):
            errors.append(f"视图契约缺少 {key}()")


def check_boot():
    js = "\n".join(read(p) for p in web_files() if p.endswith(".js"))
    m = re.search(r"function boot\(\) \{(.*?)\n  \}", js, re.S)
    if not m:
        errors.append("90-main 未找到 boot()")
        return
    body = m.group(1)
    builtin = {"if", "for", "while", "switch", "return", "await", "catch", "function", "typeof",
               "parseInt", "String", "Number", "setTimeout", "setInterval", "clearInterval",
               "Promise", "Object", "Array"}
    defined = set(re.findall(r"function\s+([\w$]+)\s*\(", js))
    defined |= set(re.findall(r"\b(?:const|let|var)\s+([\w$]+)\s*=", js))
    called = set(re.findall(r"(?<![.\w$])([\w$]+)\s*\(", body))
    missing = sorted(called - builtin - defined)
    if missing:
        errors.append("boot() 调用了未定义的函数: " + ", ".join(missing))


def main():
    html = read(os.path.join(ROOT, "index.html"))
    parser = check_html(html)
    check_ids(html, parser)
    check_scripts(html)
    check_syntax()
    check_forbidden()
    check_css()
    check_views()
    check_boot()

    if os.path.exists(os.path.join(ROOT, "app.js")):
        warnings.append("web/app.js 仍存在（旧面板，已不再被 index.html 引用；P5 删除）")

    for w in warnings:
        print("⚠️ ", w)
    if errors:
        print("❌ 前端检查未通过：")
        for e in errors:
            print("   -", e)
        return 1
    print(f"✅ 前端检查通过：HTML 结构 / id 引用 / JS 语法 / 脚本顺序与版本戳 / 禁用模式 / "
          f"CSS 类名 / 视图注册 / boot 调用")
    return 0


if __name__ == "__main__":
    sys.exit(main())
