
import urllib.request, json, base64
# 1. worker info
print("WORKER:", urllib.request.urlopen("http://127.0.0.1:11435/", timeout=5).read().decode())
# 2. health via backend
try:
    print("BACKEND:", urllib.request.urlopen("http://127.0.0.1:8080/api/health", timeout=5).read().decode())
except Exception as e:
    print("BACKEND ERR:", e)
