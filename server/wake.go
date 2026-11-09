package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Tin nhắn/lệnh tới ngay thay vì đợi lượt báo cáo kế tiếp (5 giây):
//   - agent luôn giữ sẵn một yêu cầu /api/agent/wait; có lệnh mới cho máy đó thì server trả lời ngay,
//     agent báo cáo liền và nhận lệnh;
//   - Console đang mở trang Chat giữ một yêu cầu /api/chat-wait; có tin mới thì trả lời ngay.
//
// Không có gì thì sau waitMax trả lời rỗng và bên kia hỏi lại (nhỏ hơn WriteTimeout 30 giây của server).
const waitMax = 20 * time.Second

var wakeMu sync.Mutex
var wakeCh = map[string]chan struct{}{} // theo ID máy

// wake báo cho agent của máy id đang đợi (nếu có). Gọi được khi đang giữ s.mu.
func wake(id string) {
	wakeMu.Lock()
	if c := wakeCh[id]; c != nil {
		close(c)
		delete(wakeCh, id)
	}
	wakeMu.Unlock()
}

// GET /api/agent/wait (Bearer token của máy)
func (s *Server) hAgentWait(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	m := s.agentMachine(r)
	pending := false
	if m != nil {
		for _, c := range s.st.Commands { // có việc từ trước khi bắt đầu đợi: trả lời ngay
			if c.Machine == m.ID && c.Status == "pending" {
				pending = true
				break
			}
		}
	}
	s.mu.RUnlock()
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	if pending {
		jok(w, map[string]bool{"wake": true})
		return
	}
	wakeMu.Lock()
	c := wakeCh[m.ID]
	if c == nil {
		c = make(chan struct{})
		wakeCh[m.ID] = c
	}
	wakeMu.Unlock()
	t := time.NewTimer(waitMax)
	defer t.Stop()
	select {
	case <-c:
		jok(w, map[string]bool{"wake": true})
	case <-t.C:
		jok(w, map[string]bool{"wake": false})
	case <-r.Context().Done():
	}
}

// ---------- Console: đợi tin chat mới ----------

var chatMu sync.Mutex
var chatVer int64
var chatCh = make(chan struct{})

// chatBump: có tin chat mới (hai chiều).
func chatBump() {
	chatMu.Lock()
	chatVer++
	close(chatCh)
	chatCh = make(chan struct{})
	chatMu.Unlock()
}

// GET /api/chat-wait?v=<số phiên bản Console đang có>: trả lời ngay khi khác, không thì đợi tới khi có tin mới.
func (s *Server) hChatWait(w http.ResponseWriter, r *http.Request, me *Account) {
	v, err := strconv.ParseInt(r.URL.Query().Get("v"), 10, 64)
	chatMu.Lock()
	cur, c := chatVer, chatCh
	chatMu.Unlock()
	if err == nil && v == cur {
		t := time.NewTimer(waitMax)
		defer t.Stop()
		select {
		case <-c:
		case <-t.C:
		case <-r.Context().Done():
			return
		}
		chatMu.Lock()
		cur = chatVer
		chatMu.Unlock()
	}
	jok(w, map[string]int64{"v": cur})
}
