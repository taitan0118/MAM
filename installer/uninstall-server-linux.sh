#!/bin/sh
# Gỡ MAM Server (giữ dữ liệu ở /var/lib/mam) / remove MAM Server (data in /var/lib/mam is kept) / 卸载（数据保留在 /var/lib/mam）
[ "$(id -u)" = 0 ] || { echo "sudo required"; exit 1; }
systemctl disable --now mam-server 2>/dev/null
rm -f /etc/systemd/system/mam-server.service; rm -rf /opt/mam
systemctl daemon-reload
echo "Done. Data kept in /var/lib/mam"
