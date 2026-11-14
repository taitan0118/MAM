<#
 MAM Server - cài đặt / install / 安装
 Chạy bằng quyền Administrator (nên dùng install-server.bat, nó tự xin quyền).
 Tùy chọn: -Port 7070  -Lang vi|en|zh  -DataDir "D:\MAMdata"
#>
param([string]$Lang = '', [int]$Port = 7070, [string]$DataDir = '')
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch { }
$ErrorActionPreference = 'Continue'   # lệnh native ghi stderr không được làm script dừng (PowerShell 5)
if (-not $Lang) { $c = (Get-UICulture).Name.ToLower(); if ($c.StartsWith('zh')) { $Lang = 'zh' } elseif ($c.StartsWith('vi')) { $Lang = 'vi' } else { $Lang = 'en' } }
$M = @{
  admin  = @('Cần chạy bằng quyền Administrator (bấm phải vào install-server.bat > Run as administrator).', 'Please run as Administrator (right-click install-server.bat > Run as administrator).', '请以管理员身份运行（右键 install-server.bat > 以管理员身份运行）。')
  nofile = @('Không thấy mam-server.exe cạnh file cài này.', 'mam-server.exe was not found next to this installer.', '在安装脚本旁边找不到 mam-server.exe。')
  copy   = @('Đang chép chương trình vào ', 'Copying the program to ', '正在复制程序到 ')
  fw     = @('Mở cổng tường lửa (mọi loại mạng, chỉ dải LAN) ', 'Opening the firewall port (any network type, local subnet only) ', '开放防火墙端口（任何网络类型，仅限本地子网） ')
  task   = @('Đăng ký chạy cùng Windows (tài khoản SYSTEM, tự chạy lại khi lỗi)', 'Registering to start with Windows (SYSTEM account, restarts on failure)', '注册为随 Windows 启动（SYSTEM 账户，出错自动重启）')
  start  = @('Đang khởi động server...', 'Starting the server...', '正在启动服务器...')
  ok     = @('XONG. Mở một trong các địa chỉ sau bằng trình duyệt để tạo tài khoản Super admin đầu tiên:', 'DONE. Open one of these addresses in a browser to create the first Super admin account:', '完成。用浏览器打开下列任一地址，创建第一个超级管理员账号：')
  fail   = @('Server chưa trả lời. Xem nhật ký: ', 'The server did not answer. See the log: ', '服务器没有响应。请查看日志：')
  data   = @('Dữ liệu nằm ở: ', 'Data is stored in: ', '数据保存在：')
}
function T($k) { $i = 0; if ($Lang -eq 'en') { $i = 1 } elseif ($Lang -eq 'zh') { $i = 2 }; return $M[$k][$i] }
$id = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not (New-Object Security.Principal.WindowsPrincipal($id)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { Write-Host (T 'admin'); exit 1 }
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$src = Join-Path $here 'mam-server.exe'
if (-not (Test-Path $src)) { Write-Host (T 'nofile'); exit 1 }
$dir = Join-Path $env:ProgramFiles 'MAM Server'
if (-not $DataDir) { $DataDir = Join-Path $env:ProgramData 'MAM' }
cmd /c 'schtasks /end /tn "MAM Server" >nul 2>&1'
Start-Sleep -Seconds 1
Write-Host ((T 'copy') + $dir)
New-Item -ItemType Directory -Force -Path $dir -ErrorAction Stop | Out-Null
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
Copy-Item $src (Join-Path $dir 'mam-server.exe') -Force -ErrorAction Stop
# agent đóng sẵn để nút "Tải agent" trong Console dùng được
foreach ($p in @(@('windows\mam-agent.exe','mam-agent.exe'), @('windows\mam-agent-x86.exe','mam-agent-x86.exe'), @('linux\mam-agent','mam-agent-linux-amd64'), @('linux\mam-agent-arm64','mam-agent-linux-arm64'), @('mac\mam-agent-amd64','mam-agent-mac-amd64'), @('mac\mam-agent-arm64','mam-agent-mac-arm64'))) {
  Copy-Item (Join-Path $here ('..\..\agent\' + $p[0])) (Join-Path $dir $p[1]) -Force -ErrorAction SilentlyContinue
}
Copy-Item (Join-Path $here 'LICENSE.txt') $dir -Force -ErrorAction SilentlyContinue
# dữ liệu có băm mật khẩu và token: chỉ SYSTEM và Administrators được đọc
icacls $DataDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
Write-Host ((T 'fw') + $Port)
netsh advfirewall firewall delete rule name="MAM Server" | Out-Null
netsh advfirewall firewall add rule name="MAM Server" dir=in action=allow protocol=TCP localport=$Port profile=any remoteip=LocalSubnet | Out-Null
Write-Host (T 'task')
$exe = Join-Path $dir 'mam-server.exe'
$xml = @"
<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>MAM Server - Missing App Monitor</Description></RegistrationInfo>
  <Triggers><BootTrigger><Enabled>true</Enabled></BootTrigger></Triggers>
  <Principals><Principal id="Author"><UserId>S-1-5-18</UserId><RunLevel>HighestAvailable</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit><Hidden>true</Hidden><StartWhenAvailable>true</StartWhenAvailable>
    <RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure>
  </Settings>
  <Actions Context="Author"><Exec><Command>"$exe"</Command><Arguments>-addr :$Port -data "$DataDir" -lang $Lang</Arguments></Exec></Actions>
</Task>
"@
$tmp = Join-Path $env:TEMP 'mam-server-task.xml'
[IO.File]::WriteAllText($tmp, $xml, [Text.Encoding]::Unicode)
schtasks /create /tn 'MAM Server' /xml $tmp /f | Out-Null
$rc = $LASTEXITCODE
Remove-Item $tmp -Force
if ($rc -ne 0) { Write-Host ('schtasks /create failed, code ' + $rc); exit 1 }
Write-Host (T 'start')
schtasks /run /tn 'MAM Server' | Out-Null
$up = $false
for ($i = 0; $i -lt 15; $i++) {
  Start-Sleep -Seconds 1
  try { $wc = New-Object Net.WebClient; $null = $wc.DownloadString("http://127.0.0.1:$Port/api/status"); $up = $true; break } catch { }
}
if (-not $up) { Write-Host ((T 'fail') + (Join-Path $DataDir 'server.log')); exit 1 }
Write-Host ''
Write-Host (T 'ok')
Write-Host "  http://localhost:$Port"
foreach ($a in @(Get-WmiObject Win32_NetworkAdapterConfiguration | Where-Object { $_.IPEnabled })) { foreach ($ip in @($a.IPAddress)) { if ($ip -and $ip -notmatch ':') { Write-Host "  http://${ip}:$Port" } } }
Write-Host ''
Write-Host ((T 'data') + $DataDir)
