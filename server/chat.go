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
	Machine string    `json:"machine"`
	By      string    `json:"by"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
	ByID    string    `json:"byId,omitempty"` // id tài khoản gửi (để lấy ảnh đại diện)
	Client  bool      `json:"client"`         // true: do người ngồi máy gửi
	Seen    bool      `json:"seen"`           // admin đã đọc (chỉ có nghĩa với tin của máy)
}

// addChat: gọi khi đang giữ s.mu.
func (s *Server) addChat(machine, by, text string, client bool, byID ...string) {
	l := &ChatLine{Machine: machine, By: by, Text: text, At: time.Now(), Client: client, Seen: !client}
	if len(byID) > 0 {
		l.ByID = byID[0]
	}
	s.st.Chat = append(s.st.Chat, l)
	if len(s.st.Chat) > 3000 {
		s.st.Chat = s.st.Chat[len(s.st.Chat)-2000:]
	}
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
		var in struct{ Text string }
		if readJSON(r, &in) != nil {
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
		t := strings.TrimSpace(in.Text)
		if _, code, msg := s.issue(me, r, []string{id}, "message", map[string]string{"text": t}); code != 0 {
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
