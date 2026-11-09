package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Lỗi của máy: agent Windows đọc nhật ký sự kiện (System, Application) mức Nghiêm trọng/Lỗi và gửi về.
// Mỗi máy một file events/<id>.jsonl, chỉ ghi thêm vào cuối nên không làm phình state.json;
// dọn mỗi ngày: bỏ mục cũ hơn 180 ngày, giữ tối đa evMax mục mới nhất.
const (
	evKeep = 180 * 24 * 3600
	evMax  = 5000
)

type SysEvent struct {
	ID  string `json:"id"`
	At  int64  `json:"at"`
	Log string `json:"log"`
	Src string `json:"src"`
	EID int    `json:"eid"`
	Lvl int    `json:"lvl"` // 1 nghiêm trọng, 2 lỗi
	Msg string `json:"msg"`
}

var evMu sync.Mutex // giữ khi đọc/ghi file events/; không bao giờ giữ cùng lúc chờ s.mu

func evFile(dir, id string) string { return filepath.Join(dir, "events", id+".jsonl") }

func cut(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n] + "…"
}
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

func evRead(dir, id string) []SysEvent {
	f, err := os.Open(evFile(dir, id))
	if err != nil {
		return nil
	}
	defer f.Close()
	out := []SysEvent{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e SysEvent
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.ID != "" {
			out = append(out, e)
		}
	}
	return out
}

// evAppend: kiểm tra dữ liệu agent gửi, bỏ mục trùng (agent gửi lại khi khởi động lại), ghi thêm vào cuối file.
func evAppend(dir, id string, in []SysEvent) {
	if len(in) == 0 {
		return
	}
	if len(in) > 500 {
		in = in[:500]
	}
	now := time.Now().Unix()
	evMu.Lock()
	defer evMu.Unlock()
	seen := map[string]bool{}
	for _, e := range evRead(dir, id) {
		seen[e.ID] = true
	}
	var b strings.Builder
	for _, e := range in {
		e.ID, e.Log, e.Src, e.Msg = cut(e.ID, 40), cut(e.Log, 60), cut(e.Src, 120), cut(e.Msg, 500)
		if e.ID == "" || seen[e.ID] || e.At < now-evKeep || e.At > now+86400 || (e.Lvl != 1 && e.Lvl != 2) {
			continue
		}
		seen[e.ID] = true
		j, _ := json.Marshal(e)
		b.Write(j)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return
	}
	os.MkdirAll(filepath.Join(dir, "events"), 0o700)
	f, err := os.OpenFile(evFile(dir, id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	f.WriteString(b.String())
	f.Close()
}

// evPrune: gọi mỗi ngày; viết lại file nào có mục quá hạn hoặc quá nhiều.
func evPrune(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "events", "*.jsonl"))
	old := time.Now().Unix() - evKeep
	for _, p := range files {
		id := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		evMu.Lock()
		all := evRead(dir, id)
		keep := all[:0:0]
		for _, e := range all {
			if e.At >= old {
				keep = append(keep, e)
			}
		}
		if len(keep) > evMax {
			sort.Slice(keep, func(i, j int) bool { return keep[i].At < keep[j].At })
			keep = keep[len(keep)-evMax:]
		}
		if len(keep) != len(all) {
			var b strings.Builder
			for _, e := range keep {
				j, _ := json.Marshal(e)
				b.Write(j)
				b.WriteByte('\n')
			}
			if os.WriteFile(p+".tmp", []byte(b.String()), 0o600) == nil {
				os.Rename(p+".tmp", p)
			}
		}
		evMu.Unlock()
	}
}

// GET /api/machines/<id>/events: lỗi Windows + cảnh báo MAM của máy trong 180 ngày, mới nhất trước.
func (s *Server) eventsExtra(w http.ResponseWriter, r *http.Request, me *Account, id string, parts []string) bool {
	if parts[0] != "events" || r.Method != "GET" {
		return false
	}
	s.mu.RLock()
	m := s.machineByID(id)
	if m == nil || m.Revoked || !canSee(me, m) {
		s.mu.RUnlock()
		jerr(w, 404, "Không có máy này")
		return true
	}
	dir, osName := s.dir, m.OS
	since := time.Now().Add(-evKeep * time.Second)
	al := []map[string]interface{}{}
	for _, a := range s.st.Alerts {
		if a.Machine == id && a.Opened.After(since) {
			al = append(al, map[string]interface{}{"at": a.Opened.Unix(), "kind": a.Kind, "sev": a.Sev, "msg": a.Msg})
		}
	}
	s.mu.RUnlock()
	evMu.Lock()
	ev := evRead(dir, id)
	evMu.Unlock()
	old := since.Unix()
	out := ev[:0]
	for _, e := range ev {
		if e.At >= old {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	jok(w, map[string]interface{}{"events": out, "alerts": al, "os": osName, "days": evKeep / 86400})
	return true
}
