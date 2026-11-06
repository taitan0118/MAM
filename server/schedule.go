package main

import (
	"net/http"
	"strings"
	"time"
)

// Schedule là lệnh hẹn giờ chạy lặp lại cho một nhóm máy (hoặc tất cả). Chạy với quyền của người tạo.
type Schedule struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Args    map[string]string `json:"args,omitempty"`
	Group   string            `json:"group"` // "" = tất cả máy, "__none" = máy chưa phân nhóm, còn lại là tên nhóm
	Time    string            `json:"time"`  // HH:MM, giờ của server
	Days    []int             `json:"days"`  // 0=Chủ nhật..6=Thứ bảy; rỗng = mỗi ngày
	By      string            `json:"by"`    // tên đăng nhập người tạo
	Enabled bool              `json:"enabled"`
	LastRun string            `json:"lastRun"` // yyyy-mm-dd của lần chạy gần nhất
	Result  string            `json:"result"`
}

var schedTypes = map[string]bool{"lock": true, "restart": true, "shutdown": true, "wol": true, "message": true, "clean": true, "freeram": true}

func (s *Server) schedLoop() {
	for range time.NewTicker(20 * time.Second).C {
		s.runSchedules(time.Now())
	}
}

func hasDay(d []int, w int) bool {
	if len(d) == 0 {
		return true
	}
	for _, x := range d {
		if x == w {
			return true
		}
	}
	return false
}

// runSchedules chạy các lịch đến giờ. Mỗi lịch chạy tối đa một lần mỗi ngày, đúng phút đã hẹn.
func (s *Server) runSchedules(now time.Time) {
	type job struct {
		sc  Schedule
		me  *Account
		ids []string
	}
	var jobs []job
	today, hm := now.Format("2006-01-02"), now.Format("15:04")
	s.mu.Lock()
	for _, sc := range s.st.Schedules {
		if !sc.Enabled || sc.Time != hm || sc.LastRun == today || !hasDay(sc.Days, int(now.Weekday())) {
			continue
		}
		sc.LastRun = today
		var me *Account
		for _, a := range s.st.Accounts {
			if a.User == sc.By {
				me = a
			}
		}
		if me == nil || me.Role == "view" {
			sc.Result = "Bỏ qua: người tạo lịch không còn quyền"
			continue
		}
		ids := []string{}
		for _, m := range s.st.Machines {
			if !m.Revoked && (sc.Group == "" || m.Group == strings.TrimPrefix(sc.Group, "__none")) {
				ids = append(ids, m.ID)
			}
		}
		jobs = append(jobs, job{*sc, me, ids})
	}
	if len(jobs) > 0 {
		s.save()
	}
	s.mu.Unlock()
	for _, j := range jobs {
		res, code, msg := s.issue(j.me, &http.Request{RemoteAddr: "lịch hẹn giờ", Header: http.Header{}}, j.ids, j.sc.Type, j.sc.Args)
		out := msg
		if code == 0 {
			out = "Đã gửi"
			if r, ok := res["sent"].([]string); ok {
				out = "Đã gửi cho " + itoa(len(r)) + " máy"
			}
		}
		s.mu.Lock()
		for _, sc := range s.st.Schedules {
			if sc.ID == j.sc.ID {
				sc.Result = out
			}
		}
		s.save()
		s.mu.Unlock()
	}
}

func (s *Server) hSchedules(w http.ResponseWriter, r *http.Request, me *Account) {
	switch r.Method {
	case "GET":
		s.mu.RLock()
		defer s.mu.RUnlock()
		out := []*Schedule{} // [] chứ không phải null, nếu không Console lỗi khi vẽ danh sách
		for _, sc := range s.st.Schedules {
			if me.Role == "super" || len(me.Scope) == 0 || (sc.Group != "" && sc.Group != "__none" && inScope(me, sc.Group)) {
				out = append(out, sc)
			}
		}
		jok(w, out)
	case "POST":
		if me.Role == "view" {
			jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
			return
		}
		var in Schedule
		if readJSON(r, &in) != nil || !schedTypes[in.Type] {
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
		if _, err := time.Parse("15:04", in.Time); err != nil {
			jerr(w, 400, "Giờ phải dạng HH:MM")
			return
		}
		in.Name = strings.TrimSpace(clip(in.Name, 60))
		if in.Name == "" {
			in.Name = cmdLabel[in.Type]
		}
		if in.Type == "message" {
			t := strings.TrimSpace(in.Args["text"])
			if t == "" || len([]rune(t)) > 300 {
				jerr(w, 400, "Nội dung tin nhắn 1 đến 300 ký tự")
				return
			}
			in.Args = map[string]string{"text": t}
		} else {
			in.Args = nil
		}
		days := []int{}
		for _, d := range in.Days {
			if d >= 0 && d <= 6 {
				days = append(days, d)
			}
		}
		s.mu.Lock()
		if me.Role != "super" && len(me.Scope) > 0 && (in.Group == "" || in.Group == "__none" || !inScope(me, in.Group)) {
			s.mu.Unlock()
			jerr(w, 403, "Chọn một nhóm trong phạm vi của bạn")
			return
		}
		if in.Group != "" && in.Group != "__none" && !s.groupExists(in.Group) {
			s.mu.Unlock()
			jerr(w, 400, "Nhóm không tồn tại")
			return
		}
		if len(s.st.Schedules) >= 100 {
			s.mu.Unlock()
			jerr(w, 400, "Tối đa 100 lịch")
			return
		}
		sc := &Schedule{ID: rid(5), Name: in.Name, Type: in.Type, Args: in.Args, Group: in.Group, Time: in.Time, Days: days, By: me.User, Enabled: true}
		s.st.Schedules = append(s.st.Schedules, sc)
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Lịch hẹn giờ", "Tạo lịch "+sc.Name+" lúc "+sc.Time, sc.Name, sensitiveCmd[sc.Type])
		jok(w, sc)
	case "PUT", "DELETE":
		id := r.URL.Query().Get("id")
		s.mu.Lock()
		for i, sc := range s.st.Schedules {
			if sc.ID != id {
				continue
			}
			if me.Role != "super" && sc.By != me.User {
				s.mu.Unlock()
				jerr(w, 403, "Chỉ người tạo hoặc Super admin sửa lịch này")
				return
			}
			if r.Method == "DELETE" {
				s.st.Schedules = append(s.st.Schedules[:i], s.st.Schedules[i+1:]...)
			} else {
				sc.Enabled = !sc.Enabled
			}
			s.save()
			s.mu.Unlock()
			s.auditBy(me, r, "Lịch hẹn giờ", map[string]string{"PUT": "Bật/tắt lịch ", "DELETE": "Xóa lịch "}[r.Method]+sc.Name, sc.Name, false)
			jok(w, map[string]bool{"ok": true})
			return
		}
		s.mu.Unlock()
		jerr(w, 404, "Không có lịch này")
	default:
		jerr(w, 405, "Phương thức không hỗ trợ")
	}
}
