#!/bin/sh
# Dựng toàn bộ gói phát hành MAM. Cần Go 1.24+ và python3. Chạy: ./build.sh  -> dist/MAM-<phiên bản>.zip
set -e
cd "$(dirname "$0")"
VER=$(grep -o 'Version *= *"[^"]*"' server/main.go | head -1 | cut -d'"' -f2)
OUT="dist/MAM-$VER"
rm -rf dist; mkdir -p "$OUT/server/windows" "$OUT/server/linux" "$OUT/server/mac" "$OUT/agent/windows" "$OUT/agent/windows-legacy" "$OUT/agent/linux" "$OUT/agent/mac" "$OUT/console/windows" "$OUT/console/mac" "$OUT/console/linux" "$OUT/docs" "$OUT/source"
cp server/web/missing-intro.js agent/missing-intro.js  # cửa sổ agent dùng chung hiệu ứng logo với Console
echo "== Kiểm thử / tests"; (cd server && go vet ./... && go test ./...)
echo "== Server (Windows, Linux, macOS)"
(cd server && GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "../$OUT/server/windows/mam-server.exe" .
 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "../$OUT/server/linux/mam-server" .
 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "../$OUT/server/linux/mam-server-arm64" .
 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "../$OUT/server/mac/mam-server-amd64" .
 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "../$OUT/server/mac/mam-server-arm64" .)
echo "== Console desktop (Windows, Linux, macOS)"
(cd console && go vet ./... && go test ./...
 LD="-s -w -X main.Version=$VER"
 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LD -H windowsgui" -o "../$OUT/console/windows/mam-console.exe" .
 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LD" -o "../$OUT/console/linux/mam-console" .
 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$LD" -o "../$OUT/console/linux/mam-console-arm64" .
 for A in arm64 amd64; do
  case $A in arm64) N="MAM Console (Apple Silicon).app";; *) N="MAM Console (Intel).app";; esac
  APP="../$OUT/console/mac/$N/Contents"; mkdir -p "$APP/MacOS" "$APP/Resources"
  GOOS=darwin GOARCH=$A go build -trimpath -ldflags "$LD" -o "$APP/MacOS/mam-console" .
  cp ../installer/console/mam-console.icns "$APP/Resources/"
  printf '<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0"><dict><key>CFBundleName</key><string>MAM Console</string><key>CFBundleDisplayName</key><string>MAM Console</string><key>CFBundleIdentifier</key><string>com.missing.mam-console</string><key>CFBundleExecutable</key><string>mam-console</string><key>CFBundleIconFile</key><string>mam-console</string><key>CFBundleVersion</key><string>%s</string><key>CFBundleShortVersionString</key><string>%s</string><key>CFBundlePackageType</key><string>APPL</string><key>LSMinimumSystemVersion</key><string>10.13</string><key>NSHighResolutionCapable</key><true/></dict></plist>\n' "$VER" "$VER" > "$APP/Info.plist"
 done)
cp installer/console/install-console-windows.bat "$OUT/console/windows/"
cp installer/console/install-console-linux.sh installer/console/mam-console.png "$OUT/console/linux/"
echo "== Agent (Windows, Linux, macOS)"
(cd agent && GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "../$OUT/agent/windows/mam-agent.exe" .
 GOOS=windows GOARCH=386 go build -trimpath -ldflags "-s -w" -o "../$OUT/agent/windows/mam-agent-x86.exe" .
 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "../$OUT/agent/linux/mam-agent" .
 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "../$OUT/agent/linux/mam-agent-arm64" .
 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "../$OUT/agent/mac/mam-agent-amd64" .
 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "../$OUT/agent/mac/mam-agent-arm64" .)
# server đọc phiên bản agent từ chuỗi MAMVER= trong file: thiếu là tự cập nhật không chạy, nên dừng build
for f in "$OUT/agent/windows/mam-agent.exe" "$OUT/agent/windows/mam-agent-x86.exe"; do grep -aq "MAMVER=$VER" "$f" || { echo "LỖI: $f thiếu chuỗi MAMVER=$VER"; exit 1; }; done
(cd agent-ps && python3 build.py && cp MAM-Agent.ps1 "../$OUT/agent/windows-legacy/")
# Android: giao mã nguồn project (build APK bằng Android Studio trên máy có Android SDK)
mkdir -p "$OUT/agent/android"
(cd agent-android && find . -type d -name build -prune -o -type f -print | cpio -pdm --quiet "../$OUT/agent/android" 2>/dev/null) || cp -r agent-android/. "$OUT/agent/android/"
echo "== Bộ cài một file .exe / single-file setup"
(cd installer/setup-server && rm -rf payload/agents payload/mam-agent.exe && mkdir -p payload/agents && cp "../../$OUT/server/windows/mam-server.exe" payload/ && A="../../$OUT/agent" &&
 cp "$A/windows/mam-agent.exe" "$A/windows/mam-agent-x86.exe" payload/agents/ && cp "$A/linux/mam-agent" payload/agents/mam-agent-linux-amd64 && cp "$A/linux/mam-agent-arm64" payload/agents/mam-agent-linux-arm64 &&
 cp "$A/mac/mam-agent-amd64" payload/agents/mam-agent-mac-amd64 && cp "$A/mac/mam-agent-arm64" payload/agents/mam-agent-mac-arm64 && GOOS=windows GOARCH=amd64 go vet . && GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$VER" -o "../../$OUT/MAM-Server-Setup.exe" .)
(cd installer/setup-agent && mkdir -p payload && cp "../../$OUT/agent/windows/mam-agent.exe" payload/ && GOOS=windows GOARCH=amd64 go vet . && GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$VER" -o "../../$OUT/MAM-Agent-Setup.exe" .)
echo "== Bộ cài / installers"
cp installer/install-server.* installer/uninstall-server.* "$OUT/server/windows/"
cp installer/mam-server.service installer/install-server-linux.sh installer/uninstall-server-linux.sh "$OUT/server/linux/"
cp installer/com.missing.mam-server.plist installer/install-server-mac.sh installer/uninstall-server-mac.sh "$OUT/server/mac/"
cp installer/install-agent.bat installer/uninstall-agent.bat "$OUT/agent/windows/"
cp installer/install-agent-legacy.bat installer/uninstall-agent-legacy.bat "$OUT/agent/windows-legacy/"
cp installer/mam-agent.service installer/install-agent-linux.sh installer/uninstall-agent-linux.sh "$OUT/agent/linux/"
cp installer/com.missing.mam-agent.plist installer/com.missing.mam-agent-ui.plist installer/install-agent-mac.sh installer/uninstall-agent-mac.sh "$OUT/agent/mac/"
cp LICENSE.txt "$OUT/"; cp LICENSE.txt "$OUT/server/windows/"; cp README.txt "$OUT/"
[ -d docs-out ] && cp docs-out/* "$OUT/docs/" || true
echo "== Mã nguồn / source"
zip -qr "$OUT/source/MAM-source-$VER.zip" server agent console agent-ps agent-android installer docs tests i18n-src build.sh LICENSE.txt README.txt -x '*/node_modules/*' 'installer/*/payload/*' 'console/*.exe' 'installer/*/*.exe' '*.bak' 'agent-ps/MAM-Agent.ps1' 'agent-android/*/build/*' 'agent-android/.gradle/*'
(cd dist && zip -qr "MAM-$VER.zip" "MAM-$VER")
(cd dist && sha256sum "MAM-$VER.zip" > "MAM-$VER.zip.sha256")
ls -la dist
