# Nội dung tài liệu MAM, mỗi chuỗi là bộ ba (vi, en, zh). Khối: p, ul, ol, code, img, note, warn, table.
V = "1.1.58"
G = []   # hướng dẫn
def S(title, *blocks): G.append((title, list(blocks)))

S(("1. MAM là gì, gồm những phần nào","1. What MAM is and what it consists of","1. MAM 是什么，由哪些部分组成"),
 ("p",("MAM (Missing App Monitor) là phần mềm giám sát và điều khiển máy tính trong mạng LAN của một công ty, phòng máy, trường học hoặc gia đình. Quản trị viên mở một trang web (Console) để xem tình trạng từng máy, nhận cảnh báo khi máy quá nóng, đầy ổ cứng hoặc mất kết nối, và ra lệnh khóa màn hình, gửi tin nhắn, khởi động lại, tắt máy hay bật máy từ xa.",
       "MAM (Missing App Monitor) is software to monitor and control the computers on the LAN of a company, computer lab, school or home. An administrator opens a web page (the Console) to see the state of every machine, gets alerts when a machine overheats, runs out of disk space or loses connection, and can lock the screen, send a message, restart, shut down or wake a machine remotely.",
       "MAM（Missing App Monitor）是用于监控和管理公司、机房、学校或家庭局域网中计算机的软件。管理员打开一个网页（控制台）即可查看每台设备的状态，在设备过热、磁盘已满或断线时收到告警，并可远程锁屏、发送消息、重启、关机或开机。")),
 ("rows",[[('Thành phần', 'Component', '组成部分'), ('Vai trò', 'Role', '作用')], ['MAM Server', ('Máy chủ trung tâm, chạy một lần trên một máy cố định. Lưu dữ liệu, phục vụ Console, giao tiếp với agent.', 'Central server, runs once on one always-on machine. Stores data, serves the Console, talks to agents.', '中心服务器，在一台常开的机器上运行一次。保存数据、提供控制台、与代理通信。')], ['MAM Console', ('Trang web quản trị, mở bằng trình duyệt tại địa chỉ của server.', 'The admin web page, opened in a browser at the server address.', '管理网页，用浏览器访问服务器地址。')], ['MAM Agent', ('Chương trình nhỏ chạy ngầm trên mỗi máy cần giám sát. Gửi số liệu về server khoảng 5 giây một lần và nhận lệnh.', 'A small background program on every monitored machine. Sends metrics to the server about every 5 seconds and receives commands.', '在每台被监控设备上后台运行的小程序。约每 5 秒向服务器上报数据并接收命令。')]]),
 ("note",("Có hai loại agent: mam-agent.exe (Windows 10/11, Server 2016 trở lên) và MAM-Agent.ps1 (PowerShell, chạy được từ Windows 7 và Server 2008 R2). Hai loại nói cùng một giao thức nên dùng lẫn nhau trong cùng một hệ thống.",
          "There are two agents: mam-agent.exe (Windows 10/11, Server 2016 and later) and MAM-Agent.ps1 (PowerShell, works from Windows 7 and Server 2008 R2). They speak the same protocol, so you can mix them in one system.",
          "代理有两种：mam-agent.exe（Windows 10/11、Server 2016 及更高版本）和 MAM-Agent.ps1（PowerShell，从 Windows 7 和 Server 2008 R2 起可用）。两者使用相同协议，可在同一系统中混用。")))

S(("2. Chuẩn bị","2. Before you start","2. 准备工作"),
 ("ul",[("Một máy làm server, bật thường xuyên, có IP cố định hoặc được giữ chỗ trong DHCP. Windows 10/11, Server 2016 trở lên hoặc Linux. Rất nhẹ: vài chục MB RAM cho vài trăm máy.",
         "One always-on machine as the server, with a fixed IP or a DHCP reservation. Windows 10/11, Server 2016 or later, or Linux. Very light: a few tens of MB of RAM for a few hundred machines.",
         "一台常开的服务器，使用固定 IP 或 DHCP 保留地址。Windows 10/11、Server 2016 及更高版本，或 Linux。非常轻量：几百台设备只需几十 MB 内存。"),
        ("Quyền Administrator trên server và trên từng máy client khi cài.","Administrator rights on the server and on each client when installing.","安装时需要服务器和每台客户端的管理员权限。"),
        ("Các máy client nối được tới server qua mạng LAN, cổng TCP 7070 (đổi được).","Client machines that can reach the server over the LAN on TCP port 7070 (changeable).","客户端能通过局域网访问服务器的 TCP 7070 端口（可更改）。"),
        ("Gói cài MAM-1.1.58.zip đã giải nén. Bên trong có thư mục server, agent, docs và source.","The extracted MAM-1.1.0.zip package, containing the folders server, agent, docs and source.","已解压的 MAM-1.1.0.zip 安装包，内含 server、agent、docs 和 source 文件夹。")]),
 ("warn",("MAM chạy trên mạng LAN bằng HTTP thường. Đừng mở cổng 7070 ra Internet. Muốn truy cập từ xa hãy dùng VPN, hoặc bật HTTPS bằng cờ -cert và -key.",
          "MAM runs over plain HTTP on the LAN. Do not expose port 7070 to the Internet. For remote access use a VPN, or turn on HTTPS with the -cert and -key flags.",
          "MAM 在局域网内使用普通 HTTP。不要把 7070 端口暴露到互联网。需要远程访问请使用 VPN，或用 -cert 和 -key 参数启用 HTTPS。")))

S(("3. Cài server","3. Install the server","3. 安装服务器"),
 ("ol",[("Trên máy server, mở thư mục server\\windows trong gói cài.","On the server machine open the server\\windows folder of the package.","在服务器上打开安装包中的 server\\windows 文件夹。"),
        ("Cách dễ nhất: bấm đúp MAM-Server-Setup.exe ở thư mục gốc của gói (hoặc install-server.bat trong server\\windows). Windows hỏi quyền Administrator, chọn Yes.","Easiest way: double-click MAM-Server-Setup.exe in the package root (or install-server.bat in server\\windows). Windows asks for Administrator rights, choose Yes.","最简单的方法：双击安装包根目录中的 MAM-Server-Setup.exe（或 server\\windows 中的 install-server.bat）。Windows 请求管理员权限时选择“是”。"),
        ("Chọn ngôn ngữ (1 Việt, 2 English, 3 中文) và cổng (Enter = 7070).","Choose the language (1 Vietnamese, 2 English, 3 Chinese) and the port (Enter = 7070).","选择语言（1 越南语、2 英语、3 中文）和端口（回车 = 7070）。"),
        ("Bộ cài chép chương trình vào C:\\Program Files\\MAM Server, mở cổng tường lửa cho mạng nội bộ (mọi loại mạng, chỉ trong dải LAN), đăng ký chạy cùng Windows bằng tài khoản SYSTEM (tự chạy lại khi lỗi) và khởi động server.",
         "The installer copies the program to C:\\Program Files\\MAM Server, opens the firewall port for the local network (any network profile, local subnet only), registers it to start with Windows under the SYSTEM account (restarting on failure) and starts the server.",
         "安装程序会把程序复制到 C:\\Program Files\\MAM Server，为局域网开放防火墙端口（任何网络类型，仅限本地子网），以 SYSTEM 账户注册为随 Windows 启动（出错自动重启），并启动服务器。"),
        ("Cuối cùng nó in địa chỉ, ví dụ http://192.168.1.10:7070. Ghi lại địa chỉ này, bạn sẽ dùng cho Console và cho agent.",
         "At the end it prints the address, e.g. http://192.168.1.10:7070. Write it down: you need it for the Console and for the agents.",
         "最后会显示访问地址，例如 http://192.168.1.10:7070。请记下该地址，控制台和代理都要用到。")]),
 ("p",("Dữ liệu nằm ở C:\\ProgramData\\MAM (chỉ SYSTEM và Administrators đọc được). Cài lại hoặc nâng cấp chỉ cần chạy lại install-server.bat, dữ liệu được giữ nguyên.",
       "Data lives in C:\\ProgramData\\MAM (readable only by SYSTEM and Administrators). To reinstall or upgrade just run install-server.bat again; the data is kept.",
       "数据保存在 C:\\ProgramData\\MAM（仅 SYSTEM 和管理员可读）。重新安装或升级只需再次运行 install-server.bat，数据会保留。")),
 ("p",("Linux: chép thư mục server/linux lên máy, chạy sudo ./install-server-linux.sh vi 7070 (đổi vi thành en hoặc zh cho ngôn ngữ). Server chạy bằng tài khoản mam, dữ liệu ở /var/lib/mam.",
       "Linux: copy the server/linux folder to the machine and run sudo ./install-server-linux.sh en 7070 (use vi, en or zh for the language). The server runs as the mam user, data in /var/lib/mam.",
       "Linux：把 server/linux 文件夹复制到机器上，运行 sudo ./install-server-linux.sh zh 7070（语言可选 vi、en、zh）。服务器以 mam 用户运行，数据位于 /var/lib/mam。")),
 ("code","mam-server.exe -addr :7070 -data \"D:\\MAMdata\" -lang en -cert server.crt -key server.key"),
 ("p",("Tùy chọn dòng lệnh: -addr (địa chỉ và cổng), -data (thư mục dữ liệu), -lang (vi, en, zh cho dòng lệnh và nhật ký), -cert và -key (bật HTTPS).",
       "Command-line options: -addr (address and port), -data (data folder), -lang (vi, en, zh for the command line and logs), -cert and -key (turn on HTTPS).",
       "命令行选项：-addr（地址和端口）、-data（数据文件夹）、-lang（命令行和日志语言 vi、en、zh）、-cert 与 -key（启用 HTTPS）。")))

S(("4. Lần đầu vào Console: tạo Super admin","4. First visit: create the Super admin","4. 首次访问：创建超级管理员"),
 ("p",("Mở địa chỉ server bằng trình duyệt. Lần đầu bạn sẽ thấy màn hình Cài đặt lần đầu. Nhập họ tên, tên đăng nhập (chữ thường, số, dấu chấm hoặc gạch dưới, 3 đến 32 ký tự) và mật khẩu (ít nhất 8 ký tự, có cả chữ và số). Không có mật khẩu mặc định.",
       "Open the server address in a browser. The first time you see the First-time setup screen. Enter a full name, a username (lowercase letters, digits, dot or underscore, 3 to 32 characters) and a password (at least 8 characters with letters and digits). There is no default password.",
       "用浏览器打开服务器地址。首次会看到“首次设置”页面。输入姓名、用户名（小写字母、数字、点或下划线，3 到 32 个字符）和密码（至少 8 个字符，需包含字母和数字）。没有默认密码。")),
 ("img","setup",("Màn hình cài đặt lần đầu","The first-time setup screen","首次设置页面")),
 ("note",("Sau khi tạo, nên tạo thêm một Super admin dự phòng ở mục Tài khoản. Nếu quên mật khẩu mà không còn Super admin nào khác, chỉ khôi phục được ngay tại máy server (xem mục 14).",
          "After creating it, add a backup Super admin under Accounts. If you forget the password and no other Super admin exists, recovery is only possible at the server machine itself (see section 14).",
          "创建后，建议在“账号”中再添加一个备用超级管理员。如果忘记密码且没有其他超级管理员，只能在服务器本机上恢复（见第 14 节）。")),
 ("p",("Ngôn ngữ Console tự chọn theo trình duyệt (Trung, Anh hoặc Việt). Đổi bất cứ lúc nào bằng ô chọn ở góc trên bên phải, kể cả ở màn hình đăng nhập. Lựa chọn được nhớ trên trình duyệt đó.",
       "The Console language follows your browser (Chinese, English or Vietnamese). Change it any time with the selector at the top right, including on the sign-in screen. The choice is remembered in that browser.",
       "控制台语言会根据浏览器自动选择（中文、英语或越南语）。可随时通过右上角的下拉框更改，登录页面也可以。该选择会保存在当前浏览器中。")))

S(("5. Cài agent trên máy client","5. Install the agent on client machines","5. 在客户端安装代理"),
 ("p",("Cách dễ nhất (Windows 10/11, Server 2016 trở lên): mở Console bằng địa chỉ IP của server (không dùng localhost) > Cài đặt > \"Tải MAM-Agent.exe\". File tải về đã có sẵn địa chỉ server, cổng và mã đăng ký. Chép sang máy client, bấm đúp, chọn Yes ở hộp quyền Administrator: vài giây sau máy hiện trong Console, không phải gõ gì. Cài ngầm không hỏi: MAM-Agent.exe /S. Chỉ Super admin tải được file này vì nó chứa mã đăng ký.",
       "The easiest way (Windows 10/11, Server 2016 or later): open the Console with the server's IP address (not localhost) > Settings > \"Download MAM-Agent.exe\". The downloaded file already contains the server address, port and enrollment key. Copy it to the client, double-click it and choose Yes in the Administrator prompt: within seconds the machine appears in the Console, with nothing to type. Silent install: MAM-Agent.exe /S. Only a Super admin can download it because it contains the enrollment key.",
       "最简单的方式（Windows 10/11、Server 2016 及以上）：用服务器的 IP 地址（不要用 localhost）打开控制台 > 设置 > “下载 MAM-Agent.exe”。下载的文件已包含服务器地址、端口和注册码。复制到客户端，双击，并在管理员权限提示中选择“是”：几秒后设备就会出现在控制台，无需输入任何内容。静默安装：MAM-Agent.exe /S。该文件含注册码，因此只有超级管理员可以下载。")),
 ("p",("Cách thủ công, nếu không dùng file tải từ Console:","The manual way, if you do not use the file downloaded from the Console:","手动方式（不使用从控制台下载的文件时）：")),
 ("p",("Windows 10/11 và Server 2016 trở lên:","Windows 10/11 and Server 2016 or later:","Windows 10/11 和 Server 2016 及更高版本：")),
 ("ol",[("Chép MAM-Agent-Setup.exe sang máy client (USB hoặc thư mục chia sẻ). Máy Windows 7/8 dùng thư mục agent\\windows-legacy.","Copy MAM-Agent-Setup.exe to the client (USB or a shared folder). Windows 7/8 machines use the agent\\windows-legacy folder.","把 MAM-Agent-Setup.exe 复制到客户端（U 盘或共享文件夹）。Windows 7/8 设备请使用 agent\\windows-legacy 文件夹。"),
        ("Bấm đúp MAM-Agent-Setup.exe (hoặc install-agent.bat), chọn Yes ở hộp quyền Administrator.","Double-click MAM-Agent-Setup.exe (or install-agent.bat) and choose Yes in the Administrator prompt.","双击 MAM-Agent-Setup.exe（或 install-agent.bat），在管理员权限提示中选择“是”。"),
        ("Chọn ngôn ngữ của agent, nhập địa chỉ server (ví dụ 192.168.1.10:7070) và mã đăng ký nếu server có đặt (không thì để trống).","Pick the agent language, enter the server address (e.g. 192.168.1.10:7070) and the enrollment key if the server has one (otherwise leave it empty).","选择代理语言，输入服务器地址（如 192.168.1.10:7070），如果服务器设置了注册码则一并输入（否则留空）。"),
        ("Vài giây sau máy tự hiện trong Console, không cần đăng nhập hay duyệt.","Within seconds the machine appears in the Console, with no sign-in or approval.","几秒钟后设备会自动出现在控制台中，无需登录或审批。")]),
 ("p",("Windows 7, 8, Server 2008 R2: dùng agent\\windows-legacy\\install-agent-legacy.bat (cùng cách làm, chạy bằng PowerShell). Cài hàng loạt bằng dòng lệnh, ví dụ qua chính sách nhóm hoặc PsExec:",
       "Windows 7, 8, Server 2008 R2: use agent\\windows-legacy\\install-agent-legacy.bat (same steps, runs with PowerShell). To install in bulk from the command line, e.g. through group policy or PsExec:",
       "Windows 7、8、Server 2008 R2：使用 agent\\windows-legacy\\install-agent-legacy.bat（步骤相同，通过 PowerShell 运行）。如需通过命令行批量安装（例如组策略或 PsExec）：")),
 ("code","mam-agent.exe install -server http://192.168.1.10:7070 -lang en -key MA-CUA-BAN -name KE-TOAN-02\npowershell -ExecutionPolicy Bypass -File MAM-Agent.ps1 -Install -Server http://192.168.1.10:7070 -Lang en"),
 ("p",("Agent chạy như một tác vụ SYSTEM ẩn, tự chạy cùng Windows và tự chạy lại khi lỗi. Thư mục cài C:\\Program Files\\MAM Agent chỉ SYSTEM và Administrators sửa được; nhật ký ở agent.log trong thư mục đó. Cài lại agent trên cùng một máy không tạo máy trùng: server nhận ra qua địa chỉ MAC.",
       "The agent runs as a hidden SYSTEM task, starts with Windows and restarts on failure. Its folder C:\\Program Files\\MAM Agent can only be changed by SYSTEM and Administrators; the log is agent.log in that folder. Reinstalling on the same machine does not create a duplicate: the server recognises it by MAC address.",
       "代理作为隐藏的 SYSTEM 任务运行，随 Windows 启动，出错自动重启。其文件夹 C:\\Program Files\\MAM Agent 仅 SYSTEM 和管理员可修改，日志为该文件夹内的 agent.log。在同一台设备上重新安装不会产生重复设备：服务器通过 MAC 地址识别。")),
 ("note",("Gỡ agent bắt buộc nhập mật khẩu gỡ do Super admin đặt ở Cài đặt > Mật khẩu gỡ agent (server kiểm tra), nên nhân viên dùng máy không tự gỡ được và tài khoản quản trị không bị lộ. Quản trị viên cục bộ của máy vẫn có thể tắt tác vụ bằng tay: MAM không thay được việc phân quyền Windows.",
          "Uninstalling an agent requires a Super admin username and password (checked by the server), so the people using the machine cannot remove it. A local administrator of that machine can still stop the task by hand: MAM cannot replace Windows permissions.",
          "卸载代理必须输入超级管理员的账号和密码（由服务器验证），因此使用设备的人无法自行卸载。该设备的本地管理员仍可手动停止任务：MAM 不能取代 Windows 的权限管理。")))

S(("6. Tổng quan và danh sách máy","6. Overview and the machine list","6. 总览与设备列表"),
 ("img","overview",("Tổng quan: số máy, máy online, cảnh báo, máy quá nhiệt","Overview: machines, online, alerts, overheating","总览：设备数、在线数、告警、过热设备")),
 ("p",("Tổng quan cho biết ngay số máy, số máy đang online (báo cáo trong 25 giây gần nhất), số cảnh báo đang mở và số máy quá nhiệt, kèm biểu đồ online theo nhóm và các cảnh báo mới.",
       "The Overview shows at a glance the number of machines, how many are online (reported in the last 25 seconds), the open alerts and the overheating machines, plus online status per group and the latest alerts.",
       "总览一目了然地显示设备总数、在线设备数（最近 25 秒内有上报）、未关闭告警数和过热设备数，并按分组显示在线情况和最新告警。")),
 ("img","machines",("Danh sách máy: tìm, lọc theo nhóm và trạng thái","Machine list: search, filter by group and status","设备列表：搜索，按分组和状态筛选")),
 ("p",("Ô tìm kiếm tìm theo tên máy, IP, MAC và người đang dùng. Tick một hoặc nhiều máy để hiện thanh thao tác hàng loạt: khóa màn hình, gửi tin nhắn, bật máy (WoL), khởi động lại, tắt máy, đổi nhóm. Danh sách tự làm mới, không mất vị trí cuộn và nội dung đang gõ.",
       "The search box finds by machine name, IP, MAC and current user. Tick one or more machines to show the bulk bar: lock screen, send message, wake up (WoL), restart, shut down, change group. The list refreshes by itself without losing your scroll position or what you are typing.",
       "搜索框可按设备名、IP、MAC 和当前用户查找。勾选一台或多台设备后会显示批量操作栏：锁定屏幕、发送消息、开机 (WoL)、重启、关机、更改分组。列表自动刷新，不会丢失滚动位置和正在输入的内容。")))

S(("7. Chi tiết một máy","7. Machine details","7. 设备详情"),
 ("img","detail",("Chi tiết máy: CPU, RAM, nhiệt độ, ổ đĩa và các nút lệnh","Machine details: CPU, RAM, temperature, drives and the command buttons","设备详情：CPU、内存、温度、磁盘与命令按钮")),
 ("ul",[("Giám sát: CPU, RAM, nhiệt độ CPU và dung lượng từng ổ, kèm biểu đồ lấy mẫu mỗi 30 giây, giữ tối đa 4 giờ kể từ lúc server chạy.","Monitoring: CPU, RAM, CPU temperature and each drive's space, with charts sampled every 30 seconds and kept for up to 4 hours since the server started.","监控：CPU、内存、CPU 温度和各磁盘空间，图表每 30 秒采样一次，最多保留服务器启动后的 4 小时。"),
        ("Phần cứng: hệ điều hành, hãng và model, CPU, card đồ họa, bo mạch chủ, số serial, IP, MAC, miền. Agent tự đọc và cập nhật mỗi giờ.","Hardware: operating system, make and model, CPU, graphics, motherboard, serial number, IP, MAC, domain. The agent reads it and refreshes hourly.","硬件：操作系统、品牌型号、CPU、显卡、主板、序列号、IP、MAC、域。由代理读取，每小时更新。"),
        ("Ổ cứng: loại, dung lượng, tình trạng và độ mòn theo Windows; có cảnh báo khi Windows dự báo ổ sắp hỏng (S.M.A.R.T).","Disks: type, capacity, health and wear as reported by Windows, with a warning when Windows predicts a disk failure (S.M.A.R.T).","磁盘：Windows 报告的类型、容量、健康状况和磨损，Windows 预测磁盘即将故障 (S.M.A.R.T) 时会提示。"),
        ("Lệnh gần đây: 15 lệnh cuối, ai gửi, kết quả.","Recent commands: the last 15 commands, who sent them, the result.","最近命令：最近 15 条命令、发送人和结果。")]),
 ("img","hardware",("Thẻ Phần cứng","The Hardware tab","硬件标签页")),
 ("note",("Nhiệt độ CPU lấy qua Windows (WMI). Nhiều máy, nhất là máy bàn và máy ảo, không công khai cảm biến này; khi đó Console ghi N/A và các mục khác vẫn chạy bình thường.",
          "CPU temperature is read through Windows (WMI). Many machines, especially desktops and virtual machines, do not expose this sensor; the Console then shows N/A and everything else works normally.",
          "CPU 温度通过 Windows (WMI) 读取。许多设备（尤其是台式机和虚拟机）不开放该传感器，此时控制台显示 N/A，其他功能正常。")))

S(("8. Ra lệnh cho máy","8. Sending commands","8. 向设备下发命令"),
 ("rows",[[('Lệnh', 'Command', '命令'), ('Cách thực hiện', 'How it works', '实现方式')], [('Khóa màn hình', 'Lock screen', '锁定屏幕'), ('Ngắt phiên đang ngồi (tsdiscon), về màn hình đăng nhập. Cần Windows Pro trở lên.', 'Disconnects the active session (tsdiscon) back to the sign-in screen. Needs Windows Pro or later.', '断开当前会话 (tsdiscon)，回到登录界面。需要 Windows 专业版及以上。')], [('Gửi tin nhắn', 'Send message', '发送消息'), ('Hộp thoại hiện 2 phút trên màn hình (msg.exe), tối đa 300 ký tự. Cần Windows Pro trở lên.', 'A dialog shown for 2 minutes (msg.exe), at most 300 characters. Needs Windows Pro or later.', '屏幕上显示 2 分钟的对话框 (msg.exe)，最多 300 个字符。需要 Windows 专业版及以上。')], [('Khởi động lại, Tắt máy', 'Restart, Shut down', '重启、关机'), ('shutdown /t 10: người dùng có 10 giây. Ghi nhật ký loại Nhạy cảm.', 'shutdown /t 10: the user gets 10 seconds. Logged as Sensitive.', 'shutdown /t 10：用户有 10 秒时间。记为“敏感”日志。')], [('Bật máy (Wake-on-LAN)', 'Wake up (Wake-on-LAN)', '开机 (Wake-on-LAN)'), ('Server gửi gói UDP đánh thức tới MAC của máy. Máy phải bật WoL trong BIOS và card mạng, cùng mạng LAN.', "The server sends a UDP magic packet to the machine's MAC. WoL must be enabled in BIOS and NIC, on the same LAN.", '服务器向设备的 MAC 发送 UDP 魔术包。需在 BIOS 和网卡中启用 WoL，且在同一局域网。')]]),
 ("img","message",("Gửi tin nhắn tới các máy đã chọn","Sending a message to the selected machines","向所选设备发送消息")),
 ("p",("Lệnh tới máy đang offline bị bỏ qua và Console báo rõ. Mọi lệnh đều được ghi vào Nhật ký thao tác: ai gửi, lúc nào, tới máy nào. Lệnh chờ máy nhận trong vài giây; nếu quá hạn mà agent không phản hồi, lệnh được đánh dấu lỗi.",
       "Commands to offline machines are skipped and the Console tells you. Every command is written to the Audit log: who sent it, when, to which machine. A command waits a few seconds for the machine to pick it up; if the agent does not respond in time the command is marked as failed.",
       "发往离线设备的命令会被跳过，控制台会明确提示。所有命令都会记录到操作日志：谁、何时、发给哪台设备。命令会等待设备在几秒内接收；若代理超时未响应，命令会标记为失败。")),
 ("img","commands",("Thẻ Lệnh gần đây","The Recent commands tab","最近命令标签页")))

S(("9. Nhóm máy, cảnh báo, tài khoản, nhật ký","9. Groups, alerts, accounts, audit log","9. 分组、告警、账号、操作日志"),
 ("p",("Nhóm máy: Super admin tạo nhóm theo tầng, phòng ban hay khu vực (ví dụ Tầng 2 · Kỹ thuật) rồi chuyển máy vào nhóm. Xóa nhóm không mất máy, các máy quay về Chưa phân nhóm.",
       "Groups: a Super admin creates groups by floor, department or area (e.g. Floor 2 · Technical) and moves machines into them. Deleting a group does not lose machines; they go back to Ungrouped.",
       "分组：超级管理员按楼层、部门或区域创建分组（例如 2 楼 · 技术），并把设备移入其中。删除分组不会丢失设备，设备会回到“未分组”。")),
 ("img","alerts",("Cảnh báo tự mở và tự đóng","Alerts open and close by themselves","告警自动产生与关闭")),
 ("p",("Cảnh báo tự mở khi máy vượt ngưỡng và tự đóng khi hết vượt. Có thể đóng tay. Ngưỡng mặc định, chỉnh ở Cài đặt (chỉ Super admin):",
       "Alerts open when a machine crosses a threshold and close when it recovers; you can also close them by hand. Default thresholds, adjustable in Settings (Super admin only):",
       "设备超过阈值时自动产生告警，恢复后自动关闭，也可手动关闭。默认阈值可在“设置”中调整（仅超级管理员）：")),
 ("rows",[[('Điều kiện', 'Condition', '条件'), ('Mặc định', 'Default', '默认')], [('Nhiệt độ CPU', 'CPU temperature', 'CPU 温度'), '≥ 85 °C'], [('CPU cao liên tục', 'Sustained CPU', 'CPU 持续高负载'), '≥ 90 % (~30 s)'], [('Ổ cứng còn trống', 'Free disk space', '磁盘剩余空间'), '≤ 10 %'], [('Mất kết nối', 'Lost connection', '断线'), ('5 phút', '5 min', '5 分钟')]]),
 ("p",("Tài khoản và vai trò:","Accounts and roles:","账号与角色：")),
 ("rows",[[('Vai trò', 'Role', '角色'), ('Quyền', 'Permissions', '权限')], ['Super admin', ('Toàn quyền, gồm tài khoản, nhóm, cài đặt, thu hồi máy.', 'Full control, including accounts, groups, settings and revoking machines.', '全部权限，包括账号、分组、设置、吊销设备。')], [('Kỹ thuật viên', 'Technician', '技术员'), ('Xem máy, xử lý cảnh báo, ra lệnh, chỉ cho máy trong nhóm được giao. Lệnh nguy hiểm vẫn được nhưng ghi nhật ký Nhạy cảm. Không quản lý tài khoản, nhóm, cài đặt; chỉ xem nhật ký của chính mình.', 'View machines, handle alerts, send commands, only for machines in assigned groups. Dangerous commands are allowed but logged as Sensitive. Cannot manage accounts, groups or settings; sees only their own audit log.', '查看设备、处理告警、下发命令，仅限所分配分组内的设备。危险命令允许但记为“敏感”日志。不能管理账号、分组、设置；只能查看自己的日志。')], [('Chỉ xem', 'View only', '只读'), ('Chỉ theo dõi, không ra lệnh.', 'Monitoring only, no commands.', '仅监控，不能下发命令。')]]),
 ("img","accounts",("Quản lý tài khoản và vai trò","Managing accounts and roles","管理账号与角色")),
 ("p",("Quyền được server kiểm tra ở từng thao tác, không chỉ ẩn nút trên giao diện. Nhật ký thao tác ghi mọi việc quản trị: ai làm, lúc nào, từ IP nào, lên đối tượng nào; chỉ ghi thêm, không có nút sửa hay xóa, kể cả với Super admin.",
       "The server checks permissions on every action, not just by hiding buttons. The Audit log records every admin action: who, when, from which IP, on what; it is append-only with no edit or delete button, even for a Super admin.",
       "服务器对每个操作都会检查权限，而不只是隐藏按钮。操作日志记录所有管理操作：谁、何时、从哪个 IP、对什么对象；只能追加，没有编辑或删除按钮，超级管理员也一样。")),
 ("img","audit",("Nhật ký thao tác","The Audit log","操作日志")))

S(("10. Quên mật khẩu, đổi mật khẩu","10. Forgotten and changed passwords","10. 忘记密码与修改密码"),
 ("img","forgot",("Hộp thoại Quên mật khẩu","The Forgot password dialog","“忘记密码”对话框")),
 ("ul",[("Còn Super admin khác: người đó vào Tài khoản, chọn dòng của bạn, bấm Đặt lại mật khẩu và đưa bạn mật khẩu tạm (chỉ hiện một lần). Bạn đăng nhập và hệ thống bắt đặt mật khẩu mới ngay.",
         "Another Super admin exists: they open Accounts, select your row, click Reset password and give you the temporary password (shown once). You sign in and are forced to set a new one right away.",
         "还有其他超级管理员：对方进入“账号”，选中您的行，点击“重置密码”，把临时密码（仅显示一次）交给您。您登录后会被要求立即设置新密码。"),
        ("Là Super admin duy nhất: ngồi tại máy server, mở cửa sổ lệnh bằng Administrator và chạy lệnh dưới đây. Lệnh in ra mật khẩu tạm. Chạy được cả khi server đang tắt.",
         "You are the only Super admin: sit at the server machine, open an Administrator command window and run the command below. It prints a temporary password. It works even when the server is stopped.",
         "您是唯一的超级管理员：在服务器本机上以管理员身份打开命令窗口，运行下面的命令。它会输出临时密码，即使服务器未运行也可使用。")]),
 ("code","\"C:\\Program Files\\MAM Server\\mam-server.exe\" reset-admin admin"),
 ("p",("Mọi lần đặt lại đều ghi vào Nhật ký thao tác. Đăng nhập sai 5 lần thì khóa 1 phút. Phiên đăng nhập kéo dài 12 giờ. Tự đổi mật khẩu ở Tài khoản, mục Mật khẩu của tôi.",
       "Every reset is written to the Audit log. Five wrong sign-ins lock the account for 1 minute. A session lasts 12 hours. Change your own password under Accounts, My password.",
       "每次重置都会记录到操作日志。连续输错 5 次密码将锁定 1 分钟。登录会话有效期 12 小时。可在“账号”的“我的密码”中自行修改密码。")))

S(("11. Cài đặt, sao lưu, nâng cấp, gỡ","11. Settings, backup, upgrade, uninstall","11. 设置、备份、升级与卸载"),
 ("img","settings",("Cài đặt: ngưỡng cảnh báo, lệnh cài agent, mã đăng ký","Settings: alert thresholds, agent install commands, enrollment key","设置：告警阈值、代理安装命令、注册码")),
 ("ul",[("Mã đăng ký (tùy chọn): nếu đặt, agent phải kèm -key MÃ khi cài. Hữu ích khi mạng có người lạ.","Enrollment key (optional): if set, agents must install with -key KEY. Useful when strangers can reach the network.","注册码（可选）：设置后，代理安装时必须附带 -key 注册码。当网络中可能有陌生人时很有用。"),
        ("Đổi ổ lưu dữ liệu (Super admin): Cài đặt > Nơi lưu dữ liệu, nhập thư mục mới còn trống (ví dụ D:\\MAMdata) rồi bấm Chuyển dữ liệu. Server chép dữ liệu sang, dùng ngay chỗ mới, không cần chạy lại, và nhớ cả sau khi khởi động lại. Dữ liệu cũ được giữ nguyên, hãy tự xóa sau khi kiểm tra. Ổ mới (USB, ổ mạng) phải luôn gắn khi server chạy.","Move the data to another drive (Super admin): Settings > Data location, enter an empty new folder (e.g. D:\\MAMdata) and press Move data. The server copies the data, uses the new place at once with no restart, and remembers it after a restart. The old data is kept; delete it yourself after checking. The new drive (USB, network drive) must always be attached while the server runs.","更换数据存放磁盘（超级管理员）：设置 > 数据存放位置，输入一个空的新文件夹（例如 D:\\MAMdata），点击“移动数据”。服务器会复制数据并立即改用新位置，无需重启，重启后也会记住。旧数据会保留，检查无误后请自行删除。新磁盘（U 盘、网络盘）在服务器运行期间必须始终连接。"),
        ("Sao lưu: chép cả thư mục dữ liệu (C:\\ProgramData\\MAM). Server cũng tự sao lưu mỗi ngày vào thư mục con backup. Khôi phục: dừng server, chép trả lại, chạy lại.","Backup: copy the whole data folder (C:\\ProgramData\\MAM). The server also makes a daily backup into the backup subfolder. Restore: stop the server, copy it back, start again.","备份：复制整个数据文件夹（C:\\ProgramData\\MAM）。服务器也会每天自动备份到 backup 子文件夹。恢复：停止服务器，复制回去，再启动。"),
        ("Nâng cấp server: chạy lại install-server.bat của bản mới, dữ liệu được giữ. Nâng cấp agent: chạy lại install-agent.bat trên từng máy.","Upgrade the server: run the new version's install-server.bat; data is kept. Upgrade agents: run install-agent.bat again on each machine.","升级服务器：运行新版本的 install-server.bat，数据会保留。升级代理：在每台设备上重新运行 install-agent.bat。"),
        ("Thu hồi máy (Super admin): gỡ máy khỏi hệ thống và chặn token cũ; cài lại agent thì máy đăng ký lại.","Revoke a machine (Super admin): removes it from the system and blocks its old token; reinstalling the agent registers it again.","吊销设备（超级管理员）：将设备从系统中移除并阻止其旧令牌；重新安装代理后设备会再次注册。"),
        ("Gỡ server: uninstall-server.bat (giữ dữ liệu, trừ khi bạn chọn xóa). Gỡ agent: uninstall-agent.bat hoặc MAM-Agent-Setup.exe, nhập mật khẩu gỡ.","Remove the server: uninstall-server.bat (keeps data unless you choose to delete it). Remove an agent: uninstall-agent.bat or MAM-Agent-Setup.exe, enter the uninstall password.","卸载服务器：uninstall-server.bat（默认保留数据，除非您选择删除）。卸载代理：uninstall-agent.bat 或 MAM-Agent-Setup.exe，输入卸载密码。")]))

S(("12. Xử lý sự cố","12. Troubleshooting","12. 故障排除"),
 ("rows",[[('Hiện tượng', 'Symptom', '现象'), ('Cách xử lý', 'What to do', '处理方法')], [('Máy không hiện trong Console', 'A machine does not appear in the Console', '设备未出现在控制台'), ('Mở http://địa-chỉ-server:7070 trên chính máy client; không vào được thì kiểm tra tường lửa và địa chỉ. Xem agent.log trong C:\\Program Files\\MAM Agent. Server có mã đăng ký thì phải cài kèm -key.', 'Open http://server-address:7070 on the client itself; if that fails check the firewall and the address. Read agent.log in C:\\Program Files\\MAM Agent. If the server has an enrollment key, install with -key.', '在客户端本机打开 http://服务器地址:7070；打不开则检查防火墙和地址。查看 C:\\Program Files\\MAM Agent 中的 agent.log。服务器设置了注册码时，安装需加 -key。')], [('Máy hiện Offline dù đang bật', 'Shows Offline while powered on', '设备开机却显示离线'), ('Agent bị tắt hoặc mất mạng. Server đổi địa chỉ thì cài lại agent. Máy đã bị thu hồi cũng ngủ 5 phút rồi thử lại.', 'The agent stopped or the network is down. If the server address changed, reinstall the agent. A revoked machine also sleeps 5 minutes and retries.', '代理已停止或网络中断。服务器地址变更时请重新安装代理。被吊销的设备会休眠 5 分钟后重试。')], [('Khóa màn hình hoặc nhắn tin báo lỗi', 'Lock or message fails', '锁屏或消息失败'), ('Cần Windows Pro trở lên (bản Home không có tsdiscon và msg).', 'Needs Windows Pro or later (Home has no tsdiscon or msg).', '需要 Windows 专业版及以上（家庭版没有 tsdiscon 和 msg）。')], [('Bật máy (WoL) không dậy', 'Wake-on-LAN does nothing', '唤醒无效'), ('Bật Wake-on-LAN trong BIOS và trong thuộc tính card mạng, tắt Fast Startup, máy phải cùng mạng LAN với server.', "Enable Wake-on-LAN in the BIOS and in the NIC properties, disable Fast Startup, and keep the machine on the server's LAN.", '在 BIOS 和网卡属性中启用 Wake-on-LAN，关闭快速启动，设备需与服务器在同一局域网。')], [('Không vào được Console', 'The Console does not open', '无法打开控制台'), ('Kiểm tra tác vụ MAM Server đang chạy, cổng không bị chương trình khác dùng, tường lửa cho phép cổng. Xem server.log trong thư mục dữ liệu.', 'Check that the MAM Server task is running, the port is not used by another program, and the firewall allows it. Read server.log in the data folder.', '检查 MAM Server 任务是否运行、端口是否被其他程序占用、防火墙是否放行。查看数据文件夹中的 server.log。')]]))

S(("13. Giới hạn cần biết","13. Known limits","13. 已知限制"),
 ("ul",[("Chưa có: điều khiển màn hình từ xa, kiểm kê phần mềm, báo cáo gửi tự động.","Not included: remote screen control, software inventory, scheduled reports.","未包含：远程屏幕控制、软件清点、自动发送报表。"),
        ("mam-agent.exe cần Windows 10 / Server 2016 trở lên; Windows 7/8 dùng MAM-Agent.ps1.","mam-agent.exe needs Windows 10 / Server 2016 or later; Windows 7/8 use MAM-Agent.ps1.","mam-agent.exe 需要 Windows 10 / Server 2016 及以上；Windows 7/8 使用 MAM-Agent.ps1。"),
        ("Biểu đồ chỉ giữ trong bộ nhớ server (4 giờ gần nhất), khởi động lại server thì biểu đồ bắt đầu lại. Cảnh báo, nhật ký và dữ liệu máy được lưu ở đĩa.","Charts are kept in the server's memory only (last 4 hours); restarting the server restarts them. Alerts, the audit log and machine data are stored on disk.","图表仅保存在服务器内存中（最近 4 小时），重启服务器后重新开始。告警、日志和设备数据保存在磁盘上。"),
        ("Giao tiếp bằng HTTP thường trong LAN; cần mã hóa thì bật HTTPS hoặc dùng VPN.","Communication is plain HTTP inside the LAN; for encryption turn on HTTPS or use a VPN.","局域网内通信使用普通 HTTP；需要加密请启用 HTTPS 或使用 VPN。"),
        ("Bản dịch tiếng Anh và tiếng Trung do tác giả soạn, nên nhờ người bản ngữ rà lại trước khi dùng chính thức.","The English and Chinese texts were written by the author; please have a native speaker review them before official use.","英文和中文文本由作者编写，正式使用前建议请母语者校对。")]),
 ("p",("Bản quyền © 2026 Nguyễn Tấn Tài (Missing).",
       "Copyright © 2026 Nguyễn Tấn Tài (Missing).",
       "版权所有 © 2026 Nguyễn Tấn Tài (Missing)。")))

# ---- Giới thiệu (leaflet) ----
INTRO_TAG = ("Giám sát và điều khiển máy tính trong mạng LAN, dễ cài, chạy thật, không phụ thuộc đám mây.",
             "Monitor and control the computers on your LAN: easy to install, runs for real, no cloud needed.",
             "监控和管理局域网内的计算机：安装简单、真实可用、无需云服务。")
INTRO = [
 (("Thấy ngay tình trạng mọi máy","See every machine at a glance","一眼看清所有设备"),
  ("CPU, RAM, nhiệt độ, dung lượng ổ đĩa, người đang dùng, phần cứng và tình trạng ổ cứng, cập nhật khoảng 5 giây một lần.","CPU, RAM, temperature, disk space, current user, hardware and disk health, refreshed about every 5 seconds.","CPU、内存、温度、磁盘空间、当前用户、硬件和磁盘健康状况，约每 5 秒刷新一次。")),
 (("Cảnh báo tự động","Automatic alerts","自动告警"),
  ("Máy quá nóng, CPU cao kéo dài, ổ cứng gần đầy, mất kết nối: cảnh báo tự mở và tự đóng khi hết vấn đề.","Overheating, sustained high CPU, nearly full disk, lost connection: alerts open by themselves and close when the problem is gone.","过热、CPU 长时间高负载、磁盘将满、断线：告警自动产生，问题消失后自动关闭。")),
 (("Ra lệnh từ xa","Remote commands","远程命令"),
  ("Khóa màn hình, gửi tin nhắn, khởi động lại, tắt máy, bật máy bằng Wake-on-LAN, cho một máy hoặc cả nhóm.","Lock the screen, send a message, restart, shut down, wake up with Wake-on-LAN, for one machine or a whole group.","锁定屏幕、发送消息、重启、关机、通过 Wake-on-LAN 开机，可针对单台设备或整个分组。")),
 (("Phân quyền và nhật ký","Roles and audit log","权限与日志"),
  ("Ba vai trò: Super admin, Kỹ thuật viên (theo nhóm máy được giao), Chỉ xem. Nhật ký chỉ ghi thêm, không sửa, không xóa.","Three roles: Super admin, Technician (limited to assigned groups), View only. An append-only audit log that cannot be edited or deleted.","三种角色：超级管理员、技术员（限所分配分组）、只读。日志只能追加，不能修改或删除。")),
 (("Cài trong vài phút","Installed in minutes","几分钟完成安装"),
  ("Bộ cài .bat cho server và agent, máy tự đăng ký không cần duyệt, hỗ trợ cả Windows 7 đến Windows 11 và Server.","A .bat installer for the server and the agents, machines register themselves without approval, supporting Windows 7 through Windows 11 and Server.","服务器和代理都有 .bat 安装程序，设备自动注册无需审批，支持 Windows 7 到 Windows 11 及 Server。")),
 (("Ba ngôn ngữ","Three languages","三种语言"),
  ("Console, server, agent và bộ cài đều có tiếng Việt, tiếng Anh và tiếng Trung giản thể.","The Console, server, agents and installers come in Vietnamese, English and Simplified Chinese.","控制台、服务器、代理和安装程序都提供越南语、英语和简体中文。")),
 (("Dữ liệu ở lại trong mạng của bạn","Your data stays on your network","数据留在您自己的网络内"),
  ("Không đám mây, không tài khoản bên thứ ba, không phí thuê bao. Mã nguồn đi kèm gói cài.","No cloud, no third-party account, no subscription. The source code ships with the package.","没有云、没有第三方账号、没有订阅费。安装包附带源代码。")),
]
INTRO_FIT = (("Phù hợp với","Good for","适用于"),
  [("Công ty nhỏ và vừa, văn phòng, phòng kế toán","Small and mid-sized companies and offices","中小型公司和办公室"),("Phòng máy trường học, trung tâm đào tạo","School computer labs and training centres","学校机房和培训中心"),("Quán net, cửa hàng có nhiều máy POS","Internet cafés and shops with many POS machines","网吧和拥有多台 POS 机的商店"),("Gia đình có vài máy cần trông chừng","Homes with a few machines to look after","需要照看几台电脑的家庭")])
INTRO_REQ = (("Yêu cầu","Requirements","系统要求"),
  [("Server: Windows 10/11, Server 2016+ hoặc Linux","Server: Windows 10/11, Server 2016+ or Linux","服务器：Windows 10/11、Server 2016+ 或 Linux"),("Client: Windows 7 đến 11, Server 2008 R2+","Clients: Windows 7 to 11, Server 2008 R2+","客户端：Windows 7 到 11、Server 2008 R2+"),("Mạng LAN, cổng TCP 7070","A LAN, TCP port 7070","局域网，TCP 7070 端口"),("Trình duyệt hiện đại để mở Console","A modern browser for the Console","用于打开控制台的现代浏览器")])
INTRO_NOT = (("Chưa có trong bản 1.0","Not in version 1.0","1.0 版尚未包含"),
  ("Điều khiển màn hình từ xa, kiểm kê phần mềm, báo cáo gửi tự động. Khóa màn hình và nhắn tin cần Windows Pro trở lên.","Remote screen control, software inventory, scheduled reports. Lock screen and messaging need Windows Pro or later.","远程屏幕控制、软件清点、自动报表。锁屏和消息需要 Windows 专业版及以上。"))

import os as _os
exec(open(_os.path.join(_os.path.dirname(_os.path.abspath(__file__)),'content_more.py'),encoding='utf8').read())
