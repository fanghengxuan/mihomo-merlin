#!/usr/bin/env python3
"""前端一致性检查：跑在打包前，拦住 HTML 结构错误这类低级问题。

检查项：
1. HTML 标签是否平衡（之前少写一个 </div>，导致订阅弹窗被套进隐藏的导入弹窗里）
2. app.js 里引用的 #id 是否都存在于 HTML
3. 弹窗（.scrim）是否是 body 的直接子元素，且彼此不嵌套
4. app.js 语法（有 node 时用 node --check）
"""
import re
import subprocess
import sys
from html.parser import HTMLParser

ROOT = "web"
VOID = {"br", "img", "input", "meta", "link", "hr", "use", "path", "circle", "rect", "source", "area", "base", "col", "embed", "param", "track", "wbr"}

errors = []


class Checker(HTMLParser):
    def __init__(self):
        super().__init__()
        self.stack = []
        self.dialogs = {}

    def handle_starttag(self, tag, attrs):
        d = dict(attrs)
        if "scrim" in (d.get("class") or ""):
            self.dialogs[d.get("id") or tag] = list(self.stack)
        if tag not in VOID:
            self.stack.append((tag, d.get("id")))

    def handle_endtag(self, tag):
        if tag in VOID:
            return
        if not self.stack:
            errors.append(f"多余的结束标签 </{tag}>")
            return
        top_tag, top_id = self.stack[-1]
        if top_tag == tag:
            self.stack.pop()
            return
        # 不是栈顶：说明中间少写了结束标签（典型的漏 </div>）
        errors.append(
            f"标签不匹配：遇到 </{tag}>，但当前打开的是 <{top_tag}{'#'+top_id if top_id else ''}>（很可能少写了 </{top_tag}>）"
        )
        for i in range(len(self.stack) - 1, -1, -1):
            if self.stack[i][0] == tag:
                del self.stack[i:]
                return


def main():
    html = open(f"{ROOT}/index.html", encoding="utf-8").read()
    js = open(f"{ROOT}/app.js", encoding="utf-8").read()
    css = open(f"{ROOT}/app.css", encoding="utf-8").read()

    p = Checker()
    p.feed(html)
    if p.stack:
        errors.append("未闭合的标签: " + ", ".join(f"<{t}{'#'+i if i else ''}>" for t, i in p.stack))

    ids = set(re.findall(r'id="([^"]+)"', html))
    used = set(re.findall(r"\$\('#([\w-]+)'\)", js)) | set(re.findall(r"getElementById\('([\w-]+)'\)", js))
    missing = sorted(used - ids)
    if missing:
        errors.append("app.js 引用了不存在的 id: " + ", ".join(missing))

    for name, ancestors in p.dialogs.items():
        parents = [t for t, _ in ancestors]
        if "body" not in parents:
            errors.append(f"弹窗 #{name} 不在 body 下（祖先链 {parents}）")

    # 关键类名是否在 CSS 里有定义
    for cls in ("scrim", "dialog", "card", "btn", "node", "group", "sub-card"):
        if f".{cls}" not in css:
            errors.append(f"CSS 缺少 .{cls} 定义")


    # 启动流程里调用的函数必须都有定义（曾因删代码把 startReachPolling 删掉导致整页空白）
    boot = re.search(r"async function boot\(\) \{(.*?)\n\}", js, re.S)
    if boot:
        body = boot.group(1)
        called = set(re.findall(r"(?<![.\w])([a-zA-Z_]\w*)\s*\(", body))
        builtin = {"if", "for", "while", "switch", "return", "await", "catch", "function", "typeof", "parseInt", "String", "Number", "setTimeout", "clearInterval", "setInterval"}
        missing = []
        for fn in sorted(called - builtin):
            if not re.search(r"function\s+" + re.escape(fn) + r"\b", js) and not re.search(r"\b(?:const|let|var)\s+" + re.escape(fn) + r"\b", js):
                missing.append(fn)
        if missing:
            errors.append("boot() 调用了未定义的函数: " + ", ".join(missing))


    # 事件回调里 await 之后再用 e.currentTarget 会变成 null（曾导致刷新按钮报错）
    for m in re.finditer(r"(?:async\s*)?\s*(?:function\s+\w+\s*)?\(\s*e\s*\)\s*=>\s*\{(.*?)\n\s*\}", js, re.S):
        body = m.group(1)
        if re.search(r"await\b", body) and re.search(r"\bawait\b[\s\S]*e\.currentTarget", body):
            errors.append("事件回调在 await 之后仍使用 e.currentTarget（会报 null）")

    # closest('.xxx') 里的 class 必须真的存在于 HTML（组件改结构后常漏改）
    html_classes = set()
    for attr in re.findall(r'class="([^"]+)"', html):
        html_classes.update(attr.split())
    for attr in re.findall(r'class="([^"]+)"', js):   # JS 里动态生成的 class 也算
        html_classes.update(attr.split())
    for cls in set(re.findall(r"\.closest\(['\"]\.([\w-]+)", js)):
        if cls not in html_classes:
            errors.append(f"app.js 调用了 .closest('.{cls}')，但 HTML 里没有这个 class")

    try:
        subprocess.run(["node", "--check", f"{ROOT}/app.js"], check=True, capture_output=True)
    except FileNotFoundError:
        print("（未找到 node，跳过 app.js 语法检查）")
    except subprocess.CalledProcessError as e:
        errors.append("app.js 语法错误: " + e.stderr.decode("utf-8", "ignore")[:400])

    if errors:
        print("❌ 前端检查未通过：")
        for e in errors:
            print("   -", e)
        return 1

    print("✅ 前端检查通过：标签平衡 / id 引用完整 / 弹窗位置正确 / app.js 语法正常")
    return 0


if __name__ == "__main__":
    sys.exit(main())
