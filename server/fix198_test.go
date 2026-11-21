package main

import (
	"testing"
	"time"
)

func mocGia(dev, trang string, at time.Time) *DevEvent {
	return &DevEvent{ID: dev + at.String(), Dev: dev, Up: trang != "down", At: at, Trang: trang}
}

func trangs(l []*DevEvent, dev string) []string {
	var r []string
	for _, e := range l {
		if e.Dev == dev {
			r = append(r, mocTrang(e))
		}
	}
	return r
}

// Lỗi sửa ở 1.1.98 đẻ ra một cặp mốc "treo rồi hoạt động lại" giả sau mỗi lượt đo luồng hình.
// Dọn xong lịch sử của camera đó phải sạch, chỉ còn đúng một mốc hoạt động ban đầu.
func TestDonMocTreoGia(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 16, 5, 0, 0, time.Local)
	s := &Server{st: State{}}
	for i := 0; i < 4; i++ { // bốn vòng giả: đo xong là up, phút sau thành novideo
		s.st.DevLog = append(s.st.DevLog,
			mocGia("cam1", "novideo", t0.Add(time.Duration(i)*5*time.Minute+time.Minute)),
			mocGia("cam1", "up", t0.Add(time.Duration(i+1)*5*time.Minute)))
	}
	s.st.DevLog = append([]*DevEvent{mocGia("cam1", "up", t0)}, s.st.DevLog...)
	if n := s.donMocTreoGia(); n != 8 {
		t.Fatalf("phải dọn 8 mốc giả, dọn %d", n)
	}
	if got := trangs(s.st.DevLog, "cam1"); len(got) != 1 || got[0] != "up" {
		t.Fatalf("phải còn đúng một mốc hoạt động, còn %v", got)
	}
}

// Treo THẬT (kéo dài hơn một chu kỳ đo) và mất kết nối thật thì không được đụng tới.
func TestDonMocTreoGiaGiuMocThat(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 8, 0, 0, 0, time.Local)
	s := &Server{st: State{DevLog: []*DevEvent{
		mocGia("cam1", "up", t0),
		mocGia("cam1", "novideo", t0.Add(time.Minute)), // treo suốt nửa tiếng: thật
		mocGia("cam1", "up", t0.Add(31*time.Minute)),
		mocGia("cam1", "down", t0.Add(40*time.Minute)),
		mocGia("cam1", "up", t0.Add(50*time.Minute)),
	}}}
	if n := s.donMocTreoGia(); n != 0 {
		t.Fatalf("không được dọn mốc thật, đã dọn %d", n)
	}
	if got := trangs(s.st.DevLog, "cam1"); len(got) != 5 {
		t.Fatalf("phải giữ đủ 5 mốc, còn %v", got)
	}
}

// Nhiều thiết bị ghi xen kẽ nhau trong cùng một danh sách: không được lẫn mốc của máy này sang máy kia.
func TestDonMocTreoGiaKhongLanThietBi(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 9, 0, 0, 0, time.Local)
	s := &Server{st: State{DevLog: []*DevEvent{
		mocGia("cam1", "up", t0),
		mocGia("cam2", "down", t0),
		mocGia("cam1", "novideo", t0.Add(time.Minute)), // giả: kẹp giữa hai mốc up của cam1
		mocGia("cam2", "up", t0.Add(2*time.Minute)),
		mocGia("cam1", "up", t0.Add(5*time.Minute)),
	}}}
	if n := s.donMocTreoGia(); n != 2 {
		t.Fatalf("phải dọn 2 mốc của cam1, dọn %d", n)
	}
	if got := trangs(s.st.DevLog, "cam1"); len(got) != 1 || got[0] != "up" {
		t.Fatalf("cam1 phải còn một mốc up, còn %v", got)
	}
	if got := trangs(s.st.DevLog, "cam2"); len(got) != 2 || got[0] != "down" || got[1] != "up" {
		t.Fatalf("cam2 phải nguyên vẹn, còn %v", got)
	}
}
