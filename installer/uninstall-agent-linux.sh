#!/bin/sh
# Gỡ MAM Agent trên Linux.  Dùng: sudo ./uninstall-agent-linux.sh
[ "$(id -u)" = 0 ] || { echo "Chạy bằng sudo / Run with sudo"; exit 1; }
systemctl disable --now mam-agent 2>/dev/null || true
rm -f /etc/systemd/system/mam-agent.service /etc/xdg/autostart/mam-agent-ui.desktop
rm -rf /opt/mam-agent
systemctl daemon-reload || true
# gỡ phần chặn web MAM đã thêm vào /etc/hosts (nếu có)
sed -i '/# MAM-BEGIN/,/# MAM-END/d' /etc/hosts 2>/dev/null || true
echo "Đã gỡ MAM Agent."
