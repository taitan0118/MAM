package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Dữ liệu kiểu cũ (state.json + audit.jsonl) được nhập vào mam.db; save() chỉ ghi dòng đổi; xóa thì mất dòng.
func TestSQLiteMigrateAndSave(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"accounts":[{"id":"a1","user":"admin","role":"super"}],
		"groups":["Kho"],"machines":[{"id":"m1","name":"PC-1"},{"id":"m2","name":"PC-2"}],
		"settings":{"temp":80},"chat":[{"text":"chào"}],"uptime":{"m1":[[1,2]]},"contactLabel":"Zalo"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "audit.jsonl"), []byte(`{"t":"2026-01-01T00:00:00Z","user":"admin","msg":"cũ"}`+"\n"), 0o600)
	s, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.st.Machines) != 2 || s.st.Settings.TempC != 80 || s.st.ContactLabel != "Zalo" || len(s.st.Uptime["m1"]) != 1 {
		t.Fatalf("nhập thiếu: %+v", s.st)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json.migrated")); err != nil {
		t.Fatal("phải giữ state.json cũ dưới tên .migrated")
	}
	s.audit(AuditEntry{User: "admin", Msg: "mới"})
	if a := s.readAudit(0); len(a) != 2 || a[0].Msg != "cũ" || a[1].Msg != "mới" {
		t.Fatalf("nhật ký sai: %+v", a)
	}
	if a := s.readAudit(1); len(a) != 1 || a[0].Msg != "mới" {
		t.Fatalf("readAudit(1) phải lấy mục mới nhất: %+v", a)
	}

	// đổi một máy, xóa một máy, thêm nhóm: chỉ các dòng liên quan được ghi
	s.mu.Lock()
	s.st.Machines[0].Name = "PC-1-moi"
	s.st.Machines = s.st.Machines[:1]
	s.st.Groups = append(s.st.Groups, "Tầng 2")
	before := map[string]rowVal{}
	for k, v := range s.rows {
		before[k] = v
	}
	s.save()
	s.mu.Unlock()
	changed := 0
	for k, v := range s.rows {
		if before[k] != v {
			changed++
		}
	}
	if changed != 2 || len(s.rows) != len(before)-1 { // dòng máy m1 + dòng "state" (nhóm); dòng m2 mất
		t.Fatalf("ghi %d dòng, còn %d/%d dòng", changed, len(s.rows), len(before))
	}

	s.db.Close()
	s2, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.st.Machines) != 1 || s2.st.Machines[0].Name != "PC-1-moi" || strings.Join(s2.st.Groups, ",") != "Kho,Tầng 2" || len(s2.st.Chat) != 1 {
		t.Fatalf("mở lại sai: %+v", s2.st)
	}
	if n := s2.purgeAudit(func(x time.Time) bool { return x.Year() == 2026 && x.Month() == 1 }, false); n != 1 || len(s2.readAudit(0)) != 1 {
		t.Fatalf("dọn nhật ký: xóa %d", n)
	}
}
