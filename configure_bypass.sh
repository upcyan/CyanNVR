#!/bin/bash

# 旁路由网络配置脚本
# 自动配置网络参数

set -e

echo "=== 旁路由网络配置脚本 ==="
echo ""

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echo "❌ 请以 root 用户执行此脚本"
    echo "使用: sudo ./configure_bypass.sh"
    exit 1
fi

echo "✅ 检测到 root 权限"

# 获取网络信息
echo ""
echo "📋 请输入网络信息："

# 获取默认网卡
DEFAULT_IFACE=$(ip route | grep default | awk '{print $5}' | head -1)
echo "检测到默认网卡: $DEFAULT_IFACE"

read -p "旁路由 IP 地址 (如 192.168.1.2): " BYPASS_IP
read -p "子网掩码 (默认 255.255.255.0): " NETMASK
NETMASK=${NETMASK:-255.255.255.0}
read -p "网关 (主路由 IP, 如 192.168.1.1): " GATEWAY
read -p "DNS 服务器 (默认 8.8.8.8): " DNS
DNS=${DNS:-8.8.8.8}

# 配置静态 IP
echo ""
echo "🔧 配置静态 IP..."

# 创建网络配置脚本
cat > /tmp/configure_network.sh << EOF
#!/bin/bash

# 配置静态 IP
echo "配置静态 IP..."

# 备份原配置
cp /etc/network/interfaces /etc/network/interfaces.backup

# 创建新配置
cat > /etc/network/interfaces << INTERFACES
# 旁路由网络配置
auto lo
iface lo inet loopback

auto $DEFAULT_IFACE
iface $DEFAULT_IFACE inet static
    address $BYPASS_IP
    netmask $NETMASK
    gateway $GATEWAY
    dns-nameservers $DNS
INTERFACES

echo "✅ 网络配置已更新"
echo "重启网络服务..."

# 重启网络服务
systemctl restart networking

echo "✅ 网络服务已重启"
EOF

chmod +x /tmp/configure_network.sh

# 执行网络配置
/tmp/configure_network.sh

# 配置 IP 转发
echo ""
echo "🔧 配置 IP 转发..."

echo 1 > /proc/sys/net/ipv4/ip_forward
echo "net.ipv4.ip_forward = 1" >> /etc/sysctl.conf

sysctl -p

echo "✅ IP 转发已启用"

# 配置 iptables 规则
echo ""
echo "🔧 配置 iptables 规则..."

# 允许转发
iptables -A FORWARD -i $DEFAULT_IFACE -o eth0 -j ACCEPT
iptables -A FORWARD -i eth0 -o $DEFAULT_IFACE -m state --state RELATED,ESTABLISHED -j ACCEPT

# 保存 iptables 规则
iptables-save > /etc/iptables/rules.v4

echo "✅ iptables 规则已配置"

# 显示配置信息
echo ""
echo "✅ 旁路由网络配置完成！"
echo ""
echo "📋 配置信息:"
echo "  - 旁路由 IP: $BYPASS_IP"
echo "  - 子网掩码: $NETMASK"
echo "  - 网关: $GATEWAY"
echo "  - DNS: $DNS"
echo "  - 网卡: $DEFAULT_IFACE"
echo ""
echo "🔗 下一步:"
echo "  1. 在主路由 DHCP 中设置网关为: $BYPASS_IP"
echo "  2. 在主路由 DHCP 中设置 DNS 为: $BYPASS_IP"
echo "  3. 启动软路由服务:"
echo "     cd /vol1/1000/NasAppFiles/softrouter && ./start.sh"
echo ""
echo "⚠️  注意: 修改网络配置后需要重新连接"
