package main

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ---------- yêu cầu hỗ trợ: phía agent ----------

func (s *Server) hAgentRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID         string
		Text       string
		Urgent     bool
		User       string
		Reporter   string
		Phone      string
		EmpID      string
		BadPC      string
		DeviceType string
	}
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	t := strings.TrimSpace(in.Text)
	if t == "" || len([]rune(t)) > 500 {
		jerr(w, 400, "Nội dung 1 đến 500 ký tự")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	now := time.Now()
	if in.ID != "" { // người ngồi máy viết thêm vào yêu cầu cũ
		for _, q := range s.st.Requests {
			if q.ID == in.ID && q.Machine == m.ID {
				q.Replies = append(q.Replies, Reply{By: m.Name, Text: t, At: now, Client: true})
				if q.Status == "done" {
					q.Status = "new"
				}
				q.Updated = now
				s.save()
				jok(w, map[string]string{"id": q.ID})
				return
			}
		}
		jerr(w, 404, "Không có yêu cầu này")
		return
	}
	open := 0
	for _, q := range s.st.Requests {
		if q.Machine == m.ID && q.Status != "done" {
			open++
		}
	}
	if open >= 5 {
		jerr(w, 429, "Máy này đã có 5 yêu cầu chưa xong, hãy chờ kỹ thuật viên xử lý")
		return
	}
	dt := ""
	for _, x := range s.deviceTypes() {
		if x == in.DeviceType {
			dt = x
		}
	}
	q := &Request{ID: rid(4), DeviceType: dt, Machine: m.ID, Name: m.Name, User: clip(in.User, 60),
		Reporter: clip(in.Reporter, 60), Phone: clip(in.Phone, 30), EmpID: clip(in.EmpID, 30), BadPC: clip(in.BadPC, 60),
		Text: t, Urgent: in.Urgent, Status: "new", Created: now, Updated: now}
	s.st.Requests = append(s.st.Requests, q)
	if len(s.st.Requests) > 2000 {
		s.st.Requests = s.st.Requests[len(s.st.Requests)-1500:]
	}
	s.save()
	s.audit(AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: clientIP(r), Kind: "Yêu cầu hỗ trợ", Msg: "Máy " + m.Name + " gửi yêu cầu hỗ trợ: " + clip(t, 80), Target: m.Name, Sens: in.Urgent})
	if in.Urgent {
		s.notify(s.st.Settings, "Yêu cầu hỗ trợ gấp: "+m.Name, t)
	}
	jok(w, map[string]string{"id": q.ID})
}

func (s *Server) hAgentRequests(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.agentMachine(r)
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	out := []*Request{}
	for i := len(s.st.Requests) - 1; i >= 0 && len(out) < 20; i-- {
		if q := s.st.Requests[i]; q.Machine == m.ID {
			out = append(out, q)
		}
	}
	jok(w, out)
}

// ---------- yêu cầu hỗ trợ: phía Console ----------

func (s *Server) visibleRequest(me *Account, q *Request) bool {
	if me.Role == "super" || len(me.Scope) == 0 {
		return true
	}
	m := s.machineByID(q.Machine)
	return m != nil && canSee(me, m)
}

func (s *Server) hRequests(w http.ResponseWriter, r *http.Request, me *Account) {
	st := r.URL.Query().Get("status")
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*Request{}
	for i := len(s.st.Requests) - 1; i >= 0 && len(out) < 300; i-- {
		q := s.st.Requests[i]
		if (st == "" || q.Status == st || (st == "open" && q.Status != "done")) && s.visibleRequest(me, q) {
			out = append(out, q)
		}
	}
	jok(w, out)
}

// /api/requests/{id}/reply  (POST {text})   /api/requests/{id}/status  (PUT {status})
func (s *Server) hRequest(w http.ResponseWriter, r *http.Request, me *Account) {
	if me.Role == "view" {
		jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/requests/"), "/")
	if len(parts) != 2 {
		jerr(w, 404, "Không có yêu cầu này")
		return
	}
	if r.Method == "GET" && parts[1] == "suggest" {
		s.suggest(w, me, parts[0])
		return
	}
	var in struct{ Text, Status, Solution string }
	if readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	s.mu.Lock()
	var q *Request
	for _, x := range s.st.Requests {
		if x.ID == parts[0] {
			q = x
		}
	}
	if q == nil || !s.visibleRequest(me, q) {
		s.mu.Unlock()
		jerr(w, 404, "Không có yêu cầu này")
		return
	}
	now := time.Now()
	msg := ""
	switch parts[1] {
	case "reply":
		t := strings.TrimSpace(in.Text)
		if t == "" || len([]rune(t)) > 500 {
			s.mu.Unlock()
			jerr(w, 400, "Nội dung 1 đến 500 ký tự")
			return
		}
		q.Replies = append(q.Replies, Reply{By: me.Name, Text: t, At: now})
		if q.Status == "new" {
			q.Status = "working"
		}
		msg = "Trả lời yêu cầu hỗ trợ của " + q.Name + ": " + clip(t, 80)
	case "status":
		if in.Status != "new" && in.Status != "working" && in.Status != "done" {
			s.mu.Unlock()
			jerr(w, 400, "Trạng thái không hợp lệ")
			return
		}
		sol := strings.TrimSpace(in.Solution)
		if in.Status == "done" {
			if sol == "" || len([]rune(sol)) > 500 {
				s.mu.Unlock()
				jerr(w, 400, "Nhập cách xử lý (1 đến 500 ký tự) trước khi chuyển sang Đã xong")
				return
			}
			q.Solution, q.SolvedBy = sol, me.Name
		}
		q.Status = in.Status
		msg = "Đổi trạng thái yêu cầu hỗ trợ của " + q.Name + " thành " + in.Status
		if sol != "" && in.Status == "done" {
			msg += " — cách xử lý: " + clip(sol, 80)
		}
	default:
		s.mu.Unlock()
		jerr(w, 404, "Không có yêu cầu này")
		return
	}
	q.Updated = now
	name := q.Name
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Yêu cầu hỗ trợ", msg, name, false)
	jok(w, map[string]bool{"ok": true})
}

// ---------- báo cáo ----------

// groupPred: g rỗng = mọi máy; "__none" = máy chưa phân nhóm; còn lại = đúng tên nhóm. Gọi khi đang giữ s.mu.
// Cảnh báo/yêu cầu của máy không còn trong danh sách (đã xóa) bị loại khi có lọc nhóm.
func (s *Server) groupPred(me *Account, g string) func(machineID string) bool {
	scoped := me.Role != "super" && len(me.Scope) > 0
	if (g == "" || g == "all") && !scoped {
		return func(string) bool { return true }
	}
	grp := map[string]string{}
	for _, m := range s.st.Machines {
		grp[m.ID] = m.Group
	}
	return func(id string) bool {
		x, ok := grp[id]
		if scoped && (!ok || !inScope(me, x)) {
			return false // ngoài phạm vi nhóm được cấp
		}
		if g == "" || g == "all" {
			return true
		}
		return ok && (x == g || (g == "__none" && x == ""))
	}
}

func (s *Server) hReport2(w http.ResponseWriter, r *http.Request, me *Account) {
	// Khoảng ngày: from/to (YYYY-MM-DD, gồm cả ngày cuối); không có thì lấy N ngày gần nhất.
	day0 := time.Now().Truncate(24 * time.Hour)
	since, until := day0, day0.AddDate(0, 0, 1)
	if f, e1 := time.ParseInLocation("2006-01-02", r.URL.Query().Get("from"), time.Local); e1 == nil {
		if t, e2 := time.ParseInLocation("2006-01-02", r.URL.Query().Get("to"), time.Local); e2 == nil && !t.Before(f) {
			since, until = f, t.AddDate(0, 0, 1)
		}
	} else {
		n, _ := strconv.Atoi(r.URL.Query().Get("days"))
		if n < 1 {
			n = 7
		}
		since = day0.AddDate(0, 0, 1-n)
	}
	days := int(until.Sub(since).Hours()/24 + 0.5)
	if days > 366 {
		http.Error(w, "Khoảng ngày tối đa 366 ngày", 400)
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	inG := s.groupPred(me, r.URL.Query().Get("group"))
	total, online := 0, 0
	for _, m := range s.st.Machines {
		if !m.Revoked && inG(m.ID) {
			total++
			if s.isOnline(m) {
				online++
			}
		}
	}
	byKind := map[string]int{}
	perMachine := map[string]int{}
	opened, openNow, closed := 0, 0, 0
	var durSum time.Duration
	for _, a := range s.st.Alerts {
		if !inG(a.Machine) {
			continue
		}
		if a.Status == "open" {
			openNow++
		}
		if a.Opened.Before(since) || !a.Opened.Before(until) {
			continue
		}
		opened++
		byKind[a.Kind]++
		perMachine[a.Machine]++ // theo mã máy: máy đổi tên vẫn là một dòng
		if a.Status == "closed" && !a.Closed.IsZero() {
			closed++
			durSum += a.Closed.Sub(a.Opened)
		}
	}
	type kv struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	top := []kv{}
	names := map[string]string{}
	for _, a := range s.st.Alerts { // tên cũ trong cảnh báo, dùng khi máy đã bị xóa khỏi danh sách
		names[a.Machine] = a.Name
	}
	for _, m := range s.st.Machines {
		names[m.ID] = m.Name
	}
	for id, c := range perMachine {
		top = append(top, kv{names[id], c})
	}
	sort.Slice(top, func(i, j int) bool {
		return top[i].Count > top[j].Count || (top[i].Count == top[j].Count && top[i].Name < top[j].Name)
	})
	if len(top) > 10 {
		top = top[:10]
	}
	reqNew, reqDone, reqAll := 0, 0, 0
	byType := map[string]map[string]int{}
	for _, q := range s.st.Requests {
		if q.Created.Before(since) || !q.Created.Before(until) || !s.visibleRequest(me, q) || !inG(q.Machine) {
			continue
		}
		reqAll++
		dt := q.DeviceType
		if dt == "" {
			dt = "Không rõ"
		}
		if byType[dt] == nil {
			byType[dt] = map[string]int{}
		}
		if q.Status == "done" {
			byType[dt]["done"]++
		} else {
			byType[dt]["open"]++
		}
		if q.Status == "done" {
			reqDone++
		}
		if q.Status == "new" {
			reqNew++
		}
	}
	avg := 0.0
	if closed > 0 {
		avg = durSum.Minutes() / float64(closed)
	}
	step := 1
	if days > 14 {
		step = 7
	}
	nb := (days + step - 1) / step
	type bucket struct {
		Label string `json:"label"`
		N     int    `json:"n"`
	}
	byDay := make([]bucket, nb)
	for i := range byDay {
		byDay[i].Label = since.AddDate(0, 0, i*step).Format("02/01")
	}
	for _, a := range s.st.Alerts {
		if a.Opened.Before(since) || !a.Opened.Before(until) || !inG(a.Machine) {
			continue
		}
		idx := int(a.Opened.Sub(since).Hours()/24) / step
		if idx >= 0 && idx < nb {
			byDay[idx].N++
		}
	}
	jok(w, map[string]interface{}{"byDay": byDay, "byType": byType, "days": days, "from": since.Format("2006-01-02"), "to": until.AddDate(0, 0, -1).Format("2006-01-02"), "machines": total, "online": online, "alerts": opened, "alertsOpen": openNow,
		"byKind": byKind, "top": top, "avgCloseMin": avg, "requests": reqAll, "requestsDone": reqDone, "requestsNew": reqNew})
}

func csvSafe(s string) string { // chống chèn công thức khi mở bằng Excel
	if s != "" && strings.ContainsAny(s[:1], "=+-@\t\r") {
		return "'" + s
	}
	return s
}

var exportNames = map[string]string{"requests": "Yêu cầu hỗ trợ", "alerts": "Cảnh báo", "machines": "Máy", "inventory": "Kiểm kê", "uptime": "Thời gian hoạt động"}
var reqStatusVN = map[string]string{"new": "Mới", "working": "Đang xử lý", "done": "Đã xong"}

// exportRows: gọi khi đang giữ s.mu.
func (s *Server) exportRows(what, group string, me *Account) ([][]string, bool) {
	inG := s.groupPred(me, group)
	rows := [][]string{}
	switch what {
	case "machines":
		rows = append(rows, []string{"Tên máy", "Nhóm", "IP", "MAC", "Hệ điều hành", "Trạng thái", "CPU %", "RAM %", "Nhiệt độ °C", "Ổ trống nhất %", "Người dùng", "Thấy lần cuối"})
		for _, m := range s.st.Machines {
			if m.Revoked || !inG(m.ID) {
				continue
			}
			d := s.machineSummary(m)
			st := "Offline"
			if d["online"].(bool) {
				st = "Online"
			}
			f := func(k string) string {
				switch v := d[k].(type) {
				case float64:
					return strconv.FormatFloat(v, 'f', 0, 64)
				case *float64:
					if v != nil {
						return strconv.FormatFloat(*v, 'f', 0, 64)
					}
				}
				return ""
			}
			u, _ := d["user"].(string)
			rows = append(rows, []string{m.Name, nz(m.Group), m.IP, m.MAC, m.OS, st, f("cpu"), f("ram"), f("temp"), f("diskFree"), u, m.LastSeen.Format("2006-01-02 15:04:05")})
		}
	case "alerts":
		rows = append(rows, []string{"Máy", "Loại", "Mức", "Nội dung", "Mở lúc", "Đóng lúc", "Trạng thái", "Người đóng"})
		for _, a := range s.st.Alerts {
			if !inG(a.Machine) {
				continue
			}
			c := ""
			if !a.Closed.IsZero() {
				c = a.Closed.Format("2006-01-02 15:04:05")
			}
			st := "Đang mở"
			if a.Status != "open" {
				st = "Đã đóng"
			}
			rows = append(rows, []string{a.Name, a.Kind, a.Sev, a.Msg, a.Opened.Format("2006-01-02 15:04:05"), c, st, a.By})
		}
	case "requests":
		rows = append(rows, []string{"Mã", "Máy", "Loại thiết bị", "Người báo", s.contactLabel(), "Mã NV", "Máy hư", "Người dùng", "Nội dung", "Gấp", "Trạng thái", "Gửi lúc", "Số trả lời", "Cách xử lý"})
		for _, q := range s.st.Requests {
			if s.visibleRequest(me, q) && inG(q.Machine) {
				rows = append(rows, []string{q.ID, q.Name, q.DeviceType, q.Reporter, q.Phone, q.EmpID, q.BadPC, q.User, q.Text, map[bool]string{true: "có", false: ""}[q.Urgent], reqStatusVN[q.Status], q.Created.Format("2006-01-02 15:04:05"), strconv.Itoa(len(q.Replies)), q.Solution})
			}
		}
	case "inventory":
		rows = append(rows, []string{"Máy", "IP", "Nhóm", "Hệ điều hành", "Model", "CPU", "RAM (GB)", "Serial", "Bản quyền phần mềm"})
		hw := func(m *Machine, k string) string {
			if v := m.Hw[k]; v != nil {
				return fmt.Sprint(v)
			}
			return ""
		}
		for _, m := range s.st.Machines {
			if m.Revoked || !inG(m.ID) {
				continue
			}
			rows = append(rows, []string{m.Name, m.IP, nz(m.Group), m.OS, hw(m, "model"), hw(m, "cpu"), hw(m, "ramGB"), hw(m, "serial"), m.LicenseNote})
		}
	default:
		return nil, false
	}
	return rows, true
}

func grpNote(g string) string {
	switch g {
	case "", "all":
		return ""
	case "__none":
		return " · chưa phân nhóm"
	}
	return " · nhóm " + g
}

// /api/export/{machines|alerts|requests}.csv  và  /api/export/{machines|alerts|requests|all}.xlsx
func (s *Server) hExport(w http.ResponseWriter, r *http.Request, me *Account) {
	name := strings.TrimPrefix(r.URL.Path, "/api/export/")
	xlsx := strings.HasSuffix(name, ".xlsx")
	what := strings.TrimSuffix(strings.TrimSuffix(name, ".csv"), ".xlsx")
	group := r.URL.Query().Get("group")
	s.mu.RLock()
	var sheets []xSheet
	kinds := []string{what}
	if xlsx && what == "all" {
		kinds = []string{"requests", "alerts", "machines"}
	}
	for _, k := range kinds {
		rows, ok := [][]string(nil), true
		if k == "uptime" { // ?from=&to= YYYY-MM-DD (giờ máy chủ, gồm cả ngày cuối)
			f, e1 := time.ParseInLocation("2006-01-02", r.URL.Query().Get("from"), time.Local)
			t, e2 := time.ParseInLocation("2006-01-02", r.URL.Query().Get("to"), time.Local)
			if e1 != nil || e2 != nil || t.Before(f) || t.Sub(f) > 400*24*time.Hour {
				s.mu.RUnlock()
				jerr(w, 400, "Khoảng ngày không hợp lệ")
				return
			}
			rows = s.uptimeRows(me, group, f, t.AddDate(0, 0, 1))
		} else {
			rows, ok = s.exportRows(k, group, me)
		}
		if !ok {
			s.mu.RUnlock()
			jerr(w, 404, "Không có báo cáo này")
			return
		}
		sheets = append(sheets, xSheet{exportNames[k], rows})
	}
	s.mu.RUnlock()
	day := time.Now().Format("20060102")
	if xlsx {
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mam-%s-%s.xlsx"`, what, day))
		for _, sh := range sheets {
			for _, row := range sh.Rows[1:] {
				for i := range row {
					row[i] = csvSafe(row[i])
				}
			}
		}
		writeXLSX(w, sheets)
		s.auditBy(me, r, "Báo cáo", "Xuất báo cáo "+what+grpNote(group)+" (Excel)", "—", false)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mam-%s-%s.csv"`, what, day))
	w.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(w)
	for _, row := range sheets[0].Rows {
		for i := range row {
			row[i] = csvSafe(row[i])
		}
		cw.Write(row)
	}
	cw.Flush()
	s.auditBy(me, r, "Báo cáo", "Xuất báo cáo "+what+grpNote(group)+" (CSV)", "—", false)
}

// suggest: tối đa 3 yêu cầu đã xong, có cách xử lý, giống yêu cầu {id} nhất (trùng từ khóa; cùng máy được cộng điểm).
func (s *Server) suggest(w http.ResponseWriter, me *Account, id string) {
	words := func(x string) map[string]bool {
		m := map[string]bool{}
		for _, f := range strings.FieldsFunc(strings.ToLower(x), func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) }) {
			if len([]rune(f)) >= 3 {
				m[f] = true
			}
		}
		return m
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cur *Request
	for _, x := range s.st.Requests {
		if x.ID == id && s.visibleRequest(me, x) {
			cur = x
		}
	}
	if cur == nil {
		jerr(w, 404, "Không có yêu cầu này")
		return
	}
	cw := words(cur.Text + " " + cur.BadPC)
	type hit struct {
		q     *Request
		score int
	}
	var hits []hit
	for _, x := range s.st.Requests {
		if x.ID == cur.ID || x.Status != "done" || x.Solution == "" || !s.visibleRequest(me, x) {
			continue
		}
		n := 0
		for k := range words(x.Text + " " + x.BadPC) {
			if cw[k] {
				n++
			}
		}
		if n == 0 {
			continue
		}
		if x.Machine == cur.Machine {
			n++
		}
		hits = append(hits, hit{x, n})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := []map[string]string{}
	for i := 0; i < len(hits) && i < 3; i++ {
		x := hits[i].q
		out = append(out, map[string]string{"id": x.ID, "name": x.Name, "text": x.Text, "solution": x.Solution, "by": x.SolvedBy, "at": x.Updated.Format(time.RFC3339)})
	}
	jok(w, out)
}

// /api/contact-label: GET ai cũng xem được; PUT {label} chỉ Super admin.
func (s *Server) hContactLabel(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "GET" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		jok(w, map[string]string{"label": s.contactLabel()})
		return
	}
	var in struct{ Label string }
	if r.Method != "PUT" || me.Role != "super" || readJSON(r, &in) != nil {
		jerr(w, 403, "Chỉ Super admin đổi tên ô liên hệ")
		return
	}
	in.Label = strings.TrimSpace(in.Label)
	if len([]rune(in.Label)) > 30 {
		jerr(w, 400, "Tên ô liên hệ tối đa 30 ký tự")
		return
	}
	s.mu.Lock()
	s.st.ContactLabel = in.Label
	s.save()
	lb := s.contactLabel()
	s.mu.Unlock()
	s.auditBy(me, r, "Cài đặt", "Đổi tên ô liên hệ trong yêu cầu hỗ trợ: "+lb, "", false)
	jok(w, map[string]string{"label": lb})
}

// /api/remote-password: GET {set} ai cũng xem được; PUT {password} chỉ Super admin (rỗng = tắt, quay về xin phép máy).
func (s *Server) hRemotePassword(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "GET" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		jok(w, map[string]bool{"set": s.st.RemoteHash != ""})
		return
	}
	var in struct{ Password string }
	if r.Method != "PUT" || me.Role != "super" || readJSON(r, &in) != nil {
		jerr(w, 403, "Chỉ Super admin đặt mật khẩu điều khiển từ xa")
		return
	}
	if in.Password != "" {
		if len([]rune(in.Password)) < 8 || len(in.Password) > 128 {
			jerr(w, 400, "Mật khẩu điều khiển từ xa ít nhất 8 ký tự")
			return
		}
	}
	s.mu.Lock()
	if in.Password == "" {
		s.st.RemoteSalt, s.st.RemoteHash = "", ""
	} else {
		s.st.RemoteSalt = rid(16)
		s.st.RemoteHash = hashPw(in.Password, s.st.RemoteSalt, pbkdfIter)
	}
	set := s.st.RemoteHash != ""
	s.save()
	s.mu.Unlock()
	msg := "Tắt mật khẩu điều khiển từ xa (quay về xin phép máy)"
	if set {
		msg = "Đổi mật khẩu điều khiển từ xa"
	}
	s.auditBy(me, r, "Cài đặt", msg, "", true)
	jok(w, map[string]bool{"set": set})
}

// /api/uninstall-password (chỉ Super admin): GET {set, at, by}; PUT {password} đặt/đổi; DELETE xóa (không ai gỡ được trên máy).
func (s *Server) hUninstallPassword(w http.ResponseWriter, r *http.Request, me *Account) {
	if me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin quản lý mật khẩu gỡ")
		return
	}
	view := func() map[string]interface{} {
		v := map[string]interface{}{"set": s.st.UninstHash != "", "by": s.st.UninstBy}
		if !s.st.UninstAt.IsZero() {
			v["at"] = s.st.UninstAt
		}
		return v
	}
	switch r.Method {
	case "GET":
		s.mu.RLock()
		defer s.mu.RUnlock()
		jok(w, view())
		return
	case "PUT":
		var in struct{ Password string }
		if readJSON(r, &in) != nil || len([]rune(in.Password)) < 8 || len(in.Password) > 128 {
			jerr(w, 400, "Mật khẩu gỡ ít nhất 8 ký tự")
			return
		}
		s.mu.Lock()
		s.st.UninstSalt = rid(16)
		s.st.UninstHash = hashPw(in.Password, s.st.UninstSalt, pbkdfIter)
	case "DELETE":
		s.mu.Lock()
		s.st.UninstSalt, s.st.UninstHash = "", ""
	default:
		jerr(w, 405, "Phương thức không hỗ trợ")
		return
	}
	s.st.UninstAt, s.st.UninstBy = time.Now(), me.Name
	s.save()
	v := view()
	s.mu.Unlock()
	msg := "Xóa mật khẩu gỡ agent"
	if r.Method == "PUT" {
		msg = "Đổi mật khẩu gỡ agent"
	}
	s.auditBy(me, r, "Cài đặt", msg, "", true)
	jok(w, v)
}

// /api/device-types: GET ai cũng xem được; PUT {types:[...]} chỉ Super admin.
func (s *Server) hDeviceTypes(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "GET" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		jok(w, s.deviceTypes())
		return
	}
	if r.Method != "PUT" || me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin tạo và xóa loại thiết bị")
		return
	}
	var in struct{ Types []string }
	if readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	seen := map[string]bool{}
	var out []string
	for _, x := range in.Types {
		x = strings.TrimSpace(x)
		if x == "" || seen[strings.ToLower(x)] {
			continue
		}
		if len([]rune(x)) > 30 {
			jerr(w, 400, "Tên loại thiết bị tối đa 30 ký tự")
			return
		}
		seen[strings.ToLower(x)] = true
		out = append(out, x)
	}
	if len(out) < 1 || len(out) > 20 {
		jerr(w, 400, "Cần từ 1 đến 20 loại thiết bị")
		return
	}
	s.mu.Lock()
	s.st.DeviceTypes = out
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Cài đặt", "Đổi danh sách loại thiết bị: "+strings.Join(out, ", "), "", false)
	jok(w, out)
}
