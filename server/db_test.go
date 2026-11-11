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
	defer s2.db.Close()
	if len(s2.st.Machines) != 1 || s2.st.Machines[0].Name != "PC-1-moi" || strings.Join(s2.st.Groups, ",") != "Kho,Tầng 2" || len(s2.st.Chat) != 1 {
		t.Fatalf("mở lại sai: %+v", s2.st)
	}
	if n := s2.purgeAudit(func(x time.Time) bool { return x.Year() == 2026 && x.Month() == 1 }, false); n != 1 || len(s2.readAudit(0)) != 1 {
		t.Fatalf("dọn nhật ký: xóa %d", n)
	}
}

// Phần cứng và từng lần bật máy nằm dòng riêng: báo cáo thường chỉ ghi dòng máy và dòng lần bật đang mở.
func TestSQLiteSplitRows(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"accounts":[{"id":"a1","user":"admin","role":"super"}],
		"machines":[{"id":"m1","name":"PC-1","hw":{"apps":[{"n":"Excel"}]}}],
		"settings":{"temp":80},"uptime":{"m1":[[100,200],[300,400]]}}`), 0o600)
	s, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	before := map[string]rowVal{}
	for k, v := range s.rows {
		before[k] = v
	}
	s.st.Machines[0].LastSeen = time.Now()
	s.st.Uptime["m1"][1][1] = 460 // lần bật đang mở kéo dài thêm
	s.save()
	s.mu.Unlock()
	changed := []string{}
	for k, v := range s.rows {
		if before[k] != v {
			changed = append(changed, strings.ReplaceAll(k, "\x00", ":"))
		}
	}
	if len(changed) != 2 {
		t.Fatalf("chỉ được ghi dòng máy + dòng lần bật đang mở, lại ghi: %v", changed)
	}
	s.db.Close()
	s2, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.db.Close()
	m := s2.st.Machines[0]
	if apps, _ := m.Hw["apps"].([]interface{}); len(apps) != 1 {
		t.Fatalf("mất phần cứng khi mở lại: %+v", m.Hw)
	}
	if u := s2.st.Uptime["m1"]; len(u) != 2 || u[0] != [2]int64{100, 200} || u[1] != [2]int64{300, 460} {
		t.Fatalf("lịch sử bật sai: %v", u)
	}
}

// Ghi mam.db lỗi (ổ đầy, file bị khóa): phải mở cảnh báo máy chủ, lưu lại được thì tự đóng và không mất dữ liệu.
func TestSaveErrorAlert(t *testing.T) {
	dir := t.TempDir()
	s, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	open := func() bool {
		for _, a := range s.st.Alerts {
			if a.Machine == "__server" && a.Kind == "save" && a.Status == "open" {
				return true
			}
		}
		return false
	}
	s.db.Close() // từ đây mọi lần ghi đều lỗi
	s.mu.Lock()
	s.st.Groups = append(s.st.Groups, "Kho")
	s.save()
	s.save()
	s.mu.Unlock()
	if !open() || !s.saveBad || len(s.st.Alerts) != 1 {
		t.Fatalf("ghi lỗi phải mở đúng một cảnh báo máy chủ: %+v", s.st.Alerts)
	}
	if s.db, err = openDB(dir); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.save()
	s.mu.Unlock()
	if open() || s.saveBad {
		t.Fatal("lưu lại được thì cảnh báo phải tự đóng")
	}
	s.db.Close()
	s2, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.db.Close()
	if strings.Join(s2.st.Groups, ",") != "Kho" {
		t.Fatalf("dữ liệu ghi bù bị mất: %v", s2.st.Groups)
	}
}

// Server chết giữa lúc nâng cấp (đã nhập state.json, chưa nhập audit.jsonl): lần bật sau phải nhập nốt nhật ký.
func TestMigrateAuditLeftover(t *testing.T) {
	dir := t.TempDir()
	s, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.save()
	s.mu.Unlock()
	s.db.Close()
	ap := filepath.Join(dir, "audit.jsonl")
	os.WriteFile(ap, []byte(`{"t":"2026-01-01T00:00:00Z","user":"admin","msg":"cũ"}`+"\n"), 0o600)
	s2, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a := s2.readAudit(0); len(a) != 1 || a[0].Msg != "cũ" {
		t.Fatalf("chưa nhập nhật ký còn sót: %+v", a)
	}
	if _, err := os.Stat(ap + ".migrated"); err != nil {
		t.Fatal("audit.jsonl phải đổi tên thành .migrated")
	}
	// bảng nhật ký đã có dữ liệu thì không được ghi đè bằng file lạ
	s2.db.Close()
	os.WriteFile(ap, []byte(`{"t":"2026-02-01T00:00:00Z","user":"x","msg":"lạ"}`+"\n"), 0o600)
	s3, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s3.db.Close()
	if a := s3.readAudit(0); len(a) != 1 || a[0].Msg != "cũ" {
		t.Fatalf("nhật ký đang có bị ghi đè: %+v", a)
	}
}
