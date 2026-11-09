package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Khôi phục từ bản sao lưu: file zip của "Sao lưu sang ổ khác" hoặc file state-*.json của bản sao lưu hằng ngày.
// Dùng được khi đã đăng nhập Super admin, hoặc ngay trên máy chủ mới cài (chưa có tài khoản nào) — trường hợp
// ổ máy chủ cũ chết, cài lại MAM rồi nạp bản sao lưu. Trước khi ghi đè luôn lưu một bản hiện trạng để quay lại được.

var reRestoreFile = regexp.MustCompile(`^(audit\.jsonl|events/[A-Za-z0-9_-]+\.jsonl|avatars/[A-Za-z0-9_.-]+\.jpg)$`)

// parseBackup: đọc state và các file kèm theo; tên file bị lọc chặt để không ghi ra ngoài thư mục dữ liệu.
func parseBackup(data []byte) (*State, map[string][]byte, error) {
	files := map[string][]byte{}
	var raw []byte
	if bytes.HasPrefix(data, []byte("PK")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, nil, err
		}
		for _, f := range zr.File {
			if f.Name != "state.json" && !reRestoreFile.MatchString(f.Name) {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			b, err := io.ReadAll(io.LimitReader(rc, 1<<30))
			rc.Close()
			if err != nil {
				return nil, nil, err
			}
			if f.Name == "state.json" {
				raw = b
			} else {
				files[f.Name] = b
			}
		}
	} else {
		raw = data
	}
	if raw == nil {
		return nil, nil, errors.New("thiếu state.json")
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, nil, err
	}
	if len(st.Accounts) == 0 {
		return nil, nil, errors.New("không có tài khoản")
	}
	return &st, files, nil
}

// hRestoreRoute: máy chủ mới (chưa có tài khoản) thì không cần đăng nhập, còn lại chỉ Super admin.
func (s *Server) hRestoreRoute(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	fresh := len(s.st.Accounts) == 0
	s.mu.RUnlock()
	if fresh {
		if r.Header.Get("X-MAM") != "1" {
			jerr(w, 403, "Thiếu header X-MAM")
			return
		}
		s.hRestore(w, r, nil)
		return
	}
	s.authed("super", s.hRestore)(w, r)
}

// POST /api/restore?name=state-….json | ?mirror=mam-….zip | thân là file tải lên (zip hoặc json).
func (s *Server) hRestore(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method != "POST" {
		jerr(w, 405, "Sai phương thức")
		return
	}
	q := r.URL.Query()
	s.mu.RLock()
	dir, mdir := s.dir, s.st.Settings.BackupDir
	s.mu.RUnlock()
	var data []byte
	var err error
	src := "file tải lên"
	switch {
	case q.Get("name") != "":
		if !reBackupName.MatchString(q.Get("name")) {
			jerr(w, 400, "Tên file không hợp lệ")
			return
		}
		src = q.Get("name")
		data, err = os.ReadFile(filepath.Join(dir, "backup", src))
	case q.Get("mirror") != "":
		if !reMirror.MatchString(q.Get("mirror")) || mdir == "" {
			jerr(w, 400, "Tên file không hợp lệ")
			return
		}
		src = q.Get("mirror")
		data, err = os.ReadFile(filepath.Join(mdir, src))
	default:
		data, err = io.ReadAll(io.LimitReader(r.Body, 2<<30))
	}
	if err != nil {
		jerr(w, 404, "Không đọc được file sao lưu")
		return
	}
	st, files, err := parseBackup(data)
	if err != nil {
		jerr(w, 400, "File sao lưu không hợp lệ hoặc bị hỏng")
		return
	}
	s.backup() // hiện trạng trước khi khôi phục, để lỡ chọn nhầm vẫn quay lại được
	evMu.Lock()
	old, _ := filepath.Glob(filepath.Join(dir, "events", "*.jsonl"))
	for _, p := range old {
		os.Remove(p)
	}
	for n, b := range files {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if strings.HasPrefix(n, "avatars/") {
			p = filepath.Join(avatarDir, filepath.Base(n))
		}
		os.MkdirAll(filepath.Dir(p), 0o700)
		if err = os.WriteFile(p+".tmp", b, 0o600); err == nil {
			err = os.Rename(p+".tmp", p)
		}
		if err != nil {
			break
		}
	}
	evMu.Unlock()
	if err != nil {
		jerr(w, 500, "Không ghi được dữ liệu khôi phục: "+err.Error())
		return
	}
	s.mu.Lock()
	if s.st.Settings.BackupDir != "" { // giữ thư mục sao lưu đang dùng trên máy chủ này
		st.Settings.BackupDir = s.st.Settings.BackupDir
	}
	if st.Settings.TempC == 0 {
		st.Settings = defaultSettings()
	}
	s.st = *st
	s.tok = map[string]*Machine{}
	for _, m := range s.st.Machines {
		s.tok[m.TokenHash] = m
	}
	s.live = map[string]*Live{}
	s.sess = map[string]*Session{} // tài khoản có thể khác: mọi người đăng nhập lại
	s.save()
	nm, na := len(s.st.Machines), len(s.st.Accounts)
	s.mu.Unlock()
	e := AuditEntry{User: "system", Name: "Hệ thống", Role: "sys", IP: clientIP(r), Kind: "Sao lưu", Msg: "Khôi phục dữ liệu từ " + src, Target: "—"}
	if me != nil {
		e.User, e.Name, e.Role = me.User, me.Name, me.Role
	}
	s.audit(e)
	jok(w, map[string]string{"ok": "1", "machines": strconv.Itoa(nm), "accounts": strconv.Itoa(na)})
}
