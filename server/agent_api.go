package main

import (
	"net/http"
	"strings"
	"time"
)

type reportIn struct {
	CPU     float64                `json:"cpu"`
	RAM     float64                `json:"ram"`
	Temp    *float64               `json:"temp"`
	Uptime  int64                  `json:"uptime"`
	Disks   []Disk                 `json:"disks"`
	User    string                 `json:"user"`
	IP      string                 `json:"ip"`
	Ver     string                 `json:"ver"`
	Arch    string                 `json:"arch"`
	Hw      map[string]interface{} `json:"hw"`
	Threats []ThreatIn             `json:"threats"`
	Macs    []string               `json:"macs"`
	Events  []SysEvent             `json:"events"`
	Host    string                 `json:"host"`
}

func (s *Server) agentMachine(r *http.Request) *Machine {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil
	}
	m := s.tok[sha(strings.TrimPrefix(h, "Bearer "))]
	if m == nil || m.Revoked {
		return nil
	}
	return m
}

// agentRevoked: token có trong sổ nhưng đã bị thu hồi (khác với token lạ).
func (s *Server) agentRevoked(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	m := s.tok[sha(strings.TrimPrefix(h, "Bearer "))]
	return m != nil && m.Revoked
}

// Máy client tự đăng ký: không cần tài khoản, không ai phải duyệt.
func (s *Server) hEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jerr(w, 405, "Sai phương thức")
		return
	}
	var in struct {
		Name, OS, MAC, IP, Key string
		Hw                     map[string]interface{}
	}
	if readJSON(r, &in) != nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 60 {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if k := s.st.Settings.EnrollKey; k != "" && in.Key != k {
		jerr(w, 403, "Sai mã đăng ký")
		return
	}
	tok := rid(32)
	var m *Machine
	if in.MAC != "" {
		for _, x := range s.st.Machines {
			if !x.Revoked && x.MAC == in.MAC {
				m = x
				break
			}
		}
	}
	if m != nil { // cài lại agent trên cùng máy: cấp token mới, giữ nhóm và tên
		delete(s.tok, m.TokenHash)
	} else {
		m = &Machine{ID: rid(5), Registered: time.Now(), Name: strings.TrimSpace(in.Name)}
		s.st.Machines = append(s.st.Machines, m)
	}
	m.TokenHash = sha(tok)
	m.OS, m.IP, m.MAC, m.Hw, m.LastSeen = in.OS, in.IP, in.MAC, in.Hw, time.Now()
	s.tok[m.TokenHash] = m
	s.save()
	s.audit(AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: clientIP(r), Kind: "Máy", Msg: "Máy " + m.Name + " cài agent và tự đăng ký", Target: m.Name})
	jok(w, map[string]interface{}{"id": m.ID, "token": tok, "interval": 5})
}

func (s *Server) hReport(w http.ResponseWriter, r *http.Request) {
	var in reportIn
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	s.mu.Lock()
	m := s.agentMachine(r)
	if m == nil {
		rev := s.agentRevoked(r)
		s.mu.Unlock()
		if rev {
			jerr(w, 403, "revoked")
		} else {
			jerr(w, 401, "unknown")
		}
		return
	}
	now := time.Now()
	l := s.live[m.ID]
	if l == nil {
		l = &Live{}
		s.live[m.ID] = l
	}
	l.CPU, l.RAM, l.Temp, l.Uptime, l.Disks, l.User, l.Ver, l.At = clamp(in.CPU), clamp(in.RAM), in.Temp, in.Uptime, in.Disks, in.User, in.Ver, now
	if len(in.Macs) > 0 && len(in.Macs) <= 8 {
		m.MACs = in.Macs
	}
	if in.IP != "" {
		m.IP = in.IP
	}
	if in.Hw != nil {
		m.Hw = in.Hw
	}
	m.LastSeen = now
	persist := false
	if h := cut(in.Host, 63); h != "" && h != m.Host {
		// đổi tên máy trong Windows (System > About > Rename) thì tên trên MAM đổi theo; lần đầu chỉ ghi nhận
		if m.Host != "" && m.Name != h {
			old := m.Name
			m.Name = h
			persist = true
			s.audit(AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: clientIP(r), Kind: "Máy", Msg: "Máy đổi tên trong Windows: " + old + " thành " + h, Target: h})
		}
		m.Host = h
	}
	s.upMark(m.ID, now.Unix(), in.Uptime)  // không lưu ngay: 1000 máy bật cùng lúc sẽ nghẽn; watchdog lưu mỗi phút
	defer evAppend(s.dir, m.ID, in.Events) // chạy sau khi đã nhả s.mu
	s.threatsIn(m, in.Threats)
	s.patchCheck(m)
	if now.Sub(l.lastHist) >= 30*time.Second {
		smp := Sample{T: now.Unix(), CPU: float32(l.CPU), RAM: float32(l.RAM), Temp: -1}
		if l.Temp != nil {
			smp.Temp = float32(*l.Temp)
		}
		l.Hist = append(l.Hist, smp)
		if len(l.Hist) > 480 {
			l.Hist = l.Hist[len(l.Hist)-480:]
		}
		l.lastHist = now
	}
	s.checkMachine(m, l)
	out := []map[string]interface{}{}
	for _, c := range s.st.Commands {
		if c.Machine == m.ID && c.Status == "pending" {
			c.Status = "sent"
			persist = true
			out = append(out, map[string]interface{}{"id": c.ID, "type": c.Type, "args": c.Args})
		}
	}
	if persist {
		s.save()
	}
	interval := 5
	pol := s.policyFor(m)
	scr := s.screenPendingFor(m.ID)
	dts := s.deviceTypes()
	cl := s.contactLabel()
	s.mu.Unlock()
	jok(w, map[string]interface{}{"commands": out, "name": m.Name, "interval": interval, "policy": pol, "update": s.updateFor(in.Ver, in.Arch), "screen": scr, "deviceTypes": dts, "contactLabel": cl})
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// Gỡ agent bằng mật khẩu gỡ do Super admin đặt trong Console: agent hỏi server trước khi tự xóa.
// Không dùng tài khoản quản trị để khỏi lộ tài khoản ra máy người dùng.
func (s *Server) hUninstall(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	key := "unins|" + clientIP(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	if f := s.fails[key]; f != nil && time.Now().Before(f.until) {
		jerr(w, 429, "Sai quá nhiều lần, thử lại sau 1 phút")
		return
	}
	if s.st.UninstHash == "" {
		jerr(w, 409, "Quản trị viên chưa đặt mật khẩu gỡ")
		return
	}
	if hashPw(in.Code, s.st.UninstSalt, pbkdfIter) != s.st.UninstHash {
		f := s.fails[key]
		if f == nil {
			f = &failRec{}
			s.fails[key] = f
		}
		f.n++
		left := 5 - f.n
		if f.n >= 5 {
			f.until, f.n = time.Now().Add(time.Minute), 0
		}
		s.audit(AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: clientIP(r), Kind: "Máy", Msg: "Nhập sai mật khẩu gỡ trên " + m.Name, Target: m.Name, Sens: true})
		if left <= 0 {
			jerr(w, 403, "Mật khẩu gỡ không đúng. Đã khóa 1 phút")
		} else {
			jerr(w, 403, "Mật khẩu gỡ không đúng. Còn "+itoa(left)+" lần thử")
		}
		return
	}
	delete(s.fails, key)
	m.Revoked = true
	delete(s.live, m.ID)
	s.save()
	s.audit(AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: clientIP(r), Kind: "Máy", Msg: "Gỡ agent khỏi máy " + m.Name + " bằng mật khẩu gỡ", Target: m.Name, Sens: true})
	jok(w, map[string]bool{"ok": true})
}

// Người ngồi máy trả lời tin nhắn của admin: ghi vào nhật ký để admin xem ở Console.
func (s *Server) hReply(w http.ResponseWriter, r *http.Request) {
	var in struct{ Text string }
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	t := strings.TrimSpace(in.Text)
	if t == "" || len([]rune(t)) > 300 {
		jerr(w, 400, "Nội dung 1 đến 300 ký tự")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	s.addChat(m.ID, m.Name, t, true)
	s.save()
	s.audit(AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: clientIP(r), Kind: "Tin nhắn", Msg: "Máy " + m.Name + " trả lời: " + t, Target: m.Name})
	for _, sc := range s.screens {
		if sc.Machine == m.ID && sc.Status == "active" {
			sc.Chat = append(sc.Chat, ChatMsg{By: m.Name, Text: t, At: time.Now(), Mine: false})
			if len(sc.Chat) > 200 {
				sc.Chat = sc.Chat[len(sc.Chat)-200:]
			}
			break
		}
	}
	jok(w, map[string]bool{"ok": true})
}

// hAgentRename: người dùng đổi tên máy ngay trên client, cập nhật lên server (cùng giới hạn như admin đổi).
func (s *Server) hAgentRename(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if n := len([]rune(in.Name)); n < 1 || n > 40 {
		jerr(w, 400, "Tên máy 1 đến 40 ký tự")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	if old := m.Name; old != in.Name {
		m.Name = in.Name
		s.save()
		s.audit(AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: clientIP(r), Kind: "Máy", Msg: "Người dùng đổi tên máy " + old + " thành " + in.Name, Target: in.Name})
	}
	jok(w, map[string]bool{"ok": true})
}
