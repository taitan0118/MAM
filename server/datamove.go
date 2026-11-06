package main

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// redirect.txt nằm trong thư mục khởi động (-data): ghi đường dẫn thư mục dữ liệu thật sau khi người quản trị chuyển ổ.
const redirectFile = "redirect.txt"

// resolveDir đi theo redirect.txt (tối đa 3 lần). Thư mục đích không truy cập được thì báo lỗi,
// không tự tạo dữ liệu trống (tránh tưởng mất hết tài khoản khi ổ USB/ổ mạng chưa gắn).
func resolveDir(dir string) (string, error) {
	for i := 0; i < 3; i++ {
		b, err := os.ReadFile(filepath.Join(dir, redirectFile))
		if err != nil {
			return dir, nil
		}
		next := strings.TrimSpace(string(b))
		if next == "" {
			return dir, nil
		}
		if st, err := os.Stat(next); err != nil || !st.IsDir() {
			return dir, errors.New("Dữ liệu đã chuyển tới " + next + " nhưng không truy cập được thư mục đó")
		}
		dir = next
	}
	return dir, nil
}

func within(base, p string) bool {
	r, err := filepath.Rel(base, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if st, e := os.Stat(dst); e != nil || st.Size() != n {
			err = errors.New("Chép dữ liệu không đủ dung lượng, hãy kiểm tra ổ đĩa")
		}
	}
	return err
}

// moveData chép state.json, audit.jsonl và backup/ sang thư mục mới rồi chuyển server sang đó ngay, không cần chạy lại.
// Dữ liệu cũ được giữ nguyên (không xóa) để người quản trị tự xóa sau khi đã kiểm tra.
func (s *Server) moveData(newDir string) (string, error) {
	newDir = filepath.Clean(strings.TrimSpace(newDir))
	if newDir == "." || !filepath.IsAbs(newDir) {
		return "", errors.New("Nhập đường dẫn đầy đủ, ví dụ D:\\MAMdata")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	old := s.dir
	if newDir == old || within(old, newDir) || within(newDir, old) {
		return "", errors.New("Thư mục mới phải khác và không nằm trong (hay chứa) thư mục dữ liệu hiện tại")
	}
	made := false
	if st, err := os.Stat(newDir); err == nil {
		ents, _ := os.ReadDir(newDir)
		if !st.IsDir() || len(ents) > 0 {
			return "", errors.New("Thư mục mới phải còn trống hoặc chưa tồn tại")
		}
	} else {
		made = true
	}
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		return "", errors.New("Không tạo được thư mục mới: " + err.Error())
	}
	fail := func(err error) (string, error) {
		if made {
			os.RemoveAll(newDir)
		} else {
			ents, _ := os.ReadDir(newDir)
			for _, e := range ents {
				os.RemoveAll(filepath.Join(newDir, e.Name()))
			}
		}
		return "", err
	}
	secureDir(newDir)
	s.save() // ghi những gì đang nằm trong bộ nhớ trước khi chép
	for _, f := range []string{"state.json", "audit.jsonl"} {
		if _, err := os.Stat(filepath.Join(old, f)); err != nil {
			continue
		}
		if err := copyFile(filepath.Join(old, f), filepath.Join(newDir, f)); err != nil {
			return fail(err)
		}
	}
	if files, _ := filepath.Glob(filepath.Join(old, "backup", "state-*.json")); len(files) > 0 {
		os.MkdirAll(filepath.Join(newDir, "backup"), 0o700)
		for _, f := range files {
			if err := copyFile(f, filepath.Join(newDir, "backup", filepath.Base(f))); err != nil {
				return fail(err)
			}
		}
	}
	if files, _ := filepath.Glob(filepath.Join(old, "avatars", "*.jpg")); len(files) > 0 {
		os.MkdirAll(filepath.Join(newDir, "avatars"), 0o700)
		for _, f := range files {
			if err := copyFile(f, filepath.Join(newDir, "avatars", filepath.Base(f))); err != nil {
				return fail(err)
			}
		}
	}
	rp := filepath.Join(s.boot, redirectFile)
	if err := os.WriteFile(rp+".tmp", []byte(newDir+"\n"), 0o600); err != nil {
		return fail(errors.New("Không ghi được con trỏ dữ liệu: " + err.Error()))
	}
	if err := os.Rename(rp+".tmp", rp); err != nil {
		return fail(err)
	}
	s.dir = newDir
	avatarDir = filepath.Join(newDir, "avatars")
	return old, nil
}

func (s *Server) hDataDir(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "GET" {
		s.mu.RLock()
		d := s.dir
		s.mu.RUnlock()
		jok(w, map[string]string{"dir": d})
		return
	}
	var in struct {
		Path string `json:"path"`
	}
	if r.Method != "POST" || readJSON(r, &in) != nil {
		jerr(w, 400, "Dữ liệu không hợp lệ")
		return
	}
	old, err := s.moveData(in.Path)
	if err != nil {
		jerr(w, 400, err.Error())
		return
	}
	s.audit(AuditEntry{User: me.User, Name: me.Name, Role: me.Role, IP: clientIP(r), Kind: "Cài đặt", Msg: "Chuyển thư mục dữ liệu từ " + old + " sang " + s.dir, Sens: true})
	jok(w, map[string]string{"dir": s.dir, "old": old})
}
