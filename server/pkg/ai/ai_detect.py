#!/usr/bin/env python3
"""Local object detection worker (Frigate-style, on-device).

Runs a small HTTP server that accepts a JPEG snapshot and returns detected
objects as JSON: {"objects": [{"label": "person", "confidence": 0.83, "box": [x,y,w,h]}]}.

Detection engines, in priority order:
  1. YOLOv8 ONNX (onnxruntime) — if a model file is present (default path or
     $NVR_AI_MODEL_PATH). Detects 80 COCO classes (person/car/...).
  2. OpenCV HOG people detector — zero-download fallback.

Set $NVR_AI_MODEL_PATH to point at a yolov8*.onnx file to use YOLO.
"""
import base64
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

import cv2
import numpy as np

# COCO class labels (indices match YOLOv8 output).
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

# NVR-relevant classes get Chinese labels; others keep English.
LABEL_ZH = {
    "person": "人员", "bicycle": "自行车", "car": "车辆", "motorcycle": "摩托车",
    "bus": "客车", "truck": "卡车", "dog": "犬只", "cat": "猫",
    "fire hydrant": "消防栓", "stop sign": "停车标志",
    "airplane": "飞机", "bus": "客车", "train": "火车", "boat": "船只",
    "traffic light": "红绿灯", "stop sign": "停车标志",
    "backpack": "背包", "umbrella": "雨伞", "handbag": "手提包",
    "suitcase": "行李箱", "bottle": "瓶子", "cup": "杯子",
    "chair": "椅子", "couch": "沙发", "potted plant": "盆栽",
    "bed": "床", "dining table": "餐桌", "toilet": "马桶",
    "tv": "电视", "laptop": "笔记本", "mouse": "鼠标", "remote": "遥控器",
    "keyboard": "键盘", "cell phone": "手机", "microwave": "微波炉",
    "oven": "烤箱", "toaster": "烤面包机", "sink": "水槽", "refrigerator": "冰箱",
}

# Detection config
CONF_THRESHOLD = float(os.environ.get("NVR_AI_CONF", "0.20"))
NMS_THRESHOLD = 0.45
MODEL_PATH = os.environ.get("NVR_AI_MODEL_PATH", "models/yolov8n.onnx")
# Categories to detect (empty = all COCO classes). Set via NVR_AI_CLASSES env (comma-separated).
FILTER_CLASSES = [c.strip().lower() for c in os.environ.get("NVR_AI_CLASSES", "").split(",") if c.strip()]

_sess = None
_engine = "opencv-hog"


def _load_yolo():
    global _sess, _engine
    candidates = [MODEL_PATH]
    # Fall back to a path relative to this script.
    here = os.path.dirname(os.path.abspath(__file__))
    candidates.append(os.path.join(here, "models", "yolov8n.onnx"))
    candidates.append(os.path.join(here, "..", "..", "..", "..", "SimpleNVR", "models", "yolov8n.onnx"))
    for c in candidates:
        if os.path.exists(c):
            try:
                import onnxruntime as ort  # noqa: PLC0415
                _sess = ort.InferenceSession(c, providers=["CPUExecutionProvider"])
                _engine = "yolov8-onnx"
                print(f"using YOLOv8 ONNX: {c}", flush=True)
                return
            except Exception as e:  # noqa: BLE001
                print(f"failed to load {c}: {e}", flush=True)
    print("YOLO model not found, falling back to OpenCV HOG", flush=True)


def _yolo_detect(img):
    h, w = img.shape[:2]
    resized = cv2.resize(img, (640, 640))
    blob = resized[:, :, ::-1].astype(np.float32) / 255.0
    blob = blob.transpose(2, 0, 1)[None, ...]
    out = _sess.run(None, {"images": blob})[0][0]  # [84, 8400]
    coords = out[:4]
    scores = out[4:]
    conf = scores.max(0)
    cls_id = scores.argmax(0)
    keep = conf > CONF_THRESHOLD
    if not keep.any():
        return []
    boxes = coords[:, keep]
    confs = conf[keep]
    ids = cls_id[keep]
    # Decode cx,cy,w,h -> x1,y1,x2,y2 in image coords, then NMS.
    scale = np.array([w / 640, h / 640, w / 640, h / 640])
    dets = []
    for b, c, ci in zip(boxes.T, confs, ids):
        cx, cy, bw, bh = b
        x1 = int((cx - bw / 2) * scale[0])
        y1 = int((cy - bh / 2) * scale[1])
        x2 = int((cx + bw / 2) * scale[0])
        y2 = int((cy + bh / 2) * scale[1])
        dets.append((int(ci), float(c), [x1, y1, x2 - x1, y2 - y1]))
    dets.sort(key=lambda d: d[1], reverse=True)
    picked = []
    for d in dets:
        overlap = False
        for p in picked:
            if _iou(d[2], p[2]) > NMS_THRESHOLD:
                overlap = True
                break
        if not overlap:
            picked.append(d)
    out_list = []
    for ci, c, box in picked:
        # Filter by class if FILTER_CLASSES is configured
        label = COCO_LABELS[ci] if ci < len(COCO_LABELS) else f"class{ci}"
        if FILTER_CLASSES and label.lower() not in FILTER_CLASSES:
            continue
        out_list.append({"label": label, "confidence": float(c), "box": box})
    return out_list


def _iou(a, b):
    ax, ay, aw, ah = a
    bx, by, bw, bh = b
    ix = max(0, min(ax + aw, bx + bw) - max(ax, bx))
    iy = max(0, min(ay + ah, by + bh) - max(ay, by))
    inter = ix * iy
    ua = aw * ah + bw * bh - inter
    return inter / ua if ua > 0 else 0


def _hog_detect(img):
    out = []
    boxes, weights = HOG.detectMultiScale(img, winStride=(8, 8), padding=(8, 8), scale=1.05)
    for (x, y, w, h), conf in zip(boxes, weights):
        out.append({"label": "person", "confidence": float(conf), "box": [int(x), int(y), int(w), int(h)]})
    return out


_has_hog = hasattr(cv2, "HOGDescriptor")
if _has_hog:
    HOG = cv2.HOGDescriptor()
    HOG.setSVMDetector(cv2.HOGDescriptor_getDefaultPeopleDetector())
else:
    HOG = None
    print("HOGDescriptor unavailable in this OpenCV build; YOLO-only mode", flush=True)
_load_yolo()


def detect(img):
    if _sess is not None:
        try:
            return _yolo_detect(img)
        except Exception as e:  # noqa: BLE001
            print(f"yolo detect error: {e}", flush=True)
    if _has_hog and HOG is not None:
        return _hog_detect(img)
    return []


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        try:
            data = json.loads(body)
            jpg = base64.b64decode(data.get("image", ""))
            img = cv2.imdecode(np.frombuffer(jpg, np.uint8), cv2.IMREAD_COLOR)
            if img is None:
                self._reply(400, {"error": "invalid image"})
                return
            objs = detect(img)
            self._reply(200, {"objects": objs})
        except Exception as e:  # noqa: BLE001
            self._reply(500, {"error": str(e)})

    def do_GET(self):
        if self.path == "/health":
            self._reply(200, {"ok": True, "engine": _engine, "model_loaded": _sess is not None})
            return
        self._reply(200, {"ok": True, "engine": _engine, "labels": sorted(set(LABEL_ZH) | {"person", "car", "truck"})})

    def _reply(self, code, payload):
        data = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *args):
        pass


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 11435
    print(f"local AI detect worker (engine={_engine}) on 0.0.0.0:{port}", flush=True)
    HTTPServer(("0.0.0.0", port), Handler).serve_forever()


if __name__ == "__main__":
    main()
