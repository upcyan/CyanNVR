#!/bin/bash

# 代理测试脚本

echo "=== 代理测试脚本 ==="

# 获取宿主机 IP
HOST_IP=$(hostname -I | awk '{print $1}')
echo "宿主机 IP: $HOST_IP"

# 测试列表
TESTS=(
    "https://github.com"
    "https://google.com"
    "https://youtube.com"
    "https://telegram.org"
    "https://twitter.com"
    "https://facebook.com"
    "https://netflix.com"
    "https://openai.com"
    "https://baidu.com"
)

# 测试 SOCKS5 代理
echo ""
echo "=== 测试 SOCKS5 代理 (${HOST_IP}:10808) ==="

for url in "${TESTS[@]}"; do
    echo -n "测试 $url ... "
    result=$(curl --socks5 ${HOST_IP}:10808 $url -s -o /dev/null -w "%{http_code}" --connect-timeout 10)
    if [ "$result" -eq 200 ] || [ "$result" -eq 301 ] || [ "$result" -eq 302 ]; then
        echo "✅ 成功 (HTTP $result)"
    else
        echo "❌ 失败 (HTTP $result)"
    fi
done

# 测试 HTTP 代理
echo ""
echo "=== 测试 HTTP 代理 (${HOST_IP}:10809) ==="

for url in "${TESTS[@]}"; do
    echo -n "测试 $url ... "
    result=$(curl -x http://${HOST_IP}:10809 $url -s -o /dev/null -w "%{http_code}" --connect-timeout 10)
    if [ "$result" -eq 200 ] || [ "$result" -eq 301 ] || [ "$result" -eq 302 ]; then
        echo "✅ 成功 (HTTP $result)"
    else
        echo "❌ 失败 (HTTP $result)"
    fi
done

# 测试 Git 代理
echo ""
echo "=== 测试 Git 代理 ==="

echo "测试 Git 配置..."
git config --global --get http.proxy
git config --global --get https.proxy

echo ""
echo "=== 测试完成 ==="
