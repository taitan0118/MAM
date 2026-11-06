#!/bin/sh
# Gỡ MAM Agent trên macOS.  Dùng: sudo ./uninstall-agent-mac.sh
[ "$(id -u)" = 0 ] || { echo "Chạy bằng sudo / Run with sudo"; exit 1; }
launchctl bootout system/com.missing.mam-agent 2>/dev/null || true
launchctl bootout gui/$(stat -f%u /dev/console)/com.missing.mam-agent-ui 2>/dev/null || true
rm -f /Library/LaunchDaemons/com.missing.mam-agent.plist /Library/LaunchAgents/com.missing.mam-agent-ui.plist
/usr/local/opt/mam-agent/mam-agent dnsrestore 2>/dev/null || true  # trả DNS cũ nếu bộ lọc chặn web đang bật
rm -rf /usr/local/opt/mam-agent
sed -i '' '/# MAM-BEGIN/,/# MAM-END/d' /etc/hosts 2>/dev/null || true
echo "Đã gỡ MAM Agent."
