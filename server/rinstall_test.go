package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// API cài từ xa: chỉ Super admin, phải có tài khoản + mật khẩu, chỉ IP nội bộ, tối đa 50 máy.
func TestRemoteInstallAPI(t *testing.T) {
	f := setup(t)
	body := map[string]interface{}{"ips": []string{"192.168.1.20"}, "user": "Administrator", "password": "x"}
	code, _, _ := f.tech.do("POST", "/api/remote-install", body, nil)
	expect(t, code, 403, "tech cài từ xa")
	code, _, _ = f.super.do("POST", "/api/remote-install", map[string]interface{}{"ips": []string{"192.168.1.20"}, "user": "Administrator"}, nil)
	expect(t, code, 400, "thiếu mật khẩu")
	code, _, _ = f.super.do("POST", "/api/remote-install", map[string]interface{}{"ips": []string{"8.8.8.8"}, "user": "a", "password": "b"}, nil)
	expect(t, code, 400, "IP ngoài mạng nội bộ")
	many := []string{}
	for i := 0; i < 51; i++ {
		many = append(many, "10.0.0.1")
	}
	code, _, _ = f.super.do("POST", "/api/remote-install", map[string]interface{}{"ips": many, "user": "a", "password": "b"}, nil)
	expect(t, code, 400, "quá 50 máy")
	// Console mở bằng 127.0.0.1 (httptest): không gắn được địa chỉ server cho agent nên phải từ chối
	code, _, _ = f.super.do("POST", "/api/remote-install", body, nil)
	expect(t, code, 400, "mở Console bằng localhost")
	code, _, _ = f.super.do("GET", "/api/remote-install", nil, nil)
	expect(t, code, 200, "xem tiến độ")
}

// Thử thật trên Windows: đặt MAM_RI_TEST_IP=<IP một máy trong LAN> rồi chạy go test -run TestRemoteInstallWrongPassword.
// Tài khoản/mật khẩu cố tình sai: phải báo lỗi dễ hiểu (sai mật khẩu, hoặc bị từ chối vào ADMIN$), không treo lâu.
func TestRemoteInstallWrongPassword(t *testing.T) {
	ip := os.Getenv("MAM_RI_TEST_IP")
	if ip == "" {
		t.Skip("đặt MAM_RI_TEST_IP để thử thật")
	}
	t0 := time.Now()
	err := remoteInstall(ip, "mam-khong-co-tai-khoan-nay", "sai-mat-khau-"+rid(4), riPack{exe64: []byte("x")}, func(string) {})
	remoteCleanup(ip)
	d := time.Since(t0)
	t.Logf("sau %v: %v", d.Round(time.Millisecond), err)
	if err == nil || !(strings.HasPrefix(err.Error(), "Sai tài khoản") || strings.HasPrefix(err.Error(), "Bị từ chối vào ổ quản trị")) {
		t.Fatalf("cần lỗi sai tài khoản/bị từ chối, nhận: %v", err)
	}
	if d > 20*time.Second {
		t.Fatalf("báo lỗi quá chậm: %v", d)
	}
}
