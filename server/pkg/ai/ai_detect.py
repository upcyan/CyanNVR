#!/usr/bin/env python3
"""Local object detection worker — multi-model, multi-backend.

POST /            {"image": "<base64 jpg>"} -> {"objects": [{label, confidence, box}]}
GET  /            engine/model/backend info
GET  /health      liveness + model_loaded + backend
GET  /models      installed models (with metadata) + downloadable catalog
POST /load        {"path": "/models/xxx.onnx"} -> hot-swap active model
POST /download    {"name": "yolov8s"} -> fetch a catalog model into MODELS_DIR

Supported ONNX detection families (auto-detected from output tensor):
  * ultralytics YOLOv8/v11 export   out like [1, 4+nc, anchors]
  * NMS-baked exports (RT-DETR and similar)  out like [1, N, 6]
  * any other shape -> error logged, next engine tried

Fallback: OpenCV HOG people detector when no ONNX model loads.

推理后端（execution provider）：
  默认 auto，按 CUDA > ROCm > OpenVINO > DirectML > CPU 的顺序挑选第一个
  「既能创建、又能真正跑通一次推理」的 provider，其余情况自动回退。

  为什么必须实跑一次验证：EP 能被创建 ≠ 能在该硬件上执行。实测某 Pascal
  显卡上 CUDAExecutionProvider 创建成功、get_providers() 也报告 CUDA，
  但第一次卷积就抛 CUDNN_STATUS_EXECUTION_FAILED_CUDART。
  只检查 provider 列表会把这种情况误判为「GPU 加速可用」。

Env:
  NVR_AI_MODEL_PATH     active model path (or use POST /load)
  NVR_AI_CONF           confidence threshold (default 0.20)
  NVR_AI_CLASSES        comma-separated label filter (empty = all)
  NVR_AI_MODELS_DIR     directory scanned by GET /models
  NVR_AI_PROVIDER       auto|auto-bench|cpu|cuda|rocm|openvino|directml|tensorrt (default auto)
                        auto       = 按 AUTO_ORDER 顺序取第一个可用（最快可用）
                        auto-bench = 启动时逐个 EP 跑微型基准，按实测速度择优
  NVR_AI_THREADS        intra-op 线程数，0=交给 ORT 自适应（默认 0）
  NVR_AI_INPUT_SIZE     覆盖模型输入尺寸（如 480 可显著提速，0=用模型自带）
  NVR_AI_ASSETS_BASE    模型下载源前缀（默认 ultralytics 官方 Release）
"""
import base64
import glob
import json
import os
import re
import shutil
import socket
import sys
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer

import cv2
import numpy as np

COCO_LABELS = [
    "person", "bicycle", "car", "motorcycle", "airplane", "bus", "train",
    "truck", "boat", "traffic light", "fire hydrant", "stop sign",
    "parking meter", "bench", "bird", "cat", "dog", "horse", "sheep", "cow",
    "elephant", "bear", "zebra", "giraffe", "backpack", "umbrella", "handbag",
    "tie", "suitcase", "frisbee", "skis", "snowboard", "sports ball", "kite",
    "baseball bat", "baseball glove", "skateboard", "surfboard", "tennis racket",
    "bottle", "wine glass", "cup", "fork", "knife", "spoon", "bowl", "banana",
    "apple", "sandwich", "orange", "broccoli", "carrot", "hot dog", "pizza",
    "donut", "cake", "chair", "couch", "potted plant", "bed", "dining table",
    "toilet", "tv", "laptop", "mouse", "remote", "keyboard", "cell phone",
    "microwave", "oven", "toaster", "sink", "refrigerator", "book", "clock",
    "vase", "scissors", "teddy bear", "hair drier", "toothbrush",
]

LABEL_ZH = {
    "person": "人员", "bicycle": "自行车", "car": "车辆", "motorcycle": "摩托车",
    "bus": "客车", "truck": "卡车", "train": "火车", "boat": "船只",
    "dog": "犬只", "cat": "猫", "bird": "鸟", "horse": "马",
    "fire hydrant": "消防栓", "stop sign": "停车标志", "traffic light": "红绿灯",
    "backpack": "背包", "umbrella": "雨伞", "handbag": "手提包",
    "suitcase": "行李箱", "bottle": "瓶子", "cup": "杯子",
    "chair": "椅子", "couch": "沙发", "potted plant": "盆栽", "bed": "床",
    "dining table": "餐桌", "tv": "电视", "laptop": "笔记本",
    "cell phone": "手机", "refrigerator": "冰箱", "bench": "长椅",
    "tie": "领带", "sports ball": "球类", "knife": "刀具",
}

CONF_THRESHOLD = float(os.environ.get("NVR_AI_CONF", "0.20"))
NMS_THRESHOLD = 0.45
MODEL_PATH = os.environ.get("NVR_AI_MODEL_PATH", "models/yolov8n.onnx")
FILTER_CLASSES = [c.strip().lower() for c in os.environ.get("NVR_AI_CLASSES", "").split(",") if c.strip()]
MODELS_DIR = os.environ.get("NVR_AI_MODELS_DIR", "")
AI_PROVIDER = os.environ.get("NVR_AI_PROVIDER", "auto").strip().lower()
AI_THREADS = int(os.environ.get("NVR_AI_THREADS", "0") or "0")
AI_INPUT_SIZE = int(os.environ.get("NVR_AI_INPUT_SIZE", "0") or "0")

_sess = None
_model_path = None
_engine = "opencv-hog"
_input_size = 640          # from session metadata when static
_labels = COCO_LABELS      # may be overridden by sidecar file
_family = None             # "v8" | "nms"
_backend = "none"          # 实际生效的推理后端，如 cpu / cuda
_backend_chain = []        # 尝试过的 EP 链，便于排查
_backend_error = ""        # 若发生回退，记录原因
_backend_bench = ""        # auto-bench 模式的实测结果串（如 "cuda 12ms · cpu 187ms"）

# 归一化后端名 -> onnxruntime execution provider 名
PROVIDER_EP = {
    "cuda": "CUDAExecutionProvider",
    "rocm": "ROCMExecutionProvider",
    "openvino": "OpenVINOExecutionProvider",
    "directml": "DmlExecutionProvider",
    "tensorrt": "TensorrtExecutionProvider",
    "cpu": "CPUExecutionProvider",
}

# auto 模式的尝试顺序。CPU 永远排在最后兜底。
AUTO_ORDER = ["cuda", "rocm", "openvino", "directml", "cpu"]

# 官方模型直链。ultralytics 在 GitHub Release 里同时发布 .pt 与 .onnx，
# 属于权威来源（第三方 HF 镜像仓库经常失效或要求鉴权）。
# 这些地址逐个实测过可下载（HTTP 200）。
ULTRA_ASSETS = os.environ.get(
    "NVR_AI_ASSETS_BASE",
    "https://github.com/ultralytics/assets/releases/download/v8.4.0",
).rstrip("/")

# 模型目录：可下载的候选模型。url 为空表示需用户自行导出后放入模型目录。
MODEL_CATALOG = {
    "yolov8n": {
        "file": "yolov8n.onnx",
        "desc": "YOLOv8n 通用检测 · 最快，适合多路实时",
        "url": f"{ULTRA_ASSETS}/yolov8n.onnx",
        "approx_mb": 13,
    },
    "yolov8s": {
        "file": "yolov8s.onnx",
        "desc": "YOLOv8s 通用检测 · 精度更高，速度约为 n 的一半",
        "url": f"{ULTRA_ASSETS}/yolov8s.onnx",
        "approx_mb": 44,
    },
    "yolov8m": {
        "file": "yolov8m.onnx",
        "desc": "YOLOv8m 通用检测 · 精度最高，适合低路数或单路",
        "url": f"{ULTRA_ASSETS}/yolov8m.onnx",
        "approx_mb": 98,
    },
    "yolo11n": {
        "file": "yolo11n.onnx",
        "desc": "YOLO11n 通用检测 · 新一代，同尺寸精度优于 v8n",
        "url": f"{ULTRA_ASSETS}/yolo11n.onnx",
        "approx_mb": 11,
    },
    "yolo11s": {
        "file": "yolo11s.onnx",
        "desc": "YOLO11s 通用检测 · 精度与速度的折中",
        "url": f"{ULTRA_ASSETS}/yolo11s.onnx",
        "approx_mb": 38,
    },
    "yolov8n-pose": {
        "file": "yolov8n-pose.onnx",
        "desc": "YOLOv8n-pose 人体姿态 · 可判断跌倒等姿态异常",
        "url": f"{ULTRA_ASSETS}/yolov8n-pose.onnx",
        "approx_mb": 13,
    },
    "yolov8n-seg": {
        "file": "yolov8n-seg.onnx",
        "desc": "YOLOv8n-seg 实例分割 · 可得到目标轮廓而非方框",
        "url": f"{ULTRA_ASSETS}/yolov8n-seg.onnx",
        "approx_mb": 12,
    },
}


def _prepare_cuda_libs():
    """自动检测 GPU 架构，安装匹配的 CUDA 运行时。

    多版本共存策略（按 GPU 架构降级）：
    1. CUDA 13 + cuDNN 9 → Turing sm_75+（最新）
    2. CUDA 12 + cuDNN 9 → Volta sm_70+（当前默认）
    3. CUDA 11 + cuDNN 8 → Pascal sm_61（Tesla P4 等老卡）

    如果首次安装的版本在推理时崩溃（如 Pascal 上的 CUDNN 5003），
    ai_detect.py 会回退并安装旧版。此处仅做首次安装。

    为什么要预加载：pip 安装的 nvidia-* 包把 .so 放在
    site-packages/nvidia/<lib>/lib/ 下，不在 ld 搜索路径里。
    用 RTLD_GLOBAL 预加载，让后续 ORT dlopen provider 库时能解析符号。
    """
    try:
        import sysconfig

        purelib = sysconfig.get_paths().get("purelib") or ""
        root = os.path.join(purelib, "nvidia")
        if not os.path.isdir(root):
            return

        # 按依赖顺序收集目录：cudnn依赖cuda_runtime和cublas，
        # cublas依赖cuda_runtime，所以cudnn必须先加载。
        _CUDA_LOAD_ORDER = ["cudnn", "cublas", "cublas_lt", "cuda_runtime",
                            "cuda_nvrtc", "cufft", "curand"]
        available = {d for d in os.listdir(root)
                     if os.path.isdir(os.path.join(root, d, "lib"))}
        ordered = [d for d in _CUDA_LOAD_ORDER if d in available]
        # 加上未在预设列表中的目录
        ordered += sorted(available - set(ordered))
        lib_dirs = [os.path.join(root, d, "lib") for d in ordered]
        if not lib_dirs:
            return

        os.environ["LD_LIBRARY_PATH"] = ":".join(
            lib_dirs + [os.environ.get("LD_LIBRARY_PATH", "")]
        ).rstrip(":")

        import ctypes
        loaded = 0
        for d in lib_dirs:
            for so in sorted(glob.glob(os.path.join(d, "*.so*"))):
                try:
                    ctypes.CDLL(so, mode=ctypes.RTLD_GLOBAL)
                    loaded += 1
                except OSError:
                    pass
        print(f"prepared {loaded} NVIDIA runtime libs from {len(lib_dirs)} dirs", flush=True)
    except Exception as e:  # noqa: BLE001
        print(f"prepare cuda libs failed: {e}", flush=True)


def _try_install_cuda_deps():
    """检测当前 GPU 架构并安装匹配的 CUDA 运行时依赖。

    如果 onnxruntime-gpu 已经安装了但缺少 CUDA 库，
    根据 GPU compute capability 选择正确的组合：
    - CUDA 13 + cuDNN 9：sm_75+（Turing/Ampere/Ada/Blackwell）
    - CUDA 12 + cuDNN 9：sm_70+（Volta/Pascal）
    - CUDA 11 + cuDNN 8：sm_61（Pascal，cuDNN 8 最后版本）

    当前的实测发现（Tesla P4, Pascal sm_61）：
    - CUDA 13 + cuDNN 9 → CUDNN 5003 EXECUTION_FAILED
    - CUDA 12 + cuDNN 9 → CUDNN 5003 EXECUTION_FAILED
    - CUDA 11 + cuDNN 8 → 正常，推理 10.8ms（2.9x 加速）
    """
    try:
        import subprocess

        # 检测 GPU compute capability
        sm = _detect_gpu_sm()
        if sm is None:
            print("未检测到 NVIDIA GPU，跳过 CUDA 依赖安装", flush=True)
            return

        major, minor = sm
        print(f"检测到 GPU compute capability {major}.{minor}", flush=True)

        # 检查已安装的 CUDA 库
        try:
            import onnxruntime as ort  # noqa: PLC0415
            # 如果 CUDA EP 已经可用，无需安装
            if "CUDAExecutionProvider" in ort.get_available_providers():
                print("CUDA EP 已可用，跳过依赖安装", flush=True)
                return
        except ImportError:
            pass

        # 根据架构选择 CUDA 版本
        # sm_61 = Pascal → CUDA 11 + cuDNN 8（cuDNN 9 在 CUDA 11/12 上对 Pascal 会崩）
        # sm_70 = Volta  → CUDA 12 + cuDNN 9
        # sm_75+         → CUDA 12 + cuDNN 9（最新稳定组合）
        if major < 7 or (major == 6 and minor < 2):
            # Maxwell/早期 Pascal → CUDA 11 + cuDNN 8
            cuda_ver = "11"
            cudnn_pkg = "nvidia-cudnn-cu11>=8.0,<9.0"
            cublas_pkg = "nvidia-cublas-cu11"
        elif major <= 7 and minor <= 1:
            # Pascal → CUDA 11 + cuDNN 8（cuDNN 9 + CUDA 12 在 Pascal 上卷积崩）
            cuda_ver = "11"
            cudnn_pkg = "nvidia-cudnn-cu11>=8.0,<9.0"
            cublas_pkg = "nvidia-cublas-cu11"
        else:
            # Volta/Turing/Ampere+ → CUDA 12 + cuDNN 9
            cuda_ver = "12"
            cudnn_pkg = "nvidia-cudnn-cu12"
            cublas_pkg = "nvidia-cublas-cu12"

        # 优先从 /data/ 卷安装本地wheel（避免网络超时），fallback到在线安装
        data_dir = "/data"
        if os.path.isdir(data_dir):
            # 收集nvidia-cu{N}相关的wheel + onnxruntime-gpu（可能在同版本或交叉版本）
            pattern = re.compile(rf"nvidia[_-].*cu{cuda_ver}", re.IGNORECASE)
            local_wheels = sorted(
                f for f in glob.glob(os.path.join(data_dir, "nvidia-*.whl"))
                if pattern.search(os.path.basename(f))
            )
            # 也加入onnxruntime-gpu wheel（如果镜像里只有CPU版）
            ort_gpu = glob.glob(os.path.join(data_dir, "onnxruntime_gpu*.whl"))
            local_wheels.extend(ort_gpu)
            # numpy降级（cu11依赖numpy 1.x）
            numpy_wheels = glob.glob(os.path.join(data_dir, "numpy-1.*.whl"))
            local_wheels.extend(numpy_wheels[:1])
            if local_wheels:
                print(f"从本地 /data/ 安装 CUDA {cuda_ver} 运行时（{len(local_wheels)}个wheel）", flush=True)
                # 清理旧版onnxruntime残留（1.30的.so文件会与1.17冲突），
                # 但不要卸载nvidia包（pip uninstall会连带删除）
                import shutil, sysconfig
                base = os.path.join(sysconfig.get_paths().get("purelib", ""), "onnxruntime")
                for p in [base, base + "-1.30.0.dist-info"]:
                    if os.path.exists(p):
                        shutil.rmtree(p, ignore_errors=True)
                subprocess.run(
                    [sys.executable, "-m", "pip", "install", "--break-system-packages",
                     "--force-reinstall", "--no-deps", "-q"] + local_wheels,
                    check=True, capture_output=True, timeout=300,
                )
        else:
            pkgs = [
                cudnn_pkg, cublas_pkg,
                f"nvidia-cuda-runtime-cu{cuda_ver}",
                f"nvidia-cufft-cu{cuda_ver}",
                f"nvidia-curand-cu{cuda_ver}",
            ]
            print(f"在线安装 CUDA {cuda_ver} 运行时: {' '.join(pkgs)}", flush=True)
            subprocess.run(
                [sys.executable, "-m", "pip", "install", "--break-system-packages",
                 "--no-cache-dir", "-q", "--timeout=300"] + pkgs,
                check=True, capture_output=True, timeout=600,
            )
        print("CUDA 运行时安装完成", flush=True)
    except subprocess.TimeoutExpired:
        print("CUDA 运行时安装超时，将回退到 CPU", flush=True)
    except Exception as e:  # noqa: BLE001
        print(f"CUDA 运行时安装失败: {e}", flush=True)


def _detect_gpu_sm():
    """检测第一张 NVIDIA GPU 的 compute capability (major, minor)。

    返回 None 表示没有可用的 NVIDIA GPU。
    """
    try:
        import subprocess
        out = subprocess.check_output(
            ["nvidia-smi", "--query-gpu=compute_cap", "--format=csv,noheader"],
            text=True, timeout=10, stderr=subprocess.DEVNULL,
        ).strip()
        parts = out.split(".")
        if len(parts) == 2:
            return int(parts[0]), int(parts[1])
    except Exception:  # noqa: BLE001
        pass
    return None


def _ep_chain():
    """按配置算出要尝试的 execution provider 链。

    auto: 按 AUTO_ORDER 挑选「本机已编译进去」的 EP，末尾补 CPU 兜底。
    指定值: 只用该 EP（仍会补 CPU 兜底，避免整机不能推理）。
    """
    import onnxruntime as ort  # noqa: PLC0415

    avail = set(ort.get_available_providers())
    names = AUTO_ORDER if AI_PROVIDER == "auto" else ([AI_PROVIDER] + ["cpu"])
    chain = []
    for n in names:
        ep = PROVIDER_EP.get(n)
        if ep and ep in avail and ep not in chain:
            chain.append(ep)
    if "CPUExecutionProvider" in avail and "CPUExecutionProvider" not in chain:
        chain.append("CPUExecutionProvider")
    return chain


def _backend_label(ep):
    """CUDAExecutionProvider -> cuda"""
    for name, provider in PROVIDER_EP.items():
        if provider == ep:
            return name
    return ep or "none"


def _make_session(path, providers):
    """创建 InferenceSession，并应用 CPU 调优参数。"""
    import onnxruntime as ort  # noqa: PLC0415

    so = ort.SessionOptions()
    # 全量图优化：常量折叠、算子融合等，对 CPU 推理有稳定收益。
    so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
    if AI_THREADS > 0:
        # 单帧检测是「一发一收」的同步负载，并行度靠 intra-op 就够；
        # inter-op 开多线程只会增加调度开销，因此固定为 1。
        so.intra_op_num_threads = AI_THREADS
        so.inter_op_num_threads = 1
    return ort.InferenceSession(path, sess_options=so, providers=providers)


def _validate_session(sess, size):
    """真跑一次推理，确认该 EP 在当前硬件上确实能执行。

    只检查 get_providers() 是不够的 —— 实测存在「EP 创建成功、首次卷积
    即崩」的情况（Pascal 显卡 + 现代 cuDNN 报 CUDNN_STATUS_EXECUTION_FAILED_CUDART）。
    这里用随机输入做一次完整前向，失败即抛出，由调用方回退到下一个 EP。
    """
    name = sess.get_inputs()[0].name
    dummy = np.random.rand(1, 3, size, size).astype(np.float32)
    sess.run(None, {name: dummy})


# ---------- labels ----------

def _load_labels(model_file):
    """Sidecar labels: <model>.txt / <model>.json / <stem>.labels."""
    base = os.path.splitext(model_file)[0]
    for cand in (base + ".txt", model_file + ".txt", base + ".json", model_file + ".json"):
        if os.path.exists(cand):
            try:
                if cand.endswith(".json"):
                    data = json.load(open(cand, encoding="utf-8"))
                    names = data.get("names") if isinstance(data, dict) else data
                    if isinstance(names, dict):
                        names = [names[str(i)] for i in sorted(names, key=lambda x: int(x))]
                    if isinstance(names, list):
                        return names
                else:
                    names = [l.strip() for l in open(cand, encoding="utf-8") if l.strip()]
                    if names:
                        return names
            except Exception as e:  # noqa: BLE001
                print(f"label sidecar {cand} failed: {e}", flush=True)
    return COCO_LABELS


# ---------- model loading ----------

def _probe_size(sess):
    try:
        shp = sess.get_inputs()[0].shape  # e.g. [1,3,640,640]
        if isinstance(shp[2], int) and shp[2] > 0:
            return int(shp[2])
    except Exception:  # noqa: BLE001
        pass
    return 640


def _input_dynamic(sess):
    """模型输入是否为动态尺寸（导出时没有固定 H/W）。

    只有动态输入才允许用 NVR_AI_INPUT_SIZE 覆盖边长；静态模型传别的尺寸
    会直接形状不匹配报错，所以必须先判断。
    """
    try:
        shp = sess.get_inputs()[0].shape
        return not (isinstance(shp[2], int) and isinstance(shp[3], int))
    except Exception:  # noqa: BLE001
        return True


def _classify_family(out_shape):
    """从输出张量形状判断检测家族。

    维度可能是符号而不是数字：ultralytics 导出的 YOLO11 常见
    ['batch', 84, 'anchors']，只统计 int 维度个数会把动态模型误判成不支持。
    因此改为看「语义位置」：
      * 末维为 6            -> [1, N, 6]，已解码的框+置信度+类别
      * 末维小且存在第三维   -> 实际是 [1, 6, N] 的通道优先布局
      * 其余                -> [1, 4+nc, anchors]
    """
    dims = list(out_shape)
    if len(dims) < 2:
        raise RuntimeError(f"unsupported output rank {out_shape}")

    last = dims[-1]
    if last == 6:
        return "nms"
    # 形状 [1, 6, N]：倒数第二维是 6 且末维是明显的 anchor 数量
    if len(dims) >= 3 and dims[-2] == 6:
        return "nms"
    return "v8"


def _rss_mb():
    """当前进程 RSS（MB），用于展示各后端的内存代价。"""
    try:
        with open("/proc/self/statm") as f:
            pages = int(f.read().split()[1])
        return pages * os.sysconf("SC_PAGE_SIZE") / (1024 * 1024)
    except Exception:  # noqa: BLE001
        return 0.0


def _bench_session(sess, size, warmup=3, iters=7):
    """微型基准：首帧（冷启动）+ 预热 + 计时，返回记分卡。

    首帧单独计时：CUDA/cuDNN 的算法选择都发生在第一次推理，
    对「启动后多久能出第一帧」这个体感指标最有参考价值。
    """
    name = sess.get_inputs()[0].name
    x = np.zeros((1, 3, size, size), dtype=np.float32)
    t0 = time.perf_counter()
    out = sess.run(None, {name: x})
    first_ms = (time.perf_counter() - t0) * 1000.0
    for _ in range(max(0, warmup - 1)):
        sess.run(None, {name: x})
    times = []
    last = None
    for _ in range(iters):
        t0 = time.perf_counter()
        last = sess.run(None, {name: x})
        times.append((time.perf_counter() - t0) * 1000.0)
    return {
        "mean": sum(times) / len(times),
        "peak": max(times),
        "first": first_ms,
        "out": last[0] if last else None,
    }


def _bench_pick(path):
    """auto-bench：每个可用 EP 都实测，按记分卡择优。

    规则：
      1) 资格门：能加载、能真跑、输出全有限且与 CPU 基准偏差 < 1.0
         （抓「能创建会话但算出垃圾」的坏核，比只看不崩更严格）；
      2) 排序：资格合格者按平均毫秒升序，最快的胜出；
      3) TensorRT 不参与启动实测（首跑建引擎数十秒），需要时手动选择。
    CPU 永远参与：既是兜底，也是精度基准。
    返回 (最快会话, 记分卡字符串, 失败原因, 排序后的 EP 链)。
    """
    import onnxruntime as ort  # noqa: PLC0415

    avail = set(ort.get_available_providers())
    probe = []
    for n in AUTO_ORDER:
        ep = PROVIDER_EP.get(n)
        if ep and ep in avail and ep not in probe:
            probe.append(ep)

    measured = {}  # label -> scorecard dict（含 sess/ep）
    reasons = []
    for ep in probe:
        label = _backend_label(ep)
        rss0 = _rss_mb()
        try:
            cand = _make_session(path, [ep])
            model_size = _probe_size(cand)
            size = AI_INPUT_SIZE if (AI_INPUT_SIZE and _input_dynamic(cand)) else model_size
            _validate_session(cand, size)     # 真跑一帧，确认 EP 在本硬件上能执行
            card = _bench_session(cand, size)
            card["rss"] = max(0.0, _rss_mb() - rss0)
            card["sess"] = cand
            card["ep"] = ep
            measured[label] = card
            print(f"bench {label}: 均{card['mean']:.1f} 峰{card['peak']:.1f} "
                  f"首帧{card['first']:.0f} ms RSS+{card['rss']:.0f}MB", flush=True)
        except Exception as e:  # noqa: BLE001
            reasons.append(f"{label}: {str(e)[:160]}")
            print(f"bench {label} 不可用：{str(e)[:200]}", flush=True)

    # 精度基准 = CPU 的输出；非 CPU 后端与它逐元素比对
    ref = measured.get("cpu", {}).get("out")
    for label, m in measured.items():
        if ref is None or label == "cpu":
            m["delta"] = None
            m["ok"] = True
            continue
        try:
            diff = float(np.max(np.abs(np.asarray(m["out"], np.float32)
                                       - np.asarray(ref, np.float32))))
        except Exception:  # noqa: BLE001
            diff = float("inf")
        m["delta"] = diff
        m["ok"] = bool(np.isfinite(diff) and diff < 1.0)
        if not m["ok"]:
            reasons.append(f"{label}: 输出与 CPU 基准偏差过大 Δ={diff:.4g}")
            print(f"bench {label} 精度不合格（Δ={diff:.4g}），不参与择优", flush=True)

    ranked = sorted([(l, m) for l, m in measured.items() if m["ok"]],
                    key=lambda t: t[1]["mean"])
    if not ranked:
        return None, "", " | ".join(reasons), []

    def fmt(label, m):
        delta = "基准" if m["delta"] is None else f"Δ{m['delta']:.3g}"
        return (f"{label} 均{m['mean']:.0f}/峰{m['peak']:.0f}/首帧{m['first']:.0f}ms"
                f" {delta} +{m['rss']:.0f}MB")

    bench_str = " · ".join(fmt(l, m) for l, m in ranked)
    winner_label, winner = ranked[0][0], ranked[0][1]
    ordered = [winner["ep"]] + [m["ep"] for l, m in ranked if m["ep"] != winner["ep"]]
    print(f"实测择优（合格按均值排序）：{bench_str} -> 使用 {winner_label}", flush=True)
    return winner["sess"], bench_str, " | ".join(reasons), ordered


def _load_model(path):
    """Load an ONNX detection model, picking a backend that actually works.

    Raises on failure so callers can surface the reason.
    """
    global _sess, _engine, _input_size, _labels, _family, _model_path
    global _backend, _backend_chain, _backend_error

    global _backend_bench
    sess = None
    reasons = []
    bench_str = ""

    if AI_PROVIDER == "auto-bench":
        # 启动实测择优：逐个 EP 建会话跑基准，用真实速度决定用哪个。
        sess, bench_str, err, chain = _bench_pick(path)
        if err:
            reasons.append(err)
    else:
        chain = _ep_chain()
        # 从最优 provider 开始尝试；失败就把该 EP 摘掉再试下一个，
        # CPU 始终留在链尾，保证任何情况下都还有可用后端。
        for i, ep in enumerate(chain):
            try:
                cand = _make_session(path, chain[i:])
                model_size = _probe_size(cand)
                size = AI_INPUT_SIZE if (AI_INPUT_SIZE and _input_dynamic(cand)) else model_size
                _validate_session(cand, size)
                sess = cand
                break
            except Exception as e:  # noqa: BLE001
                reasons.append(f"{_backend_label(ep)}: {str(e)[:160]}")
                print(f"provider {_backend_label(ep)} 不可用，回退下一档：{str(e)[:200]}", flush=True)
                sess = None

    if sess is None:
        raise RuntimeError("无可用推理后端: " + " | ".join(reasons))

    inp = sess.get_inputs()[0]
    out = sess.get_outputs()[0].shape

    model_size = _probe_size(sess)
    size = AI_INPUT_SIZE if (AI_INPUT_SIZE and _input_dynamic(sess)) else model_size

    # classify family by output shape（兼容动态轴，见 _classify_family）
    fam = _classify_family(out)

    used = sess.get_providers()[0]
    _sess = sess
    _family = fam
    _input_size = size
    _labels = _load_labels(path)
    _model_path = path
    _engine = f"onnx:{fam}:{os.path.basename(path)}"
    _backend = _backend_label(used)
    _backend_chain = chain
    _backend_error = " | ".join(reasons)
    _backend_bench = bench_str
    print(f"loaded model {path} family={fam} input={inp.shape} out={out} "
          f"backend={_backend} size={size}", flush=True)


def load_model(path):
    try:
        _load_model(path)
        return {"ok": True, "path": path, "engine": _engine}
    except Exception as e:  # noqa: BLE001
        print(f"load {path} failed: {e}", flush=True)
        return {"ok": False, "error": str(e)}


def _initial_load():
    # 预加载 CUDA 库（在 import onnxruntime 之前）
    _prepare_cuda_libs()
    here = os.path.dirname(os.path.abspath(__file__))
    cands = [MODEL_PATH,
             os.path.join(here, "models", "yolov8n.onnx"),
             os.path.join(here, "..", "..", "..", "..", "CyanNVR", "models", "yolov8n.onnx")]
    # prefer larger (s/m) models if present
    for pat in ("yolov8m*.onnx", "yolov8s*.onnx", "*rtdetr*.onnx"):
        cands += sorted(glob.glob(os.path.join(MODELS_DIR or os.path.join(here, "models"), pat)))
    for c in cands:
        if c and os.path.exists(c):
            if load_model(c)["ok"]:
                return
    print("no ONNX model loaded; HOG fallback" if hasattr(cv2, "HOGDescriptor")
          else "no ONNX model loaded; no detector available", flush=True)


# ---------- inference ----------

def _preprocess(img, size):
    resized = cv2.resize(img, (size, size))
    blob = resized[:, :, ::-1].astype(np.float32) / 255.0
    return blob.transpose(2, 0, 1)[None, ...]


def _infer_name(sess):
    try:
        return sess.get_inputs()[0].name
    except Exception:  # noqa: BLE001
        return "images"


def _postprocess_common(dets, w, h):
    """dets: list[(cls_id, conf, [x,y,w,h])] -> filter+NMS+label."""
    dets.sort(key=lambda d: d[1], reverse=True)
    picked = []
    for d in dets:
        if all(_iou(d[2], p[2]) <= NMS_THRESHOLD for p in picked):
            picked.append(d)
    out_list = []
    for ci, conf, box in picked:
        label = _labels[ci] if ci < len(_labels) else f"class{ci}"
        if FILTER_CLASSES and label.lower() not in FILTER_CLASSES:
            continue
        out_list.append({"label": label, "confidence": float(conf), "box": [int(v) for v in box]})
    return out_list


def _decode_v8(raw, w, h, nc):
    coords = raw[:4]
    scores = raw[4:] if raw.shape[0] > 4 else raw
    if scores.shape[0] == 1:  # single-class model exported as [5, N]
        scores = scores.repeat(4, axis=0)[:4] if False else scores
        cls_id = np.zeros(scores.shape[1], dtype=int)
        conf = scores[0]
    else:
        conf = scores.max(0)
        cls_id = scores.argmax(0)
    keep = conf > CONF_THRESHOLD
    if not keep.any():
        return []
    scale = np.array([w / _input_size, h / _input_size, w / _input_size, h / _input_size])
    dets = []
    for b, cf, ci in zip(coords[:, keep].T, conf[keep], cls_id[keep]):
        cx, cy, bw, bh = b
        x1 = (cx - bw / 2) * scale[0]
        y1 = (cy - bh / 2) * scale[1]
        ww = bw * scale[0]
        hh = bh * scale[1]
        dets.append((int(ci), float(cf), [x1, y1, ww, hh]))
    return dets


def _decode_nms(raw, w, h):
    """raw: [N,6] = x1,y1,x2,y2,score,cls (already letterboxed coords)."""
    sx, sy = w / _input_size, h / _input_size
    dets = []
    for row in raw:
        score = float(row[4])
        if score <= CONF_THRESHOLD:
            continue
        x1, y1, x2, y2 = row[:4] * np.array([sx, sy, sx, sy])
        dets.append((int(row[5]), score, [x1, y1, x2 - x1, y2 - y1]))
    return dets


def _iou(a, b):
    ix = max(0, min(a[0] + a[2], b[0] + b[2]) - max(a[0], b[0]))
    iy = max(0, min(a[1] + a[3], b[1] + b[3]) - max(a[1], b[1]))
    inter = ix * iy
    union = a[2] * a[3] + b[2] * b[3] - inter
    return inter / union if union > 0 else 0


def _sess_detect(img):
    h, w = img.shape[:2]
    blob = _preprocess(img, _input_size)
    name = _infer_name(_sess)
    raw = _sess.run(None, {name: blob})[0]
    raw = np.squeeze(raw)
    if _family == "nms":
        dets = _decode_nms(raw, w, h)
    else:
        nc = len(_labels)
        dets = _decode_v8(raw, w, h, nc)
    return _postprocess_common(dets, w, h)


# ---------- fallback ----------

_has_hog = hasattr(cv2, "HOGDescriptor")
HOG = None
if _has_hog:
    HOG = cv2.HOGDescriptor()
    HOG.setSVMDetector(cv2.HOGDescriptor_getDefaultPeopleDetector())


def _hog_detect(img):
    boxes, weights = HOG.detectMultiScale(img, winStride=(8, 8), padding=(8, 8), scale=1.05)
    return [{"label": "person", "confidence": float(c), "box": [int(x), int(y), int(w), int(h)]}
            for (x, y, w, h), c in zip(boxes, weights)]


def detect(img):
    if _sess is not None:
        try:
            return _sess_detect(img)
        except Exception as e:  # noqa: BLE001
            print(f"inference error: {e}", flush=True)
    if _has_hog and HOG is not None:
        return _hog_detect(img)
    return []


# ---------- http ----------

def _model_roots():
    """扫描模型的目录列表（去重，只保留真实存在的）。"""
    here = os.path.join(os.path.dirname(os.path.abspath(__file__)), "models")
    return [r for r in dict.fromkeys([MODELS_DIR, here, "/models"]) if r and os.path.isdir(r)]


def _installed_models():
    """列出磁盘上已安装的模型，附带体积与激活状态。"""
    out = []
    for root in _model_roots():
        for p in sorted(glob.glob(os.path.join(root, "*.onnx"))):
            try:
                size = os.path.getsize(p)
            except OSError:
                size = 0
            out.append({
                "path": p,
                "name": os.path.splitext(os.path.basename(p))[0],
                "size_mb": round(size / 1048576, 1),
                "active": p == _model_path,
            })
    return out


class Handler(BaseHTTPRequestHandler):

    def _reply(self, code, payload):
        data = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/health":
            self._reply(200, {"ok": True, "engine": _engine,
                              "model": _model_path, "model_loaded": _sess is not None,
                              "backend": _backend})
        elif self.path == "/models":
            installed = _installed_models()
            self._reply(200, {
                "active": _model_path,
                "backend": _backend,
                "backend_chain": _backend_chain,
                "backend_fallback": _backend_error,
                "backend_bench": _backend_bench,
                "installed": installed,
                # 兼容旧字段：早期前端按 models 取路径列表，保留以免破坏调用方。
                "models": [m["path"] for m in installed],
                "catalog": [dict(id=k, **v) for k, v in MODEL_CATALOG.items()],
            })
        else:
            self._reply(200, {"ok": True, "engine": _engine, "model": _model_path,
                              "backend": _backend, "backend_bench": _backend_bench,
                              "provider_setting": AI_PROVIDER,
                              "threads": AI_THREADS, "input_size": _input_size,
                              "labels": sorted(set(LABEL_ZH))})

    def _download(self, data):
        """按目录里的直链下载模型文件。"""
        name = (data.get("name") or "").strip()
        spec = MODEL_CATALOG.get(name)
        if not spec:
            self._reply(404, {"error": f"unknown model {name}",
                              "available": list(MODEL_CATALOG)})
            return
        url = spec.get("url") or ""
        if not url:
            self._reply(400, {
                "error": f"{name} 未内置下载直链，请自行导出 ONNX 后放入 "
                         f"{MODELS_DIR or '/models'}",
            })
            return
        dest_dir = MODELS_DIR or "/models"
        try:
            os.makedirs(dest_dir, exist_ok=True)
            dest = os.path.join(dest_dir, spec["file"])
            tmp = dest + ".part"
            # 先下到 .part 再改名，避免下载中断留下半个模型文件被当成可用模型。
            with urllib.request.urlopen(url, timeout=300) as r, open(tmp, "wb") as f:
                shutil.copyfileobj(r, f)
            os.replace(tmp, dest)
        except Exception as e:  # noqa: BLE001
            self._reply(500, {"error": f"下载失败: {e}"})
            return
        self._reply(200, {"ok": True, "path": dest,
                          "size_mb": round(os.path.getsize(dest) / 1048576, 1)})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        try:
            data = json.loads(body)
            if self.path == "/load":
                self._reply(200, load_model(data.get("path", "")))
                return
            if self.path == "/download":
                self._download(data)
                return
            img = cv2.imdecode(np.frombuffer(base64.b64decode(data.get("image", "")), np.uint8),
                               cv2.IMREAD_COLOR)
            if img is None:
                self._reply(400, {"error": "invalid image"})
                return
            self._reply(200, {"objects": detect(img)})
        except Exception as e:  # noqa: BLE001
            self._reply(500, {"error": str(e)})

    def log_message(self, *args):  # noqa: D102
        pass


_initial_load()


class _UnixHTTPServer(HTTPServer):
    """基于 Unix Domain Socket 的 HTTP 服务。

    相比 TCP：不占用端口、不经过网络协议栈（真 IPC，延迟更低），
    且文件权限可限制为仅本进程用户可写，绝不会被局域网访问。
    """

    address_family = socket.AF_UNIX
    daemon_threads = True

    def server_bind(self):
        # 清理上次遗留的 socket 文件，否则 bind 会失败
        try:
            os.unlink(self.server_address)
        except OSError:
            pass
        self.socket.bind(self.server_address)
        os.chmod(self.server_address, 0o600)
        self.server_name = "localhost"
        self.server_port = 0

    def server_activate(self):
        self.socket.listen(self.request_queue_size)


def _make_server(addr):
    """按地址创建服务：unix:/path 走 UDS，host:port 走 TCP。

    默认（无参数）绑 127.0.0.1，不再绑 0.0.0.0——
    检测接口仅服务本机后端，暴露到局域网既无必要也不安全。
    """
    if addr.startswith("unix:"):
        path = addr[len("unix:"):]
        parent = os.path.dirname(path)
        if parent:
            os.makedirs(parent, exist_ok=True)
        return _UnixHTTPServer(path, Handler), f"socket={path}"

    if ":" in addr:
        host, _, port_s = addr.rpartition(":")
        host = host or "127.0.0.1"
        port = int(port_s)
    else:
        host, port = "127.0.0.1", int(addr)
    return HTTPServer((host, port), Handler), f"addr={host}:{port}"


def main():
    addr = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1:11435"
    srv, desc = _make_server(addr)
    print(f"detect worker engine={_engine} backend={_backend} "
          f"provider_setting={AI_PROVIDER} {desc}", flush=True)
    if _backend_error:
        print(f"backend fallback detail: {_backend_error}", flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
