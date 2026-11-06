#!/bin/sh
# MAM Agent cho macOS.  Dùng: sudo ./install-agent-mac.sh <server:cổng> [vi|en|zh] [mã đăng ký]
# Ví dụ: sudo ./install-agent-mac.sh 192.168.1.10:7070 vi
set -e
[ "$(id -u)" = 0 ] || { echo "Chạy bằng sudo / Run with sudo"; exit 1; }
SRV="$1"; LANGC="${2:-vi}"; KEY="$3"
[ -n "$SRV" ] || { echo "Thiếu địa chỉ server. Ví dụ: sudo ./install-agent-mac.sh 192.168.1.10:7070"; exit 1; }
case "$SRV" in http://*|https://*) ;; *) SRV="http://$SRV";; esac
cd "$(dirname "$0")"
mkdir -p /usr/local/opt/mam-agent /usr/local/var/log
BIN=mam-agent-arm64; [ "$(uname -m)" = "x86_64" ] && BIN=mam-agent-amd64
# cài đè bản mới: dừng dịch vụ cũ trước rồi thay binary
launchctl bootout system/com.missing.mam-agent 2>/dev/null || true
install -m 0755 "$BIN" /usr/local/opt/mam-agent/mam-agent
# giữ config cũ nếu đã có (giữ token để khỏi đăng ký lại)
if [ ! -f /usr/local/opt/mam-agent/config.json ]; then
  cat > /usr/local/opt/mam-agent/config.json <<EOF
{"server":"$SRV","key":"$KEY","id":"","token":"","name":"","lang":"$LANGC"}
EOF
  chmod 600 /usr/local/opt/mam-agent/config.json
else
  echo "Đã có config cũ — giữ nguyên mã máy/token. Muốn đổi server thì sửa /usr/local/opt/mam-agent/config.json hoặc gỡ rồi cài lại."
fi
# lõi chạy nền (root): số liệu, lệnh, chặn web
cp com.missing.mam-agent.plist /Library/LaunchDaemons/
launchctl bootstrap system /Library/LaunchDaemons/com.missing.mam-agent.plist
# cửa sổ nhắn tin / xem màn hình chạy trong phiên người dùng
cp com.missing.mam-agent-ui.plist /Library/LaunchAgents/
echo "Đã cài lõi agent."
echo "LƯU Ý macOS:"
echo " - Xem màn hình cần cấp quyền 'Ghi màn hình' cho mam-agent trong System Settings > Privacy & Security."
echo " - Điều khiển chuột/phím cần 'Accessibility' và cài cliclick (brew install cliclick)."
echo " - Cửa sổ UI: người dùng đăng nhập lại, hoặc chạy: launchctl bootstrap gui/\$(id -u) /Library/LaunchAgents/com.missing.mam-agent-ui.plist"
