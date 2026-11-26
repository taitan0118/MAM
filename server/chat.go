package main

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

// Chat hai chiều giữa kỹ thuật viên (Console) và người ngồi máy (cửa sổ MAM).
// Tin của Console đi xuống máy bằng lệnh "message" sẵn có; tin của máy đi lên qua /api/agent/reply.
type ChatLine struct {
	ID      string    `json:"id"` // để thả cảm xúc / thu hồi / sửa đúng tin; bản cũ chưa có thì vá lúc khởi động
	Machine string    `json:"machine"`
	By      string    `json:"by"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
	ByID    string    `json:"byId,omitempty"` // id tài khoản gửi (để lấy ảnh đại diện)
	Client  bool      `json:"client"`         // true: do người ngồi máy gửi
	Seen    bool      `json:"seen"`           // admin đã đọc (chỉ có nghĩa với tin của máy)
	Cam     string    `json:"cam,omitempty"`  // cảm xúc bên Console thả
	CamMay  string    `json:"camMay,omitempty"`
	Thu     bool      `json:"thu,omitempty"` // đã thu hồi: xóa hẳn nội dung, chỉ còn dòng "đã thu hồi"
	Sua     bool      `json:"sua,omitempty"` // đã sửa ít nhất một lần
	// Trả lời một tin cũ. Chép sẵn tên và nội dung tin được trả lời thay vì tra ngược theo TraID:
	// cửa sổ MAM chỉ giữ 100 tin gần nhất, tra ngược là trả lời tin cũ sẽ hiện trống trơn.
	// shortcut: tin gốc sửa sau đó thì phần trích dẫn vẫn là chữ cũ; nâng cấp khi nào thấy vướng.
	TraID   string `json:"traId,omitempty"`
	TraBy   string `json:"traBy,omitempty"`
	TraText string `json:"traText,omitempty"`
}

// camCho: chỉ nhận đúng mấy biểu tượng này. Cảm xúc đi thẳng xuống máy rồi hiện lên màn hình người
// khác, nhận chữ tùy ý là mở cửa cho người ta nhét gì vào cũng được.
var camCho = map[string]bool{"👍": true, "❤️": true, "😂": true, "😮": true, "😢": true, "😡": true}

// addChat: gọi khi đang giữ s.mu. Trả về dòng vừa thêm để nơi gọi lấy ID gửi xuống máy.
func (s *Server) addChat(machine, by, text string, client bool, byID ...string) *ChatLine {
	l := &ChatLine{ID: rid(8), Machine: machine, By: by, Text: text, At: time.Now(), Client: client, Seen: !client}
	if len(byID) > 0 {
		l.ByID = byID[0]
	}
	s.st.Chat = append(s.st.Chat, l)
	chatBump()
	if len(s.st.Chat) > 3000 {
		s.st.Chat = s.st.Chat[len(s.st.Chat)-2000:]
	}
	return l
}

// vaIDChat: tin nhắn lưu từ bản ≤ 1.1.107 chưa có ID nên không thả cảm xúc hay thu hồi được.
// Chạy một lần lúc khởi động, khi đang giữ s.mu. Trả về số dòng đã vá.
func (s *Server) vaIDChat() int {
	n := 0
	for _, c := range s.st.Chat {
		if c.ID == "" {
			c.ID, n = rid(8), n+1
		}
	}
	return n
}

// chatTin: Console thả cảm xúc / thu hồi / sửa một tin. Chỉ sửa và thu hồi được tin của CHÍNH
// Console gửi — tin của người ngồi máy là lời của họ, admin không được sửa miệng người khác.
func (s *Server) chatTin(w http.ResponseWriter, r *http.Request, me *Account, machine string) {
	var in struct{ ID, Lam, Cam, Text string }
	if readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	text := strings.TrimSpace(in.Text)
	if in.Lam == "sua" && (len([]rune(text)) < 1 || len([]rune(text)) > 300) {
		jerr(w, 400, "Nội dung 1 đến 300 ký tự")
		return
	}
	if in.Lam == "cam" && in.Cam != "" && !camCho[in.Cam] {
		jerr(w, 400, "Cảm xúc không hợp lệ")
		return
	}

	s.mu.Lock()
	m := s.machineByID(machine)
	if m == nil || !canControl(me, m) {
		s.mu.Unlock()
		jerr(w, 404, "Không có máy này")
		return
	}
	var l *ChatLine
	for _, c := range s.st.Chat {
		if c.Machine == machine && c.ID == in.ID {
			l = c
		}
	}
	if l == nil {
		s.mu.Unlock()
		jerr(w, 404, "Không có tin nhắn này")
		return
	}
	args := map[string]string{"mid": l.ID, "lam": in.Lam}
	switch in.Lam {
	case "cam":
		l.Cam, args["cam"] = in.Cam, in.Cam
	case "thu", "sua":
		if l.Client {
			s.mu.Unlock()
			jerr(w, 403, "Đây là tin của người ngồi máy, không sửa hay thu hồi được")
			return
		}
		if l.Thu {
			s.mu.Unlock()
			jerr(w, 400, "Tin này đã thu hồi rồi")
			return
		}
		if in.Lam == "thu" {
			l.Thu, l.Text, l.Sua = true, "", false
		} else {
			l.Text, l.Sua, args["text"] = text, true, text
		}
	default:
		s.mu.Unlock()
		jerr(w, 400, "Không hiểu muốn làm gì với tin này")
		return
	}
	// Gửi thẳng vào hàng lệnh, KHÔNG qua issue: issue bỏ qua máy đang offline, mà thu hồi thì phải
	// tới nơi kể cả máy đang tắt — lệnh nằm chờ, máy bật lên báo cáo là nhận được ngay.
	s.st.Commands = append(s.st.Commands, &Command{ID: rid(6), Machine: machine, Type: "chatact", Args: args,
		By: me.User, Created: time.Now(), Status: "pending"})
	if len(s.st.Commands) > 3000 {
		s.st.Commands = s.st.Commands[len(s.st.Commands)-2000:]
	}
	chatBump()
	s.save()
	s.mu.Unlock()
	wake(machine)
	jok(w, map[string]bool{"ok": true})
}

// GET /api/chat[?unread=1]            danh sách cuộc trò chuyện
// GET /api/chat/{id}                  tin của một máy (đánh dấu đã đọc)
// POST /api/chat/{id} {text}          gửi tin xuống máy
func (s *Server) hChat(w http.ResponseWriter, r *http.Request, me *Account) {
	if me.Role == "view" {
		jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/chat"), "/")
	if id == "" {
		s.chatList(w, r, me)
		return
	}
	lam := ""
	if i := strings.Index(id, "/"); i >= 0 {
		id, lam = id[:i], id[i+1:]
	}
	if lam == "tin" { // POST /api/chat/{máy}/tin: thả cảm xúc, thu hồi, sửa
		if r.Method != "POST" {
			jerr(w, 405, "Phương thức không hợp lệ")
			return
		}
		s.chatTin(w, r, me, id)
		return
	}
	switch r.Method {
	case "GET":
		s.mu.Lock()
		m := s.machineByID(id)
		if m == nil || !canControl(me, m) {
			s.mu.Unlock()
			jerr(w, 404, "Không có máy này")
			return
		}
		out, changed := []*ChatLine{}, false
		for _, c := range s.st.Chat {
			if c.Machine != id {
				continue
			}
			if c.Client && !c.Seen {
				c.Seen, changed = true, true
			}
			out = append(out, c)
		}
		if len(out) > 200 {
			out = out[len(out)-200:]
		}
		if changed {
			s.save()
		}
		online := s.isOnline(m)
		name := m.Name
		s.mu.Unlock()
		jok(w, map[string]interface{}{"id": id, "name": name, "online": online, "lines": out, "av": avatarVer("m", id)})
	case "POST":
		var in struct{ Text, TraID string }
		if readJSON(r, &in) != nil {
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
		t := strings.TrimSpace(in.Text)
		args := map[string]string{"text": t}
		if in.TraID != "" {
			s.mu.RLock()
			for _, c := range s.st.Chat {
				if c.Machine == id && c.ID == in.TraID && !c.Thu {
					args["traId"], args["traBy"], args["traText"] = c.ID, c.By, clip(c.Text, 120)
				}
			}
			s.mu.RUnlock()
		}
		if _, code, msg := s.issue(me, r, []string{id}, "message", args); code != 0 {
			jerr(w, code, msg)
			return
		}
		jok(w, map[string]bool{"ok": true})
	default:
		jerr(w, 405, "Phương thức không hợp lệ")
	}
}

func (s *Server) chatList(w http.ResponseWriter, r *http.Request, me *Account) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	type conv struct {
		ID     string    `json:"id"`
		Name   string    `json:"name"`
		Online bool      `json:"online"`
		Last   string    `json:"last"`
		LastAt time.Time `json:"lastAt"`
		Mine   bool      `json:"mine"` // tin cuối do Console gửi
		Unread int       `json:"unread"`
		Av     int64     `json:"av"`
	}
	by := map[string]*conv{}
	total := 0
	for _, c := range s.st.Chat {
		m := s.machineByID(c.Machine)
		if m == nil || !canControl(me, m) {
			continue
		}
		v := by[c.Machine]
		if v == nil {
			v = &conv{ID: m.ID, Name: m.Name, Online: s.isOnline(m), Av: avatarVer("m", m.ID)}
			by[c.Machine] = v
		}
		v.Last, v.LastAt, v.Mine = clip(c.Text, 80), c.At, !c.Client
		if c.Client && !c.Seen {
			v.Unread++
			total++
		}
	}
	if r.URL.Query().Get("unread") == "1" {
		jok(w, map[string]int{"unread": total})
		return
	}
	out := []*conv{}
	for _, v := range by {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastAt.After(out[j].LastAt) })
	jok(w, map[string]interface{}{"convs": out, "unread": total})
}
