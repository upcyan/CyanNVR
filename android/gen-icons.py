#!/usr/bin/env python3
"""CyanNVR Android 图标生成器。

复用 fpk/gen-icons.py 的渲染实现（那是几何/配色的唯一真相源），生成：
  - 传统图标   mipmap-{m,h,x,xx,xxx}hdpi/ic_launcher.png       （API 24/25，圆角矩形设计原样）
  - 圆形图标   mipmap-*/ic_launcher_round.png                  （满幅底 + 圆形裁切，前景缩 80%）
  - 自适应图标 mipmap-anydpi-v26/ic_launcher{,_round}.xml      （API 26+）
      foreground: 满幅渐变底 + 前景等比缩 80%，drawable-xxxhdpi/ic_launcher_foreground.png
      background: 纯色 @color/ic_launcher_background
  - values/colors.xml

前景缩 80% 的原因：自适应图标 108dp 画布的安全区是中央 66dp 圆（半径 33/54），
原始几何的红点外缘距中心约 47/100，会被激进的 OEM 圆形蒙版裁掉；等比缩 80%
后外缘距中心约 38/54，圆 / 方圆 / 圆角矩形等常见蒙版下均完整可见。

用法：
    python3 gen-icons.py            # 生成全部
    python3 gen-icons.py --check    # 校验输出 PNG（复用 fpk 的结构探针）
"""

import importlib.util
import math
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
RES = os.path.join(HERE, "app", "src", "main", "res")

# 前景整体缩放（自适应/圆形图标的可见安全区约束）
FG_SCALE = 0.80

DENSITIES = {  # 目录 -> 边长(px)
    "mdpi": 48,
    "hdpi": 72,
    "xhdpi": 96,
    "xxhdpi": 144,
    "xxxhdpi": 192,
}
FG_SIZE = 432  # 108dp @ xxxhdpi(4x)


def load_generator():
    """按路径加载 fpk/gen-icons.py（文件名含连字符，不能直接 import）。"""
    path = os.path.join(ROOT, "fpk", "gen-icons.py")
    spec = importlib.util.spec_from_file_location("cyannvr_icons", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def scale_foreground(mod, s):
    """以画布中心 (50,50) 为原点等比缩放全部前景几何。"""
    mod.RING_CX = 50.0 + (mod.RING_CX - 50.0) * s
    mod.RING_CY = 50.0 + (mod.RING_CY - 50.0) * s
    mod.RING_R_OUT *= s
    mod.RING_R_IN *= s
    mod.PUPIL_R *= s
    mod.HL_ARC_CX = 50.0 + (mod.HL_ARC_CX - 50.0) * s
    mod.HL_ARC_CY = 50.0 + (mod.HL_ARC_CY - 50.0) * s
    mod.HL_ARC_R *= s
    mod.HL_ARC_WIDTH *= s
    mod.DOT_CX = 50.0 + (mod.DOT_CX - 50.0) * s
    mod.DOT_CY = 50.0 + (mod.DOT_CY - 50.0) * s
    mod.DOT_R *= s


def circle_crop(size, buf):
    """把整幅 RGBA 图裁成内切圆（圆形图标用），边缘做 1px 抗锯齿。"""
    c = size / 2.0
    r = c
    px = 1.0
    for j in range(size):
        dy = (j + 0.5) - c
        row = j * size * 4
        for i in range(size):
            dx = (i + 0.5) - c
            d = math.hypot(dx, dy)
            if d >= r:
                buf[row + i * 4 + 3] = 0
            elif d > r - px:
                buf[row + i * 4 + 3] = int(buf[row + i * 4 + 3] * (r - d) / px)
    return buf


ADAPTIVE_XML = """<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background"/>
    <foreground android:drawable="@drawable/ic_launcher_foreground"/>
</adaptive-icon>
"""

COLORS_XML = """<?xml version="1.0" encoding="utf-8"?>
<resources>
    <!-- 深青渐变底的左上端色，自适应图标背景层（前景满幅覆盖后仅露缝时可见） -->
    <color name="ic_launcher_background">#0F5A6E</color>
</resources>
"""


def emit(mod, path, size, maskable=False):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    wrote = mod.write_png(path, size, mod.render(size, maskable=maskable))
    print(f"  {size:>4}px{' [maskable]' if maskable else ''}"
          f" {'已更新' if wrote else '未变化'}  {os.path.relpath(path, ROOT)}")


def main():
    mod = load_generator()

    if "--check" in sys.argv:
        ok = True
        for dens, size in DENSITIES.items():
            for name in ("ic_launcher.png", "ic_launcher_round.png"):
                p = os.path.join(RES, f"mipmap-{dens}", name)
                if not os.path.exists(p):
                    print(f"  缺失 {p}")
                    ok = False
                    continue
                w, _, _ = mod.probe(p, size)
                if w != size:
                    print(f"  尺寸不符 {p}: {w} != {size}")
                    ok = False
        fg = os.path.join(RES, "drawable-xxxhdpi", "ic_launcher_foreground.png")
        if not os.path.exists(fg):
            print(f"  缺失 {fg}")
            ok = False
        else:
            w, _, _ = mod.probe(fg, FG_SIZE)
            if w != FG_SIZE:
                print(f"  前景尺寸不符: {w} != {FG_SIZE}")
                ok = False
        print("检查通过" if ok else "检查失败")
        return 0 if ok else 1

    print("生成 Android 图标（复用 fpk/gen-icons.py 渲染）")

    # 1) 传统图标：原始几何、圆角矩形设计原样（API 24/25 及所有兜底）
    for dens, size in DENSITIES.items():
        emit(mod, os.path.join(RES, f"mipmap-{dens}", "ic_launcher.png"), size)

    # 2) 此后几何缩放 80%：自适应前景 + 圆形图标都要求内容落在蒙版安全区内
    scale_foreground(mod, FG_SCALE)

    emit(mod, os.path.join(RES, "drawable-xxxhdpi", "ic_launcher_foreground.png"),
         FG_SIZE, maskable=True)

    for dens, size in DENSITIES.items():
        p = os.path.join(RES, f"mipmap-{dens}", "ic_launcher_round.png")
        os.makedirs(os.path.dirname(p), exist_ok=True)
        mod.write_png(p, size, circle_crop(size, mod.render(size, maskable=True)))
        print(f"  {size:>4}px [round]  {os.path.relpath(p, ROOT)}")

    # 3) 自适应图标 XML + 背景色
    for name in ("ic_launcher.xml", "ic_launcher_round.xml"):
        p = os.path.join(RES, "mipmap-anydpi-v26", name)
        os.makedirs(os.path.dirname(p), exist_ok=True)
        if not os.path.exists(p) or open(p, encoding="utf-8").read() != ADAPTIVE_XML:
            with open(p, "w", encoding="utf-8") as f:
                f.write(ADAPTIVE_XML)
            print(f"  已写入  {os.path.relpath(p, ROOT)}")
    p = os.path.join(RES, "values", "colors.xml")
    if not os.path.exists(p) or open(p, encoding="utf-8").read() != COLORS_XML:
        with open(p, "w", encoding="utf-8") as f:
            f.write(COLORS_XML)
        print(f"  已写入  {os.path.relpath(p, ROOT)}")

    print("完成。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
