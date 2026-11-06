package main

import (
	"net"
	"net/http"
	"strings"
)

// Tải agent cho Linux, macOS, Android. Windows đã có nút riêng (agent_download.go) vì cần dán sẵn địa chỉ server.
// Linux/macOS: Console đưa một dòng lệnh; máy đích tự tải script và file agent từ server rồi cài thành dịch vụ.
// Hai đường /dl và /install công khai trong mạng LAN: file agent không chứa bí mật, mã đăng ký (nếu có) gõ vào dòng lệnh.
var agentFiles = map[string]string{
	"windows": "mam-agent.exe", "windows32": "mam-agent-x86.exe",
	"linux-amd64": "mam-agent-linux-amd64", "linux-arm64": "mam-agent-linux-arm64",
	"mac-amd64": "mam-agent-mac-amd64", "mac-arm64": "mam-agent-mac-arm64",
}

const apkName = "MAM-Agent.apk"

// GET /api/agent-downloads (Super admin): file nào server đang có sẵn để phát.
func (s *Server) hAgentDownloads(w http.ResponseWriter, r *http.Request, me *Account) {
	have := map[string]bool{}
	for k, f := range agentFiles {
		have[k] = s.findFile(f) != ""
	}
	have["android"] = s.findFile(apkName) != ""
	jok(w, have)
}

// GET /dl/agent/{khóa} và /dl/MAM-Agent.apk
func (s *Server) hDL(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/dl/")
	name := ""
	if rest == apkName {
		name = apkName
	} else if k := strings.TrimPrefix(rest, "agent/"); k != rest {
		name = agentFiles[k]
	}
	p := ""
	if name != "" {
		p = s.findFile(name)
	}
	if p == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, p)
}

func serverURL(r *http.Request) (string, bool) {
	hn, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		hn = strings.Trim(r.Host, "[]")
	}
	if ip := net.ParseIP(hn); hn == "localhost" || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified())) || !hostRe.MatchString(r.Host) {
		return "", false
	}
	if r.TLS != nil {
		return "https://" + r.Host, true
	}
	return "http://" + r.Host, true
}

// GET /install/linux.sh và /install/mac.sh
func (s *Server) hInstallScript(w http.ResponseWriter, r *http.Request) {
	tpl := map[string]string{"/install/linux.sh": linuxScript, "/install/mac.sh": macScript}[r.URL.Path]
	srv, ok := serverURL(r)
	if tpl == "" || !ok {
		http.Error(w, "echo 'Hay mo Console bang dia chi IP cua server (khong dung localhost)'; exit 1", 400)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(strings.ReplaceAll(tpl, "__SRV__", srv)))
}

const linuxScript = `#!/bin/sh
# MAM Agent cho Linux. Dùng: curl -fsSL __SRV__/install/linux.sh | sudo sh -s -- [mã đăng ký] [vi|en|zh]
set -e
[ "$(id -u)" = 0 ] || { echo "Chạy bằng sudo / Run with sudo"; exit 1; }
SRV="__SRV__"; KEY="$1"; LANGC="${2:-vi}"
case "$(uname -m)" in aarch64|arm64) A=arm64;; *) A=amd64;; esac
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
if command -v curl >/dev/null 2>&1; then curl -fsSL "$SRV/dl/agent/linux-$A" -o "$T/mam-agent"; else wget -qO "$T/mam-agent" "$SRV/dl/agent/linux-$A"; fi
mkdir -p /opt/mam-agent
systemctl stop mam-agent 2>/dev/null || true
install -m 0755 "$T/mam-agent" /opt/mam-agent/mam-agent
if [ ! -f /opt/mam-agent/config.json ]; then
  printf '{"server":"%s","key":"%s","id":"","token":"","name":"","lang":"%s"}\n' "$SRV" "$KEY" "$LANGC" > /opt/mam-agent/config.json
  chmod 600 /opt/mam-agent/config.json
else
  echo "Đã có config cũ, giữ nguyên mã máy/token."
fi
cat > /etc/systemd/system/mam-agent.service <<'UNIT'
[Unit]
Description=MAM Agent - Missing App Monitor (client)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/opt/mam-agent/mam-agent -config /opt/mam-agent/config.json run
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable mam-agent
systemctl restart mam-agent
mkdir -p /etc/xdg/autostart
cat > /etc/xdg/autostart/mam-agent-ui.desktop <<'DESK'
[Desktop Entry]
Type=Application
Name=MAM
Exec=/opt/mam-agent/mam-agent -config /opt/mam-agent/config.json ui
X-GNOME-Autostart-enabled=true
NoDisplay=true
DESK
sleep 2
systemctl --no-pager --lines=3 status mam-agent || true
echo "Đã cài. Xem màn hình cần thêm: sudo apt install scrot xdotool (hoặc tương đương)."
`

const macScript = `#!/bin/sh
# MAM Agent cho macOS. Dùng: curl -fsSL __SRV__/install/mac.sh | sudo sh -s -- [mã đăng ký] [vi|en|zh]
set -e
[ "$(id -u)" = 0 ] || { echo "Chạy bằng sudo / Run with sudo"; exit 1; }
SRV="__SRV__"; KEY="$1"; LANGC="${2:-vi}"
A=arm64; [ "$(uname -m)" = "x86_64" ] && A=amd64
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
curl -fsSL "$SRV/dl/agent/mac-$A" -o "$T/mam-agent"
mkdir -p /usr/local/opt/mam-agent /usr/local/var/log
launchctl bootout system/com.missing.mam-agent 2>/dev/null || true
install -m 0755 "$T/mam-agent" /usr/local/opt/mam-agent/mam-agent
if [ ! -f /usr/local/opt/mam-agent/config.json ]; then
  printf '{"server":"%s","key":"%s","id":"","token":"","name":"","lang":"%s"}\n' "$SRV" "$KEY" "$LANGC" > /usr/local/opt/mam-agent/config.json
  chmod 600 /usr/local/opt/mam-agent/config.json
else
  echo "Đã có config cũ, giữ nguyên mã máy/token."
fi
cat > /Library/LaunchDaemons/com.missing.mam-agent.plist <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>com.missing.mam-agent</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/local/opt/mam-agent/mam-agent</string>
		<string>-config</string><string>/usr/local/opt/mam-agent/config.json</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>/usr/local/var/log/mam-agent.log</string>
	<key>StandardErrorPath</key><string>/usr/local/var/log/mam-agent.log</string>
</dict>
</plist>
PLIST
launchctl bootstrap system /Library/LaunchDaemons/com.missing.mam-agent.plist
cat > /Library/LaunchAgents/com.missing.mam-agent-ui.plist <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>com.missing.mam-agent-ui</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/local/opt/mam-agent/mam-agent</string>
		<string>-config</string><string>/usr/local/opt/mam-agent/config.json</string>
		<string>ui</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
</dict>
</plist>
PLIST
echo "Đã cài lõi agent."
echo "LƯU Ý macOS: xem màn hình cần cấp quyền 'Ghi màn hình' cho mam-agent trong System Settings > Privacy & Security; cửa sổ UI hiện sau khi người dùng đăng nhập lại."
`
