package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// POST /api/purge {kinds, from, to, dry}: dọn dữ liệu cũ theo khoảng ngày (giờ máy chủ, gồm cả ngày cuối).
// Chỉ Super admin. Xóa thật thì sao lưu trước và ghi vào nhật ký. Chỉ xóa mục đã xong/đã đóng.
func (s *Server) hPurge(w http.ResponseWriter, r *http.Request, me *Account) {
	var in struct {
		Kinds    []string `json:"kinds"`
		From, To string
		Dry      bool `json:"dry"`
	}
	if r.Method != "POST" || readJSON(r, &in) != nil || len(in.Kinds) == 0 {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	day := func(v string) (time.Time, bool) {
		t, err := time.ParseInLocation("2006-01-02", v, time.Local)
		return t, err == nil
	}
	to, ok := day(in.To)
	if !ok {
		jerr(w, 400, "Chọn ngày kết thúc")
		return
	}
	to = to.AddDate(0, 0, 1) // loại trừ
	var from time.Time
	if in.From != "" {
		if from, ok = day(in.From); !ok || !from.Before(to) {
			jerr(w, 400, "Khoảng ngày không hợp lệ")
			return
		}
	}
	has := func(t time.Time) bool { return !t.Before(from) && t.Before(to) }
	want := map[string]bool{}
	for _, k := range in.Kinds {
		switch k {
		case "requests", "alerts", "chat", "commands", "audit":
			want[k] = true
		default:
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
	}
	if want["audit"] && to.After(time.Now().AddDate(0, 0, -30)) {
		jerr(w, 400, "Nhật ký thao tác chỉ xóa được mục cũ hơn 30 ngày")
		return
	}
	if !in.Dry {
		s.backup()
	}
	n := map[string]int{}
	s.mu.Lock()
	if want["requests"] {
		keep := s.st.Requests[:0:0]
		for _, q := range s.st.Requests {
			if q.Status == "done" && has(q.Created) {
				n["requests"]++
			} else {
				keep = append(keep, q)
			}
		}
		if !in.Dry {
			s.st.Requests = keep
		}
	}
	if want["alerts"] {
		keep := s.st.Alerts[:0:0]
		for _, a := range s.st.Alerts {
			if a.Status == "closed" && has(a.Opened) {
				n["alerts"]++
			} else {
				keep = append(keep, a)
			}
		}
		if !in.Dry {
			s.st.Alerts = keep
		}
	}
	if want["chat"] {
		keep := s.st.Chat[:0:0]
		for _, c := range s.st.Chat {
			if has(c.At) {
				n["chat"]++
			} else {
				keep = append(keep, c)
			}
		}
		if !in.Dry {
			s.st.Chat = keep
		}
	}
	if want["commands"] {
		keep := s.st.Commands[:0:0]
		for _, c := range s.st.Commands {
			if (c.Status == "done" || c.Status == "failed") && has(c.Created) {
				n["commands"]++
			} else {
				keep = append(keep, c)
			}
		}
		if !in.Dry {
			s.st.Commands = keep
		}
	}
	if !in.Dry {
		s.save()
	}
	s.mu.Unlock()
	if want["audit"] {
		n["audit"] = s.purgeAudit(has, in.Dry)
	}
	if !in.Dry {
		tot := 0
		for _, v := range n {
			tot += v
		}
		s.auditBy(me, r, "Cài đặt", "Dọn dữ liệu "+in.From+" → "+in.To+": xóa "+strconv.Itoa(tot)+" mục", "", true)
	}
	jok(w, map[string]interface{}{"counts": n, "dry": in.Dry})
}

func (s *Server) purgeAudit(has func(time.Time) bool, dry bool) int {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	p := filepath.Join(s.dir, "audit.jsonl")
	b, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	var keep bytes.Buffer
	n := 0
	for _, ln := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var e AuditEntry
		if json.Unmarshal(ln, &e) == nil && has(e.T) {
			n++
			continue
		}
		keep.Write(ln)
		keep.WriteByte('\n')
	}
	if !dry && n > 0 {
		tmp := p + ".tmp"
		if os.WriteFile(tmp, keep.Bytes(), 0o600) == nil {
			os.Rename(tmp, p)
		}
	}
	return n
}
