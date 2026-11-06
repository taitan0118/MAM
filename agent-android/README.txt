MAM Agent cho Android
=====================
Bản quyền © 2026 Nguyễn Tấn Tài (Missing)

App quản lý máy Android cho MAM. Minh bạch với người dùng: hiện rõ màn hình "máy này được quản lý",
có thông báo thường trực khi đang chạy. App báo về máy chủ: model, phiên bản Android, dung lượng bộ nhớ,
mức pin, nhiệt độ pin, trạng thái online. App nhận lệnh "nhắn tin" (hiện thông báo).

App KHÔNG xem màn hình, KHÔNG điều khiển máy từ xa — Android cấm việc này với app thường, và đó là
ranh giới cố ý giữ lại. App cũng không chạy ẩn: luôn có biểu tượng và thông báo thường trực.

CÁCH DÙNG (chỉ dành cho MÁY CÔNG TY CẤP, cài có sự đồng ý của người dùng)
------------------------------------------------------------------------
1. Mở project này bằng Android Studio (Giraffe trở lên). Android Studio sẽ tự tải Gradle 8.9 + SDK.
2. Build > Build APK(s), hoặc Run để cài thẳng vào máy thử.
   (Môi trường tạo ra project này không có Android SDK nên KHÔNG build sẵn APK được — phải build trên
   máy của đại ca.)
3. Mở app trên máy Android, nhập địa chỉ máy chủ MAM (ví dụ 192.168.1.10:7070) và Mã đăng ký nếu có,
   bấm "Bật quản lý". Máy sẽ hiện trong Console như các máy Windows/Linux/macOS khác.

GIỚI HẠN & LƯU Ý QUAN TRỌNG
---------------------------
- Lệnh tắt máy / khởi động lại / khóa / chặn web KHÔNG áp dụng được với app Android thường. Muốn làm
  được những việc đó, phải triển khai theo Android Enterprise (Device Owner / MDM): doanh nghiệp ghi
  danh thiết bị khi cấp máy mới. Đó là một hướng mở rộng riêng, chưa làm trong bản này.
- CHỈ cài trên máy công ty cấp, cho người dùng biết. Cài lén lên máy cá nhân của người khác để theo dõi
  là vi phạm pháp luật về quyền riêng tư ở hầu hết các nước — không làm.
- Google Play cấm app giám sát/điều khiển ẩn. App này minh bạch nên hợp lệ để phân phối nội bộ (cài trực
  tiếp APK). Nếu đưa lên Play Store cần khai báo rõ mục đích quản lý thiết bị doanh nghiệp.
