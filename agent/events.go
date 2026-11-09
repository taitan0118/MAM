package main

// Lỗi của máy: mỗi 10 phút đọc nhật ký sự kiện Windows (mức Nghiêm trọng/Lỗi) kể từ lần đọc trước,
// gom lại rồi gửi kèm báo cáo, mỗi lần tối đa 200 mục. Lần đầu đọc lùi 180 ngày.
// Mốc "đã đọc tới đâu" chỉ ghi ra file khi mọi mục đã gửi xong, nên agent tắt giữa chừng thì lần sau đọc lại
// (server bỏ mục trùng theo ID).
import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SysEvent struct {
	ID  string `json:"id"`
	At  int64  `json:"at"`
	Log string `json:"log"`
	Src string `json:"src"`
	EID int    `json:"eid"`
	Lvl int    `json:"lvl"`
	Msg string `json:"msg"`
}

var sysEv struct {
	sync.Mutex
	pending []SysEvent
	next    int64 // mốc sẽ ghi ra file khi pending gửi hết
}

func evSinceFile() string { return filepath.Join(filepath.Dir(*cfgPath), "events-since") }

func toEvents(b []byte) []SysEvent {
	var l []SysEvent
	if json.Unmarshal(b, &l) != nil {
		var one SysEvent
		if json.Unmarshal(b, &one) != nil || one.ID == "" {
			return nil
		}
		l = []SysEvent{one}
	}
	return l
}

func eventLoop() {
	time.Sleep(45 * time.Second)
	since := time.Now().Add(-180 * 24 * time.Hour).Unix()
	if b, err := os.ReadFile(evSinceFile()); err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && v > since {
			since = v
		}
	}
	for {
		start := time.Now().Unix()
		if l, ok := scanEvents(since); ok {
			sysEv.Lock()
			sysEv.pending = append(sysEv.pending, l...)
			if n := len(sysEv.pending); n > 3000 {
				sysEv.pending = sysEv.pending[n-3000:]
			}
			sysEv.next = start - 120 // lùi 2 phút cho mục ghi trễ; server bỏ trùng
			empty := len(sysEv.pending) == 0
			sysEv.Unlock()
			since = start - 120
			if empty {
				os.WriteFile(evSinceFile(), []byte(strconv.FormatInt(since, 10)), 0o600)
			}
		}
		time.Sleep(10 * time.Minute)
	}
}

// takeEvents: tối đa 200 mục để gửi kèm báo cáo lần này.
func takeEvents() []SysEvent {
	sysEv.Lock()
	defer sysEv.Unlock()
	n := len(sysEv.pending)
	if n > 200 {
		n = 200
	}
	return append([]SysEvent(nil), sysEv.pending[:n]...)
}

// ackEvents: server đã nhận n mục đầu; gửi hết thì ghi mốc ra file.
func ackEvents(n int) {
	sysEv.Lock()
	defer sysEv.Unlock()
	if n > len(sysEv.pending) {
		n = len(sysEv.pending)
	}
	sysEv.pending = sysEv.pending[n:]
	if len(sysEv.pending) == 0 && sysEv.next > 0 {
		os.WriteFile(evSinceFile(), []byte(strconv.FormatInt(sysEv.next, 10)), 0o600)
	}
}
