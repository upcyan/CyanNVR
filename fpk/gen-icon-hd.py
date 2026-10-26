#!/usr/bin/env python3
"""CyanNVR 1024x1024 高清图标重绘。

与 fpk/gen-icons.py 同一组设计常量（构图/配色/主体完全一致）：
  深青渐变圆角背景 + 蓝色镜头(圆环+瞳孔) + 白色高光弧线 + 右上红点。

质量提升（不改变设计）：
  1. 1024x1024 原生渲染 + 4x4 超采样（4096 网格盒式降采样）
  2. 高光弧线端点圆头 —— 与 web/public/icon.svg 的 stroke-linecap="round"
     语义一致；gen-icons.py 的角度硬截断在端点处有锯齿
  3. 全部边缘用解析距离场过渡带抗锯齿，超采样兜底残余锯齿

用法：python3 gen-icon-hd.py   # 产出 web/public/icon-1024.png
"""
import math, os, struct, sys, zlib
import numpy as np

VIEW = 100.0
RING_CX, RING_CY = 44.0, 54.0
RING_R_OUT, RING_R_IN = 30.0, 22.0
PUPIL_R = 22.0
HL_R, HL_W = 16.0, 4.0
HL_START, HL_END = 210.0, 270.0
DOT_CX, DOT_CY, DOT_R = 78.0, 24.0, 9.0
CYAN = (0x2E, 0xA8, 0xFF)
RED = (0xFF, 0x4D, 0x4F)
WHITE = (255.0, 255.0, 255.0)
BG_TOP = (0x0F, 0x5A, 0x6E)
BG_BOTTOM = (0x08, 0x32, 0x3E)
RADIUS = 25.4

def cov_circle(X, Y, cx, cy, r, px):
    d = np.hypot(X - cx, Y - cy)
    return np.clip((r - d) / px + 0.5, 0.0, 1.0)

def cov_round_rect(X, Y, w, h, rad, px):
    # 圆角矩形 SDF：内侧距离 -> 覆盖率过渡带
    qx = np.abs(X - w / 2) - (w / 2 - rad)
    qy = np.abs(Y - h / 2) - (h / 2 - rad)
    ax, ay = np.maximum(qx, 0.0), np.maximum(qy, 0.0)
    d = np.hypot(ax, ay) + np.minimum(np.maximum(qx, qy), 0.0) - rad
    return np.clip(-d / px + 0.5, 0.0, 1.0)

def cov_hl_arc(X, Y, px):
    """高光弧线：圆弧带 + 角度范围 + 两端圆头（SVG round cap 语义）。"""
    dx, dy = X - 44.0, Y - 54.0
    dist = np.hypot(dx, dy)
    half = HL_W / 2
    band = np.clip((half - np.abs(dist - HL_R)) / px + 0.5, 0.0, 1.0)
    ang = np.degrees(np.arctan2(dy, dx)) % 360.0
    # 角度带：仅在 210..270 扇区内绘制，边界按弧长像素过渡。
    # 取模距离在扇区外会得到很大的「绕行值」，必须先乘 in_range 归零，
    # 否则整圆都会被点亮（第一版就栽在这）。
    feather = np.degrees(px / HL_R)  # 1px 对应的角度
    d_start = (ang - HL_START) % 360.0
    d_end = (HL_END - ang) % 360.0
    inside = np.minimum(d_start, d_end)
    ang_cov = np.clip(inside / feather + 0.5, 0.0, 1.0)
    ang_cov = ang_cov * ((ang >= HL_START) & (ang <= HL_END))
    # 圆头端点：半径 half 的圆盘
    sa, ea = math.radians(HL_START), math.radians(HL_END)
    cap1 = cov_circle(X, Y, 44.0 + HL_R * math.cos(sa), 54.0 + HL_R * math.sin(sa), half, px)
    cap2 = cov_circle(X, Y, 44.0 + HL_R * math.cos(ea), 54.0 + HL_R * math.sin(ea), half, px)
    return np.maximum(band * ang_cov, np.maximum(cap1, cap2))

def render_hd(size=1024, ss=4):
    S = size * ss
    px = VIEW / S
    lin = np.linspace(0.0, VIEW, S, endpoint=False)
    Y, X = np.meshgrid(lin + px / 2, lin + px / 2, indexing="ij")
    # 0) 背景
    t = np.clip((X + Y) / (VIEW * 2.0), 0.0, 1.0)
    bg_cov = cov_round_rect(X, Y, VIEW, VIEW, RADIUS, px)
    img = np.zeros((S, S, 4))
    for c in range(3):
        img[..., c] = (BG_TOP[c] + (BG_BOTTOM[c] - BG_TOP[c]) * t) * bg_cov
    img[..., 3] = bg_cov
    def over(rgb, a):
        na = a + img[..., 3] * (1 - a)
        for c in range(3):
            img[..., c] = (rgb[c] * a + img[..., c] * img[..., 3] * (1 - a)) / np.maximum(na, 1e-6) * na / np.maximum(na, 1e-6) * 0 + (rgb[c] * a + img[..., c] * img[..., 3] * (1 - a)) / np.maximum(na, 1e-6) * na
        img[..., 3] = na
    # 1) 圆环 2) 瞳孔
    over(CYAN, cov_ring := np.clip(cov_circle(X, Y, RING_CX, RING_CY, RING_R_OUT, px) - cov_circle(X, Y, RING_CX, RING_CY, RING_R_IN, px), 0, 1))
    over(CYAN, cov_circle(X, Y, RING_CX, RING_CY, PUPIL_R, px))
    # 3) 高光弧线
    over(WHITE, cov_hl_arc(X, Y, px) * 0.9)
    # 4) 红点
    over(RED, cov_circle(X, Y, DOT_CX, DOT_CY, DOT_R, px))
    # 4x4 盒式降采样
    img = img.reshape(size, ss, size, ss, 4).mean(axis=(1, 3))
    return img

def write_png(path, img):
    size = img.shape[0]
    raw = b"".join(b"\x00" + img[y].astype(np.uint8).tobytes() for y in range(size))
    def chunk(tag, data):
        c = struct.pack(">I", len(data)) + tag + data
        return c + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b"")
    with open(path, "wb") as f:
        f.write(png)

def main():
    out = os.path.join(os.path.dirname(__file__), "..", "web", "public", "icon-1024.png")
    img = render_hd(1024)
    write_png(os.path.abspath(out), img)
    print("OK", os.path.abspath(out))

if __name__ == "__main__":
    main()
