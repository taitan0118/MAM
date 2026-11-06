package main

import (
	"fmt"
	"time"
)

// checkMachine chạy mỗi lần máy báo cáo. Gọi khi đang giữ s.mu.
func (s *Server) checkMachine(m *Machine, l *Live) {
	cfg := s.st.Settings
	if l.CPU >= cfg.CPU {
		l.CPUHigh++
	} else {
		l.CPUHigh = 0
	}
	s.cond(m, "cpu", "warn", l.CPUHigh >= 6, fmt.Sprintf("CPU %.0f%% liên tục (ngưỡng %.0f%%)", l.CPU, cfg.CPU))
	hot := l.Temp != nil && *l.Temp >= cfg.TempC
	tmsg := ""
	if l.Temp != nil {
		tmsg = fmt.Sprintf("Nhiệt độ CPU %.0f°C (ngưỡng %.0f°C)", *l.Temp, cfg.TempC)
	}
	s.cond(m, "temp", "crit", hot, tmsg)
	low, dmsg := false, ""
	for _, d := range l.Disks {
		if d.TotalGB > 0 && d.FreeGB/d.TotalGB*100 <= cfg.DiskFreePc {
			low = true
			dmsg = fmt.Sprintf("Ổ %s chỉ còn %.0f%% trống (ngưỡng %.0f%%)", d.Name, d.FreeGB/d.TotalGB*100, cfg.DiskFreePc)
			break
		}
	}
	s.cond(m, "disk", "warn", low, dmsg)
	s.cond(m, "offline", "crit", false, "") // máy đã báo lại: đóng cảnh báo mất kết nối
}

// cond mở cảnh báo khi điều kiện đúng, tự đóng khi hết đúng.
func (s *Server) cond(m *Machine, kind, sev string, on bool, msg string) {
	var open *Alert
	for i := len(s.st.Alerts) - 1; i >= 0 && i > len(s.st.Alerts)-400; i-- {
		a := s.st.Alerts[i]
		if a.Machine == m.ID && a.Kind == kind && a.Status == "open" {
			open = a
			break
		}
	}
	switch {
	case on && open == nil:
		s.st.Alerts = append(s.st.Alerts, &Alert{ID: rid(5), Machine: m.ID, Name: m.Name, Kind: kind, Sev: sev, Msg: msg, Opened: time.Now(), Status: "open"})
		if len(s.st.Alerts) > 2000 {
			s.st.Alerts = s.st.Alerts[len(s.st.Alerts)-1500:]
		}
		s.save()
		s.notify(s.st.Settings, "Cảnh báo: "+m.Name, msg)
	case !on && open != nil:
		open.Status, open.Closed, open.By = "closed", time.Now(), "tự động"
		s.save()
	}
}

// watchdog chạy nền: mất kết nối, lệnh treo, lưu lastSeen, sao lưu hằng ngày.
func (s *Server) watchdog() {
	tick := time.NewTicker(15 * time.Second)
	lastSave, lastBackup := time.Now(), time.Time{}
	for range tick.C {
		s.mu.Lock()
		cfg := s.st.Settings
		now := time.Now()
		for _, m := range s.st.Machines {
			if m.Revoked || m.LastSeen.IsZero() {
				continue
			}
			gap := now.Sub(m.LastSeen)
			if gap > time.Duration(cfg.OfflineMin*float64(time.Minute)) {
				s.cond(m, "offline", "crit", true, fmt.Sprintf("Mất kết nối %.0f phút", gap.Minutes()))
			}
		}
		for _, c := range s.st.Commands {
			if c.Status == "sent" && now.Sub(c.Created) > 3*time.Minute {
				c.Status, c.Result, c.Done = "failed", "Agent không phản hồi", now
			}
			if c.Status == "pending" && now.Sub(c.Created) > 3*time.Minute {
				c.Status, c.Result, c.Done = "failed", "Máy không nhận lệnh (offline)", now
			}
		}
		s.sweep(now)
		s.sweepScreens(now)
		for k, v := range s.sess {
			if now.After(v.Exp) {
				delete(s.sess, k)
			}
		}
		if now.Sub(lastSave) > time.Minute {
			s.save()
			lastSave = now
		}
		s.mu.Unlock()
		if now.Sub(lastBackup) > 24*time.Hour {
			s.backup()
			lastBackup = now
		}
	}
}

// ThreatIn là một phát hiện virus/nghi ngờ do agent gửi lên (xem agent/threat.go).
type ThreatIn struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Cause  string `json:"cause"`
	Status string `json:"status"`
}

// threatsIn mở cảnh báo cho mỗi phát hiện mới (theo ID, mỗi ID chỉ báo một lần). Cảnh báo virus
// không tự đóng: người quản trị phải xem và đóng tay. Gọi khi đang giữ s.mu.
func (s *Server) threatsIn(m *Machine, in []ThreatIn) {
	if len(in) > 30 {
		in = in[:30]
	}
	for _, t := range in {
		if t.ID == "" || len(t.ID) > 40 || t.Name == "" {
			continue
		}
		seen := false
		for _, id := range m.ThreatSeen {
			if id == t.ID {
				seen = true
				break
			}
		}
		if seen {
			continue
		}
		m.ThreatSeen = append(m.ThreatSeen, t.ID)
		if len(m.ThreatSeen) > 300 {
			m.ThreatSeen = m.ThreatSeen[len(m.ThreatSeen)-200:]
		}
		kind, sev, msg := "suspect", "warn", "Nghi ngờ mã độc: "+clip(t.Name, 120)
		if t.Kind == "virus" {
			kind, sev = "virus", "crit"
			msg = "Phát hiện virus " + clip(t.Name, 120)
			if t.Detail != "" {
				msg += " tại " + clip(t.Detail, 200)
			}
		} else if t.Detail != "" {
			msg += " - " + clip(t.Detail, 200)
		}
		if t.Status != "" {
			msg += " [" + clip(t.Status, 60) + "]"
		}
		if t.Kind == "virus" && t.Cause != "" {
			msg += ". Nguyên nhân có thể: " + clip(t.Cause, 160)
		}
		s.st.Alerts = append(s.st.Alerts, &Alert{ID: rid(5), Machine: m.ID, Name: m.Name, Kind: kind, Sev: sev, Msg: msg, Opened: time.Now(), Status: "open"})
		if len(s.st.Alerts) > 2000 {
			s.st.Alerts = s.st.Alerts[len(s.st.Alerts)-1500:]
		}
		s.save()
		s.notify(s.st.Settings, "Cảnh báo: "+m.Name, msg)
	}
}

// patchCheck cảnh báo khi máy Windows hơn 90 ngày chưa cài bản vá nào (lastPatch do agent đọc từ Get-HotFix);
// tự đóng khi đã vá. Gọi khi đang giữ s.mu.
func (s *Server) patchCheck(m *Machine) {
	lp, _ := m.Hw["lastPatch"].(string)
	t, err := time.Parse("2006-01-02", lp)
	if err != nil {
		return
	}
	days := int(time.Since(t).Hours() / 24)
	s.cond(m, "patch", "warn", days > 90, fmt.Sprintf("Máy chưa cài bản vá Windows nào trong %d ngày (lần cuối %s)", days, lp))
}
