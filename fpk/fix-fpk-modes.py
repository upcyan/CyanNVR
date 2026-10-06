#!/usr/bin/env python3
"""修正 fpk 包内的 Unix 权限位、行尾与校验和（Windows 出包后处理）

背景（实测于官方 fnpack 1.2.3 Windows 版）：
  * Windows 平台没有 Unix 模式位，fnpack 打出的包内所有文件都是 666、
    目录都是 777。cmd/* 生命周期脚本因此失去可执行位，飞牛安装时报
    `fork/exec .../cmd/install_init: permission denied` 并回滚。
    （对比：Linux 上的 fnpack 保留源文件真实模式，无需本处理。）
  * 同一原因，fnpack 把 manifest 整体写成 CRLF（Linux 版产物为 LF）。
  * fnpack 会在 manifest 末尾追加一行 `checksum = <md5(app.tgz)>`，
    这是包完整性校验值。改动 app.tgz 后必须重算，否则安装校验失败。

本脚本就地改写已生成的 fpk（幂等）：
  1) 权限位按内容重写：目录 755；`#!` 脚本与 ELF 二进制 755；其余 644。
  2) 包内文本成员（manifest、cmd/*、wizard/*、config/*、ai_detect.py、
     ui/config）去除 CRLF。
  3) 重算并回写 manifest 的 checksum 行（与新的 app.tgz 保持一致）。

用法：
    python3 fix-fpk-modes.py <file.fpk>          # 就地修正
    python3 fix-fpk-modes.py <file.fpk> --check  # 只检查，有问题时退出码 1
"""

import gzip
import hashlib
import io
import os
import re
import sys
import tarfile

DIR_MODE = 0o755
EXEC_MODE = 0o755
FILE_MODE = 0o644

# 进包后必须为 LF 的文本成员
TEXT_MEMBERS_EXACT = ("manifest", "ai_detect.py", "ui/config")
TEXT_MEMBERS_PREFIX = ("cmd/", "wizard/", "config/")

CHECKSUM_RE = re.compile(rb"(?m)^checksum(\s*)=(\s*)([0-9a-fA-F]{32})\s*$")


def is_text_member(name):
    if name in TEXT_MEMBERS_EXACT:
        return True
    return name.startswith(TEXT_MEMBERS_PREFIX)


def wants_exec(name, data):
    """按内容自证是否需要可执行位（不依赖 Windows 上不存在的 unix 位）。"""
    if name.startswith("cmd/"):
        return True
    if data.startswith(b"#!"):
        return True
    if data.startswith(b"\x7fELF"):
        return True
    return False


def read_members(tf):
    items = []
    for m in tf.getmembers():
        data = None
        if m.isfile():
            f = tf.extractfile(m)
            data = f.read() if f else b""
        items.append((m, data))
    return items


def analyze(items, label, issues):
    n_dir = n_file = n_exec = 0
    for m, data in items:
        if m.isdir():
            n_dir += 1
            if (m.mode & 0o777) != DIR_MODE:
                issues.append("%s%s 目录模式 %o 应为 %o" % (label, m.name, m.mode & 0o777, DIR_MODE))
        elif m.isfile():
            n_file += 1
            want = EXEC_MODE if wants_exec(m.name, data) else FILE_MODE
            if want == EXEC_MODE:
                n_exec += 1
            if (m.mode & 0o777) != want:
                issues.append("%s%s 文件模式 %o 应为 %o" % (label, m.name, m.mode & 0o777, want))
            if is_text_member(m.name) and b"\r\n" in data:
                issues.append("%s%s 含 CRLF（飞牛上无法执行/解析）" % (label, m.name))
    return n_dir, n_file, n_exec


def rebuild(items, label, changes=None, transform=None):
    """按原顺序重建 tar；重写权限位、去 CRLF（可选替换内容）。"""
    buf = io.BytesIO()
    dst = tarfile.open(fileobj=buf, mode="w", format=tarfile.PAX_FORMAT)
    for m, data in items:
        if transform is not None:
            new = transform(m.name, data)
            if new is not None:
                data = new

        out = tarfile.TarInfo(m.name)
        out.mtime = m.mtime
        out.uid = m.uid
        out.gid = m.gid
        out.uname = m.uname
        out.gname = m.gname
        out.type = m.type
        if m.issym():
            out.linkname = m.linkname

        if m.isdir():
            out.mode = DIR_MODE
            out.size = 0
            dst.addfile(out, None)
        elif m.isfile():
            if m.name != "app.tgz" and b"\r\n" in data and (is_text_member(m.name) or m.name == "manifest"):
                data = data.replace(b"\r\n", b"\n")
                if changes is not None:
                    changes.append("%s%s 去 CRLF" % (label, m.name))
            want = EXEC_MODE if wants_exec(m.name, data) else FILE_MODE
            if (m.mode & 0o777) != want and changes is not None:
                changes.append("%s%s 模式 %o -> %o" % (label, m.name, m.mode & 0o777, want))
            out.mode = want
            out.size = len(data)
            dst.addfile(out, io.BytesIO(data))
        else:
            out.mode = m.mode
            out.size = 0
            dst.addfile(out, None)
    dst.close()
    return buf.getvalue()


def split_fpk(blob):
    outer = tarfile.open(fileobj=io.BytesIO(blob), mode="r:*")
    outer_items = read_members(outer)
    tgz = None
    for m, data in outer_items:
        if m.name == "app.tgz":
            tgz = data
    if tgz is None:
        raise SystemExit("错误：包内未找到 app.tgz")
    inner = tarfile.open(fileobj=io.BytesIO(tgz), mode="r:*")
    return outer_items, read_members(inner), tgz


def current_checksum(outer_items):
    for m, data in outer_items:
        if m.name == "manifest" and data:
            mm = CHECKSUM_RE.search(data)
            if mm:
                return mm.group(3).decode()
    return None


def cmd_check(fp):
    with open(fp, "rb") as f:
        blob = f.read()
    outer_items, inner_items, tgz = split_fpk(blob)
    issues = []
    od, of, oe = analyze(outer_items, "外层 ", issues)
    id_, if_, ie = analyze(inner_items, "内层 ", issues)

    # checksum 一致性
    recorded = current_checksum(outer_items)
    actual = hashlib.md5(tgz).hexdigest()
    if recorded is None:
        issues.append("manifest 缺少 checksum 行（无法校验包完整性）")
    elif recorded.lower() != actual:
        issues.append("checksum 不匹配：manifest=%s，实际 md5(app.tgz)=%s" % (recorded, actual))

    print("外层: %d 目录 / %d 文件（其中可执行 %d）" % (od, of, oe))
    print("内层: %d 目录 / %d 文件（其中可执行 %d）" % (id_, if_, ie))
    print("checksum: manifest=%s 实际=%s %s" % (recorded, actual, "一致" if recorded == actual else "不一致"))
    if issues:
        print("!! 发现 %d 处问题：" % len(issues))
        for x in issues:
            print("   -", x)
        return 1
    print("检查通过：包内权限位、行尾与 checksum 均正常")
    return 0


def cmd_fix(fp):
    with open(fp, "rb") as f:
        blob = f.read()
    outer_items, inner_items, _ = split_fpk(blob)

    changes = []
    # 1) 内层：修权限位 / 去 CRLF，重新压缩
    new_inner_raw = rebuild(inner_items, "app.tgz:", changes)
    new_tgz = gzip.compress(new_inner_raw, compresslevel=9, mtime=0)

    # 2) 重算 checksum（fnpack 的口径 = md5(压缩后的 app.tgz 字节)）
    new_md5 = hashlib.md5(new_tgz).hexdigest()
    old_md5 = current_checksum(outer_items)

    # 3) 外层：替换 app.tgz，并更新 manifest 的 checksum 行
    def repl(name, data):
        if name == "app.tgz":
            return new_tgz
        if name == "manifest":
            if data is None:
                return None
            if CHECKSUM_RE.search(data):
                new, n = CHECKSUM_RE.subn(
                    lambda m: b"checksum%s=%s%s" % (m.group(1), m.group(2), new_md5.encode()),
                    data,
                )
                if n and new != data:
                    changes.append("manifest checksum %s -> %s" % (old_md5, new_md5))
                return new
            # 无 checksum 行则补一行（与 fnpack 对齐格式）
            if not data.endswith(b"\n"):
                data += b"\n"
            data += b"checksum              = %s\n" % new_md5.encode()
            changes.append("manifest 补写 checksum %s" % new_md5)
            return data
        return None

    new_outer_raw = rebuild(outer_items, "", changes, transform=repl)
    new_blob = gzip.compress(new_outer_raw, compresslevel=9, mtime=0)

    # 4) 落盘前自检：修正后的包必须完全干净
    o2, i2, tgz2 = split_fpk(new_blob)
    issues = []
    analyze(o2, "外层 ", issues)
    analyze(i2, "内层 ", issues)
    rec = current_checksum(o2)
    act = hashlib.md5(tgz2).hexdigest()
    if rec != act:
        issues.append("自检 checksum 不一致：%s vs %s" % (rec, act))
    if issues:
        print("!! 修正后自检未通过，已放弃写入：")
        for x in issues:
            print("   -", x)
        return 1

    with open(fp, "wb") as f:
        f.write(new_blob)
    print("已修正：%s" % fp)
    print("  改动 %d 项" % len(changes))
    for c in changes[:6]:
        print("    -", c)
    if len(changes) > 6:
        print("    ... 其余 %d 项" % (len(changes) - 6))
    print("  %d bytes -> %d bytes" % (len(blob), len(new_blob)))
    return 0


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        return 2
    fp = sys.argv[1]
    if not os.path.isfile(fp):
        print("找不到文件：%s" % fp)
        return 2
    if "--check" in sys.argv[2:]:
        return cmd_check(fp)
    return cmd_fix(fp)


if __name__ == "__main__":
    sys.exit(main())
