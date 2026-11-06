#!/bin/sh
# MAM Agent cho Linux / for Linux.  Dùng: sudo ./install-agent-linux.sh <server:cổng> [vi|en|zh] [mã đăng ký]
# Ví dụ: sudo ./install-agent-linux.sh 192.168.1.10:7070 vi
set -e
[ "$(id -u)" = 0 ] || { echo "Chạy bằng sudo / Run with sudo"; exit 1; }
SRV="$1"; LANGC="${2:-vi}"; KEY="$3"
[ -n "$SRV" ] || { echo "Thiếu địa chỉ server. Ví dụ: sudo ./install-agent-linux.sh 192.168.1.10:7070"; exit 1; }
case "$SRV" in http://*|https://*) ;; *) SRV="http://$SRV";; esac
cd "$(dirname "$0")"
mkdir -p /opt/mam-agent
# cài đè bản mới: dừng dịch vụ cũ, thay binary (ghi đè được dù đang chạy), rồi chạy lại
systemctl stop mam-agent 2>/dev/null || true
install -m 0755 mam-agent /opt/mam-agent/mam-agent
# giữ config cũ nếu đã có (giữ token để khỏi đăng ký lại); chưa có thì tạo mới
if [ ! -f /opt/mam-agent/config.json ]; then
  cat > /opt/mam-agent/config.json <<EOF
{"server":"$SRV","key":"$KEY","id":"","token":"","name":"","lang":"$LANGC"}
EOF
  chmod 600 /opt/mam-agent/config.json
else
  echo "Đã có config cũ — giữ nguyên mã máy/token. Muốn đổi server thì sửa /opt/mam-agent/config.json hoặc gỡ rồi cài lại."
fi
cp mam-agent.service /etc/systemd/system/mam-agent.service
systemctl daemon-reload
systemctl enable mam-agent
systemctl restart mam-agent
# cửa sổ nhắn tin / xin phép xem màn hình: chạy trong phiên đồ họa của người dùng
cat > /etc/xdg/autostart/mam-agent-ui.desktop <<EOF
[Desktop Entry]
Type=Application
Name=MAM
Exec=/opt/mam-agent/mam-agent -config /opt/mam-agent/config.json ui
X-GNOME-Autostart-enabled=true
NoDisplay=true
EOF
sleep 2
systemctl --no-pager --lines=3 status mam-agent || true
echo "Đã cài. Xem màn hình cần thêm: sudo apt install scrot xdotool (hoặc tương đương)."
