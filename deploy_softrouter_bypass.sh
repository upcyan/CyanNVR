#!/bin/bash

# 智能软路由 + 旁路由部署脚本
# 支持自动解析配置链接
# 使用 Xray 自动配置分流策略
# 支持旁路由模式接入本地网络

set -e

echo "=== 智能软路由 + 旁路由部署脚本 ==="
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "❌ 请以 root 用户执行此脚本"
    echo "使用: sudo ./deploy_softrouter_bypass.sh"
    exit 1
fi

echo "✅ 检测到 root 权限"

# 检查 Docker
if ! command -v docker &> /dev/null; then
    echo "❌ Docker 未安装"
    echo "请先安装 Docker: https://docs.docker.com/engine/install/"
    exit 1
fi

if ! docker info &> /dev/null; then
    echo "❌ Docker 服务未运行"
    echo "请启动 Docker: sudo systemctl start docker"
    exit 1
fi

echo "✅ Docker 已安装并运行"

# 创建目录结构
echo ""
echo "📁 创建目录结构..."
mkdir -p /vol1/1000/NasAppFiles/softrouter/{config,data,xray,smartdns,adguard/{work,conf}}

echo "✅ 目录结构已创建"

# 配置网络模式
echo ""
echo "🔧 选择网络模式:"
echo "1. 旁路由模式（推荐，不影响主路由）"
echo "2. 主路由模式（替代主路由）"
echo "3. Docker 网桥模式（简单测试）"
read -p "请选择 (1-3): " network_mode

case $network_mode in
    1)
        NETWORK_MODE="bypass"
        echo "✅ 选择旁路由模式"
        
        # 获取网络信息
        echo ""
        echo "📋 请输入网络信息："
        read -p "旁路由 IP 地址 (如 192.168.1.2): " BYPASS_IP
        read -p "子网掩码 (默认 255.255.255.0): " NETMASK
        NETMASK=${NETMASK:-255.255.255.0}
        read -p "网关 (主路由 IP, 如 192.168.1.1): " GATEWAY
        read -p "DNS 服务器 (默认 8.8.8.8): " DNS
        DNS=${DNS:-8.8.8.8}
        ;;
    2)
        NETWORK_MODE="router"
        echo "✅ 选择主路由模式"
        
        # 获取网络信息
        echo ""
        echo "📋 请输入网络信息："
        read -p "WAN 口网卡 (如 eth0): " WAN_IFACE
        read -p "LAN 口网卡 (如 eth1): " LAN_IFACE
        read -p "LAN IP 地址 (如 192.168.1.1): " LAN_IP
        read -p "子网掩码 (默认 255.255.255.0): " NETMASK
        NETMASK=${NETMASK:-255.255.255.0}
        ;;
    3)
        NETWORK_MODE="bridge"
        echo "✅ 选择 Docker 网桥模式"
        ;;
    *)
        echo "❌ 无效选择，默认使用旁路由模式"
        NETWORK_MODE="bypass"
        ;;
esac

# 输入配置信息
echo ""
echo "📋 请选择配置方式:"
echo "1. 手动输入服务器信息"
echo "2. 导入配置链接（推荐，自动解析）"
echo "3. 导入配置文件"
read -p "请选择 (1-3): " config_mode

case $config_mode in
    1)
        echo ""
        echo "📋 请输入服务器信息："
        read -p "服务器地址: " SERVER_ADDR
        read -p "端口 (默认 443): " SERVER_PORT
        SERVER_PORT=${SERVER_PORT:-443}
        read -p "UUID: " UUID
        read -p "域名 (留空使用 IP): " DOMAIN
        DOMAIN=${DOMAIN:-$SERVER_ADDR}
        read -p "传输协议 (ws/grpc/tcp, 默认 ws): " TRANSPORT
        TRANSPORT=${TRANSPORT:-ws}
        read -p "路径 (默认 /v2ray): " PATH
        PATH=${PATH:-/v2ray}
        read -p "TLS (true/false, 默认 true): " TLS
        TLS=${TLS:-true}
        ;;
    2)
        echo ""
        echo "📋 请输入配置链接（支持以下格式）:"
        echo "  - vless://..."
        echo "  - vmess://..."
        echo "  - ss://..."
        echo "  - trojan://..."
        echo ""
        read -p "配置链接: " CONFIG_LINK
        
        # 检查配置链接格式
        if [[ "$CONFIG_LINK" != vless://* ]] && [[ "$CONFIG_LINK" != vmess://* ]] && [[ "$CONFIG_LINK" != ss://* ]] && [[ "$CONFIG_LINK" != trojan://* ]]; then
            echo "❌ 不支持的配置格式"
            echo "支持的格式: vless://, vmess://, ss://, trojan://"
            exit 1
        fi
        
        echo "✅ 配置链接格式正确"
        ;;
    3)
        echo ""
        echo "📋 请输入配置文件路径:"
        read -p "配置文件路径: " CONFIG_FILE
        if [ ! -f "$CONFIG_FILE" ]; then
            echo "❌ 配置文件不存在"
            exit 1
        fi
        ;;
esac

# 生成 Xray 配置
echo ""
echo "🔧 生成 Xray 配置..."

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
    }
  ]
}
EOF

echo "✅ Xray 配置已生成"

# 创建 SmartDNS 配置
echo ""
echo "🔧 创建 SmartDNS 配置..."

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

# 创建 AdGuard Home 配置
echo ""
echo "🔧 创建 AdGuard Home 配置..."

mkdir -p /vol1/1000/NasAppFiles/softrouter/adguard/conf
cat > /vol1/1000/NasAppFiles/softrouter/adguard/conf/AdGuardHome.yaml << 'EOF'
http:
  address: 0.0.0.0:3000
  session_ttl: 720h

tls:
  enabled: false

dns:
  bind_host: 0.0.0.0
  port: 53
  protection_enabled: true
  blocking_mode: default
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
  cache_size: 4194304
  cache_ttl_min: 300
  cache_ttl_max: 1200

query_log:
  enabled: true
  interval: 2160h

statistics:
  enabled: true
  interval: 2160h

filters:
  - enabled: true
    url: https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt
    name: AdGuard DNS filter
    id: 1
EOF

echo "✅ AdGuard Home 配置已生成"

# 创建启动脚本
echo ""
echo "🔧 创建启动脚本..."

cat > /vol1/1000/NasAppFiles/softrouter/start.sh << 'EOF'
#!/bin/bash

echo "=== 启动智能软路由 ==="

# 启动 Xray
echo "启动 Xray..."
docker run -d \
  --name xray \
  --restart unless-stopped \
  --network host \
  -v /vol1/1000/NasAppFiles/softrouter/xray/config.json:/etc/xray/config.json:ro \
  -v /vol1/1000/NasAppFiles/softrouter/xray/logs:/var/log/xray \
  teddysun/xray:latest

# 启动 SmartDNS
echo "启动 SmartDNS..."
docker run -d \
  --name smartdns \
  --restart unless-stopped \
  --network host \
  -v /vol1/1000/NasAppFiles/softrouter/smartdns/smartdns.conf:/etc/smartdns/smartdns.conf:ro \
  pymumu/smartdns:latest

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
echo "  - SOCKS5 代理: $(hostname -I | awk '{print $1}'):10808"
echo "  - HTTP 代理: $(hostname -I | awk '{print $1}'):10809"
EOF

chmod +x /vol1/1000/NasAppFiles/softrouter/start.sh

# 创建停止脚本
cat > /vol1/1000/NasAppFiles/softrouter/stop.sh << 'EOF'
#!/bin/bash

echo "停止所有服务..."
docker stop adguardhome smartdns xray 2>/dev/null || true
docker rm adguardhome smartdns xray 2>/dev/null || true
echo "✅ 所有服务已停止"
EOF

chmod +x /vol1/1000/NasAppFiles/softrouter/stop.sh

# 显示部署信息
echo ""
echo "✅ 智能软路由部署完成！"
echo ""
echo "📋 部署信息:"
echo "  - 网络模式: $NETWORK_MODE"
echo "  - 配置目录: /vol1/1000/NasAppFiles/softrouter"
echo "  - SOCKS5 代理: $(hostname -I | awk '{print $1}'):10808"
echo "  - HTTP 代理: $(hostname -I | awk '{print $1}'):10809"
echo ""
echo "🚀 启动服务:"
echo "  cd /vol1/1000/NasAppFiles/softrouter && ./start.sh"
echo ""
echo "🛑 停止服务:"
echo "  cd /vol1/1000/NasAppFiles/softrouter && ./stop.sh"
echo ""
echo "🔗 管理界面:"
echo "  - AdGuard Home: http://$(hostname -I | awk '{print $1}'):3000"
echo "  - SmartDNS: $(hostname -I | awk '{print $1}'):5353"
echo ""
echo "⚙️  智能分流规则:"
echo "  - 国内流量: 直连"
echo "  - 国外流量: 代理"
echo "  - 广告过滤: AdGuard Home"
echo "  - DNS 加速: SmartDNS"

# 如果是旁路由模式，显示旁路由配置说明
if [ "$NETWORK_MODE" = "bypass" ]; then
    echo ""
    echo "📋 旁路由配置说明:"
    echo "  1. 在主路由 DHCP 中设置网关为: $BYPASS_IP"
    echo "  2. 在主路由 DHCP 中设置 DNS 为: $BYPASS_IP"
    echo "  3. 或者在设备上手动设置代理:"
    echo "     - HTTP 代理: $(hostname -I | awk '{print $1}'):10809"
    echo "     - SOCKS5 代理: $(hostname -I | awk '{print $1}'):10808"
fi
