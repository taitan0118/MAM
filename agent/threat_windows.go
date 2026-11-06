package main

import "time"

// Get-MamThreats nằm trong đây (không để trong lib.ps1) để phần quét mã độc tách riêng, lỗi cũng không ảnh hưởng phần cứng.
const threatPS = `
function Get-MamThreats {
  $script:out = @()
  function UnixOf($d) { [int64](([datetime]$d).ToUniversalTime() - [datetime]'1970-01-01').TotalSeconds }
  function Sha($s) { $h = [Security.Cryptography.SHA1]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($s)); (($h | Select-Object -First 6 | ForEach-Object { $_.ToString('x2') }) -join '') }
  # 1) Virus do Defender phat hien (su kien 1116) va ket qua xu ly (1117)
  try {
    $ev = @(Get-WinEvent -FilterHashtable @{LogName='Microsoft-Windows-Windows Defender/Operational'; Id=1116,1117; StartTime=(Get-Date).AddDays(-2)} -ErrorAction Stop)
    $acts = @{}
    $det = @()
    foreach ($e in $ev) {
      $x = [xml]$e.ToXml(); $d = @{}
      foreach ($n in $x.Event.EventData.Data) { $d[$n.Name] = [string]$n.'#text' }
      $key = ($d['Threat Name'] + '|' + $d['Path'])
      if ($e.Id -eq 1117) { if (-not $acts.ContainsKey($key)) { $acts[$key] = $d['Action Name'] } }
      else { $det += ,@($e.TimeCreated, $d, $key) }
    }
    foreach ($z in $det) {
      $d = $z[1]; $raw = [string]$d['Path']; $path = ''
      if ($raw -match '(?i)(?:file|containerfile):_([^;]+)') { $path = $Matches[1].Trim() }
      $act = 'Chưa xử lý'
      if ($acts.ContainsKey($z[2])) { $act = 'Đã xử lý (' + $acts[$z[2]] + ')' }
      $drv = ''; $zu = ''; $zr = ''
      try {
        if ($path -like '\\*') { $drv = 'Network' }
        elseif ($path -match '^[A-Za-z]:') { $drv = [string](New-Object IO.DriveInfo $path.Substring(0,1)).DriveType }
      } catch {}
      try {
        if ($path -and (Test-Path -LiteralPath $path)) {
          foreach ($ln in (Get-Content -LiteralPath $path -Stream Zone.Identifier -ErrorAction Stop)) {
            if ($ln -like 'HostUrl=*') { $zu = $ln.Substring(8) } elseif ($ln -like 'ReferrerUrl=*') { $zr = $ln.Substring(12) }
          }
        }
      } catch {}
      $script:out += @{ id = 'v' + (Sha ([string]$d['Detection ID'] + $z[2])); kind = 'virus'; name = [string]$d['Threat Name']; path = $path; process = [string]$d['Process Name']; action = $act; at = (UnixOf $z[0]); drive = $drv; zoneUrl = $zu; zoneRef = $zr; note = '' }
    }
  } catch {}
  # 2) Dau hieu nghi ngo
  $now = UnixOf (Get-Date)
  $day = (Get-Date).ToString('yyyy') + [string][math]::Floor((Get-Date).DayOfYear / 7)
  function Sus($name, $note, $proc) { $script:out += @{ id = 's' + (Sha ($name + $note + $day)); kind = 'suspect'; name = $name; path = ''; process = $proc; action = 'Cần kiểm tra'; at = $now; drive = ''; zoneUrl = ''; zoneRef = ''; note = $note } }
  try {
    $st = Get-MpComputerStatus -ErrorAction Stop
    if ($st.AMServiceEnabled -and -not $st.RealTimeProtectionEnabled) { Sus 'Tắt bảo vệ thời gian thực của Defender' 'Windows Defender đang bật nhưng bảo vệ thời gian thực bị tắt' '' }
    $pr = Get-MpPreference -ErrorAction Stop
    foreach ($p in @($pr.ExclusionPath)) {
      if ($p -match '(?i)^[A-Z]:\\?$|\\temp\\?$|\\downloads\\?$|\\users\\public|\\appdata\\') { Sus 'Loại trừ nguy hiểm trong Defender' ('Thư mục không bị quét: ' + $p) '' }
    }
    foreach ($e in @($pr.ExclusionExtension)) { if ($e -match '(?i)^\.?(exe|dll|ps1|bat|cmd|vbs|js|scr)$') { Sus 'Loại trừ nguy hiểm trong Defender' ('Đuôi file không bị quét: ' + $e) '' } }
  } catch {}
  try {
    $n = 0
    foreach ($p in (Get-CimInstance Win32_Process -ErrorAction Stop)) {
      if (-not $p.ExecutablePath) { continue }
      $bad = $false; $why = ''
      if ($p.Name -match '(?i)^(xmrig|minerd|nbminer|t-rex|phoenixminer|ethminer|cpuminer|nicehash)') { $bad = $true; $why = 'tên giống phần mềm đào coin' }
      elseif ($p.ExecutablePath -match '(?i)\\(temp|downloads|users\\public)\\|\$recycle\.bin|\\windows\\temp\\') {
        $sg = Get-AuthenticodeSignature -LiteralPath $p.ExecutablePath -ErrorAction SilentlyContinue
        if ($sg -and $sg.Status -ne 'Valid') { $bad = $true; $why = 'chạy từ thư mục tạm/tải về và không có chữ ký số hợp lệ' }
      }
      if ($bad) { Sus ('Tiến trình đáng ngờ: ' + $p.Name) ($p.ExecutablePath + ' (' + $why + ')') $p.Name; $n++; if ($n -ge 8) { break } }
    }
  } catch {}
  try {
    $keys = @('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run')
    foreach ($sid in (Get-ChildItem Registry::HKEY_USERS -ErrorAction SilentlyContinue)) { $keys += ('Registry::' + $sid.Name + '\SOFTWARE\Microsoft\Windows\CurrentVersion\Run') }
    foreach ($k in $keys) {
      $pp = Get-ItemProperty -Path $k -ErrorAction SilentlyContinue
      if (-not $pp) { continue }
      foreach ($pr in $pp.PSObject.Properties) {
        if ($pr.Name -like 'PS*') { continue }
        if ([string]$pr.Value -match '(?i)\\(temp|downloads|users\\public)\\|\\appdata\\local\\temp') { Sus 'Chương trình tự chạy khi mở máy từ thư mục tạm' ($pr.Name + ' = ' + $pr.Value) '' }
      }
    }
  } catch {}
  return ,$script:out
}
`

func scanThreats() []Threat {
	b, err := runPS(threatPS+"\nWrite-Output (ConvertTo-MamJson (Get-MamThreats))", 90*time.Second)
	if err != nil {
		return nil
	}
	return toThreats(b)
}
