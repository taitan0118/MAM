<#
 MAM Agent (PowerShell) - cho Windows 7/8/8.1/10/11 và Windows Server 2008 R2 trở lên.
 Cài:   powershell -ExecutionPolicy Bypass -File MAM-Agent.ps1 -Install -Server http://192.168.1.10:7070
 Gỡ:    powershell -ExecutionPolicy Bypass -File "C:\Program Files\MAM Agent\MAM-Agent.ps1" -Uninstall
 Chạy PowerShell 2.0 (có sẵn trên Windows 7), không cần cài thêm gì.
#>
param([switch]$Install, [switch]$Uninstall, [switch]$Run, [string]$Server = '', [string]$Key = '', [string]$Name = '', [string]$Lang = '')

try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch { }
$Version = '1.1.0'
$ScriptPath = $MyInvocation.MyCommand.Path
$Dir = Join-Path $env:ProgramFiles 'MAM Agent'
$CfgPath = Join-Path $Dir 'config.json'
$LogPath = Join-Path $Dir 'agent.log'
$ErrorActionPreference = 'Continue'

#LIB#

Add-Type -AssemblyName System.Web.Extensions
$Ser = New-Object System.Web.Script.Serialization.JavaScriptSerializer
$Ser.MaxJsonLength = 10485760
try { [Net.ServicePointManager]::SecurityProtocol = 3072 -bor 768 -bor 192 } catch { }

# Ngôn ngữ của agent: vi (mặc định), en, zh. Chọn lúc cài bằng -Lang, lưu trong config.json.
$script:L = 'vi'
function Set-MamLang($l) { $l = ([string]$l).ToLower(); if ($l.StartsWith('zh')) { $script:L = 'zh' } elseif ($l.StartsWith('en')) { $script:L = 'en' } else { $script:L = 'vi' } }
$Tx = @{
  'Thiếu -Server. Ví dụ: -Install -Server http://192.168.1.10:7070' = @('Missing -Server. Example: -Install -Server http://192.168.1.10:7070', '缺少 -Server。示例：-Install -Server http://192.168.1.10:7070')
  'Không đọc được config.json' = @('Cannot read config.json', '无法读取 config.json')
  ' khởi động, server ' = @(' started, server ', ' 已启动，服务器 ')
  'Không lấy được phần cứng: ' = @('Cannot read hardware information: ', '无法获取硬件信息：')
  'Chưa đăng ký được (' = @('Cannot register yet (', '尚未注册成功（')
  'Đã đăng ký với server, mã máy ' = @('Registered with the server, machine id ', '已向服务器注册，设备编号 ')
  'Token không còn hợp lệ, đăng ký lại' = @('Token no longer valid, registering again', '令牌已失效，重新注册')
  'Máy đã bị thu hồi khỏi hệ thống' = @('This machine has been revoked from the system', '此设备已被系统吊销')
  'Không liên lạc được server (' = @('Cannot reach the server (', '无法连接服务器（')
  'Lỗi vòng lặp: ' = @('Loop error: ', '循环出错：')
  'Đã cài MAM Agent vào ' = @('MAM Agent installed in ', 'MAM 代理已安装到 ')
  ' và đăng ký chạy cùng Windows. Máy sẽ hiện trên Console sau vài giây.' = @(' and registered to start with Windows. The machine will appear in the Console within seconds.', ' 并已注册为随 Windows 启动。几秒钟后设备会出现在控制台中。')
  'Không tìm thấy agent đã cài.' = @('No installed agent found.', '未找到已安装的代理。')
  'Tài khoản Super admin' = @('Super admin username', '超级管理员账号')
  'Mật khẩu' = @('Password', '密码')
  'Bị từ chối: ' = @('Refused: ', '被拒绝：')
  'Đã gỡ MAM Agent.' = @('MAM Agent uninstalled.', 'MAM 代理已卸载。')
  'Dùng: -Install -Server http://IP:7070 [-Lang en]  |  -Uninstall  |  -Run' = @('Usage: -Install -Server http://IP:7070 [-Lang en]  |  -Uninstall  |  -Run', '用法：-Install -Server http://IP:7070 [-Lang zh]  |  -Uninstall  |  -Run')
  'MAM: quản trị viên yêu cầu khởi động lại máy' = @('MAM: an administrator requested a restart', 'MAM：管理员请求重启计算机')
  'MAM: quản trị viên yêu cầu tắt máy' = @('MAM: an administrator requested a shutdown', 'MAM：管理员请求关闭计算机')
}
function T($k) { if ($script:L -eq 'vi' -or -not $Tx.ContainsKey($k)) { return $k }; if ($script:L -eq 'zh') { return $Tx[$k][1] } else { return $Tx[$k][0] } }

function Write-Log($m) {
  try {
    if ((Test-Path $LogPath) -and ((Get-Item $LogPath).Length -gt 2MB)) { Remove-Item $LogPath -Force }
    Add-Content -Path $LogPath -Value ((Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + '  ' + $m)
  } catch { }
}

function Load-Config { if (Test-Path $CfgPath) { return $Ser.DeserializeObject((Get-Content $CfgPath -Raw)) } return $null }
function Save-Config($c) { Set-Content -Path $CfgPath -Value (ConvertTo-MamJson $c) -Encoding UTF8 }

# Gửi POST JSON, trả về @{ code; body }
function Invoke-Mam($path, $obj, $token) {
  $wc = New-Object System.Net.WebClient
  $wc.Encoding = [Text.Encoding]::UTF8
  $wc.Headers['Content-Type'] = 'application/json'
  if ($token) { $wc.Headers['Authorization'] = 'Bearer ' + $token }
  try {
    $body = $wc.UploadString(($script:Cfg['server']).TrimEnd('/') + $path, 'POST', (ConvertTo-MamJson $obj))
    return @{ code = 200; body = $body }
  } catch [System.Net.WebException] {
    $r = $_.Exception.Response
    if ($r -ne $null) {
      $sr = New-Object IO.StreamReader($r.GetResponseStream()); $txt = $sr.ReadToEnd(); $sr.Close()
      return @{ code = [int]$r.StatusCode; body = $txt }
    }
    return @{ code = 0; body = $_.Exception.Message }
  }
}

function Get-Net {
  foreach ($n in @(Get-WmiObject Win32_NetworkAdapterConfiguration | Where-Object { $_.IPEnabled -and $_.DefaultIPGateway })) {
    foreach ($ip in @($n.IPAddress)) { if ($ip -and $ip -notmatch ':') { return @{ ip = [string]$ip; mac = ([string]$n.MACAddress).ToUpper() } } }
  }
  return @{ ip = ''; mac = '' }
}

function Get-MamMetrics {
  $cpus = @(Get-WmiObject Win32_Processor); $sum = 0; foreach ($c in $cpus) { $sum += [double]$c.LoadPercentage }
  $cpu = 0; if ($cpus.Count -gt 0) { $cpu = $sum / $cpus.Count }
  $os = Get-WmiObject Win32_OperatingSystem
  $ram = 0; if ($os.TotalVisibleMemorySize -gt 0) { $ram = 100 * (1 - ($os.FreePhysicalMemory / $os.TotalVisibleMemorySize)) }
  $up = [int]((Get-Date) - $os.ConvertToDateTime($os.LastBootUpTime)).TotalSeconds
  $disks = @()
  foreach ($d in @(Get-WmiObject Win32_LogicalDisk -Filter 'DriveType=3')) {
    if ($d.Size) { $disks += @{ name = [string]$d.DeviceID; totalGB = [math]::Round($d.Size / 1GB, 1); freeGB = [math]::Round($d.FreeSpace / 1GB, 1) } }
  }
  $user = [string](Get-WmiObject Win32_ComputerSystem).UserName
  return @{ cpu = [math]::Round($cpu, 1); ram = [math]::Round($ram, 1); uptime = $up; disks = $disks; user = $user }
}

function Invoke-MamCommand($type, $args2) {
  try {
    switch ($type) {
      'restart'  { & shutdown.exe /r /t 10 /c (T 'MAM: quản trị viên yêu cầu khởi động lại máy'); return @($true, 'Đã thực hiện') }
      'shutdown' { & shutdown.exe /s /t 10 /c (T 'MAM: quản trị viên yêu cầu tắt máy'); return @($true, 'Đã thực hiện') }
      'lock'     { & tsdiscon.exe console 2>&1 | Out-Null; return @($true, 'Đã thực hiện') }   # cần Windows Pro trở lên
      'message'  { & msg.exe * /TIME:120 ([string]$args2['text']) 2>&1 | Out-Null; return @($true, 'Đã thực hiện') }
      default    { return @($false, 'Lệnh không hỗ trợ: ' + $type) }
    }
  } catch { return @($false, 'Lỗi: ' + $_.Exception.Message) }
}

function Start-MamLoop {
  $script:Cfg = Load-Config
  if ($script:Cfg -eq $null) { Write-Log (T 'Không đọc được config.json'); return }
  if ($script:Cfg.ContainsKey('lang')) { Set-MamLang $script:Cfg['lang'] }
  Write-Log ('MAM Agent PowerShell ' + $Version + (T ' khởi động, server ') + $script:Cfg['server'])
  Start-Sleep -Seconds (Get-Random -Minimum 0 -Maximum 4)
  $hw = $null; $hwAt = [DateTime]::MinValue; $temp = $null; $tempAt = [DateTime]::MinValue; $interval = 5
  while ($true) {
    try {
      if ($hw -eq $null -or ((Get-Date) - $hwAt).TotalMinutes -gt 60) { try { $hw = Get-MamHw } catch { Write-Log ((T 'Không lấy được phần cứng: ') + $_.Exception.Message) }; $hwAt = Get-Date }
      if (-not $script:Cfg['token']) {
        $nm = $script:Cfg['name']; if (-not $nm) { $nm = $env:COMPUTERNAME }
        $net = Get-Net
        $os = [string](Get-WmiObject Win32_OperatingSystem).Caption
        $r = Invoke-Mam '/api/agent/enroll' @{ name = $nm; os = $os; mac = $net.mac; ip = $net.ip; key = [string]$script:Cfg['key']; hw = $hw } $null
        if ($r.code -ne 200) { Write-Log ((T 'Chưa đăng ký được (') + $r.code + '): ' + $r.body); Start-Sleep -Seconds 15; continue }
        $o = $Ser.DeserializeObject($r.body); $script:Cfg['id'] = $o['id']; $script:Cfg['token'] = $o['token']; Save-Config $script:Cfg
        Write-Log ((T 'Đã đăng ký với server, mã máy ') + $o['id'])
      }
      if (((Get-Date) - $tempAt).TotalSeconds -gt 60) { $temp = Get-MamTemp; $tempAt = Get-Date }
      $m = Get-MamMetrics; $net = Get-Net
      $rep = @{ cpu = $m.cpu; ram = $m.ram; temp = $temp; uptime = $m.uptime; disks = $m.disks; user = $m.user; ip = $net.ip; ver = $Version }
      if (((Get-Date) - $hwAt).TotalSeconds -lt 10) { $rep['hw'] = $hw }
      $r = Invoke-Mam '/api/agent/report' $rep $script:Cfg['token']
      if ($r.code -eq 401) { Write-Log (T 'Token không còn hợp lệ, đăng ký lại'); $script:Cfg['token'] = ''; continue }
      if ($r.code -eq 403) { Write-Log (T 'Máy đã bị thu hồi khỏi hệ thống'); Start-Sleep -Seconds 300; continue }
      if ($r.code -eq 200) {
        $o = $Ser.DeserializeObject($r.body)
        if ($o['interval'] -ge 2 -and $o['interval'] -le 60) { $interval = [int]$o['interval'] }
        foreach ($c in @($o['commands'])) {
          if ($c -eq $null) { continue }
          $res = Invoke-MamCommand ([string]$c['type']) $c['args']
          [void](Invoke-Mam '/api/agent/result' @{ id = [string]$c['id']; ok = [bool]$res[0]; output = [string]$res[1] } $script:Cfg['token'])
        }
      } else { Write-Log ((T 'Không liên lạc được server (') + $r.code + ')') }
    } catch { Write-Log ((T 'Lỗi vòng lặp: ') + $_.Exception.Message) }
    Start-Sleep -Seconds $interval
  }
}

function Install-Mam {
  if ($Lang) { Set-MamLang $Lang }
  if (-not $Server) { Write-Host (T 'Thiếu -Server. Ví dụ: -Install -Server http://192.168.1.10:7070'); exit 2 }
  if ($Server -notmatch '^https?://') { $script:Server = 'http://' + $Server }
  New-Item -ItemType Directory -Force -Path $Dir | Out-Null
  & schtasks.exe /end /tn 'MAM Agent' 2>&1 | Out-Null
  $dst = Join-Path $Dir 'MAM-Agent.ps1'
  if ($ScriptPath -ne $dst) { Copy-Item -Path $ScriptPath -Destination $dst -Force }
  Set-Content -Path $CfgPath -Value (ConvertTo-MamJson @{ server = $Server; key = $Key; name = $Name; lang = $script:L; id = ''; token = '' }) -Encoding UTF8
  & icacls.exe $Dir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' 2>&1 | Out-Null
  $xml = @"
<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>MAM Agent - giám sát máy trong mạng LAN</Description></RegistrationInfo>
  <Triggers>
    <BootTrigger><Enabled>true</Enabled></BootTrigger>
    <TimeTrigger><Repetition><Interval>PT1M</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition><StartBoundary>2020-01-01T00:00:00</StartBoundary><Enabled>true</Enabled></TimeTrigger>
  </Triggers>
  <Principals><Principal id="Author"><UserId>S-1-5-18</UserId><RunLevel>HighestAvailable</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>false</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure>
  </Settings>
  <Actions Context="Author"><Exec><Command>powershell.exe</Command><Arguments>-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$dst" -Run</Arguments></Exec></Actions>
</Task>
"@
  $xp = Join-Path $env:TEMP 'mam-task.xml'
  [IO.File]::WriteAllText($xp, $xml, [Text.Encoding]::Unicode)
  & schtasks.exe /create /tn 'MAM Agent' /xml $xp /f
  Remove-Item $xp -Force -ErrorAction SilentlyContinue
  & schtasks.exe /run /tn 'MAM Agent' | Out-Null
  Write-Host ((T 'Đã cài MAM Agent vào ') + $Dir + (T ' và đăng ký chạy cùng Windows. Máy sẽ hiện trên Console sau vài giây.'))
}

function Uninstall-Mam {
  $script:Cfg = Load-Config
  if ($script:Cfg -eq $null) { Write-Host (T 'Không tìm thấy agent đã cài.'); exit 1 }
  if ($script:Cfg.ContainsKey('lang')) { Set-MamLang $script:Cfg['lang'] }
  $u = Read-Host (T 'Tài khoản Super admin')
  $sec = Read-Host (T 'Mật khẩu') -AsSecureString
  $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec); $pw = [Runtime.InteropServices.Marshal]::PtrToStringAuto($bstr)
  $r = Invoke-Mam '/api/agent/uninstall' @{ user = $u; password = $pw } $script:Cfg['token']
  if ($r.code -ne 200) { Write-Host ((T 'Bị từ chối: ') + $r.body); exit 1 }
  & schtasks.exe /end /tn 'MAM Agent' 2>&1 | Out-Null
  & schtasks.exe /delete /tn 'MAM Agent' /f 2>&1 | Out-Null
  Start-Process cmd.exe -ArgumentList ('/c ping 127.0.0.1 -n 4 >nul & rmdir /s /q "' + $Dir + '"') -WindowStyle Hidden
  Write-Host (T 'Đã gỡ MAM Agent.')
}

if ($Install) { Install-Mam } elseif ($Uninstall) { Uninstall-Mam } elseif ($Run) { Start-MamLoop } else { Write-Host (T 'Dùng: -Install -Server http://IP:7070 [-Lang en]  |  -Uninstall  |  -Run') }
