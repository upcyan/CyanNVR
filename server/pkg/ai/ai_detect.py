#!/usr/bin/env python3
"""Local object detection worker (Frigate-style, on-device).

Runs a small HTTP server that accepts a JPEG snapshot and returns detected
objects as JSON: {"objects": [{"label": "person", "confidence": 0.83, "box": [x,y,w,h]}]}.

Uses OpenCV HOG people detector by default (zero model download). Can be
swapped for YOLO/ONNX by editing detect() without changing the interface.
"""
import base64
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

import cv2
import numpy as np

HOG = cv2.HOGDescriptor()
HOG.setSVMDetector(cv2.HOGDescriptor_getDefaultPeopleDetector())

# Labels we care about in an NVR context. Frigate-style.
INTERESTING = {"person", "car", "truck", "dog", "cat"}


def detect(img):
    """Return list of {label, confidence, box:[x,y,w,h]} for a BGR image."""
    out = []
    boxes, weights = HOG.detectMultiScale(img, winStride=(8, 8), padding=(8, 8), scale=1.05)
    for (x, y, w, h), conf in zip(boxes, weights):
        out.append({"label": "person", "confidence": float(conf), "box": [int(x), int(y), int(w), int(h)]})
    return out


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
        self._reply(200, {"ok": True, "engine": "opencv-hog", "labels": sorted(INTERESTING)})

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
    print(f"local AI detect worker on 0.0.0.0:{port}", flush=True)
    HTTPServer(("0.0.0.0", port), Handler).serve_forever()


if __name__ == "__main__":
    main()
