#!/bin/sh
# MAM Server cho macOS / for macOS / macOS 版.  Dùng: sudo ./install-server-mac.sh [vi|en|zh] [cổng/port]
set -e
LANGC="${1:-vi}"; PORT="${2:-7070}"
[ "$(id -u)" = 0 ] || { echo "Run with sudo / Chạy bằng sudo / 请用 sudo 运行"; exit 1; }
cd "$(dirname "$0")"
mkdir -p /usr/local/opt/mam /usr/local/var/mam /usr/local/var/log
BIN=mam-server-arm64; [ "$(uname -m)" = "x86_64" ] && BIN=mam-server-amd64
install -m 0755 "$BIN" /usr/local/opt/mam/mam-server
# các file agent để nút "Tải agent" trong Console phát cho Windows, Linux, macOS
for p in windows/mam-agent.exe:mam-agent.exe windows/mam-agent-x86.exe:mam-agent-x86.exe linux/mam-agent:mam-agent-linux-amd64 linux/mam-agent-arm64:mam-agent-linux-arm64 mac/mam-agent-amd64:mam-agent-mac-amd64 mac/mam-agent-arm64:mam-agent-mac-arm64; do
  [ -f "../../agent/${p%%:*}" ] && install -m 0644 "../../agent/${p%%:*}" "/usr/local/opt/mam/${p##*:}" || true
done
sed -e "s/MAM_LANG<\/key><string>vi/MAM_LANG<\/key><string>$LANGC/" -e "s/:7070/:$PORT/" com.missing.mam-server.plist > /Library/LaunchDaemons/com.missing.mam-server.plist
launchctl bootout system/com.missing.mam-server 2>/dev/null || true
launchctl bootstrap system /Library/LaunchDaemons/com.missing.mam-server.plist
sleep 2
launchctl print system/com.missing.mam-server | head -5 || true
echo "http://$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo 127.0.0.1):$PORT"
