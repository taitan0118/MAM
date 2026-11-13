package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ---------- sao lưu ----------

var reBackupName = regexp.MustCompile(`^state-\d{8}-\d{4}\.json$`)

func (s *Server) hBackups(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "POST" {
		if me.Role != "super" {
			jerr(w, 403, "Chỉ Super admin sao lưu thủ công")
			return
		}
		s.backup()
		s.auditBy(me, r, "Sao lưu", "Sao lưu dữ liệu thủ công", "—", false)
		jok(w, map[string]bool{"ok": true})
		return
	}
	d := filepath.Join(s.dir, "backup")
	files, _ := os.ReadDir(d)
	out := []map[string]interface{}{}
	for _, f := range files {
		if f.IsDir() || !reBackupName.MatchString(f.Name()) {
			continue
		}
		fi, err := f.Info()
		if err != nil {
			continue
		}
		out = append(out, map[string]interface{}{"name": f.Name(), "size": fi.Size(), "time": fi.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) > out[j]["name"].(string) })
	jok(w, out)
}

func (s *Server) hBackupFile(w http.ResponseWriter, r *http.Request, me *Account) {
	if me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin tải bản sao lưu")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/backups/")
	if !reBackupName.MatchString(name) {
		jerr(w, 400, "Tên file không hợp lệ")
		return
	}
	p := filepath.Join(s.dir, "backup", name)
	f, err := os.Open(p)
	if err != nil {
		jerr(w, 404, "Không có file này")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	io.Copy(w, f)
	s.auditBy(me, r, "Sao lưu", "Tải bản sao lưu "+name, "—", true)
}

// ---------- kiểm kê: phần cứng + ghi chú bản quyền, gộp một lần gọi ----------

func (s *Server) hInventory(w http.ResponseWriter, r *http.Request, me *Account) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []map[string]interface{}{}
	for _, m := range s.st.Machines {
		if m.Revoked || !canSee(me, m) {
			continue
		}
		hw := m.Hw
		d := map[string]interface{}{"id": m.ID, "name": m.Name, "group": m.Group, "ip": m.IP, "os": m.OS}
		if hw != nil {
			d["board"], d["cpu"], d["ramGB"], d["ramType"], d["disks"] = hw["board"], hw["cpu"], hw["ramGB"], hw["ramType"], hw["disks"]
		}
		out = append(out, d)
	}
	jok(w, out)
}

// ---------- bảo mật & bản vá: chỉ đọc, lấy từ Hw agent gửi lên ----------

func (s *Server) hSecurity(w http.ResponseWriter, r *http.Request, me *Account) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []map[string]interface{}{}
	for _, m := range s.st.Machines {
		if m.Revoked || !canSee(me, m) {
			continue
		}
		hw := m.Hw
		d := map[string]interface{}{"id": m.ID, "name": m.Name, "group": m.Group, "online": s.isOnline(m)}
		if hw != nil {
			d["av"], d["avUpdated"], d["lastPatch"] = hw["av"], hw["avUpdated"], hw["lastPatch"]
		}
		out = append(out, d)
	}
	jok(w, out)
}
