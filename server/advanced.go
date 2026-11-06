package main

import (
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Lệnh theo yêu cầu (chỉ đọc thông tin hoặc tắt một tiến trình theo PID).
var infoCmd = map[string]bool{"procs": true, "kill": true, "smart": true}

var reNum = regexp.MustCompile(`^\d{1,10}$`)

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// cleanArgs kiểm tra và làm sạch tham số của từng loại lệnh.
func cleanArgs(typ string, a map[string]string) (map[string]string, string) {
	g := func(k string, n int) string { return strings.TrimSpace(clip(a[k], n)) }
	out := map[string]string{}
	switch typ {
	case "procs", "smart":
		return nil, ""
	case "kill":
		out["pid"], out["name"] = g("pid", 10), g("name", 80)
		if !reNum.MatchString(out["pid"]) || out["pid"] == "0" || out["pid"] == "4" {
			return nil, "Mã tiến trình không hợp lệ"
		}
	}
	return out, ""
}

// ---------- dữ liệu lớn của lệnh ----------

type Blob struct {
	Machine string
	Name    string
	Mime    string
	Data    []byte
	Exp     time.Time
}

const blobCap = 120 << 20 // tổng RAM tối đa cho dữ liệu lớn

func (s *Server) putBlob(id string, b *Blob) bool {
	total := len(b.Data)
	for _, x := range s.blobs {
		total += len(x.Data)
	}
	if total > blobCap {
		return false
	}
	if b.Exp.IsZero() {
		b.Exp = time.Now().Add(10 * time.Minute)
	}
	s.blobs[id] = b
	return true
}

// cmdByID tìm lệnh. Gọi khi đang giữ s.mu.
func (s *Server) cmdByID(id string) *Command {
	for i := len(s.st.Commands) - 1; i >= 0; i-- {
		if s.st.Commands[i].ID == id {
			return s.st.Commands[i]
		}
	}
	return nil
}

// sweep dọn dữ liệu lớn hết hạn (chạy trong watchdog, đang giữ s.mu).
func (s *Server) sweep(now time.Time) {
	for k, b := range s.blobs {
		if now.After(b.Exp) {
			delete(s.blobs, k)
		}
	}
}

// ---------- phía agent ----------

// hResultData: agent gửi kết quả có kèm dữ liệu lớn (base64).
func (s *Server) hResult(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     string
		OK     bool
		Output string
		Data   string // base64
		Mime   string
		Name   string
	}
	r.Body = http.MaxBytesReader(w, r.Body, 40<<20)
	if r.Method != "POST" || readJSONRaw(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	var raw []byte
	if in.Data != "" {
		var err error
		if raw, err = base64.StdEncoding.DecodeString(in.Data); err != nil {
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	if c := s.cmdByID(in.ID); c != nil && c.Machine == m.ID {
		c.Status = "done"
		if !in.OK {
			c.Status = "failed"
		}
		c.Result, c.Done = clip(in.Output, 400), time.Now()
		if raw != nil && !s.putBlob(c.ID, &Blob{Machine: m.ID, Name: clip(in.Name, 120), Mime: clip(in.Mime, 60), Data: raw}) {
			c.Status, c.Result = "failed", "Bộ nhớ server đầy, thử lại sau ít phút"
		}
		s.save()
	}
	jok(w, map[string]bool{"ok": true})
}

func readJSONRaw(r *http.Request, v interface{}) error { return jsonDecode(r.Body, v) }

// ---------- phía Console ----------

// /api/machines/{id}/cmd/{cid}[/data] — Console đọc kết quả lệnh procs/kill/smart.
func (s *Server) machineExtra(w http.ResponseWriter, r *http.Request, me *Account, id string, parts []string) bool {
	if parts[0] != "cmd" || len(parts) < 2 || r.Method != "GET" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.machineByID(id)
	c := s.cmdByID(parts[1])
	if m == nil || c == nil || c.Machine != m.ID || !canControl(me, m) {
		jerr(w, 404, "Không có lệnh này")
		return true
	}
	b := s.blobs[c.ID]
	if len(parts) == 3 && parts[2] == "data" {
		if b == nil {
			jerr(w, 404, "Dữ liệu đã hết hạn, hãy chạy lại")
			return true
		}
		ct := b.Mime
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b.Data)
		return true
	}
	d := map[string]interface{}{"id": c.ID, "type": c.Type, "status": c.Status, "result": c.Result, "hasData": b != nil}
	if b != nil {
		d["mime"], d["size"] = b.Mime, len(b.Data)
	}
	jok(w, d)
	return true
}
