#!/bin/bash

# OpenWrt 软路由 + V2Ray 智能分流部署脚本
# 基于 Docker 容器化 OpenWrt

set -e

echo "=== OpenWrt 软路由 + V2Ray 智能分流部署脚本 ==="
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "❌ 请以 root 用户执行此脚本"
    echo "使用: sudo ./deploy_openwrt.sh"
    exit 1
fi

echo "✅ 检测到 root 权限"

# 创建目录结构
echo ""
echo "📁 创建目录结构..."
mkdir -p /vol1/1000/NasAppFiles/openwrt/{config,data,v2ray}
chmod -R 755 /vol1/1000/NasAppFiles/openwrt

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
cat > /vol1/1000/NasAppFiles/openwrt/v2ray/config.json << EOF
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

# 拉取 OpenWrt Docker 镜像
echo ""
echo "📦 拉取 OpenWrt Docker 镜像..."
docker pull sulinggg/openwrt:latest

echo "✅ OpenWrt 镜像拉取完成"

# 创建启动脚本
echo ""
echo "🔧 创建启动脚本..."
cat > /vol1/1000/NasAppFiles/openwrt/start.sh << 'EOF'
#!/bin/bash

# 获取宿主机网络接口
HOST_IFACE=$(ip route | grep default | awk '{print $5}' | head -1)
HOST_IP=$(ip addr show $HOST_IFACE | grep "inet " | awk '{print $2}' | cut -d/ -f1)

echo "检测到网络接口: $HOST_IFACE"
echo "宿主机 IP: $HOST_IP"

# 启动 OpenWrt
echo "启动 OpenWrt..."
docker run -d \
  --name openwrt \
  --restart unless-stopped \
  --network host \
  --privileged \
  sulinggg/openwrt:latest /sbin/init

echo "✅ OpenWrt 已启动"
echo ""
echo "📋 管理信息:"
echo "  - OpenWrt Web 界面: http://$HOST_IP:8080"
echo "  - 默认密码: password"
echo ""
echo "⚙️  配置步骤:"
echo "  1. 访问 http://$HOST_IP:8080"
echo "  2. 登录（用户名: root, 密码: password）"
echo "  3. 进入 网络 → 接口"
echo "  4. 配置 WAN 口为静态 IP"
echo "  5. 安装 V2Ray 插件"
echo "  6. 配置智能分流规则"
EOF

chmod +x /vol1/1000/NasAppFiles/openwrt/start.sh

# 创建 V2Ray 插件安装脚本
cat > /vol1/1000/NasAppFiles/openwrt/install_v2ray.sh << 'EOF'
#!/bin/bash

# 在 OpenWrt 容器中安装 V2Ray
echo "在 OpenWrt 中安装 V2Ray..."

docker exec openwrt sh -c "
  # 更新软件包列表
  opkg update
  
  # 安装 V2Ray
  opkg install v2ray-core
  
  # 安装 LuCI 界面
  opkg install luci-app-v2ray
  
  # 安装中文语言包
  opkg install luci-i18n-v2ray-base-zh-cn
  
  # 重启 LuCI
  /etc/init.d/uhttpd restart
"

echo "✅ V2Ray 已安装"
echo ""
echo "📋 配置 V2Ray:"
echo "  1. 访问 LuCI 界面"
echo "  2. 进入 服务 → V2Ray"
echo "  3. 配置服务器信息"
echo "  4. 配置智能分流规则"
EOF

chmod +x /vol1/1000/NasAppFiles/openwrt/install_v2ray.sh

# 创建停止脚本
cat > /vol1/1000/NasAppFiles/openwrt/stop.sh << 'EOF'
#!/bin/bash

echo "停止 OpenWrt..."
docker stop openwrt
docker rm openwrt
echo "✅ OpenWrt 已停止"
EOF

chmod +x /vol1/1000/NasAppFiles/openwrt/stop.sh

echo "✅ 启动脚本已创建"

# 显示部署信息
echo ""
echo "✅ OpenWrt 软路由 + V2Ray 智能分流部署完成！"
echo ""
echo "📋 部署信息:"
echo "  - 配置目录: /vol1/1000/NasAppFiles/openwrt"
echo "  - V2Ray 配置: /vol1/1000/NasAppFiles/openwrt/v2ray/config.json"
echo ""
echo "🚀 启动服务:"
echo "  cd /vol1/1000/NasAppFiles/openwrt && ./start.sh"
echo ""
echo "📦 安装 V2Ray 插件:"
echo "  cd /vol1/1000/NasAppFiles/openwrt && ./install_v2ray.sh"
echo ""
echo "🛑 停止服务:"
echo "  cd /vol1/1000/NasAppFiles/openwrt && ./stop.sh"
echo ""
echo "🔗 管理界面:"
echo "  - OpenWrt Web: http://$(hostname -I | awk '{print $1}'):8080"
echo "  - 默认用户名: root"
echo "  - 默认密码: password"
echo ""
echo "⚙️  智能分流规则:"
echo "  - 国内流量: 直连"
echo "  - 国外流量: 代理"
echo "  - 广告过滤: OpenWrt 插件"
echo "  - DNS 加速: OpenWrt DNS"
