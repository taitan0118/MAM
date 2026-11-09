# MAM - Missing App Monitor

Phần mềm quản lý, giám sát và điều khiển máy tính trong mạng LAN: một **server**, các máy cài **agent**, và **Console** (web trong server hoặc app desktop).

- Giám sát CPU, RAM, nhiệt độ, ổ đĩa; cảnh báo theo ngưỡng
- Xem và điều khiển màn hình từ xa, chat hai chiều, yêu cầu hỗ trợ
- Chính sách: chặn web (bộ lọc DNS), USB, ứng dụng
- Lịch hẹn giờ, Wake-on-LAN, báo cáo và kiểm kê xuất Excel
- Ba vai trò: Super admin, Kỹ thuật viên (theo nhóm máy), Chỉ xem
- Tiếng Việt, English, 中文

**Phiên bản hiện tại: 1.1.49**

Tải bộ cài: https://github.com/taitan0118/MAM-tai-ve

## Mã nguồn

| Thư mục | Nội dung |
|---|---|
| `server/` | Server (Go) và Console web |
| `agent/` | Agent cài trên máy (Go) |
| `console/` | App Console desktop |
| `installer/` | Bộ cài Windows/Mac/Linux, icon |
| `agent-android/`, `agent-ps/` | Agent Android và PowerShell (Windows 7/8) |
| `docs/` | Hướng dẫn sử dụng (PDF vi/en/zh) |

Build: `./build.sh` (cần Go và Node). Kiểm tra: `cd server && go test ./...`

Dữ liệu server nằm trong `mam.db` (SQLite, từ 1.1.49; bản cũ tự chuyển `state.json` sang lần chạy đầu). Thư viện SQLite đã để sẵn trong `server/vendor/`, build không cần tải gì.

## Giấy phép

Dùng miễn phí cho tổ chức của chính bạn. **Không được bán lại, phân phối lại, đổi tên, hay cung cấp như dịch vụ.** Xem file [LICENSE](LICENSE).

© 2026 Nguyễn Tấn Tài (Missing)
