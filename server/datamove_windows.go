//go:build windows

package main

import "os/exec"

// thư mục mới có thể kế thừa quyền đọc của mọi người dùng; dữ liệu có băm mật khẩu và token nên chỉ giữ SYSTEM, Administrators
// và chủ thư mục (S-1-3-4): server chạy bằng tài khoản thường thì chính nó vẫn phải ghi được vào thư mục vừa tạo.
func secureDir(d string) {
	exec.Command("icacls", d, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*S-1-3-4:(OI)(CI)F").Run()
}
