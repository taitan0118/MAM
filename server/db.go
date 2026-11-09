package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	_ "github.com/ncruces/go-sqlite3/driver" // SQLite thuần Go (WebAssembly): không cần cgo, build chéo Windows/Mac/Linux như cũ
	_ "github.com/ncruces/go-sqlite3/embed"
)

// Dữ liệu nằm trong mam.db (SQLite, chế độ WAL). Trong RAM vẫn là s.st như trước; save() so từng
// bản ghi (máy, tài khoản, cảnh báo...) với lần ghi trước và chỉ ghi những dòng đổi, trong một giao dịch:
// mất điện giữa chừng thì hoặc có cả lần ghi, hoặc không có gì, không bao giờ hỏng một nửa.
// Lần đầu chạy bản dùng SQLite: nhập state.json + audit.jsonl cũ rồi đổi tên chúng thành *.migrated.

const dbFile = "mam.db"

// Các danh sách của State được tách mỗi phần tử một dòng; mọi trường còn lại nằm chung dòng "state".
var dbLists = []string{"accounts", "machines", "alerts", "commands", "schedules", "requests", "policies", "chat"}

type rowVal struct {
	pos  int
	data string
}

func openDB(dir string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", filepath.Join(dir, dbFile))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // một kết nối: ghi tuần tự, không bao giờ gặp SQLITE_BUSY giữa các goroutine
	db.SetConnMaxLifetime(0)
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=10000`,
		`CREATE TABLE IF NOT EXISTS rows(kind TEXT NOT NULL, id TEXT NOT NULL, pos INTEGER NOT NULL, data TEXT NOT NULL, PRIMARY KEY(kind, id))`,
		`CREATE TABLE IF NOT EXISTS audit(n INTEGER PRIMARY KEY, t INTEGER NOT NULL, data TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS audit_t ON audit(t)`,
	} {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

// splitState: State → các dòng (kind/id → vị trí + JSON).
func splitState(st *State) (map[string]rowVal, error) {
	b, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, err
	}
	out := map[string]rowVal{}
	for _, k := range dbLists {
		var items []json.RawMessage
		json.Unmarshal(top[k], &items) // null → rỗng
		delete(top, k)
		seen := map[string]bool{}
		for i, it := range items {
			var x struct {
				ID string `json:"id"`
			}
			json.Unmarshal(it, &x)
			id := x.ID
			if id == "" || seen[id] {
				id = "#" + strconv.Itoa(i) // phần tử không có id (hoặc trùng): khóa theo vị trí
			}
			seen[id] = true
			out[k+"\x00"+id] = rowVal{i, string(it)}
		}
	}
	var up map[string]json.RawMessage
	json.Unmarshal(top["uptime"], &up)
	delete(top, "uptime")
	for id, v := range up {
		out["uptime\x00"+id] = rowVal{0, string(v)}
	}
	rest, _ := json.Marshal(top)
	out["state\x00"] = rowVal{0, string(rest)}
	return out, nil
}

// loadState: các dòng → State. Trả ok=false khi DB còn trống.
func loadState(db *sql.DB, st *State) (map[string]rowVal, bool, error) {
	rs, err := db.Query(`SELECT kind, id, pos, data FROM rows ORDER BY kind, pos`)
	if err != nil {
		return nil, false, err
	}
	defer rs.Close()
	cache := map[string]rowVal{}
	lists := map[string][]json.RawMessage{}
	up := map[string]json.RawMessage{}
	var base []byte
	for rs.Next() {
		var kind, id, data string
		var pos int
		if err := rs.Scan(&kind, &id, &pos, &data); err != nil {
			return nil, false, err
		}
		cache[kind+"\x00"+id] = rowVal{pos, data}
		switch kind {
		case "state":
			base = []byte(data)
		case "uptime":
			up[id] = json.RawMessage(data)
		default:
			lists[kind] = append(lists[kind], json.RawMessage(data))
		}
	}
	if err := rs.Err(); err != nil {
		return nil, false, err
	}
	if base == nil {
		return nil, false, nil
	}
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(base, &top); err != nil {
		return nil, false, err
	}
	for k, v := range lists {
		top[k], _ = json.Marshal(v)
	}
	if len(up) > 0 {
		top["uptime"], _ = json.Marshal(up)
	}
	b, _ := json.Marshal(top)
	if err := json.Unmarshal(b, st); err != nil {
		return nil, false, err
	}
	return cache, true, nil
}

// writeRows ghi phần khác giữa cache và cur trong một giao dịch; thành công thì cache = cur.
func writeRows(db *sql.DB, cache, cur map[string]rowVal) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range cur {
		if o, ok := cache[k]; ok && o == v {
			continue
		}
		kind, id, _ := cutKey(k)
		if _, err := tx.Exec(`INSERT INTO rows(kind,id,pos,data) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET pos=excluded.pos, data=excluded.data`, kind, id, v.pos, v.data); err != nil {
			return err
		}
	}
	for k := range cache {
		if _, ok := cur[k]; !ok {
			kind, id, _ := cutKey(k)
			if _, err := tx.Exec(`DELETE FROM rows WHERE kind=? AND id=?`, kind, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func cutKey(k string) (string, string, bool) {
	if i := bytes.IndexByte([]byte(k), 0); i >= 0 {
		return k[:i], k[i+1:], true
	}
	return k, "", false
}

// migrateJSON nhập dữ liệu kiểu cũ (state.json, audit.jsonl) vào DB trống. Gọi một lần khi khởi động.
func (s *Server) migrateJSON() error {
	p := filepath.Join(s.dir, "state.json")
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &s.st); err != nil {
		return errors.New("state.json hỏng, không nhập được vào mam.db: " + err.Error())
	}
	cur, err := splitState(&s.st)
	if err != nil {
		return err
	}
	if err := writeRows(s.db, map[string]rowVal{}, cur); err != nil {
		return err
	}
	s.rows = cur
	ap := filepath.Join(s.dir, "audit.jsonl")
	if ab, err := os.ReadFile(ap); err == nil {
		if err := s.importAudit(ab); err != nil {
			return err
		}
		os.Rename(ap, ap+".migrated")
	}
	os.Rename(p, p+".migrated") // giữ lại để đối chiếu, không xóa
	return nil
}

// ---------- nhật ký thao tác ----------

func (s *Server) insertAudit(e AuditEntry) error {
	b, _ := json.Marshal(e)
	_, err := s.db.Exec(`INSERT INTO audit(t,data) VALUES(?,?)`, e.T.UnixNano(), string(b))
	return err
}

// importAudit thay toàn bộ nhật ký bằng nội dung một file audit.jsonl (nhập lần đầu, khôi phục sao lưu).
func (s *Server) importAudit(b []byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM audit`); err != nil {
		return err
	}
	for _, ln := range bytes.Split(b, []byte("\n")) {
		var e AuditEntry
		if len(bytes.TrimSpace(ln)) == 0 || json.Unmarshal(ln, &e) != nil {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO audit(t,data) VALUES(?,?)`, e.T.UnixNano(), string(bytes.TrimSpace(ln))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// exportAudit: nhật ký dạng audit.jsonl (mỗi dòng một mục, cũ trước) cho file sao lưu.
func (s *Server) exportAudit() []byte {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	var buf bytes.Buffer
	rs, err := s.db.Query(`SELECT data FROM audit ORDER BY n`)
	if err != nil {
		return nil
	}
	defer rs.Close()
	for rs.Next() {
		var d string
		if rs.Scan(&d) == nil {
			buf.WriteString(d)
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes()
}

// snapshotDB chép DB đang chạy thành một file độc lập, nhất quán (VACUUM INTO), dùng khi chuyển thư mục dữ liệu.
func (s *Server) snapshotDB(dst string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, dst)
	return err
}
