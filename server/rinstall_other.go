//go:build !windows

package main

import "errors"

// Cài từ xa dùng chia sẻ file + trình quản lý dịch vụ của Windows: chỉ server chạy trên Windows làm được.
func remoteInstall(ip, user, pass string, pk riPack, step func(string)) error {
	return errors.New("Cài agent từ xa chỉ chạy được khi server MAM chạy trên Windows")
}

func remoteCleanup(ip string) {}
