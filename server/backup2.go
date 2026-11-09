package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sao lưu sang thư mục thứ hai (ổ khác, USB, NAS) mỗi giờ để ổ máy chủ chết vẫn còn dữ liệu.
// Mỗi bản là một file zip: state.json, audit.jsonl (xuất từ mam.db, nên bản sao lưu đọc được và khôi phục được cả trên bản cũ), events/, avatars/. Ghi ra file tạm, mở lại đọc
// state.json để kiểm tra, rồi mới đổi tên thành bản chính thức.
// Giữ: 24 bản theo giờ, 7 theo ngày, 4 theo tuần, 12 theo tháng.
// Kèm kiểm tra ổ cứng của chính máy chủ mỗi 6 giờ (dung lượng trống, tình trạng ổ theo Windows).

var srvM = &Machine{ID: "__server", Name: "Máy chủ MAM"} // chủ của các cảnh báo về máy chủ

var reMirror = regexp.MustCompile(`^mam-(\d{8})-(\d{2})(\d{2})\.zip$`)

var bk2 struct {
	sync.Mutex
	running bool
	At      time.Time `json:"at"`
	OK      bool      `json:"ok"`
	Err     string    `json:"err"`
	File    string    `json:"file"`
	Size    int64     `json:"size"`
	Disk    []srvDisk `json:"disk"`
	DiskAt  time.Time `json:"diskAt"`
}

type srvDisk struct {
	Name    string  `json:"name"`
	Health  string  `json:"health"`
	Wear    float64 `json:"wear"`
	ReadErr float64 `json:"readErr"`
	FreeGB  float64 `json:"freeGB"`
	TotalGB float64 `json:"totalGB"`
}

// mirrorBackup: một lần sao lưu; trả lỗi bằng câu người đọc hiểu được.
func (s *Server) mirrorBackup() (string, int64, error) {
	s.mu.RLock()
	dest, dir := s.st.Settings.BackupDir, s.dir
	b, err := json.Marshal(&s.st)
	s.mu.RUnlock()
	if dest == "" {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", 0, fmt.Errorf("không mở được thư mục sao lưu %s: %v", dest, err)
	}
	name := "mam-" + time.Now().Format("20060102-1504") + ".zip"
	tmp := filepath.Join(dest, name+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return "", 0, fmt.Errorf("không ghi được vào %s: %v", dest, err)
	}
	zw := zip.NewWriter(f)
	put := func(n string, r io.Reader) error {
		w, err := zw.Create(n)
		if err == nil {
			_, err = io.Copy(w, r)
		}
		return err
	}
	putFile := func(n, p string) error {
		x, err := os.Open(p)
		if err != nil {
			return nil // file chưa có (máy chưa có lỗi, chưa có ảnh) thì bỏ qua
		}
		defer x.Close()
		return put(n, x)
	}
	err = put("state.json", strings.NewReader(string(b)))
	if err == nil {
		err = put("audit.jsonl", bytes.NewReader(s.exportAudit()))
	}
	if err == nil {
		evMu.Lock()
		files, _ := filepath.Glob(filepath.Join(dir, "events", "*.jsonl"))
		for _, p := range files {
			if err = putFile("events/"+filepath.Base(p), p); err != nil {
				break
			}
		}
		evMu.Unlock()
	}
	if err == nil {
		files, _ := filepath.Glob(filepath.Join(dir, "avatars", "*.jpg"))
		for _, p := range files {
			if err = putFile("avatars/"+filepath.Base(p), p); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = zw.Close()
	}
	if err == nil {
		err = f.Sync()
	}
	f.Close()
	if err == nil {
		err = verifyMirror(tmp)
	}
	if err != nil {
		os.Remove(tmp)
		return "", 0, fmt.Errorf("sao lưu vào %s thất bại: %v", dest, err)
	}
	if err := os.Rename(tmp, filepath.Join(dest, name)); err != nil {
		os.Remove(tmp)
		return "", 0, err
	}
	fi, _ := os.Stat(filepath.Join(dest, name))
	pruneMirrors(dest, time.Now())
	if fi == nil {
		return name, 0, nil
	}
	return name, fi.Size(), nil
}

// verifyMirror mở lại file zip và đọc được state.json thì mới coi là bản sao lưu dùng được.
func verifyMirror(p string) error {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "state.json" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		var st State
		if err := json.NewDecoder(r).Decode(&st); err != nil {
			return errors.New("bản sao lưu đọc lại bị hỏng")
		}
		return nil
	}
	return errors.New("bản sao lưu thiếu state.json")
}

// keepMirrors chọn bản giữ lại: mới nhất của mỗi giờ (24), ngày (7), tuần (4), tháng (12).
func keepMirrors(names []string) map[string]bool {
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // tên theo thời gian nên mới nhất đứng đầu
	keep := map[string]bool{}
	type rule struct {
		key func(t time.Time) string
		n   int
	}
	rules := []rule{
		{func(t time.Time) string { return t.Format("2006010215") }, 24},
		{func(t time.Time) string { return t.Format("20060102") }, 7},
		{func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprint(y, w) }, 4},
		{func(t time.Time) string { return t.Format("200601") }, 12},
	}
	for _, r := range rules {
		seen := map[string]bool{}
		for _, n := range names {
			m := reMirror.FindStringSubmatch(n)
			if m == nil {
				continue
			}
			t, err := time.ParseInLocation("200601021504", m[1]+m[2]+m[3], time.Local)
			if err != nil {
				continue
			}
			k := r.key(t)
			if seen[k] {
				continue
			}
			if len(seen) >= r.n {
				break
			}
			seen[k] = true
			keep[n] = true
		}
	}
	return keep
}

func pruneMirrors(dest string, now time.Time) {
	ents, _ := os.ReadDir(dest)
	names := []string{}
	for _, e := range ents {
		if reMirror.MatchString(e.Name()) {
			names = append(names, e.Name())
		} else if strings.HasSuffix(e.Name(), ".zip.tmp") { // bản dở dang của lần trước bị ngắt
			if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > 3*time.Hour {
				os.Remove(filepath.Join(dest, e.Name()))
			}
		}
	}
	keep := keepMirrors(names)
	for _, n := range names {
		if !keep[n] {
			os.Remove(filepath.Join(dest, n))
		}
	}
}

// runMirror: chạy một lần (không chồng lần), ghi trạng thái, mở/đóng cảnh báo.
func (s *Server) runMirror() error {
	bk2.Lock()
	if bk2.running {
		bk2.Unlock()
		return errors.New("Đang sao lưu, thử lại sau ít phút")
	}
	bk2.running = true
	bk2.Unlock()
	name, size, err := s.mirrorBackup()
	bk2.Lock()
	bk2.running = false
	if name != "" || err != nil {
		bk2.At, bk2.OK, bk2.File, bk2.Size, bk2.Err = time.Now(), err == nil, name, size, ""
		if err != nil {
			bk2.Err = err.Error()
		}
	}
	bk2.Unlock()
	if name == "" && err == nil {
		return nil // chưa chọn thư mục thứ hai
	}
	s.mu.Lock()
	if err != nil {
		s.cond(srvM, "backup", "crit", true, "Sao lưu sang thư mục thứ hai thất bại: "+err.Error())
	} else {
		s.cond(srvM, "backup", "crit", false, "")
	}
	s.mu.Unlock()
	return err
}

// ---------- ổ cứng của máy chủ ----------

func (s *Server) checkServerDisk() {
	s.mu.RLock()
	dir, dest, warnPc := s.dir, s.st.Settings.BackupDir, s.st.Settings.DiskFreePc
	s.mu.RUnlock()
	out := []srvDisk{}
	low, bad := "", ""
	for _, p := range []string{dir, dest} {
		if p == "" {
			continue
		}
		free, total, err := diskFree(p)
		if err != nil || total == 0 {
			continue
		}
		d := srvDisk{Name: p, FreeGB: float64(free) / 1e9, TotalGB: float64(total) / 1e9}
		out = append(out, d)
		if float64(free)/float64(total)*100 < warnPc {
			low = fmt.Sprintf("Ổ chứa %s chỉ còn %.0f GB trống", p, d.FreeGB)
		}
	}
	for _, d := range serverSmart() {
		out = append(out, d)
		if (d.Health != "" && !strings.EqualFold(d.Health, "Healthy")) || d.Wear >= 90 || d.ReadErr > 0 {
			bad = fmt.Sprintf("Ổ %s của máy chủ có dấu hiệu hỏng (tình trạng %s, mòn %.0f%%, lỗi đọc %.0f). Hãy sao lưu và thay ổ sớm", d.Name, d.Health, d.Wear, d.ReadErr)
		}
	}
	bk2.Lock()
	bk2.Disk, bk2.DiskAt = out, time.Now()
	bk2.Unlock()
	s.mu.Lock()
	s.cond(srvM, "srvdisk", "warn", low != "", low)
	s.cond(srvM, "srvsmart", "crit", bad != "", bad)
	s.mu.Unlock()
}

// serverSmart: tình trạng ổ vật lý theo Windows (Get-PhysicalDisk). HĐH khác trả rỗng.
func serverSmart() []srvDisk {
	if runtime.GOOS != "windows" {
		return nil
	}
	ps := `$r=@(); foreach($d in Get-PhysicalDisk){ $c=$null; try{ $c = $d | Get-StorageReliabilityCounter -ErrorAction Stop }catch{}; $r += [pscustomobject]@{ name=[string]$d.FriendlyName; health=[string]$d.HealthStatus; wear=[double]$c.Wear; readErr=[double]$c.ReadErrorsUncorrected } }; ConvertTo-Json -Compress -InputObject @($r)`
	c := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	hideWindow(c)
	done := make(chan []byte, 1)
	go func() { b, _ := c.Output(); done <- b }()
	select {
	case b := <-done:
		var l []srvDisk
		json.Unmarshal(b, &l)
		return l
	case <-time.After(time.Minute):
		if c.Process != nil {
			c.Process.Kill()
		}
		return nil
	}
}

// ---------- API ----------

// GET /api/backups/mirror: trạng thái; POST: {"dir": "..."} đổi thư mục, {"run": true} sao lưu ngay.
func (s *Server) hMirror(w http.ResponseWriter, r *http.Request, me *Account) {
	if me.Role != "super" {
		jerr(w, 403, "Chỉ Super admin")
		return
	}
	if r.Method == "POST" {
		var in struct {
			Dir *string `json:"dir"`
			Run bool    `json:"run"`
		}
		if readJSON(r, &in) != nil {
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
		if in.Dir != nil {
			d := strings.TrimSpace(*in.Dir)
			if d != "" && !filepath.IsAbs(d) && !strings.HasPrefix(d, `\\`) {
				jerr(w, 400, "Hãy nhập đường dẫn đầy đủ, ví dụ D:\\MAM-backup hoặc \\\\NAS\\MAM-backup")
				return
			}
			s.mu.RLock()
			same := d != "" && filepath.Clean(d) == filepath.Clean(filepath.Join(s.dir, "backup"))
			s.mu.RUnlock()
			if same {
				jerr(w, 400, "Hãy chọn thư mục ở ổ khác với thư mục dữ liệu")
				return
			}
			s.mu.Lock()
			s.st.Settings.BackupDir = d
			if d == "" {
				s.cond(srvM, "backup", "crit", false, "")
			}
			s.save()
			s.mu.Unlock()
			s.auditBy(me, r, "Sao lưu", "Đổi thư mục sao lưu thứ hai thành "+orDash(d), "—", true)
			in.Run = in.Run || d != ""
		}
		if in.Run {
			if err := s.runMirror(); err != nil {
				jerr(w, 500, err.Error())
				return
			}
			go s.checkServerDisk()
		}
	}
	s.mu.RLock()
	dir := s.st.Settings.BackupDir
	s.mu.RUnlock()
	files := []map[string]interface{}{}
	if dir != "" {
		ents, _ := os.ReadDir(dir)
		for i := len(ents) - 1; i >= 0; i-- { // tên theo thời gian: đọc ngược là mới nhất trước
			if fi, err := ents[i].Info(); err == nil && reMirror.MatchString(ents[i].Name()) {
				files = append(files, map[string]interface{}{"name": fi.Name(), "size": fi.Size(), "time": fi.ModTime()})
			}
		}
	}
	bk2.Lock()
	defer bk2.Unlock()
	jok(w, map[string]interface{}{"files": files, "dir": dir, "at": bk2.At, "ok": bk2.OK, "err": bk2.Err, "file": bk2.File, "size": bk2.Size, "disk": bk2.Disk, "diskAt": bk2.DiskAt, "running": bk2.running})
}

func orDash(s string) string {
	if s == "" {
		return "(tắt)"
	}
	return s
}
