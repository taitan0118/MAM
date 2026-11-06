package main

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Tiến trình "ui" (chạy trong phiên người dùng: cửa sổ MAM, khay, chụp màn hình) báo sống mỗi 2 giây qua /api/state?proc=1.
// Nó mà tắt (bị kill, lỗi, chưa chạy sau cập nhật) thì xem màn hình không có hình: agent tự bật lại.
var uiProcAt int64

func uiBeat()       { atomic.StoreInt64(&uiProcAt, time.Now().Unix()) }
func uiAlive() bool { return time.Now().Unix()-atomic.LoadInt64(&uiProcAt) <= 15 }

var kickMu sync.Mutex
var lastKick time.Time

// kickUI bật lại tiến trình ui nếu có người đang đăng nhập; tối đa 1 lần mỗi 45 giây. Trả true nếu vừa bật.
func kickUI() bool {
	kickMu.Lock()
	defer kickMu.Unlock()
	if activeUser() == "" || time.Since(lastKick) < 45*time.Second {
		return false
	}
	lastKick = time.Now()
	if !startUIProc() {
		return false
	}
	log.Printf("Tiến trình MAM trong phiên người dùng không chạy, đã bật lại")
	return true
}

// uiWatchdog: chạy nền trong agent; quá 90 giây sau khi khởi động mà tiến trình ui vẫn im thì bật lại.
func uiWatchdog() {
	time.Sleep(90 * time.Second)
	for {
		if !uiAlive() {
			kickUI()
		}
		time.Sleep(30 * time.Second)
	}
}
