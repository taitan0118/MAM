Kiểm thử MAM / MAM tests
1) Go (server): cd server && go test ./...          (12 test: quyền, đăng nhập, khóa, cảnh báo, khôi phục mật khẩu...)
2) Đầu cuối (cần Node + Playwright + Chromium):
   - Chạy server: MAM_DATA=/tmp/d ./mam-server -addr :7071 ; tạo admin / Matkhau2026 ở lần đầu
   - Chạy vài agent giả lập (Linux): ./mam-agent -config c1.json -sim PC-KT-01   (c1.json: {"server":"http://127.0.0.1:7071"})
   - node seed.js (nhóm, tài khoản) ; node e2e.js (quyền, đổi mật khẩu bắt buộc, reset-admin, khóa đăng nhập) ; node shots.js (chụp ảnh 3 ngôn ngữ)
Windows thật: chưa chạy tự động được trong môi trường dựng; xem mục Giới hạn của README.
