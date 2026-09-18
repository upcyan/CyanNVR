#!/bin/bash

# 启动软路由服务

echo "=== 启动软路由服务 ==="

# 启动 Xray
echo "启动 Xray..."
docker run -d \
  --name xray \
  --restart unless-stopped \
  --network host \
  -v /vol1/1000/NasAppFiles/softrouter/xray/config.json:/etc/xray/config.json:ro \
  teddysun/xray:latest

# 启动 AdGuard Home
echo "启动 AdGuard Home..."
docker run -d \
  --name adguardhome \
  --restart unless-stopped \
  --network host \
  -v /vol1/1000/NasAppFiles/softrouter/adguard/work:/opt/adguardhome/work \
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
