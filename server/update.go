package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Cập nhật agent từ server: server giữ mam-agent.exe (cùng thư mục mam-server), agent hỏi phiên bản mỗi lần báo cáo.
var reAgentVer = regexp.MustCompile(`MAMVER=(\d+\.\d+\.\d+)`)

type agentBin struct {
	path string
	mod  int64
	size int64
	ver  string
	sha  string
}

var abMu sync.Mutex
var abCache = map[string]*agentBin{}

// agentBinary trả thông tin file agent cho kiến trúc arch ("amd64" hoặc "386"), nil nếu không có.
func (s *Server) agentBinary(arch string) *agentBin {
	name := "mam-agent.exe"
	if arch == "386" {
		name = "mam-agent-x86.exe"
	}
	var path string
	cands := []string{filepath.Join(s.dir, name)}
	if self, err := os.Executable(); err == nil {
		cands = append([]string{filepath.Join(filepath.Dir(self), name)}, cands...)
	}
	var st os.FileInfo
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			path, st = c, fi
			break
		}
	}
	if path == "" {
		return nil
	}
	abMu.Lock()
	defer abMu.Unlock()
	if c := abCache[path]; c != nil && c.mod == st.ModTime().UnixNano() && c.size == st.Size() {
		return c
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	m := reAgentVer.FindSubmatch(b)
	if m == nil {
		return nil
	}
	h := sha256.Sum256(b)
	c := &agentBin{path, st.ModTime().UnixNano(), st.Size(), string(m[1]), hex.EncodeToString(h[:])}
	abCache[path] = c
	return c
}

func verNum(v string) int {
	p := strings.Split(v, ".")
	n := 0
	for i := 0; i < 3; i++ {
		x := 0
		if i < len(p) {
			x, _ = strconv.Atoi(p[i])
		}
		n = n*1000 + x
	}
	return n
}

// updateFor: thông tin cập nhật gửi cho agent trong phản hồi báo cáo, nil nếu không cần.
func (s *Server) updateFor(cur, arch string) map[string]interface{} {
	if s.st.Settings.NoAutoUpdate || cur == "" {
		return nil
	}
	b := s.agentBinary(arch)
	if b == nil || verNum(b.ver) <= verNum(cur) {
		return nil
	}
	return map[string]interface{}{"ver": b.ver, "sha": b.sha, "size": b.size}
}

func (s *Server) hAgentUpdate(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	m := s.agentMachine(r)
	s.mu.RUnlock()
	if m == nil {
		jerr(w, 401, "Token không hợp lệ")
		return
	}
	b := s.agentBinary(r.URL.Query().Get("arch"))
	if b == nil {
		jerr(w, 404, "Server không có file agent")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, b.path)
}

// hAgentUpdateInfo: Console xem phiên bản agent trên server và bao nhiêu máy đang dùng bản cũ; bật/tắt tự cập nhật.
func (s *Server) hAgentUpdateInfo(w http.ResponseWriter, r *http.Request, me *Account) {
	if r.Method == "PUT" {
		if me.Role != "super" {
			jerr(w, 403, "Chỉ Super admin đổi cài đặt")
			return
		}
		var in struct{ Auto bool }
		if readJSON(r, &in) != nil {
			jerr(w, 400, "Dữ liệu không hợp lệ")
			return
		}
		s.mu.Lock()
		s.st.Settings.NoAutoUpdate = !in.Auto
		s.save()
		s.mu.Unlock()
		s.auditBy(me, r, "Cài đặt", "Tự cập nhật agent: "+onoff(in.Auto), "—", true)
		jok(w, map[string]bool{"ok": true})
		return
	}
	b := s.agentBinary("amd64")
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]interface{}{"auto": !s.st.Settings.NoAutoUpdate, "serverVer": Version, "agentVer": "", "outdated": 0}
	if b != nil {
		out["agentVer"] = b.ver
		n := 0
		for _, m := range s.st.Machines {
			if l := s.live[m.ID]; !m.Revoked && l != nil && l.Ver != "" && verNum(l.Ver) < verNum(b.ver) {
				n++
			}
		}
		out["outdated"] = n
	}
	jok(w, out)
}
