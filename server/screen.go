package main

// Xem/điều khiển màn hình: PHẢI được người ngồi máy bấm Đồng ý mỗi lần, có thanh báo và nút Dừng
// ngay trên máy đó, tự hết hạn khi không ai thao tác, và mọi phiên đều ghi Nhật ký thao tác.
import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type InputEv struct {
	Type   string  `json:"type"` // move | down | up | wheel | key | kdown | kup | text
	X      float64 `json:"x"`    // 0..1, tỉ lệ theo chiều rộng màn hình
	Y      float64 `json:"y"`    // 0..1, tỉ lệ theo chiều cao màn hình
	Button string  `json:"button"`
	Delta  float64 `json:"delta"`
	Key    string  `json:"key"`
	Code   string  `json:"code"` // kdown | kup: KeyboardEvent.code
	Text   string  `json:"text"` // text: chuỗi gõ vào máy
}

type ChatMsg struct {
	By   string    `json:"by"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
	Mine bool      `json:"mine"` // true = admin gửi, false = người ngồi máy trả lời
}

// screenUp: một gói cập nhật (danh sách vùng đã nén JPEG, định dạng do agent và Console tự hiểu, server chỉ chuyển tiếp).
type screenUp struct {
	Seq  int
	Data []byte
}

type ScreenSession struct {
	ID        string
	Machine   string
	ByUser    string
	ByName    string
	PreAuth   bool   // đã nhập đúng mật khẩu điều khiển từ xa: agent tự đồng ý, không hỏi người ngồi máy
	Status    string // pending | active | denied | stopped | expired
	Reason    string // lý do kết thúc, hiện cho admin
	Created   time.Time
	Term      time.Time // lúc chuyển sang trạng thái kết thúc (để dọn sau một khoảng)
	LastAdmin time.Time // lần cuối admin xem khung hình hoặc gửi thao tác
	LastInput time.Time // lần cuối có thao tác chuột/phím thật
	Diag      string    // lý do chưa có hình do agent báo
	Key       []byte    // gói hình đầy đủ gần nhất (đủ cả màn hình)
	KeySeq    int
	Ups       []screenUp // các gói cập nhật từng vùng sau Key
	UpBytes   int
	NeedKey   bool // bảo agent gửi lại hình đầy đủ
	Seq       int
	W, H      int
	Input     []InputEv
	Chat      []ChatMsg // tin nhắn qua lại trong lúc phiên này đang mở
	ChatSent  int       // số tin trong Chat đã gửi cho agent (tránh gửi lại)
}

func (s *Server) screenByID(id string) *ScreenSession { return s.screens[id] }

// sweepScreens dọn phiên quá hạn. Gọi khi đang giữ s.mu, trong watchdog.
func (s *Server) sweepScreens(now time.Time) {
	for id, sc := range s.screens {
		switch sc.Status {
		case "pending":
			if now.Sub(sc.Created) > time.Minute {
				s.endScreen(sc, "expired", "Không ai phản hồi hộp thoại xin phép")
			}
		case "active":
			if now.Sub(sc.LastAdmin) > 20*time.Second {
				s.endScreen(sc, "expired", "Quản trị viên đã đóng cửa sổ xem")
			} else if now.Sub(sc.LastInput) > 3*time.Minute {
				s.endScreen(sc, "expired", "Hết thời gian không thao tác")
			}
		default:
			if now.Sub(sc.Term) > 30*time.Second {
				delete(s.screens, id)
			}
		}
	}
}

// endScreen chuyển phiên sang trạng thái kết thúc và ghi nhật ký. Gọi khi đang giữ s.mu.
func (s *Server) endScreen(sc *ScreenSession, status, reason string) {
	if sc.Status != "pending" && sc.Status != "active" {
		return
	}
	wasActive := sc.Status == "active"
	sc.Status, sc.Reason, sc.Term, sc.Key, sc.Ups = status, reason, time.Now(), nil, nil
	m := s.machineByID(sc.Machine)
	name := sc.Machine
	if m != nil {
		name = m.Name
	}
	if wasActive || status == "denied" {
		s.audit(AuditEntry{User: sc.ByUser, Name: sc.ByName, Role: "", IP: "", Kind: "Xem màn hình",
			Msg: "Phiên xem màn hình " + name + " kết thúc: " + reason, Target: name, Sens: true})
	}
}

// ---------- phía Console (admin) ----------

// /api/machines/{id}/screen[...] — điều phối trong machineExtra.
func (s *Server) screenExtra(w http.ResponseWriter, r *http.Request, me *Account, id string, parts []string) bool {
	if parts[0] != "screen" {
		return false
	}
	if !isLAN(r) { // xem/điều khiển màn hình chỉ trong mạng nội bộ, không qua Internet/tunnel
		jerr(w, 403, "Điều khiển màn hình chỉ dùng được trong mạng nội bộ")
		return true
	}
	if len(parts) == 1 && r.Method == "POST" {
		s.hScreenStart(w, r, me, id)
		return true
	}
	if len(parts) == 2 && r.Method == "GET" {
		s.hScreenStatus(w, r, me, id, parts[1])
		return true
	}
	if len(parts) == 3 && parts[2] == "frame" && r.Method == "GET" {
		s.hScreenFrame(w, r, me, id, parts[1])
		return true
	}
	if len(parts) == 3 && parts[2] == "input" && r.Method == "POST" {
		s.hScreenInput(w, r, me, id, parts[1])
		return true
	}
	if len(parts) == 3 && parts[2] == "stop" && r.Method == "POST" {
		s.hScreenStop(w, r, me, id, parts[1])
		return true
	}
	if len(parts) == 3 && parts[2] == "msg" && r.Method == "POST" {
		s.hScreenMsg(w, r, me, id, parts[1])
		return true
	}
	jerr(w, 404, "Không có phiên này")
	return true
}

func (s *Server) hScreenStart(w http.ResponseWriter, r *http.Request, me *Account, id string) {
	if me.Role == "view" {
		jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
		return
	}
	var in struct{ Password string }
	readJSON(r, &in)
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.machineByID(id)
	if m == nil {
		jerr(w, 404, "Không có máy này")
		return
	}
	if !canControl(me, m) {
		jerr(w, 403, "Máy này ngoài phạm vi nhóm của bạn")
		return
	}
	pre := false
	if s.st.RemoteHash != "" { // có đặt mật khẩu: bắt buộc nhập đúng, khóa 1 phút sau 5 lần sai
		key := "remote|" + me.User + "|" + clientIP(r)
		f := s.fails[key]
		if f != nil && time.Now().Before(f.until) {
			jerr(w, 429, "Sai quá nhiều lần, thử lại sau 1 phút")
			return
		}
		if subtle.ConstantTimeCompare([]byte(hashPw(in.Password, s.st.RemoteSalt, pbkdfIter)), []byte(s.st.RemoteHash)) != 1 {
			if f == nil {
				f = &failRec{}
				s.fails[key] = f
			}
			f.n++
			if f.n >= 5 {
				f.until, f.n = time.Now().Add(time.Minute), 0
			}
			s.audit(AuditEntry{User: me.User, Name: me.Name, Role: me.Role, IP: clientIP(r), Kind: "Xem màn hình", Msg: "Nhập sai mật khẩu điều khiển từ xa máy " + m.Name, Target: m.Name, Sens: true})
			jerr(w, 403, "Sai mật khẩu điều khiển từ xa")
			return
		}
		delete(s.fails, key)
		pre = true
	}
	if !s.isOnline(m) {
		jerr(w, 409, "Máy đang offline")
		return
	}
	for _, sc := range s.screens {
		if sc.Machine == id && (sc.Status == "pending" || sc.Status == "active") {
			jerr(w, 409, "Đã có phiên xem màn hình máy này đang diễn ra")
			return
		}
	}
	now := time.Now()
	sc := &ScreenSession{ID: rid(6), Machine: id, ByUser: me.User, ByName: me.Name, Status: "pending", Created: now, LastAdmin: now, LastInput: now, PreAuth: pre}
	wake(id)
	s.screens[sc.ID] = sc
	msg := "Gửi yêu cầu xem màn hình " + m.Name + ", đang chờ máy đó đồng ý"
	if pre {
		msg = "Nhập đúng mật khẩu điều khiển từ xa, vào điều khiển máy " + m.Name
	}
	s.audit(AuditEntry{User: me.User, Name: me.Name, Role: me.Role, IP: clientIP(r), Kind: "Xem màn hình", Msg: msg, Target: m.Name, Sens: true})
	jok(w, map[string]string{"id": sc.ID})
}

func (s *Server) screenFor(me *Account, id, sid string) *ScreenSession {
	m := s.machineByID(id)
	sc := s.screens[sid]
	if m == nil || sc == nil || sc.Machine != id || !canControl(me, m) {
		return nil
	}
	return sc
}

func (s *Server) hScreenStatus(w http.ResponseWriter, r *http.Request, me *Account, id, sid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc := s.screenFor(me, id, sid)
	if sc == nil {
		jerr(w, 404, "Không có phiên này")
		return
	}
	if sc.Status == "active" {
		sc.LastAdmin = time.Now()
	}
	jok(w, map[string]interface{}{"status": sc.Status, "reason": sc.Reason, "seq": sc.Seq, "w": sc.W, "h": sc.H, "chat": sc.Chat, "diag": sc.Diag, "agentVer": s.liveVer(id)})
}

// hScreenMsg: admin nhắn tin ngay trong lúc đang xem màn hình (hiện trong hộp thoại nhắn của modal
// xem màn hình, và máy nhận được qua kênh thao tác chuột/phím đang mở sẵn nên tới gần như ngay lập tức).
func (s *Server) hScreenMsg(w http.ResponseWriter, r *http.Request, me *Account, id, sid string) {
	if me.Role == "view" {
		jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
		return
	}
	var in struct{ Text string }
	if readJSON(r, &in) != nil {
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
	sc := s.screenFor(me, id, sid)
	if sc == nil || sc.Status != "active" {
		jerr(w, 409, "Phiên xem màn hình đã kết thúc")
		return
	}
	sc.Chat = append(sc.Chat, ChatMsg{By: me.Name, Text: t, At: time.Now(), Mine: true})
	if len(sc.Chat) > 200 {
		sc.Chat = sc.Chat[len(sc.Chat)-200:]
	}
	jok(w, map[string]bool{"ok": true})
}

// hScreenFrame: trả các gói hình mới hơn ?after=N (gói đầy đủ rồi các gói cập nhật vùng sau nó nếu viewer đã quá cũ).
// Chưa có gì mới thì chờ (tối đa ~3 giây) rồi trả 204, để viewer kéo liên tục mà không phải hỏi dồn.
// Mỗi gói tự mang độ dài ở đầu nên nối liền nhau được.
func (s *Server) hScreenFrame(w http.ResponseWriter, r *http.Request, me *Account, id, sid string) {
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	var parts [][]byte
	var seq int
	for i := 0; ; i++ {
		active := false
		parts = parts[:0]
		s.mu.Lock()
		sc := s.screenFor(me, id, sid)
		if sc != nil {
			active = sc.Status == "active"
			if active {
				sc.LastAdmin = time.Now() // đang kéo hình nghĩa là cửa sổ xem vẫn mở
			}
			seq = sc.Seq
			if sc.Key != nil {
				if after < sc.KeySeq {
					parts = append(parts, sc.Key)
					after = sc.KeySeq - 1 // sau Key lấy hết các gói cập nhật
				}
				for _, u := range sc.Ups {
					if u.Seq > after {
						parts = append(parts, u.Data)
					}
				}
			}
		}
		s.mu.Unlock()
		if sc == nil || (after == 0 && len(parts) == 0 && i == 0 && sc.Key == nil) {
			jerr(w, 404, "Chưa có khung hình")
			return
		}
		if len(parts) > 0 {
			break
		}
		if !active || i >= 375 || r.Context().Err() != nil {
			w.WriteHeader(204)
			return
		}
		time.Sleep(8 * time.Millisecond)
	}
	w.Header().Set("X-Seq", strconv.Itoa(seq))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	for _, p := range parts {
		w.Write(p)
	}
}

func (s *Server) hScreenInput(w http.ResponseWriter, r *http.Request, me *Account, id, sid string) {
	if me.Role == "view" {
		jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
		return
	}
	var in InputEv
	if readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	switch in.Type {
	case "move", "down", "up", "wheel", "key", "kdown", "kup", "text":
	default:
		jerr(w, 400, "Thao tác không hợp lệ")
		return
	}
	if len(in.Code) > 20 || len(in.Key) > 20 || len([]rune(in.Text)) > 500 {
		jerr(w, 400, "Thao tác quá dài")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sc := s.screenFor(me, id, sid)
	if sc == nil || sc.Status != "active" {
		jerr(w, 409, "Phiên xem màn hình đã kết thúc")
		return
	}
	now := time.Now()
	sc.LastAdmin, sc.LastInput = now, now
	if len(sc.Input) < 600 {
		sc.Input = append(sc.Input, in)
	}
	jok(w, map[string]bool{"ok": true})
}

func (s *Server) hScreenStop(w http.ResponseWriter, r *http.Request, me *Account, id, sid string) {
	s.mu.Lock()
	sc := s.screenFor(me, id, sid)
	if sc == nil {
		s.mu.Unlock()
		jerr(w, 404, "Không có phiên này")
		return
	}
	s.endScreen(sc, "stopped", "Quản trị viên đã dừng")
	s.mu.Unlock()
	jok(w, map[string]bool{"ok": true})
}

// ---------- phía agent (máy được xem) ----------

func (s *Server) hAgentScreenConsent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID    string
		Allow bool
	}
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	sc := s.screens[in.ID]
	if sc == nil || sc.Machine != m.ID || sc.Status != "pending" {
		jerr(w, 404, "Phiên đã hết hạn hoặc không tồn tại")
		return
	}
	if in.Allow {
		now := time.Now()
		sc.Status, sc.LastAdmin, sc.LastInput = "active", now, now
		s.audit(AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: clientIP(r), Kind: "Xem màn hình", Msg: "Máy " + m.Name + " đồng ý cho " + sc.ByName + " xem màn hình", Target: m.Name, Sens: true})
	} else {
		s.endScreen(sc, "denied", "Người ngồi máy từ chối")
	}
	jok(w, map[string]bool{"ok": true})
}

func (s *Server) hAgentScreenFrame(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   string
		Data string // base64 JPEG
		W, H int
		Key  bool // gói chứa cả màn hình
	}
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	sc := s.screens[in.ID]
	if sc == nil || sc.Machine != m.ID || sc.Status != "active" {
		jok(w, map[string]string{"status": "gone"})
		return
	}
	sc.W, sc.H, sc.Seq = in.W, in.H, sc.Seq+1
	if in.Key {
		sc.Key, sc.KeySeq, sc.Ups, sc.UpBytes, sc.NeedKey = raw, sc.Seq, nil, 0, false
	} else {
		sc.Ups = append(sc.Ups, screenUp{sc.Seq, raw})
		sc.UpBytes += len(raw)
		// viewer chậm quá hoặc chưa có hình đầy đủ: xin agent gửi lại hình đầy đủ để gói cũ được bỏ
		if sc.Key == nil || len(sc.Ups) > 80 || sc.UpBytes > 12<<20 {
			sc.NeedKey = true
		}
	}
	jok(w, map[string]interface{}{"status": "active", "key": sc.NeedKey})
}

// hAgentScreenPoll: agent hỏi còn phiên không và lấy các thao tác chuột/phím admin vừa gửi.
func (s *Server) hAgentScreenPoll(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	sc := s.screens[id]
	if sc == nil || sc.Machine != m.ID || sc.Status != "active" {
		jok(w, map[string]interface{}{"status": "gone"})
		return
	}
	sc.Diag = clip(strings.TrimSpace(r.URL.Query().Get("diag")), 200)
	in := sc.Input
	sc.Input = nil
	chat := sc.Chat[sc.ChatSent:]
	sc.ChatSent = len(sc.Chat)
	jok(w, map[string]interface{}{"status": "active", "input": in, "chat": chat})
}

func (s *Server) hAgentScreenEnded(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID, Reason string }
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	sc := s.screens[in.ID]
	if sc == nil || sc.Machine != m.ID {
		jok(w, map[string]bool{"ok": true})
		return
	}
	reason := in.Reason
	if reason == "" {
		reason = "Máy ngừng gửi hình"
	}
	s.endScreen(sc, "stopped", reason)
	jok(w, map[string]bool{"ok": true})
}

// screenPendingFor: phiên đang chờ máy m đồng ý, báo kèm trong /api/agent/report. Gọi khi đang giữ s.mu.
func (s *Server) screenPendingFor(mID string) map[string]interface{} {
	for _, sc := range s.screens {
		if sc.Machine == mID && sc.Status == "pending" {
			return map[string]interface{}{"id": sc.ID, "by": sc.ByName, "byUser": sc.ByUser, "auto": sc.PreAuth}
		}
	}
	return nil
}

// liveVer: phiên bản agent của máy (rỗng nếu chưa biết). Gọi khi đang giữ s.mu.
func (s *Server) liveVer(id string) string {
	if l := s.live[id]; l != nil {
		return l.Ver
	}
	return ""
}
