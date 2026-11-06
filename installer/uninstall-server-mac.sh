#!/bin/sh
# Gỡ MAM Server trên macOS (giữ dữ liệu ở /usr/local/var/mam) / remove (data in /usr/local/var/mam is kept)
[ "$(id -u)" = 0 ] || { echo "sudo required"; exit 1; }
launchctl bootout system/com.missing.mam-server 2>/dev/null || true
rm -f /Library/LaunchDaemons/com.missing.mam-server.plist
rm -rf /usr/local/opt/mam
echo "Done. Data kept in /usr/local/var/mam"
