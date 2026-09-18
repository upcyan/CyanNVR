#!/bin/bash

# 软路由 + V2Ray 智能分流部署脚本
# 基于 Docker 容器化部署

set -e

echo "=== 软路由 + V2Ray 智能分流部署脚本 ==="
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "❌ 请以 root 用户执行此脚本"
    echo "使用: sudo ./deploy_softrouter.sh"
    exit 1
fi

echo "✅ 检测到 root 权限"

# 创建目录结构
echo ""
echo "📁 创建目录结构..."
mkdir -p /vol1/1000/NasAppFiles/softrouter/{config,data,v2ray/config,v2ray/logs,smartdns,adguard/{work,conf}}
chmod -R 755 /vol1/1000/NasAppFiles/softrouter

echo "✅ 目录结构已创建"

# 获取 VPS 信息
echo ""
echo "📋 请输入 V2Ray 服务器信息："
read -p "VPS IP 地址: " VPS_IP
read -p "VPS 端口 (默认 443): " VPS_PORT
VPS_PORT=${VPS_PORT:-443}
read -p "UUID: " UUID
read -p "域名 (留空使用 IP): " DOMAIN
DOMAIN=${DOMAIN:-$VPS_IP}

# 生成 V2Ray 配置
echo ""
echo "🔧 生成 V2Ray 配置..."
cat > /vol1/1000/NasAppFiles/softrouter/v2ray/config.json << EOF
{
  "log": {
    "access": "/var/log/v2ray/access.log",
    "error": "/var/log/v2ray/error.log",
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
          "anthropic.com"
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
      "protocol": "vmess",
      "settings": {
        "vnext": [
          {
            "address": "$VPS_IP",
            "port": $VPS_PORT,
            "users": [
              {
                "id": "$UUID",
                "alterId": 0,
                "security": "auto"
              }
            ]
          }
        ]
      },
      "streamSettings": {
        "network": "ws",
        "security": "tls",
        "wsSettings": {
          "path": "/v2ray"
        },
        "tlsSettings": {
          "serverName": "$DOMAIN"
        }
      },
      "tag": "proxy"
    }
  ]
}
EOF

echo "✅ V2Ray 配置已生成"

# 生成 SmartDNS 配置
echo ""
echo "🔧 生成 SmartDNS 配置..."
cat > /vol1/1000/NasAppFiles/softrouter/smartdns/smartdns.conf << 'EOF'
# SmartDNS 配置
server 8.8.8.8 -group bootstrap
server 119.29.29.29 -group china
server 223.5.5.5 -group china
server 114.114.114.114 -group china

# 国内域名使用国内 DNS
domain-rules /cn/ -address #china
domain-rules /google.com/ -address #bootstrap
domain-rules /youtube.com/ -address #bootstrap
domain-rules /github.com/ -address #bootstrap

# 缓存设置
cache-size 10000
cache-persist yes
cache-file /data/smartdns.cache

# 其他设置
bind [::]:5353
prefetch-domain yes
serve-expired yes
EOF

echo "✅ SmartDNS 配置已生成"

# 生成 AdGuard Home 配置
echo ""
echo "🔧 生成 AdGuard Home 配置..."
mkdir -p /vol1/1000/NasAppFiles/softrouter/adguard/conf
cat > /vol1/1000/NasAppFiles/softrouter/adguard/conf/AdGuardHome.yaml << 'EOF'
http:
  address: 0.0.0.0:3000
  session_ttl: 720h

tls:
  enabled: false
  server_name: ""
  force_https: false
  port_https: 443
  port_dns_over_tls: 853
  port_dns_over_quic: 784
  port_sni: 443
  certificate_chain: ""
  private_key: ""
  certificate_path: ""
  private_key_path: ""

dns:
  bind_host: 0.0.0.0
  port: 53
  protection_enabled: true
  blocking_mode: default
  blocking_ipv4: ""
  blocking_ipv6: ""
  blocked_response_ttl: 10
  bootstrap_dns:
    - 8.8.8.8
  upstream_dns:
    - https://dns.google/dns-query
    - https://cloudflare-dns.com/dns-query
  fallback_dns:
    - https://dns.google/dns-query
  upstream_mode: parallel
  all_servers: false
  fastest_addr: false
  fastest_timeout: 1s
  allowed_clients:
    - 0.0.0.0/0
  disallowed_clients:
    - 127.0.0.0/8
  blocked_services:
    - id: 4
    - id: 7
  parental_enabled: false
  safebrowsing_enabled: false
  safe_search:
    enabled: false
  rewrites: []
  cache_size: 4194304
  cache_ttl_min: 300
  cache_ttl_max: 1200
  cache_optimistic: false
  bogus_nxdomain: []
  aaaa_disabled: false
  enable_dnssec: false
  edns_client_subnet:
    custom_ip: ""
    enabled: false
    use_custom: false
  max_goroutines: 300
  handle_ddns: false
  ipset: []
  ipset_timeout: 60s

query_log:
  enabled: true
  interval: 2160h
  size_memory: 1000
  anonymize_client_ip: false

statistics:
  enabled: true
  interval: 2160h

filters:
  - enabled: true
    url: https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt
    name: AdGuard DNS filter
    id: 1
  - enabled: true
    url: https://cdn.jsdelivr.net/gh/Cats-Team/AdRules@main/easylist.txt
    name: AdRules EasyList
    id: 2
EOF

echo "✅ AdGuard Home 配置已生成"

# 拉取 Docker 镜像
echo ""
echo "📦 拉取 Docker 镜像..."
docker pull v2fly/v2fly-core:latest
docker pull pymumu/smartdns:latest
docker pull adguard/adguardhome:latest

echo "✅ Docker 镜像拉取完成"

# 创建启动脚本
echo ""
echo "🔧 创建启动脚本..."
cat > /vol1/1000/NasAppFiles/softrouter/start.sh << 'EOF'
#!/bin/bash

# 启动 SmartDNS
echo "启动 SmartDNS..."
docker run -d \
  --name smartdns \
  --restart unless-stopped \
  --network host \
  -v /vol1/1000/NasAppFiles/softrouter/smartdns/smartdns.conf:/etc/sym/smartdns.conf:ro \
  pymumu/smartdns:latest

# 启动 V2Ray
echo "启动 V2Ray..."
docker run -d \
  --name v2ray \
  --restart unless-stopped \
  --network host \
  -v /vol1/1000/NasAppFiles/softrouter/v2ray/config.json:/etc/v2ray/config.json:ro \
  -v /vol1/1000/NasAppFiles/softrouter/v2ray/logs:/var/log/v2ray \
  v2fly/v2fly-core:latest \
  v2ray -config=/etc/v2ray/config.json

# 启动 AdGuard Home
echo "启动 AdGuard Home..."
docker run -d \
  --name adguardhome \
  --restart unless-stopped \
  --network host \
  -v /vol1/1000/NasAppFiles/softrouter/adguard/work:/opt/adguardhome/work \
  -v /vol1/1000/NasAppFiles/softrouter/adguard/conf:/opt/adguardhome/conf \
  adguard/adguardhome:latest

echo "✅ 所有服务已启动"
echo ""
echo "📋 服务状态:"
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
echo ""
echo "🔗 管理界面:"
echo "  - AdGuard Home: http://$(hostname -I | awk '{print $1}'):3000"
echo "  - V2Ray SOCKS5: $(hostname -I | awk '{print $1}'):10808"
echo "  - V2Ray HTTP: $(hostname -I | awk '{print $1}'):10809"
EOF

chmod +x /vol1/1000/NasAppFiles/softrouter/start.sh

# 创建停止脚本
cat > /vol1/1000/NasAppFiles/softrouter/stop.sh << 'EOF'
#!/bin/bash

echo "停止所有服务..."
docker stop adguardhome v2ray smartdns
docker rm adguardhome v2ray smartdns
echo "✅ 所有服务已停止"
EOF

chmod +x /vol1/1000/NasAppFiles/softrouter/stop.sh

echo "✅ 启动脚本已创建"

# 显示部署信息
echo ""
echo "✅ 软路由 + V2Ray 智能分流部署完成！"
echo ""
echo "📋 部署信息:"
echo "  - 配置目录: /vol1/1000/NasAppFiles/softrouter"
echo "  - V2Ray 配置: /vol1/1000/NasAppFiles/softrouter/v2ray/config.json"
echo "  - SmartDNS 配置: /vol1/1000/NasAppFiles/softrouter/smartdns/smartdns.conf"
echo "  - AdGuard Home 配置: /vol1/1000/NasAppFiles/softrouter/adguard/conf"
echo ""
echo "🚀 启动服务:"
echo "  cd /vol1/1000/NasAppFiles/softrouter && ./start.sh"
echo ""
echo "🛑 停止服务:"
echo "  cd /vol1/1000/NasAppFiles/softrouter && ./stop.sh"
echo ""
echo "🔗 管理界面:"
echo "  - AdGuard Home: http://$(hostname -I | awk '{print $1}'):3000"
echo "  - V2Ray SOCKS5: $(hostname -I | awk '{print $1}'):10808"
echo "  - V2Ray HTTP: $(hostname -I | awk '{print $1}'):10809"
echo ""
echo "⚙️  智能分流规则:"
echo "  - 国内流量: 直连"
echo "  - 国外流量: 代理"
echo "  - 广告过滤: AdGuard Home"
echo "  - DNS 加速: SmartDNS"
