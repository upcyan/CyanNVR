#!/usr/bin/env python3
"""Local object detection worker — multi-model (Frigate-style).

POST /        {"image": "<base64 jpg>"} -> {"objects": [{label, confidence, box}]}
GET  /        engine/model info
GET  /health  liveness + model_loaded
GET  /models  list candidate .onnx models found on disk
POST /load    {"path": "/models/xxx.onnx"} -> hot-swap active model

Supported ONNX detection families (auto-detected from output tensor):
  * ultralytics YOLOv8/v11 export   out like [1, 4+nc, anchors]
  * NMS-baked exports (RT-DETR and similar)  out like [1, N, 6]
  * any other shape -> error logged, next engine tried

Fallback: OpenCV HOG people detector when no ONNX model loads.

Env:
  NVR_AI_MODEL_PATH   active model path (or use POST /load)
  NVR_AI_CONF         confidence threshold (default 0.20)
  NVR_AI_CLASSES      comma-separated label filter (empty = all)
  NVR_AI_MODELS_DIR   directory scanned by GET /models
"""
import base64
import glob
import json
import os
import sys
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

_sess = None
_model_path = None
_engine = "opencv-hog"
_input_size = 640          # from session metadata when static
_labels = COCO_LABELS      # may be overridden by sidecar file
_family = None             # "v8" | "nms"


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


def _load_model(path):
    """Load an ONNX detection model. Returns True on success."""
    global _sess, _engine, _input_size, _labels, _family, _model_path
    import onnxruntime as ort  # noqa: PLC0415
    sess = ort.InferenceSession(path, providers=["CPUExecutionProvider"])
    inp = sess.get_inputs()[0]
    out = sess.get_outputs()[0].shape

    size = _probe_size(sess)
    # classify family by output rank/dims
    dims = [d for d in out if isinstance(d, int)]
    if len(dims) >= 2 and dims[-1] == 6:
        fam = "nms"           # [1, N, 6]: decoded boxes+conf+cls
    elif len(dims) >= 2:
        fam = "v8"            # [1, 4+nc, anchors]
    else:
        raise RuntimeError(f"unsupported output shape {out}")

    global_init = {"images": None, "input": None}
    _sess = sess
    _family = fam
    _input_size = size
    _labels = _load_labels(path)
    _model_path = path
    _engine = f"onnx:{fam}:{os.path.basename(path)}"
    print(f"loaded model {path} family={fam} input={inp.shape} out={out}", flush=True)


def load_model(path):
    try:
        _load_model(path)
        return {"ok": True, "path": path, "engine": _engine}
    except Exception as e:  # noqa: BLE001
        print(f"load {path} failed: {e}", flush=True)
        return {"ok": False, "error": str(e)}


def _initial_load():
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
                              "model": _model_path, "model_loaded": _sess is not None})
        elif self.path == "/models":
            here = os.path.join(os.path.dirname(os.path.abspath(__file__)), "models")
            roots = [r for r in {MODELS_DIR, here, "/models"} if r and os.path.isdir(r)]
            found = []
            for r in roots:
                found += sorted(glob.glob(os.path.join(r, "*.onnx")))
            self._reply(200, {"active": _model_path, "models": found})
        else:
            self._reply(200, {"ok": True, "engine": _engine, "model": _model_path,
                              "labels": sorted(set(LABEL_ZH))})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        try:
            data = json.loads(body)
            if self.path == "/load":
                self._reply(200, load_model(data.get("path", "")))
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


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 11435
    print(f"detect worker engine={_engine} port={port}", flush=True)
    HTTPServer(("0.0.0.0", port), Handler).serve_forever()


if __name__ == "__main__":
    main()
