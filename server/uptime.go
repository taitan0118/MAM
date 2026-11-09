package main

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Thời gian hoạt động: mỗi máy giữ danh sách các lần bật [bắt đầu, kết thúc] (giây unix) trong State.Uptime.
// Máy coi là đang bật khi agent còn báo về; mất liên lạc quá upGap giây là tắt.
// Agent gửi sẵn uptime nên khởi động lại nhanh (dưới upGap) vẫn tách thành lần bật mới.
const (
	upGap  = 120
	upKeep = 366 * 24 * 3600 // giữ chi tiết 1 năm
)

// upMark: gọi khi đang giữ s.mu, mỗi lần agent báo cáo. Trả true khi mở lần bật mới.
func (s *Server) upMark(id string, now, uptime int64) bool {
	if s.st.Uptime == nil {
		s.st.Uptime = map[string][][2]int64{}
	}
	ss := s.st.Uptime[id]
	boot := int64(0)
	if uptime > 0 {
		boot = now - uptime
	}
	n := len(ss)
	if n > 0 && now-ss[n-1][1] <= upGap && !(boot > ss[n-1][1]-10) {
		ss[n-1][1] = now
		return false
	}
	start := now
	// máy vừa khởi động: lấy giờ khởi động thật làm giờ bật (agent thường báo trễ vài chục giây)
	if boot > 0 && now-boot < 600 && (n == 0 || boot >= ss[n-1][1]) {
		start = boot
	}
	ss = append(ss, [2]int64{start, now})
	for len(ss) > 0 && ss[0][1] < now-upKeep {
		ss = ss[1:]
	}
	s.st.Uptime[id] = ss
	return true
}

// GET /api/uptime?id=<máy> hoặc ?group=<nhóm>&from=<unix>&to=<unix>: các lần bật chạm vào khoảng [from, to].
func (s *Server) hUptime(w http.ResponseWriter, r *http.Request, me *Account) {
	q := r.URL.Query()
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, err := strconv.ParseInt(q.Get("to"), 10, 64)
	if err != nil || to <= 0 {
		to = time.Now().Unix()
	}
	id := q.Get("id")
	s.mu.RLock()
	defer s.mu.RUnlock()
	inG := s.groupPred(me, q.Get("group"))
	out := []map[string]interface{}{}
	for _, m := range s.st.Machines {
		if m.Revoked || !canSee(me, m) || (id != "" && m.ID != id) || (id == "" && !inG(m.ID)) {
			continue
		}
		ss := [][2]int64{}
		for _, x := range s.st.Uptime[m.ID] {
			if x[1] >= from && x[0] <= to {
				ss = append(ss, x)
			}
		}
		out = append(out, map[string]interface{}{"id": m.ID, "name": m.Name, "group": m.Group, "s": ss})
	}
	if id != "" && len(out) == 0 {
		jerr(w, 404, "Không có máy này")
		return
	}
	jok(w, map[string]interface{}{"now": time.Now().Unix(), "gap": upGap, "machines": out})
}

// uptimeRows: mỗi máy mỗi ngày (giờ máy chủ) một dòng, cho xuất Excel/CSV. Gọi khi đang giữ s.mu.
func (s *Server) uptimeRows(me *Account, group string, from, to time.Time) [][]string {
	inG := s.groupPred(me, group)
	now := time.Now().Unix()
	rows := [][]string{{"Tên máy", "Nhóm", "Ngày", "Bật lúc", "Tắt lúc", "Số lần bật", "Tổng giờ bật"}}
	hm := func(u int64) string { return time.Unix(u, 0).Format("15:04") }
	for _, m := range s.st.Machines {
		if m.Revoked || !inG(m.ID) {
			continue
		}
		all := s.st.Uptime[m.ID]
		for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
			a, b := d.Unix(), d.AddDate(0, 0, 1).Unix()
			var tot, first, last int64
			cnt, live := 0, false
			for _, x := range all {
				st, en := x[0], x[1]
				on := now-en <= upGap
				if on {
					en = now
				}
				st, en = max(st, a), min(en, b)
				if en <= st {
					continue
				}
				if cnt == 0 {
					first = st
				}
				cnt++
				tot += en - st
				last, live = en, on && en == now
			}
			if cnt == 0 {
				continue
			}
			end := hm(last)
			if live {
				end = "Đang bật"
			} else if last == b {
				end = "24:00"
			}
			rows = append(rows, []string{m.Name, m.Group, d.Format("02/01/2006"), hm(first), end, strconv.Itoa(cnt), fmt.Sprintf("%.2f", float64(tot)/3600)})
		}
	}
	return rows
}
