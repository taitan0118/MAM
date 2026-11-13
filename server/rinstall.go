package main

import (
	"net"
	"os"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cài agent từ xa lên máy Windows trong mạng LAN (trang Phát hiện máy), cùng cách PsExec: chỉ cần cổng 445 và
// tài khoản quản trị của máy đích. Mật khẩu chỉ dùng trong lúc cài, không lưu. Phần làm thật nằm trong
// rinstall_windows.go (server phải chạy trên Windows).

type riJob struct {
	IP     string    `json:"ip"`
	State  string    `json:"state"`  // running | ok | fail
	Status string    `json:"status"` // câu tiếng Việt (Console dịch)
	By     string    `json:"by"`
	At     time.Time `json:"at"`
}

// riPack: những gì cần để cài: agent đã gắn cấu hình (64/32-bit) và agent PowerShell cho Windows 7/8.
type riPack struct {
	exe64, exe32, ps1 []byte
	url, key, lang    string
}

var riMu sync.Mutex
var riJobs = map[string]*riJob{}

func riSet(ip, state, status string) {
	riMu.Lock()
	if j := riJobs[ip]; j != nil {
		j.State, j.Status = state, status
	}
	riMu.Unlock()
}

// /api/remote-install (chỉ Super admin): GET danh sách lượt cài gần đây; POST {ips, user, password, lang} bắt đầu cài.
func (s *Server) hRemoteInstall(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "GET" {
		riMu.Lock()
		out := []riJob{}
		for _, j := range riJobs {
			out = append(out, *j)
		}
		riMu.Unlock()
		sort.Slice(out, func(i, j int) bool { return ipLess(out[i].IP, out[j].IP) })
		jok(w, out)
		return
	}
	var in struct {
		IPs            []string
		User, Password string
		Lang           string
	}
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	in.User = strings.TrimSpace(in.User)
	if in.User == "" || in.Password == "" || len(in.User) > 120 || len(in.Password) > 200 {
		jerr(w, 400, "Nhập tài khoản quản trị và mật khẩu của máy cần cài")
		return
	}
	if len(in.IPs) == 0 || len(in.IPs) > 50 {
		jerr(w, 400, "Chọn từ 1 đến 50 máy")
		return
	}
	for _, x := range in.IPs {
		ip := net.ParseIP(x).To4()
		if ip == nil || !privateNet(&net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}) {
			jerr(w, 400, "Chỉ cài được cho địa chỉ IP trong mạng nội bộ")
			return
		}
	}
	exe64, code, msg := s.bundledAgent(r, "mam-agent.exe", in.Lang)
	if code != 200 {
		jerr(w, code, msg)
		return
	}
	pk := riPack{exe64: exe64, lang: in.Lang}
	pk.exe32, _, _ = s.bundledAgent(r, "mam-agent-x86.exe", in.Lang) // thiếu bản 32-bit thì máy 32-bit báo lỗi riêng
	if p := s.findFile("MAM-Agent.ps1"); p != "" {                     // agent PowerShell cho Windows 7/8
		pk.ps1, _ = os.ReadFile(p)
	}
	pk.url, _ = consoleURL(r)
	s.mu.RLock()
	pk.key = s.st.Settings.EnrollKey
	s.mu.RUnlock()
	if pk.lang != "en" && pk.lang != "zh" {
		pk.lang = "vi"
	}
	started := []string{}
	riMu.Lock()
	for _, ip := range in.IPs {
		if j := riJobs[ip]; j != nil && j.State == "running" {
			continue
		}
		riJobs[ip] = &riJob{IP: ip, State: "running", Status: "Đang kết nối…", By: me.User, At: time.Now()}
		started = append(started, ip)
	}
	riMu.Unlock()
	s.auditBy(me, r, "Máy", "Cài agent từ xa lên "+strings.Join(started, ", ")+" (tài khoản "+in.User+")", strings.Join(started, ", "), true)
	who := *me
	cip := clientIP(r)
	sem := make(chan struct{}, 4) // tối đa 4 máy cùng lúc
	for _, ip := range started {
		go func(ip string) {
			sem <- struct{}{}
			defer func() { <-sem }()
			t0 := time.Now()
			err := remoteInstall(ip, in.User, in.Password, pk, func(st string) { riSet(ip, "running", st) })
			res := ""
			if err != nil {
				riSet(ip, "fail", err.Error())
				res = err.Error()
			} else {
				riSet(ip, "running", "Đang chờ máy báo về…")
				if s.waitEnroll(ip, t0, 90*time.Second) {
					res = "Đã cài, máy đã báo về"
					riSet(ip, "ok", res)
				} else {
					res = "Đã chạy cài đặt nhưng chưa thấy máy báo về (kiểm tra tường lửa hoặc địa chỉ server)"
					riSet(ip, "fail", res)
				}
			}
			remoteCleanup(ip)
			s.audit(AuditEntry{User: who.User, Name: who.Name, Role: who.Role, IP: cip, Kind: "Máy", Msg: "Cài agent từ xa lên " + ip + ": " + res, Target: ip})
		}(ip)
	}
	jok(w, map[string]interface{}{"started": started})
}

// waitEnroll chờ máy có IP này báo về server sau thời điểm since.
func (s *Server) waitEnroll(ip string, since time.Time, max time.Duration) bool {
	end := time.Now().Add(max)
	for time.Now().Before(end) {
		s.mu.RLock()
		for _, m := range s.st.Machines {
			if !m.Revoked && m.IP == ip && m.LastSeen.After(since) && (m.Registered.After(since) || s.live[m.ID] != nil && s.live[m.ID].At.After(since)) {
				s.mu.RUnlock()
				return true
			}
		}
		s.mu.RUnlock()
		time.Sleep(2 * time.Second)
	}
	return false
}
