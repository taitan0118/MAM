package main

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var hostRe = regexp.MustCompile(`^[A-Za-z0-9.\-:\[\]]{1,255}$`)

// findAgentExe tìm mam-agent.exe cạnh mam-server hoặc trong thư mục dữ liệu.
func (s *Server) findAgentExe() string { return s.findFile("mam-agent.exe") }

// findFile tìm một file phát hành (agent, APK) cạnh mam-server hoặc trong thư mục dữ liệu.
func (s *Server) findFile(name string) string {
	var c []string
	if self, err := os.Executable(); err == nil {
		c = append(c, filepath.Join(filepath.Dir(self), name))
	}
	c = append(c, filepath.Join(s.dir, name))
	for _, p := range c {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// bundleAgent dán cấu hình vào đuôi file .exe: JSON + độ dài uint32 LE + "MAMCFG01" (agent đọc lại bằng readBundle).
func bundleAgent(exe []byte, server, key, lang string) []byte {
	js, _ := json.Marshal(map[string]string{"server": server, "key": key, "lang": lang})
	n := make([]byte, 4)
	binary.LittleEndian.PutUint32(n, uint32(len(js)))
	out := make([]byte, 0, len(exe)+len(js)+12)
	out = append(out, exe...)
	out = append(out, js...)
	out = append(out, n...)
	return append(out, "MAMCFG01"...)
}

// hAgentDownload trả mam-agent.exe đã gắn sẵn địa chỉ server (đúng địa chỉ người quản trị đang dùng để mở Console),
// mã đăng ký và ngôn ngữ. Chỉ Super admin tải được vì file chứa mã đăng ký.
func (s *Server) hAgentDownload(w http.ResponseWriter, r *http.Request, me *Account) {
	name := "mam-agent.exe"
	if r.URL.Query().Get("os") == "windows32" {
		name = "mam-agent-x86.exe"
	}
	out, code, msg := s.bundledAgent(r, name, r.URL.Query().Get("lang"))
	if code != 200 {
		jerr(w, code, msg)
		return
	}
	s.mu.RLock()
	key := s.st.Settings.EnrollKey
	s.mu.RUnlock()
	s.audit(AuditEntry{User: me.User, Name: me.Name, Role: me.Role, IP: clientIP(r), Kind: "Máy", Msg: "Tải agent đã cài sẵn địa chỉ server", Sens: key != ""})
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="MAM-Agent.exe"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(out)
}

// bundledAgent đọc file agent (name) và gắn địa chỉ server (đúng địa chỉ người quản trị đang dùng để mở Console),
// mã đăng ký, ngôn ngữ. Lỗi thì trả mã HTTP + lời báo (tiếng Việt, Console tự dịch).
func (s *Server) bundledAgent(r *http.Request, name, lang string) ([]byte, int, string) {
	url, msg := consoleURL(r)
	if url == "" {
		return nil, 400, msg
	}
	p := s.findFile(name)
	if p == "" {
		return nil, 404, "Server chưa có file " + name + " để đóng gói (đặt cạnh mam-server)"
	}
	exe, err := os.ReadFile(p)
	if err != nil {
		return nil, 500, "Không đọc được " + name
	}
	if lang != "en" && lang != "zh" {
		lang = "vi"
	}
	s.mu.RLock()
	key := s.st.Settings.EnrollKey
	s.mu.RUnlock()
	return bundleAgent(exe, url, key, lang), 200, ""
}

// consoleURL: địa chỉ server mà người quản trị đang dùng để mở Console (máy khác trong LAN dùng được địa chỉ này).
// Mở bằng localhost thì trả "" kèm lời nhắc.
func consoleURL(r *http.Request) (string, string) {
	host := r.Host
	hn, _, err := net.SplitHostPort(host)
	if err != nil {
		hn = strings.Trim(host, "[]")
	}
	if ip := net.ParseIP(hn); hn == "localhost" || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified())) || !hostRe.MatchString(host) {
		return "", "Hãy mở Console bằng địa chỉ IP của server trong mạng LAN (không dùng localhost) rồi tải lại"
	}
	if r.TLS != nil {
		return "https://" + host, ""
	}
	return "http://" + host, ""
}
