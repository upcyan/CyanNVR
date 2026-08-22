"""Generate PWA icons (192/512 + maskable) matching the SimpleNVR brand."""
from PIL import Image, ImageDraw

BG = (23, 26, 33, 255)        # #171a21
ACCENT = (46, 168, 255, 255)  # #2ea8ff


def draw(size: int, maskable: bool):
    img = Image.new("RGBA", (size, size), BG)
    d = ImageDraw.Draw(img)
    s = size / 100.0
    if not maskable:
        r = int(22 * s)
        d.rounded_rectangle([0, 0, size - 1, size - 1], radius=r, fill=BG)

    def R(x0, y0, x1, y1, rad, **kw):
        d.rounded_rectangle([x0 * s, y0 * s, x1 * s, y1 * s], radius=rad * s, **kw)

    lw = max(2, int(6 * s))
    # camera body
    R(16, 32, 66, 70, 6, outline=ACCENT, width=lw)
    # lens dot
    cx = cy = 41 * s
    rr = 9 * s
    d.ellipse([cx - rr, cy - rr, cx + rr, cy + rr], fill=ACCENT)
    # viewfinder wedge
    d.line([(66 * s, 45 * s), (82 * s, 36 * s)], fill=ACCENT, width=lw)
    d.line([(82 * s, 36 * s), (82 * s, 64 * s)], fill=ACCENT, width=lw)
    d.line([(82 * s, 64 * s), (66 * s, 55 * s)], fill=ACCENT, width=lw)

    if maskable:
        # safe zone padding: shrink content by drawing on larger bg already
        pass
    return img


for name, size in [("icon-192.png", 192), ("icon-512.png", 512)]:
    draw(size, False).save(f"web/public/{name}", optimize=True)

# maskable: content centered within 80% safe zone
m = Image.new("RGBA", (512, 512), BG)
inner = draw(410, True)
m.paste(inner, ((512 - 410) // 2, (512 - 410) // 2), inner)
m.save("web/public/icon-maskable-512.png", optimize=True)
print("icons generated")
