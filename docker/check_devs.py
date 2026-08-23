
import urllib.request, json
def req(url, data=None, token=None, method=None):
    h = {"Content-Type": "application/json"}
    if token: h["Authorization"] = "Bearer " + token
    r = urllib.request.Request(url, data=json.dumps(data).encode() if data else None, headers=h, method=method)
    return json.loads(urllib.request.urlopen(r, timeout=8).read().decode())

B = "http://127.0.0.1:8080"
tok = req(B+"/api/auth/login", {"username":"admin","password":"admin123"})["token"]
devs = req(B+"/api/devices", token=tok)["devices"]
print("devices:", [(d["ip"], d["online"], d.get("recordMode")) for d in devs])
