#!/usr/bin/env python3
"""生成插件图标：深色圆角底 + 渐变六边形 + 白色几何 M。

产出：
  plugin/res/icon-mihomo.png   96x96（软件中心菜单图标）
  web/icon-256.png             256x256（面板 favicon / 触屏图标）
  docs/icon-preview.png        512x512（文档用）

用法：python3 tools/make_icon.py
"""
import math
import os

from PIL import Image, ImageDraw, ImageFilter

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SS = 8  # 超采样倍数

BG_TOP = (22, 32, 74)
BG_BOTTOM = (7, 11, 24)
HEX_A = (53, 208, 186)    # 青绿
HEX_B = (110, 143, 232)   # 靛蓝
INK = (11, 19, 36)


def lerp(a, b, t):
    return tuple(round(a[i] + (b[i] - a[i]) * t) for i in range(3))


def rounded_bg(size):
    img = Image.new("RGB", (size, size), BG_BOTTOM)
    d = ImageDraw.Draw(img)
    for y in range(size):
        d.line([(0, y), (size, y)], fill=lerp(BG_TOP, BG_BOTTOM, y / size))

    mask = Image.new("L", (size, size), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, size - 1, size - 1], radius=int(size * 0.235), fill=255)
    out = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    out.paste(img, (0, 0), mask)
    return out


def hexagon_points(cx, cy, r, rot=-90):
    pts = []
    for i in range(6):
        a = math.radians(rot + i * 60)
        pts.append((cx + r * math.cos(a), cy + r * math.sin(a)))
    return pts


def make(size):
    S = size * SS
    img = rounded_bg(S)
    d = ImageDraw.Draw(img, "RGBA")

    cx = cy = S / 2
    r = S * 0.335

    # 六边形渐变：先画逐行渐变，再用六边形蒙版裁剪
    grad = Image.new("RGB", (S, S))
    gd = ImageDraw.Draw(grad)
    for y in range(S):
        gd.line([(0, y), (S, y)], fill=lerp(HEX_A, HEX_B, y / S))
    hexmask = Image.new("L", (S, S), 0)
    ImageDraw.Draw(hexmask).polygon(hexagon_points(cx, cy, r), fill=255)
    img.paste(grad, (0, 0), hexmask)

    # 六边形外发光
    glow = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    ImageDraw.Draw(glow).polygon(hexagon_points(cx, cy, r * 1.02), fill=HEX_B + (70,))
    glow = glow.filter(ImageFilter.GaussianBlur(S * 0.035))
    img = Image.alpha_composite(img, glow)

    # 白色几何 M（注意：必须在 alpha_composite 之后重新拿 Draw，否则画在已丢弃的缓冲上）
    d = ImageDraw.Draw(img, "RGBA")
    w = r * 0.185          # 笔画宽度
    box_h = r * 1.02
    top = cy - box_h / 2
    bot = cy + box_h / 2
    left = cx - r * 0.46
    right = cx + r * 0.46
    mid_y = cy + box_h * 0.30

    m_pts = [
        (left, bot), (left, top), (cx, mid_y),
        (right, top), (right, bot),
    ]
    d.line(m_pts, fill=(255, 255, 255, 255), width=int(w), joint="curve")
    # 两端补圆头
    for p in (m_pts[0], m_pts[-1], m_pts[1], m_pts[3]):
        d.ellipse([p[0] - w / 2, p[1] - w / 2, p[0] + w / 2, p[1] + w / 2], fill=(255, 255, 255, 255))

    return img.resize((size, size), Image.LANCZOS)


def main():
    targets = [
        ("plugin/res/icon-mihomo.png", 96),
        ("web/icon-256.png", 256),
        ("docs/icon-preview.png", 512),
    ]
    for rel, size in targets:
        path = os.path.join(ROOT, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        icon = make(size)
        icon.save(path, "PNG", optimize=True)
        print(f"  {rel}  {size}x{size}  {os.path.getsize(path)} 字节")


if __name__ == "__main__":
    main()
