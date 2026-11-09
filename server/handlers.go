package main

import (
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const onlineWindow = 25 * time.Second

func (s *Server) isOnline(m *Machine) bool {
	l := s.live[m.ID]
	return l != nil && time.Since(l.At) < onlineWindow
}

func canControl(me *Account, m *Machine) bool {
	switch me.Role {
	case "super":
		return true
	case "tech":
		if len(me.Scope) == 0 {
			return true
		}
		for _, g := range me.Scope {
			if g == m.Group {
				return true
			}
		}
	}
	return false
}

// canSee: tài khoản được cấp phạm vi nhóm chỉ thấy máy thuộc các nhóm đó (máy chưa phân nhóm bị ẩn).
func canSee(me *Account, m *Machine) bool { return inScope(me, m.Group) }

// alertVisible: cảnh báo của máy ngoài phạm vi (hoặc của máy đã mất) bị ẩn với tài khoản có phạm vi nhóm.
func (s *Server) alertVisible(me *Account, a *Alert) bool {
	if me.Role == "super" || len(me.Scope) == 0 {
		return true
	}
	m := s.machineByID(a.Machine)
	return m != nil && canSee(me, m)
}

func (s *Server) machineSummary(m *Machine) map[string]interface{} {
	l := s.live[m.ID]
	on := s.isOnline(m)
	out := map[string]interface{}{"id": m.ID, "name": m.Name, "os": m.OS, "group": m.Group, "ip": m.IP, "mac": m.MAC, "online": on, "lastSeen": m.LastSeen, "registered": m.Registered, "av": avatarVer("m", m.ID)}
	if l != nil {
		out["cpu"], out["ram"], out["temp"], out["uptime"], out["user"], out["ver"] = l.CPU, l.RAM, l.Temp, l.Uptime, l.User, l.Ver
		minFree := 100.0
		for _, d := range l.Disks {
			if d.TotalGB > 0 {
				if p := d.FreeGB / d.TotalGB * 100; p < minFree {
					minFree = p
				}
			}
		}
		if len(l.Disks) > 0 {
			out["diskFree"] = minFree
		}
	}
	return out
}

func (s *Server) hMachines(w http.ResponseWriter, r *http.Request, me *Account) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []map[string]interface{}{}
	for _, m := range s.st.Machines {
		if !m.Revoked && canSee(me, m) {
			d := s.machineSummary(m)
			d["canControl"] = canControl(me, m)
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	jok(w, out)
}

func (s *Server) machineByID(id string) *Machine {
	for _, m := range s.st.Machines {
		if m.ID == id && !m.Revoked {
			return m
		}
	}
	return nil
}

// /api/machines/{id}  và  /api/machines/{id}/{command|group|rename}
func (s *Server) hMachine(w http.ResponseWriter, r *http.Request, me *Account) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/machines/")
	parts := strings.Split(rest, "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if id == "bulk" && r.Method == "POST" {
		s.hBulk(w, r, me)
		return
	}
	if len(parts) > 1 && (s.machineExtra(w, r, me, id, parts[1:]) || s.screenExtra(w, r, me, id, parts[1:]) || s.eventsExtra(w, r, me, id, parts[1:])) {
		return
	}
	switch {
	case action == "" && r.Method == "GET":
		s.mu.RLock()
		defer s.mu.RUnlock()
		m := s.machineByID(id)
		if m == nil || !canSee(me, m) {
			jerr(w, 404, "Không có máy này")
			return
		}
		d := s.machineSummary(m)
		d["hw"] = m.Hw
		d["remoteNote"] = m.RemoteNote
		d["licenseNote"] = m.LicenseNote
		d["canControl"] = canControl(me, m)
		if l := s.live[m.ID]; l != nil {
			d["disks"] = l.Disks
			d["hist"] = l.Hist
		}
		cmds := []*Command{}
		for i := len(s.st.Commands) - 1; i >= 0 && len(cmds) < 15; i-- {
			if s.st.Commands[i].Machine == id {
				cmds = append(cmds, s.st.Commands[i])
			}
		}
		d["commands"] = cmds
		jok(w, d)
	case action == "command" && r.Method == "POST":
		if me.Role == "view" {
			jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
			return
		}
		var in struct {
			Type string
			Args map[string]string
		}
		if readJSON(r, &in) != nil {
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
		res, code, msg := s.issue(me, r, []string{id}, in.Type, in.Args)
		if code != 0 {
			jerr(w, code, msg)
			return
		}
		jok(w, res)
	case action == "group" && r.Method == "PUT":
		if me.Role == "view" {
			jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
			return
		}
		var in struct{ Group string }
		readJSON(r, &in)
		s.mu.Lock()
		m := s.machineByID(id)
		if m == nil {
			s.mu.Unlock()
			jerr(w, 404, "Không có máy này")
			return
		}
		if !s.groupExists(in.Group) && in.Group != "" {
			s.mu.Unlock()
			jerr(w, 400, "Nhóm không tồn tại")
			return
		}
		if !canControl(me, m) {
			s.mu.Unlock()
			jerr(w, 403, "Máy này ngoài phạm vi nhóm của bạn")
			return
		}
		if me.Role == "tech" && in.Group != "" && !inScope(me, in.Group) {
			s.mu.Unlock()
			jerr(w, 403, "Nhóm đích ngoài phạm vi của bạn")
			return
		}
		old := m.Group
		m.Group = in.Group
		name := m.Name
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Máy", "Chuyển "+name+" từ "+nz(old)+" sang "+nz(in.Group), name, false)
		jok(w, map[string]bool{"ok": true})
	case action == "remote-note" && r.Method == "PUT":
		if me.Role == "view" {
			jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
			return
		}
		var in struct{ Note string }
		readJSON(r, &in)
		in.Note = strings.TrimSpace(in.Note)
		if len([]rune(in.Note)) > 200 {
			jerr(w, 400, "Tối đa 200 ký tự")
			return
		}
		s.mu.Lock()
		m := s.machineByID(id)
		if m == nil {
			s.mu.Unlock()
			jerr(w, 404, "Không có máy này")
			return
		}
		if !canControl(me, m) {
			s.mu.Unlock()
			jerr(w, 403, "Máy này ngoài phạm vi nhóm của bạn")
			return
		}
		m.RemoteNote = in.Note
		name := m.Name
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Máy", "Đổi ID/địa chỉ remote desktop của "+name, name, false)
		jok(w, map[string]bool{"ok": true})
	case action == "license-note" && r.Method == "PUT":
		if me.Role == "view" {
			jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
			return
		}
		var in struct{ Note string }
		readJSON(r, &in)
		in.Note = strings.TrimSpace(in.Note)
		if len([]rune(in.Note)) > 2000 {
			jerr(w, 400, "Tối đa 2000 ký tự")
			return
		}
		s.mu.Lock()
		m := s.machineByID(id)
		if m == nil {
			s.mu.Unlock()
			jerr(w, 404, "Không có máy này")
			return
		}
		if !canControl(me, m) {
			s.mu.Unlock()
			jerr(w, 403, "Máy này ngoài phạm vi nhóm của bạn")
			return
		}
		m.LicenseNote = in.Note
		name := m.Name
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Kiểm kê", "Đổi ghi chú bản quyền phần mềm của "+name, name, false)
		jok(w, map[string]bool{"ok": true})
	case action == "rename" && r.Method == "PUT":
		if me.Role == "view" {
			jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
			return
		}
		var in struct{ Name string }
		readJSON(r, &in)
		in.Name = strings.TrimSpace(in.Name)
		if len(in.Name) < 1 || len(in.Name) > 40 {
			jerr(w, 400, "Tên máy 1 đến 40 ký tự")
			return
		}
		s.mu.Lock()
		m := s.machineByID(id)
		if m == nil {
			s.mu.Unlock()
			jerr(w, 404, "Không có máy này")
			return
		}
		if !canControl(me, m) {
			s.mu.Unlock()
			jerr(w, 403, "Máy này ngoài phạm vi nhóm của bạn")
			return
		}
		old := m.Name
		m.Name = in.Name
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Máy", "Đổi tên máy "+old+" thành "+in.Name, in.Name, false)
		jok(w, map[string]bool{"ok": true})
	case action == "" && r.Method == "DELETE": // thu hồi token
		if me.Role != "super" {
			jerr(w, 403, "Chỉ Super admin được thu hồi máy")
			return
		}
		s.mu.Lock()
		m := s.machineByID(id)
		if m == nil {
			s.mu.Unlock()
			jerr(w, 404, "Không có máy này")
			return
		}
		m.Revoked = true
		removeAvatar("m", id)
		delete(s.st.Uptime, id)
		os.Remove(evFile(s.dir, id))
		delete(s.live, id)
		name := m.Name
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Máy", "Thu hồi token và gỡ máy "+name+" khỏi hệ thống", name, true)
		jok(w, map[string]bool{"ok": true})
	default:
		jerr(w, 405, "Sai phương thức")
	}
}

func nz(g string) string {
	if g == "" {
		return "Chưa phân nhóm"
	}
	return g
}

func inScope(me *Account, g string) bool {
	if me.Role == "super" || len(me.Scope) == 0 {
		return true
	}
	for _, x := range me.Scope {
		if x == g {
			return true
		}
	}
	return false
}

func (s *Server) groupExists(g string) bool {
	for _, x := range s.st.Groups {
		if x == g {
			return true
		}
	}
	return false
}

var sensitiveCmd = map[string]bool{"restart": true, "shutdown": true}
var cmdLabel = map[string]string{"lock": "Khóa màn hình", "message": "Gửi tin nhắn tới", "restart": "Khởi động lại", "shutdown": "Tắt máy", "wol": "Bật máy (Wake-on-LAN)",
	"procs": "Xem tiến trình của", "kill": "Tắt tiến trình trên", "smart": "Đọc tình trạng ổ cứng của", "clean": "Dọn file rác của", "freeram": "Giải phóng RAM của"}

// issue tạo lệnh cho nhiều máy; kiểm quyền từng máy.
func (s *Server) issue(me *Account, r *http.Request, ids []string, typ string, args map[string]string) (map[string]interface{}, int, string) {
	if _, ok := cmdLabel[typ]; !ok {
		return nil, 400, "Lệnh không hợp lệ"
	}
	if typ == "message" {
		t := strings.TrimSpace(args["text"])
		if t == "" || len([]rune(t)) > 300 {
			return nil, 400, "Nội dung tin nhắn 1 đến 300 ký tự"
		}
		args = map[string]string{"text": t, "from": me.Name, "fromId": me.ID}
	} else if infoCmd[typ] {
		var msg string
		if args, msg = cleanArgs(typ, args); msg != "" {
			return nil, 400, msg
		}
	} else {
		args = nil
	}
	s.mu.Lock()
	var done, denied, offline []string
	var wolTargets []*Machine
	cmdIDs := map[string]string{}
	for _, id := range ids {
		m := s.machineByID(id)
		if m == nil {
			continue
		}
		if !canControl(me, m) {
			denied = append(denied, m.Name)
			continue
		}
		if typ == "wol" {
			wolTargets = append(wolTargets, m)
			done = append(done, m.Name)
			// nhờ tối đa 2 máy đang online cùng dải mạng (/24) phát giúp: server ở khác dải/VPN thì broadcast không tới được máy đích
			n := 0
			for _, r := range s.st.Machines {
				if n < 2 && r.ID != m.ID && s.isOnline(r) && sameNet24(r.IP, m.IP) {
					s.st.Commands = append(s.st.Commands, &Command{ID: rid(6), Machine: r.ID, Type: "wolsend", Args: map[string]string{"mac": strings.Join(wolTargetsOf(m), ","), "ip": m.IP}, By: me.User, Created: time.Now(), Status: "pending"})
					wake(r.ID)
					n++
				}
			}
			continue
		}
		if !s.isOnline(m) {
			offline = append(offline, m.Name)
			continue
		}
		c := &Command{ID: rid(6), Machine: m.ID, Type: typ, Args: args, By: me.User, Created: time.Now(), Status: "pending"}
		s.st.Commands = append(s.st.Commands, c)
		wake(m.ID) // agent đang đợi sẽ báo cáo và nhận lệnh ngay, không đợi lượt 5 giây
		cmdIDs[m.ID] = c.ID
		done = append(done, m.Name)
		if typ == "message" {
			s.addChat(m.ID, me.Name, args["text"], false, me.ID)
		}
	}
	if len(s.st.Commands) > 3000 {
		s.st.Commands = s.st.Commands[len(s.st.Commands)-2000:]
	}
	s.save()
	s.mu.Unlock()
	for _, m := range wolTargets {
		for _, mac := range wolTargetsOf(m) {
			go sendWOL(mac, m.IP)
		}
	}
	if len(done) > 0 {
		who := done[0]
		if len(done) > 1 {
			who = done[0] + " và " + itoa(len(done)-1) + " máy khác"
		}
		note := ""
		if typ == "kill" {
			note = ": " + args["name"] + " (" + args["pid"] + ")"
		}
		s.auditBy(me, r, "Lệnh tới máy", cmdLabel[typ]+" "+who+note, done[0], sensitiveCmd[typ] || typ == "kill")
	}
	if len(done) == 0 && len(denied) > 0 {
		return nil, 403, "Ngoài phạm vi nhóm của bạn: " + strings.Join(denied, ", ")
	}
	if len(done) == 0 && len(offline) > 0 {
		return nil, 409, "Máy đang offline, không gửi lệnh được: " + strings.Join(offline, ", ")
	}
	return map[string]interface{}{"sent": done, "denied": denied, "offline": offline, "cmds": cmdIDs}, 0, ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func (s *Server) hBulk(w http.ResponseWriter, r *http.Request, me *Account) {
	if me.Role == "view" {
		jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
		return
	}
	var in struct {
		IDs   []string
		Type  string
		Args  map[string]string
		Group string
	}
	if readJSON(r, &in) != nil || len(in.IDs) == 0 || len(in.IDs) > 1000 {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	if in.Type == "group" {
		s.mu.Lock()
		if in.Group != "" && !s.groupExists(in.Group) {
			s.mu.Unlock()
			jerr(w, 400, "Nhóm không tồn tại")
			return
		}
		if !inScope(me, in.Group) {
			s.mu.Unlock()
			jerr(w, 403, "Nhóm đích ngoài phạm vi của bạn")
			return
		}
		n := 0
		for _, id := range in.IDs {
			if m := s.machineByID(id); m != nil && canControl(me, m) {
				m.Group = in.Group
				n++
			}
		}
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Máy", "Chuyển "+itoa(n)+" máy sang "+nz(in.Group), nz(in.Group), false)
		jok(w, map[string]int{"moved": n})
		return
	}
	res, code, msg := s.issue(me, r, in.IDs, in.Type, in.Args)
	if code != 0 {
		jerr(w, code, msg)
		return
	}
	jok(w, res)
}

// ---------- nhóm ----------

func (s *Server) hGroups(w http.ResponseWriter, r *http.Request, me *Account) {
	switch r.Method {
	case "GET":
		s.mu.RLock()
		defer s.mu.RUnlock()
		cnt := map[string]int{}
		for _, m := range s.st.Machines {
			if !m.Revoked && canSee(me, m) {
				cnt[m.Group]++
			}
		}
		out := []map[string]interface{}{}
		for _, g := range s.st.Groups {
			if !inScope(me, g) {
				continue
			}
			out = append(out, map[string]interface{}{"name": g, "count": cnt[g]})
		}
		jok(w, map[string]interface{}{"groups": out, "ungrouped": cnt[""]})
	case "POST":
		if me.Role != "super" {
			jerr(w, 403, "Chỉ Super admin tạo và xóa nhóm")
			return
		}
		var in struct{ Name string }
		readJSON(r, &in)
		in.Name = strings.TrimSpace(in.Name)
		if len([]rune(in.Name)) < 2 || len([]rune(in.Name)) > 40 {
			jerr(w, 400, "Tên nhóm 2 đến 40 ký tự")
			return
		}
		s.mu.Lock()
		if s.groupExists(in.Name) {
			s.mu.Unlock()
			jerr(w, 409, "Nhóm đã tồn tại")
			return
		}
		s.st.Groups = append(s.st.Groups, in.Name)
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Nhóm máy", "Tạo nhóm "+in.Name, in.Name, false)
		jok(w, map[string]bool{"ok": true})
	case "DELETE":
		if me.Role != "super" {
			jerr(w, 403, "Chỉ Super admin tạo và xóa nhóm")
			return
		}
		name := r.URL.Query().Get("name")
		s.mu.Lock()
		if !s.groupExists(name) {
			s.mu.Unlock()
			jerr(w, 404, "Không có nhóm này")
			return
		}
		out := s.st.Groups[:0]
		for _, g := range s.st.Groups {
			if g != name {
				out = append(out, g)
			}
		}
		s.st.Groups = out
		n := 0
		for _, m := range s.st.Machines {
			if m.Group == name {
				m.Group = ""
				n++
			}
		}
		for _, a := range s.st.Accounts {
			sc := []string{}
			for _, g := range a.Scope {
				if g != name {
					sc = append(sc, g)
				}
			}
			a.Scope = sc
		}
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Nhóm máy", "Xóa nhóm "+name+" ("+itoa(n)+" máy về Chưa phân nhóm)", name, true)
		jok(w, map[string]bool{"ok": true})
	default:
		jerr(w, 405, "Sai phương thức")
	}
}

// ---------- tổng quan, cảnh báo, cài đặt, nhật ký ----------

func (s *Server) hOverview(w http.ResponseWriter, r *http.Request, me *Account) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total, online, hot := 0, 0, 0
	type gs struct{ Total, Online int }
	g := map[string]*gs{}
	for _, m := range s.st.Machines {
		if m.Revoked || !canSee(me, m) {
			continue
		}
		total++
		on := s.isOnline(m)
		if on {
			online++
		}
		if l := s.live[m.ID]; l != nil && on && l.Temp != nil && *l.Temp >= s.st.Settings.TempC {
			hot++
		}
		k := nz(m.Group)
		if g[k] == nil {
			g[k] = &gs{}
		}
		g[k].Total++
		if on {
			g[k].Online++
		}
	}
	open := 0
	recent := []*Alert{}
	for i := len(s.st.Alerts) - 1; i >= 0; i-- {
		a := s.st.Alerts[i]
		if a.Status == "open" && s.alertVisible(me, a) {
			open++
			if len(recent) < 6 {
				recent = append(recent, a)
			}
		}
	}
	jok(w, map[string]interface{}{"total": total, "online": online, "openAlerts": open, "hot": hot, "groups": g, "recent": recent})
}

func (s *Server) hAlerts(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "POST" {
		if me.Role == "view" {
			jerr(w, 403, "Tài khoản Chỉ xem không có quyền thao tác")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/alerts/")
		id = strings.TrimSuffix(id, "/close")
		s.mu.Lock()
		for _, a := range s.st.Alerts {
			if a.ID == id && a.Status == "open" {
				m := s.machineByID(a.Machine)
				if m != nil && !canControl(me, m) {
					s.mu.Unlock()
					jerr(w, 403, "Máy này ngoài phạm vi nhóm của bạn")
					return
				}
				a.Status, a.Closed, a.By = "closed", time.Now(), me.User
				s.save()
				s.mu.Unlock()
				s.auditBy(me, r, "Cảnh báo", "Đóng cảnh báo "+a.Msg+" trên "+a.Name, a.Name, false)
				jok(w, map[string]bool{"ok": true})
				return
			}
		}
		s.mu.Unlock()
		jerr(w, 404, "Không có cảnh báo này")
		return
	}
	st := r.URL.Query().Get("status")
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*Alert{}
	for i := len(s.st.Alerts) - 1; i >= 0 && len(out) < 300; i-- {
		a := s.st.Alerts[i]
		if (st == "" || a.Status == st) && s.alertVisible(me, a) {
			out = append(out, a)
		}
	}
	jok(w, out)
}

func (s *Server) hSettings(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "GET" {
		s.mu.RLock()
		st := s.st.Settings
		s.mu.RUnlock()
		if me.Role != "super" {
			st.EnrollKey, st.Telegram, st.Webhook, st.SmtpUser, st.SmtpTo, st.BackupDir = "", "", "", "", "", ""
		}
		st.SmtpPass = "" // không trả mật khẩu SMTP về client
		jok(w, st)
		return
	}
	if me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin đổi cài đặt")
		return
	}
	var in Settings
	if readJSON(r, &in) != nil || in.TempC < 40 || in.TempC > 120 || in.CPU < 30 || in.CPU > 100 || in.DiskFreePc < 1 || in.DiskFreePc > 90 || in.OfflineMin < 1 || in.OfflineMin > 1440 {
		jerr(w, 400, "Giá trị ngoài khoảng cho phép (nhiệt độ 40–120, CPU 30–100, ổ trống 1–90%, offline 1–1440 phút)")
		return
	}
	in.EnrollKey = strings.TrimSpace(in.EnrollKey)
	s.mu.Lock()
	in.NoAutoUpdate = s.st.Settings.NoAutoUpdate
	in.NoMachineAvatar = s.st.Settings.NoMachineAvatar
	in.BackupDir = s.st.Settings.BackupDir
	if in.SmtpPass == "" {
		in.SmtpPass = s.st.Settings.SmtpPass // trống = giữ mật khẩu SMTP cũ
	}
	s.st.Settings = in
	s.save()
	s.mu.Unlock()
	s.auditBy(me, r, "Cài đặt", "Đổi ngưỡng cảnh báo: nhiệt độ "+itoa(int(in.TempC))+"°C, CPU "+itoa(int(in.CPU))+"%, ổ trống "+itoa(int(in.DiskFreePc))+"%, offline "+itoa(int(in.OfflineMin))+" phút", "—", true)
	jok(w, map[string]bool{"ok": true})
}

func (s *Server) hAudit(w http.ResponseWriter, r *http.Request, me *Account) {
	all := s.readAudit(5000)
	q := strings.ToLower(r.URL.Query().Get("q"))
	who := r.URL.Query().Get("who")
	out := []AuditEntry{}
	for i := len(all) - 1; i >= 0 && len(out) < 500; i-- {
		e := all[i]
		if me.Role != "super" && e.User != me.User {
			continue
		}
		if who != "" && e.User != who {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Msg+" "+e.User+" "+e.Name+" "+e.Target+" "+e.Kind+" "+e.IP), q) {
			continue
		}
		out = append(out, e)
	}
	jok(w, out)
}

func sameNet24(a, b string) bool {
	pa, pb := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	return pa != nil && pb != nil && pa[0] == pb[0] && pa[1] == pb[1] && pa[2] == pb[2]
}

// wolTargetsOf: MAC đã đăng ký cộng MAC thật của các card mạng agent báo về (card dây mới đánh thức được máy).
func wolTargetsOf(m *Machine) []string {
	seen, out := map[string]bool{}, []string{}
	for _, x := range append([]string{m.MAC}, m.MACs...) {
		x = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(x), "-", ":"))
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
