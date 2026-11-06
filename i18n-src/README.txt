Nguồn bản dịch Console (vi/en/zh). Sửa d1.py, d2.py, d3.py (bộ ba: tiếng Việt gốc, English, 中文; d3.py có thêm danh sách mẫu regex cho câu server sinh ra),
rồi chạy:  python3 gen.py   -> ghi lại server/web/i18n.js (được nhúng vào mam-server khi build).
Khóa là câu tiếng Việt đúng như trong app.js; thiếu bản dịch thì Console hiện nguyên câu tiếng Việt.
Câu mới trong app.js: cứ viết t('...') hoặc đưa chuỗi vào el()/toast()/modal() (tự dịch), rồi thêm dòng vào d1/d2.
Chuỗi có tham số dùng {0} {1}: t('{0} máy', n). Câu server sinh ra (nhật ký, lỗi, cảnh báo) thêm vào d3.py (khóa cố định hoặc mẫu regex).
Bản en/zh do tác giả soạn, nên nhờ người bản ngữ rà lại.
