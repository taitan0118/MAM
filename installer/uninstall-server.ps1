<# MAM Server - gỡ / uninstall / 卸载. Tùy chọn: -Lang vi|en|zh  -PurgeData (xóa cả dữ liệu) #>
param([string]$Lang = '', [switch]$PurgeData, [string]$DataDir = '')
if (-not $Lang) { $c = (Get-UICulture).Name.ToLower(); if ($c.StartsWith('zh')) { $Lang = 'zh' } elseif ($c.StartsWith('en')) { $Lang = 'en' } else { $Lang = 'vi' } }
$M = @{
  admin = @('Cần chạy bằng quyền Administrator.', 'Please run as Administrator.', '请以管理员身份运行。')
  done  = @('Đã gỡ MAM Server. Dữ liệu được giữ lại ở: ', 'MAM Server removed. Your data was kept in: ', '已卸载 MAM Server。数据保留在：')
  gone  = @('Đã gỡ MAM Server và xóa dữ liệu.', 'MAM Server removed and the data deleted.', '已卸载 MAM Server 并删除数据。')
  ask   = @('Gõ XOA để xác nhận xóa toàn bộ dữ liệu (tài khoản, máy, nhật ký): ', 'Type DELETE to confirm deleting ALL data (accounts, machines, audit log): ', '输入 DELETE 确认删除全部数据（账号、设备、日志）：')
  skip  = @('Không xóa dữ liệu.', 'Data was not deleted.', '未删除数据。')
}
function T($k) { $i = 0; if ($Lang -eq 'en') { $i = 1 } elseif ($Lang -eq 'zh') { $i = 2 }; return $M[$k][$i] }
$id = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not (New-Object Security.Principal.WindowsPrincipal($id)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { Write-Host (T 'admin'); exit 1 }
if (-not $DataDir) { $DataDir = Join-Path $env:ProgramData 'MAM' }
cmd /c 'schtasks /end /tn "MAM Server" >nul 2>&1'
cmd /c 'schtasks /delete /tn "MAM Server" /f >nul 2>&1'
Get-Process mam-server -ErrorAction SilentlyContinue | Stop-Process -Force
netsh advfirewall firewall delete rule name="MAM Server" | Out-Null
Start-Sleep -Seconds 1
Remove-Item (Join-Path $env:ProgramFiles 'MAM Server') -Recurse -Force -ErrorAction SilentlyContinue
if ($PurgeData) {
  $a = Read-Host (T 'ask')
  if ($a -eq 'XOA' -or $a -eq 'DELETE') { Remove-Item $DataDir -Recurse -Force; Write-Host (T 'gone'); exit 0 } else { Write-Host (T 'skip') }
}
Write-Host ((T 'done') + $DataDir)
