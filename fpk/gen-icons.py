#!/usr/bin/env python3
"""
CyanNVR 图标生成器（无第三方依赖，纯 Python 光栅化）

设计：深青渐变圆角背景 + 镜头 + 录制指示
  - 深青渐变圆角矩形背景 -> 应用中心 / 桌面入口 / 网页图标的统一底座
  - 外圈 + 实心瞳孔        -> 摄像机镜头，安防/NVR 的直接指代
  - 高光点                 -> 让它读起来是"玻璃镜片"而不是"靶心"
  - 右上角红点             -> 录制中，点出录像机的核心职能

前景只保留这三层意图，保证 64px 甚至 32px 下仍可辨认。

背景规格实测自飞牛官方「图标」文档中的系统设置示例图标：
  - 圆角 R = 25.4%（纯圆弧角，圆拟合误差 0.5px），左上亮、右下暗的渐变
  - maskable 版铺满整幅，由系统自行裁形
  - 桌面入口图标命名为 logo_*（旧名 icon_* 弃用，用于打破桌面/浏览器缓存）

几何尺寸（前景部分）是唯一真相源：web/public/icon.svg 与登录页内联 logo
按同一组数值手写，改动时请同步；背景只存在于光栅产物中。

用法：
    python3 gen-icons.py            # 生成全部尺寸
    python3 gen-icons.py --check    # 校验已生成文件的结构与配色
"""

import math
import os
import struct
import sys
import zlib

# ── 设计参数（坐标系 0..100，与 SVG viewBox 一致）──
VIEW = 100.0

RING_CX, RING_CY = 44.0, 54.0   # 镜头中心
RING_R_OUT = 32.0               # 外圈外径（镜筒）
RING_R_IN = 24.5                # 外圈内径（镜圈厚 7.5）
PUPIL_R = 20.0                  # 玻璃镜片：贴近内径只留 4.5 的缝，
                                # 这样读起来是"镜头"而不是"录制按钮 / 靶心"

HL1_CX, HL1_CY, HL1_R = 36.0, 46.0, 6.0    # 主高光（左上）
HL2_CX, HL2_CY, HL2_R = 50.0, 62.0, 2.5    # 次高光（右下，制造体积感）

DOT_CX, DOT_CY, DOT_R = 80.0, 22.0, 7.5    # 录制指示点（右上）
# 与外圈间距 = hypot(80-44, 22-54) - 7.5 - 32 ≈ 8.7，保证小尺寸下不粘连

# ── 配色 ──
CYAN = (0x2E, 0xA8, 0xFF)
RED = (0xFF, 0x4D, 0x4F)
WHITE = (0xFF, 0xFF, 0xFF)

# ── 背景：深青（teal）渐变 ──
# R = 25.4 个设计单位 = 画布的 25.4%，与飞牛系统图标一致
# （官方「图标」文档系统设置示例实测：R0=57/224px，纯圆弧角）。
BG_TOP = (0x0F, 0x5A, 0x6E)     # 左上端（亮）
BG_BOTTOM = (0x08, 0x32, 0x3E)  # 右下端（暗）
RADIUS = 25.4                   # 圆角半径（设计单位）


def clamp(v, lo=0.0, hi=1.0):
    return lo if v < lo else (hi if v > hi else v)


def coverage_circle(x, y, cx, cy, r, px):
    """圆的抗锯齿覆盖率：按到圆心距离在 1 像素过渡带内线性插值。"""
    if r <= 0:
        return 0.0
    d = math.hypot(x - cx, y - cy)
    return clamp((r - d) / px + 0.5)


def coverage_ring(x, y, cx, cy, r_out, r_in, px):
    """圆环覆盖率 = 外圆覆盖 - 内圆覆盖（同心嵌套，可直接相减）。"""
    return clamp(coverage_circle(x, y, cx, cy, r_out, px)
                 - coverage_circle(x, y, cx, cy, r_in, px))


def coverage_round_rect(x, y, w, h, r, px):
    """圆角矩形覆盖率：按 rounded-box SDF 距离在 1 像素过渡带内线性插值。"""
    hw, hh = w / 2.0, h / 2.0
    qx = abs(x - hw) - (hw - r)
    qy = abs(y - hh) - (hh - r)
    d = (math.hypot(max(qx, 0.0), max(qy, 0.0))
         + min(max(qx, qy), 0.0) - r)
    return clamp(0.5 - d / px)


def over(dst, src, a):
    """标准 over 合成（直通 alpha）。"""
    sr, sg, sb, sa = src
    sa = sa * a
    if sa <= 0:
        return dst
    dr, dg, db, da = dst
    out_a = sa + da * (1 - sa)
    if out_a <= 0:
        return (0.0, 0.0, 0.0, 0.0)
    out_r = (sr * sa + dr * da * (1 - sa)) / out_a
    out_g = (sg * sa + dg * da * (1 - sa)) / out_a
    out_b = (sb * sa + db * da * (1 - sa)) / out_a
    return (out_r, out_g, out_b, out_a)


def render(size, maskable=False):
    """渲染一张 RGBA 图，返回 bytearray（每像素 4 字节）。"""
    scale = size / VIEW
    px = 1.0 / scale          # 1 个目标像素折合多少设计单位
    buf = bytearray(size * size * 4)

    for j in range(size):
        y = (j + 0.5) * px
        row = j * size * 4
        for i in range(size):
            x = (i + 0.5) * px

            dst = (0.0, 0.0, 0.0, 0.0)

            # 0) 背景：深青渐变圆角矩形（R=25.4%，与飞牛系统图标一致）；
            #    渐变沿左上 -> 右下。maskable 版铺满整幅由系统裁形。
            t = clamp((x + y) / (VIEW * 2.0))
            bg = (
                BG_TOP[0] + (BG_BOTTOM[0] - BG_TOP[0]) * t,
                BG_TOP[1] + (BG_BOTTOM[1] - BG_TOP[1]) * t,
                BG_TOP[2] + (BG_BOTTOM[2] - BG_TOP[2]) * t,
                1.0,
            )
            bg_cov = 1.0 if maskable else coverage_round_rect(x, y, VIEW, VIEW, RADIUS, px)
            dst = over(dst, bg, bg_cov)

            # 1) 镜头外圈
            a = coverage_ring(x, y, RING_CX, RING_CY, RING_R_OUT, RING_R_IN, px)
            dst = over(dst, (CYAN[0], CYAN[1], CYAN[2], 1.0), a)

            # 2) 瞳孔
            a = coverage_circle(x, y, RING_CX, RING_CY, PUPIL_R, px)
            dst = over(dst, (CYAN[0], CYAN[1], CYAN[2], 1.0), a)

            # 3) 高光
            a = coverage_circle(x, y, HL1_CX, HL1_CY, HL1_R, px)
            dst = over(dst, (WHITE[0], WHITE[1], WHITE[2], 0.88), a)
            a = coverage_circle(x, y, HL2_CX, HL2_CY, HL2_R, px)
            dst = over(dst, (WHITE[0], WHITE[1], WHITE[2], 0.30), a)

            # 4) 录制指示点
            a = coverage_circle(x, y, DOT_CX, DOT_CY, DOT_R, px)
            dst = over(dst, (RED[0], RED[1], RED[2], 1.0), a)

            r, g, b, al = dst
            o = row + i * 4
            buf[o] = int(clamp(r / 255.0) * 255 + 0.5)
            buf[o + 1] = int(clamp(g / 255.0) * 255 + 0.5)
            buf[o + 2] = int(clamp(b / 255.0) * 255 + 0.5)
            buf[o + 3] = int(clamp(al) * 255 + 0.5)
    return buf


def write_png(path, size, buf):
    def chunk(typ, data):
        c = typ + data
        return struct.pack('>I', len(data)) + c + struct.pack('>I', zlib.crc32(c) & 0xffffffff)

    raw = bytearray()
    stride = size * 4
    for j in range(size):
        raw.append(0)                      # filter type 0 (None)
        raw += buf[j * stride:(j + 1) * stride]

    png = b'\x89PNG\r\n\x1a\n'
    png += chunk(b'IHDR', struct.pack('>IIBBBBB', size, size, 8, 6, 0, 0, 0))
    png += chunk(b'IDAT', zlib.compress(bytes(raw), 9))
    png += chunk(b'IEND', b'')

    # 内容未变则不写盘：否则每次构建都刷新 mtime，会让"源码比镜像新"的
    # 陈旧检测永远成立，白触发一次镜像重建。
    if os.path.exists(path):
        with open(path, 'rb') as f:
            if f.read() == png:
                return False
    with open(path, 'wb') as f:
        f.write(png)
    return True


def targets(root):
    """(路径, 尺寸, 是否遮罩版)"""
    return [
        (os.path.join(root, 'fpk/ICON.PNG'), 512, False),
        (os.path.join(root, 'fpk/ICON_256.PNG'), 256, False),
        (os.path.join(root, 'fpk/app/ui/images/logo_64.png'), 64, False),
        (os.path.join(root, 'fpk/app/ui/images/logo_256.png'), 256, False),
        (os.path.join(root, 'web/public/icon-192.png'), 192, False),
        (os.path.join(root, 'web/public/icon-512.png'), 512, False),
        (os.path.join(root, 'web/public/icon-maskable-512.png'), 512, True),
    ]


def remove_legacy(root):
    """删除旧命名的桌面入口图标（icon_* -> logo_* 改名用于打破缓存）。"""
    for name in ('icon_64.png', 'icon_256.png'):
        p = os.path.join(root, 'fpk/app/ui/images', name)
        if os.path.exists(p):
            os.remove(p)
            print(f'  清理旧文件            {os.path.relpath(p, root)}')


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    root = os.path.dirname(here)

    if '--check' in sys.argv:
        return check(targets(root))

    print('渲染 CyanNVR 图标（深青渐变圆角背景 + 镜头 + 录制点）')
    changed = 0
    for path, size, maskable in targets(root):
        os.makedirs(os.path.dirname(path), exist_ok=True)
        wrote = write_png(path, size, render(size, maskable))
        changed += 1 if wrote else 0
        tag = ' [maskable]' if maskable else ''
        mark = '已更新' if wrote else '未变化'
        print(f'  {size:>4}px{tag:<12} {mark}  {os.path.relpath(path, root)}')
    remove_legacy(root)
    print(f'完成，{changed} 个文件有变更。')
    return 0


def check(items):
    """结构校验：不依赖图像库，直接解 PNG 采样关键位置。"""
    ok = True
    for path, size, maskable in items:
        if not os.path.exists(path):
            print(f'  缺失 {path}')
            ok = False
            continue
        w, h, sampler = probe(path, size)
        cx = int(RING_CX / VIEW * size)
        cy = int(RING_CY / VIEW * size)
        ring_x = int((RING_CX + (RING_R_OUT + RING_R_IN) / 2) / VIEW * size)
        dot_x = int(DOT_CX / VIEW * size)
        dot_y = int(DOT_CY / VIEW * size)

        pupil = sampler(cy, cx)
        ring = sampler(cy, ring_x)
        dot = sampler(dot_y, dot_x)
        corner = sampler(0, 0)
        # 底边中部：位于圆角矩形内、前景之外，应为渐变末端深青色
        bg_bottom = sampler(int(0.97 * size), int(0.5 * size))

        problems = []
        if abs(pupil[0] - CYAN[0]) > 24 or pupil[2] < 200:
            problems.append(f'瞳孔非青色 {pupil[:3]}')
        if abs(ring[2] - CYAN[2]) > 30:
            problems.append(f'圆环非青色 {ring[:3]}')
        if dot[0] < 200 or dot[2] > 120:
            problems.append(f'录制点非红色 {dot[:3]}')
        if not maskable and corner[3] > 8:
            problems.append(f'角落非透明 alpha={corner[3]}')
        if maskable and corner[3] < 240:
            problems.append(f'maskable 角落应铺满 alpha={corner[3]}')
        if (abs(bg_bottom[0] - BG_BOTTOM[0]) > 24
                or abs(bg_bottom[1] - BG_BOTTOM[1]) > 24
                or abs(bg_bottom[2] - BG_BOTTOM[2]) > 24
                or bg_bottom[3] < 240):
            problems.append(f'底部背景非深青 {bg_bottom[:3]} a={bg_bottom[3]}')

        status = 'OK' if not problems else 'FAIL: ' + '; '.join(problems)
        print(f'  {os.path.basename(path):<24} {w}x{h}  {status}')
        if problems:
            ok = False
    return 0 if ok else 1


def probe(path, expect):
    """极简 PNG 读取：返回 (w,h,sampler)，仅支持本脚本生成的 RGBA/8bit。"""
    d = open(path, 'rb').read()
    pos, idat, w, h = 8, b'', None, None
    while pos < len(d):
        ln = struct.unpack('>I', d[pos:pos + 4])[0]
        typ = d[pos + 4:pos + 8]
        data = d[pos + 8:pos + 8 + ln]
        pos += 12 + ln
        if typ == b'IHDR':
            w, h, bd, ct = struct.unpack('>IIBB', data[:10])
            assert ct == 6 and bd == 8, '仅支持 RGBA 8bit'
        elif typ == b'IDAT':
            idat += data
        elif typ == b'IEND':
            break
    raw = zlib.decompress(idat)
    stride = w * 4
    rows = []
    p = 0
    for _ in range(h):
        f = raw[p]
        line = bytearray(raw[p + 1:p + 1 + stride])
        p += 1 + stride
        prev = rows[-1] if rows else bytearray(stride)
        if f == 1:
            for i in range(4, stride):
                line[i] = (line[i] + line[i - 4]) & 255
        elif f == 2:
            for i in range(stride):
                line[i] = (line[i] + prev[i]) & 255
        elif f == 3:
            for i in range(stride):
                a = line[i - 4] if i >= 4 else 0
                line[i] = (line[i] + ((a + prev[i]) >> 1)) & 255
        elif f == 4:
            for i in range(stride):
                a = line[i - 4] if i >= 4 else 0
                b = prev[i]
                c = prev[i - 4] if i >= 4 else 0
                pp = a + b - c
                pa, pb, pc = abs(pp - a), abs(pp - b), abs(pp - c)
                pr = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
                line[i] = (line[i] + pr) & 255
        rows.append(line)

    def sampler(y, x):
        o = x * 4
        r = rows[y]
        return (r[o], r[o + 1], r[o + 2], r[o + 3])

    assert w == expect and h == expect, f'尺寸不符 {w}x{h} != {expect}'
    return w, h, sampler


if __name__ == '__main__':
    sys.exit(main())
