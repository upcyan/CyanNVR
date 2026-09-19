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
  NVR_AI_PROVIDER       auto|cpu|cuda|rocm|openvino|directml (default auto)
  NVR_AI_THREADS        intra-op 线程数，0=交给 ORT 自适应（默认 0）
  NVR_AI_INPUT_SIZE     覆盖模型输入尺寸（如 480 可显著提速，0=用模型自带）
  NVR_AI_ASSETS_BASE    模型下载源前缀（默认 ultralytics 官方 Release）
"""
import base64
import glob
import json
import os
import shutil
import socket
import sys
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
    """把 pip 安装的 NVIDIA 运行时库补进动态链接搜索路径。

    背景：onnxruntime-gpu 的 wheel 不自带 CUDA 运行时，需要额外 pip 安装
    nvidia-cublas-cu12 / nvidia-cudnn-cu12 等包。这些包把 .so 放在
    site-packages/nvidia/<lib>/lib/ 下，而该目录默认不在 ld 的搜索路径里，
    于是 CUDAExecutionProvider 会因为找不到 libcublasLt.so.12 而加载失败
    （报错只写 "cannot open shared object file"，很容易误判成没装）。

    必须在 import onnxruntime 之前执行：ORT 在导入时就会 dlopen provider
    库，之后再改环境变量已经来不及。用 RTLD_GLOBAL 预加载一次，让后续
    provider 库能解析到这些符号。
    """
    try:
        import sysconfig
        purelib = sysconfig.get_paths().get("purelib") or ""
        root = os.path.join(purelib, "nvidia")
        if not os.path.isdir(root):
            return
        lib_dirs = [os.path.join(root, d, "lib") for d in sorted(os.listdir(root))
                    if os.path.isdir(os.path.join(root, d, "lib"))]
        if not lib_dirs:
            return

        os.environ["LD_LIBRARY_PATH"] = ":".join(
            lib_dirs + [os.environ.get("LD_LIBRARY_PATH", "")]
        ).rstrip(":")

        # 环境变量对已启动的进程无效，因此显式预加载。
        import ctypes
        loaded = 0
        for d in lib_dirs:
            for so in sorted(glob.glob(os.path.join(d, "*.so*"))):
                try:
                    ctypes.CDLL(so, mode=ctypes.RTLD_GLOBAL)
                    loaded += 1
                except OSError:
                    pass  # 少数库有未满足依赖，跳过即可
        print(f"prepared {loaded} NVIDIA runtime libs from {len(lib_dirs)} dirs", flush=True)
    except Exception as e:  # noqa: BLE001
        print(f"prepare cuda libs failed: {e}", flush=True)


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


def _load_model(path):
    """Load an ONNX detection model, picking a backend that actually works.

    Raises on failure so callers can surface the reason.
    """
    global _sess, _engine, _input_size, _labels, _family, _model_path
    global _backend, _backend_chain, _backend_error

    chain = _ep_chain()
    sess = None
    reasons = []

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
    # 先补齐 CUDA 库搜索路径，再触发 onnxruntime 的首次导入。
    _prepare_cuda_libs()
    here = os.path.dirname(os.path.abspath(__file__))
    cands = [MODEL_PATH,
             os.path.join(here, "models", "yolov8n.onnx"),
             os.path.join(here, "..", "..", "..", "..", "SimpleNVR", "models", "yolov8n.onnx")]
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
                "installed": installed,
                # 兼容旧字段：早期前端按 models 取路径列表，保留以免破坏调用方。
                "models": [m["path"] for m in installed],
                "catalog": [dict(id=k, **v) for k, v in MODEL_CATALOG.items()],
            })
        else:
            self._reply(200, {"ok": True, "engine": _engine, "model": _model_path,
                              "backend": _backend, "provider_setting": AI_PROVIDER,
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
