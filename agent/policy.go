package main

import (
	"strconv"
	"strings"
	"time"
)

// Policy: chính sách server gửi kèm mỗi lần báo cáo (luôn đầy đủ, rỗng nghĩa là không áp gì).
type Policy struct {
	USB   bool     `json:"usb"`
	Sites []string `json:"sites"`
	Apps  []string `json:"apps"`
	Hours *struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"hours"`
}

// inWindow: giờ hiện tại có nằm trong khung được dùng máy không (hỗ trợ khung qua nửa đêm).
func inWindow(now time.Time, from, to string) bool {
	m := func(s string) int {
		p := strings.Split(s, ":")
		if len(p) != 2 {
			return 0
		}
		h, _ := strconv.Atoi(p[0])
		mi, _ := strconv.Atoi(p[1])
		return h*60 + mi
	}
	cur, f, t := now.Hour()*60+now.Minute(), m(from), m(to)
	if f <= t {
		return cur >= f && cur < t
	}
	return cur >= f || cur < t
}
