
import urllib.request, json, time

def post(url, data=None, token=None):
    h = {"Content-Type": "application/json"}
    if token: h["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=json.dumps(data or {}).encode(), headers=h)
    return json.loads(urllib.request.urlopen(req, timeout=10).read().decode())

def get(url, token=None):
    h = {}
    if token: h["Authorization"] = "Bearer " + token
    return json.loads(urllib.request.urlopen(urllib.request.Request(url, headers=h), timeout=10).read().decode())

B = "http://127.0.0.1:8080"
tok = post(B + "/api/auth/login", {"username": "admin", "password": "admin123"})["token"]
print("LOGIN ok")
try:
    print("WORKER:", urllib.request.urlopen("http://127.0.0.1:11435/", timeout=5).read().decode()[:120])
except Exception as e:
    print("WORKER ERR:", e)

s = get(B + "/api/settings", tok)["settings"]
s["ai"]["enabled"] = True
s["ai"]["mode"] = "local"
s["ai"]["detectUrl"] = "http://127.0.0.1:11435"
s["ai"]["threshold"] = 0.2
r = urllib.request.Request(B + "/api/settings", data=json.dumps(s).encode(),
    headers={"Content-Type": "application/json", "Authorization": "Bearer " + tok}, method="PUT")
out = json.loads(urllib.request.urlopen(r, timeout=10).read().decode())
print("AI enabled:", out["settings"]["ai"]["enabled"], "mode:", out["settings"]["ai"]["mode"])

devs = get(B + "/api/devices", tok)["devices"]
print("devices:", [(d["ip"], d["online"]) for d in devs])

print("waiting 40s for detection cycle...")
time.sleep(40)

evs = get(B + "/api/events?type=ai&limit=5", tok)
print("AI events total:", evs.get("total"))
for e in (evs.get("events") or [])[:3]:
    print(" -", e.get("label"), "|", e.get("description"), "| snap:", bool(e.get("snapshot")))
