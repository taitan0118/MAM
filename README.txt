MAM - Missing App Monitor  v1.1.46
=================================================================
[VI] Phần mềm giám sát và điều khiển máy tính trong mạng LAN.
[EN] Software to monitor and control computers on a LAN.
[中文] 局域网计算机监控与管理软件。

GỒM / CONTAINS / 内容
  MAM-Server-Setup.exe   Bộ cài một file cho server, bấm đúp là chạy / one-file server setup / 服务器一键安装程序
  MAM-Agent-Setup.exe    Bộ cài một file cho máy client (Win10/11 x64) / one-file client setup / 客户端一键安装程序
  server/windows/   mam-server.exe + install-server.bat   (máy chủ / server / 服务器)
  server/linux/     mam-server + install-server-linux.sh   (tuỳ chọn / optional / 可选)
  agent/windows/    mam-agent.exe + install-agent.bat      (Windows 10/11, Server 2016+)
  agent/windows-legacy/  MAM-Agent.ps1 + install-agent-legacy.bat  (Windows 7/8, Server 2008 R2+)
  console/          MAM Console ứng dụng desktop (Windows, Mac, Linux) / desktop app / 桌面应用
  docs/             Hướng dẫn, Giới thiệu (vi / en / zh) - PDF
  source/           Mã nguồn đầy đủ (gồm cả docs/ để tạo lại PDF: python3 docs/gen_docs.py <thư mục ảnh> <thư mục ra>) / full source incl. docs/ to rebuild the PDFs / 完整源码（含 docs/ 可重新生成 PDF）

-----------------------------------------------------------------
[VI] CÀI NHANH (3 bước)
 1. Máy làm server: bấm đúp MAM-Server-Setup.exe (tự xin quyền Administrator). Hoặc dùng server\windows\install-server.bat.
    Cuối cùng nó in địa chỉ, ví dụ http://192.168.1.10:7070
    Cách cài agent dễ nhất: mở Console bằng IP của server > Cài đặt > "Tải MAM-Agent.exe", chép sang client, bấm đúp.
    Nếu bạn đổi cổng (vd 8080), nhập đúng cả cổng khi cài agent: 192.168.1.10:8080.
    Agent cài xong mà máy không hiện: xem C:\Program Files\MAM Agent\agent.log.
 2. Mở địa chỉ đó bằng trình duyệt, tạo tài khoản Super admin đầu tiên (không có mật khẩu mặc định).
 3. Mỗi máy cần giám sát: bấm đúp MAM-Agent-Setup.exe (hoặc agent\windows\install-agent.bat), nhập địa chỉ server.
    Máy tự hiện trên Console sau vài giây, không cần duyệt.
 Máy Windows 7/8: dùng agent\windows-legacy\install-agent-legacy.bat.
 Chọn ngôn ngữ (Việt/Anh/Trung) ở màn hình đầu của từng bộ cài và ở góc trên của Console.
 Quên mật khẩu: trên máy server mở cửa sổ lệnh (Administrator) chạy  mam-server reset-admin <tài khoản>
 Gỡ: uninstall-server.bat / uninstall-agent.bat (gỡ agent phải nhập mật khẩu gỡ, đặt ở Console > Cài đặt).
 Dữ liệu server: %ProgramData%\MAM (sao lưu thư mục này là đủ).

[EN] QUICK START (3 steps)
 1. On the server machine double-click MAM-Server-Setup.exe (it asks for Administrator rights), or use server\windows\install-server.bat.
    It prints the address at the end, e.g. http://192.168.1.10:7070
    Easiest agent install: open the Console by the server's IP > Settings > "Download MAM-Agent.exe", copy it to the client, double-click.
    If you changed the port (e.g. 8080), enter it in the agent installer too: 192.168.1.10:8080.
    Agent installed but not listed: read C:\Program Files\MAM Agent\agent.log.
 2. Open that address in a browser and create the first Super admin account (no default password).
 3. On every machine to monitor double-click MAM-Agent-Setup.exe (or agent\windows\install-agent.bat) and enter the server address.
    The machine shows up in the Console within seconds, no approval needed.
 Windows 7/8 machines: use agent\windows-legacy\install-agent-legacy.bat.
 Pick Vietnamese/English/Chinese on the first screen of each installer and at the top of the Console.
 Forgot the password: on the server open an Administrator command window and run  mam-server reset-admin <username>
 Uninstall: uninstall-server.bat / uninstall-agent.bat (removing an agent needs the uninstall password set in Console > Settings).
 Server data: %ProgramData%\MAM (back this folder up).

[中文] 快速开始（3 步）
 1. 在服务器上双击 MAM-Server-Setup.exe（会自动请求管理员权限），或使用 server\windows\install-server.bat。
    结束时会显示访问地址，例如 http://192.168.1.10:7070
    最简单的代理安装方式：用服务器 IP 打开控制台 > 设置 > “下载 MAM-Agent.exe”，复制到客户端后双击。
    如果改了端口（如 8080），安装代理时也要带上端口：192.168.1.10:8080。
    代理已安装但设备未出现：查看 C:\Program Files\MAM Agent\agent.log。
 2. 用浏览器打开该地址，创建第一个超级管理员账号（没有默认密码）。
 3. 在每台要监控的电脑上双击 MAM-Agent-Setup.exe（或 agent\windows\install-agent.bat），输入服务器地址。
    几秒钟后设备会出现在控制台中，无需审批。
 Windows 7/8：使用 agent\windows-legacy\install-agent-legacy.bat。
 每个安装程序的第一屏和控制台顶部都可选择越南语/英语/中文。
 忘记密码：在服务器上以管理员身份打开命令窗口，运行  mam-server reset-admin <账号>
 卸载：uninstall-server.bat / uninstall-agent.bat（卸载代理需要卸载密码，在控制台“设置”中设置）。
 服务器数据：%ProgramData%\MAM（备份该文件夹即可）。

-----------------------------------------------------------------
GIỚI HẠN / LIMITS / 限制
 - Chưa có / Not included / 不包含: gửi báo cáo tự động theo lịch / scheduled report delivery / 定时自动发送报表。
 - Điều khiển màn hình từ xa chỉ có trên Windows / Remote screen control is Windows only / 远程屏幕控制仅支持 Windows。
 - Khóa màn hình và nhắn tin cần Windows Pro trở lên / Lock and message need Windows Pro or later / 锁屏和消息需要 Windows 专业版及以上。
 - Nhiệt độ CPU nhiều máy không đọc được qua Windows / CPU temperature is often not exposed by Windows / 许多电脑无法通过 Windows 读取 CPU 温度。
 - Mạng LAN, HTTP thường. Muốn mã hóa: -cert và -key / Plain HTTP on LAN; for TLS use -cert and -key / 局域网使用 HTTP，如需加密使用 -cert 和 -key。
 - Bản en/zh do tác giả soạn, nên nhờ người bản ngữ rà lại / en and zh texts were written by the author, please have a native speaker review them / 英文和中文文本由作者编写，建议请母语者校对。

Bản quyền © 2026 Nguyễn Tấn Tài (Missing)
