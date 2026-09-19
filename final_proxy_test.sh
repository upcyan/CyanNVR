#!/bin/bash

# 最终代理测试脚本

echo "=== 最终代理测试 ==="

# 获取宿主机 IP
HOST_IP=$(hostname -I | awk '{print $1}')
echo "宿主机 IP: $HOST_IP"

# 检查 Xray 状态
echo ""
echo "检查 Xray 状态..."
if docker ps | grep -q xray; then
    echo "✅ Xray 正在运行"
else
    echo "❌ Xray 未运行"
    echo "启动 Xray..."
    docker run -d \
      --name xray \
      --restart unless-stopped \
      --network host \
      -v /vol1/1000/NasAppFiles/softrouter/xray/config.json:/etc/xray/config.json:ro \
      teddysun/xray:latest
    sleep 3
fi

# 测试代理
echo ""
echo "测试代理连接..."

echo "1. 测试 SOCKS5 代理..."
result=$(curl --socks5 ${HOST_IP}:10808 https://github.com -s -o /dev/null -w "%{http_code}" --connect-timeout 10)
if [ "$result" -eq 200 ]; then
    echo "✅ SOCKS5 代理正常 (HTTP $result)"
else
    echo "❌ SOCKS5 代理异常 (HTTP $result)"
fi

echo "2. 测试 HTTP 代理..."
result=$(curl -x http://${HOST_IP}:10809 https://github.com -s -o /dev/null -w "%{http_code}" --connect-timeout 10)
if [ "$result" -eq 200 ]; then
    echo "✅ HTTP 代理正常 (HTTP $result)"
else
    echo "❌ HTTP 代理异常 (HTTP $result)"
fi

echo "3. 测试 Google..."
result=$(curl --socks5 ${HOST_IP}:10808 https://google.com -s -o /dev/null -w "%{http_code}" --connect-timeout 10)
if [ "$result" -eq 200 ] || [ "$result" -eq 301 ]; then
    echo "✅ Google 正常 (HTTP $result)"
else
    echo "❌ Google 异常 (HTTP $result)"
fi

echo "4. 测试百度..."
result=$(curl --socks5 ${HOST_IP}:10808 https://baidu.com -s -o /dev/null -w "%{http_code}" --connect-timeout 10)
if [ "$result" -eq 200 ]; then
    echo "✅ 百度正常 (HTTP $result)"
else
    echo "❌ 百度异常 (HTTP $result)"
fi

echo ""
echo "=== 测试完成 ==="
echo ""
echo "📋 总结:"
echo "  - SOCKS5 代理: ${HOST_IP}:10808"
echo "  - HTTP 代理: ${HOST_IP}:10809"
echo "  - 管理界面: http://${HOST_IP}:3000"
