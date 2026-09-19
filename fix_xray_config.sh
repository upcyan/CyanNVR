#!/bin/bash

# 修复 Xray 配置并重启

echo "=== 修复 Xray 配置 ==="

# 停止 Xray
docker stop xray 2>/dev/null || true
docker rm xray 2>/dev/null || true

# 创建新的配置文件
cat > /vol1/1000/NasAppFiles/softrouter/xray/config.json << 'EOF'
{
  "log": {
    "access": "/var/log/xray/access.log",
    "error": "/var/log/xray/error.log",
    "loglevel": "warning"
  },
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": [
      {
        "type": "field",
        "ip": ["geoip:cn", "geoip:private"],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "domain": ["geosite:cn"],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "domain": [
          "google.com", "googleapis.com", "youtube.com", "ytimg.com",
          "github.com", "githubusercontent.com", "github.io",
          "telegram.org", "t.me",
          "twitter.com", "x.com", "twimg.com",
          "facebook.com", "fbcdn.net",
          "instagram.com",
          "netflix.com", "nflxvideo.net",
          "openai.com", "chat.openai.com",
          "anthropic.com", "Claude.ai"
        ],
        "outboundTag": "proxy"
      },
      {
        "type": "field",
        "protocol": ["bittorrent"],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "port": "0-65535",
        "outboundTag": "proxy"
      }
    ]
  },
  "inbounds": [
    {
      "listen": "0.0.0.0",
      "port": 10808,
      "protocol": "socks",
      "settings": {
        "auth": "noauth",
        "udp": true
      },
      "tag": "socks-inbound"
    },
    {
      "listen": "0.0.0.0",
      "port": 10809,
      "protocol": "http",
      "settings": {},
      "tag": "http-inbound"
    }
  ],
  "outbounds": [
    {
      "protocol": "freedom",
      "tag": "direct"
    },
    {
      "protocol": "blackhole",
      "tag": "block"
    },
    {
      "protocol": "vless",
      "settings": {
        "vnext": [
          {
            "address": "38.47.124.210",
            "port": 46880,
            "users": [
              {
                "id": "cbc710c4-1a87-44ea-9d07-331442d85363",
                "encryption": "none",
                "flow": "xtls-rprx-vision"
              }
            ]
          }
        ]
      },
      "streamSettings": {
        "network": "tcp",
        "security": "reality",
        "realitySettings": {
          "serverName": "aws.amazon.com",
          "fingerprint": "chrome",
          "publicKey": "a-vZi-scO5nloLVK5lHbQ1SAXlJvBhuDyRI3BHIbQEY",
          "shortId": "",
          "spiderX": "/"
        }
      },
      "tag": "proxy"
    }
  ]
}
EOF

echo "✅ 配置文件已创建"

# 启动 Xray
docker run -d \
  --name xray \
  --restart unless-stopped \
  --network host \
  -v /vol1/1000/NasAppFiles/softrouter/xray/config.json:/etc/xray/config.json:ro \
  teddysun/xray:latest

echo "✅ Xray 已启动"

# 等待启动
sleep 3

# 测试代理
echo ""
echo "测试代理连接..."
curl --socks5 127.0.0.1:10808 https://github.com -s -o /dev/null -w "HTTP 状态码: %{http_code}\n"

echo ""
echo "=== 修复完成 ==="
