package main

import (
	"encoding/json"
	"testing"
)

// Chấm sức khỏe ổ: số liệu lấy từ máy thật của đại ca (SSD Kingston còn 37% tuổi thọ mà Windows vẫn báo Healthy).
func TestRateDisk(t *testing.T) {
	cases := []struct {
		ten    string
		d      map[string]interface{}
		health string
		wear   interface{}
	}{
		{"SSD mòn nhiều", map[string]interface{}{"smart": true, "type": "SSD", "lifeLeft": 37.0, "health": "Healthy"}, "Cần chú ý", 63.0},
		{"SSD còn tốt", map[string]interface{}{"smart": true, "type": "SSD", "lifeLeft": 95.0, "health": "Healthy"}, "Tốt", 5.0},
		{"HDD sạch", map[string]interface{}{"smart": true, "realloc": 0.0, "pending": 0.0, "offline": 0.0, "health": "Healthy"}, "Tốt", nil},
		{"HDD có khối chờ thay", map[string]interface{}{"smart": true, "pending": 3.0, "health": "Healthy"}, "Sắp hỏng", nil},
		{"HDD đã thay khối", map[string]interface{}{"smart": true, "realloc": 8.0, "health": "Healthy"}, "Cần chú ý", nil},
		{"Windows báo sắp hỏng", map[string]interface{}{"smart": true, "predictFail": true, "health": "Healthy"}, "Sắp hỏng", nil},
	}
	for _, c := range cases {
		if !rateDisk(c.d) {
			t.Fatalf("%s: không chấm", c.ten)
		}
		if c.d["health"] != c.health {
			t.Errorf("%s: health = %v, muốn %v", c.ten, c.d["health"], c.health)
		}
		if c.wear != nil && c.d["wear"] != c.wear {
			t.Errorf("%s: wear = %v, muốn %v", c.ten, c.d["wear"], c.wear)
		}
	}
	// Số khối hỏng tới hàng triệu là agent đọc lố bảng S.M.A.R.T (ổ Seagate trên VN10531 trả 0xFF010000):
	// bỏ qua, đừng báo "Sắp hỏng" oan.
	rac := map[string]interface{}{"smart": true, "pending": 4278255616.0, "offline": 4278255616.0, "health": "Healthy"}
	rateDisk(rac)
	if rac["health"] != "Tốt" || rac["note"] != "" {
		t.Errorf("số rác vẫn bị tính: %v / %q", rac["health"], rac["note"])
	}
	// Ổ không đọc được S.M.A.R.T: giữ nguyên trạng thái Windows, không đoán.
	no := map[string]interface{}{"health": "Healthy", "model": "Virtual Disk"}
	if rateDisk(no) || no["health"] != "Healthy" {
		t.Errorf("ổ không có S.M.A.R.T bị sửa: %v", no)
	}
	// Ổ sắp hỏng phải nêu lý do để admin biết đường thay.
	bad := map[string]interface{}{"smart": true, "pending": 3.0, "uncorrect": 2.0}
	rateDisk(bad)
	if bad["note"] != "3 khối chờ thay, 2 lỗi không sửa được" {
		t.Errorf("note = %q", bad["note"])
	}
}

func TestRateDisksTrongHw(t *testing.T) {
	hw := map[string]interface{}{"disks": []interface{}{
		map[string]interface{}{"smart": true, "lifeLeft": 10.0},
		map[string]interface{}{"health": "OK"},
		"rác",
	}}
	rateDisks(hw)
	ds := hw["disks"].([]interface{})
	if d := ds[0].(map[string]interface{}); d["health"] != "Cần chú ý" {
		t.Errorf("ổ 1: %v", d["health"])
	}
	if d := ds[1].(map[string]interface{}); d["health"] != "OK" {
		t.Errorf("ổ 2 bị sửa: %v", d["health"])
	}
}

// Bảng "Kiểm tra S.M.A.R.T ngay" phải nói đúng như bảng Ổ cứng vật lý: cùng một hàm chấm.
func TestRateDiskList(t *testing.T) {
	out := rateDiskList([]byte(`[{"model":"KINGSTON","smart":true,"type":"SSD","lifeLeft":37,"health":"Healthy"},{"model":"USB DISK","health":"Healthy"}]`))
	var ds []map[string]interface{}
	if err := json.Unmarshal(out, &ds); err != nil {
		t.Fatalf("kết quả không đọc được: %v", err)
	}
	if ds[0]["health"] != "Cần chú ý" || ds[0]["note"] != "tuổi thọ còn 37%" {
		t.Errorf("ổ SSD: %v / %v", ds[0]["health"], ds[0]["note"])
	}
	if ds[1]["health"] != "Healthy" {
		t.Errorf("ổ không có S.M.A.R.T bị sửa: %v", ds[1]["health"])
	}
	// Dữ liệu lạ thì trả nguyên chứ không làm hỏng kết quả lệnh.
	if s := string(rateDiskList([]byte("không phải JSON"))); s != "không phải JSON" {
		t.Errorf("dữ liệu lạ bị đổi: %q", s)
	}
}
