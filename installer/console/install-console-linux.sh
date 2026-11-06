#!/bin/sh
# Cài MAM Console cho người dùng hiện tại: chép vào ~/.local/share/mam-console và tạo mục trong menu ứng dụng.
set -e
cd "$(dirname "$0")"
case "$(uname -m)" in aarch64|arm64) BIN=mam-console-arm64;; *) BIN=mam-console;; esac
D="$HOME/.local/share/mam-console"; mkdir -p "$D" "$HOME/.local/share/applications"
cp "$BIN" "$D/mam-console"; chmod +x "$D/mam-console"; cp mam-console.png "$D/"
cat > "$HOME/.local/share/applications/mam-console.desktop" <<EOT
[Desktop Entry]
Type=Application
Name=MAM Console
Comment=Missing App Monitor
Exec=$D/mam-console
Icon=$D/mam-console.png
Categories=System;Monitor;
Terminal=false
EOT
echo "Đã cài MAM Console. Mở từ menu ứng dụng, hoặc chạy: $D/mam-console"
