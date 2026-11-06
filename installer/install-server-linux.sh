#!/bin/sh
# MAM Server cho Linux / for Linux / Linux 版.  Dùng: sudo ./install-server-linux.sh [vi|en|zh] [cổng/port]
set -e
LANGC="${1:-vi}"; PORT="${2:-7070}"
[ "$(id -u)" = 0 ] || { echo "Run with sudo / Chạy bằng sudo / 请用 sudo 运行"; exit 1; }
cd "$(dirname "$0")"
id mam >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin mam
mkdir -p /opt/mam
install -m 0755 mam-server /opt/mam/mam-server
# các file agent để nút "Tải agent" trong Console phát cho Windows, Linux, macOS
for p in windows/mam-agent.exe:mam-agent.exe windows/mam-agent-x86.exe:mam-agent-x86.exe linux/mam-agent:mam-agent-linux-amd64 linux/mam-agent-arm64:mam-agent-linux-arm64 mac/mam-agent-amd64:mam-agent-mac-amd64 mac/mam-agent-arm64:mam-agent-mac-arm64; do
  [ -f "../../agent/${p%%:*}" ] && install -m 0644 "../../agent/${p%%:*}" "/opt/mam/${p##*:}" || true
done
sed -e "s/MAM_LANG=vi/MAM_LANG=$LANGC/" -e "s/:7070/:$PORT/" mam-server.service > /etc/systemd/system/mam-server.service
systemctl daemon-reload
systemctl enable --now mam-server
sleep 2
systemctl --no-pager --lines=3 status mam-server || true
echo "http://$(hostname -I | awk '{print $1}'):$PORT"
