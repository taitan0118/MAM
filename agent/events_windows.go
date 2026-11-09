package main

import (
	"strconv"
	"time"
)

// Bỏ DistributedCOM: Windows nào cũng ghi hàng loạt lỗi 10016 vô hại, chỉ làm rối danh sách.
const eventsPS = `
function Get-MamEvents([int64]$since) {
  $out = @()
  $t0 = ([datetime]'1970-01-01').AddSeconds($since).ToLocalTime()
  try {
    $ev = Get-WinEvent -FilterHashtable @{LogName='System','Application'; Level=1,2; StartTime=$t0} -MaxEvents 3000 -ErrorAction Stop
    foreach ($e in $ev) {
      if ($e.ProviderName -eq 'Microsoft-Windows-DistributedCOM') { continue }
      $m = ''
      try { $m = [string]$e.Message } catch {}
      $m = (($m -split "` + "`" + `r?` + "`" + `n") | Where-Object { $_.Trim() } | Select-Object -First 2) -join ' '
      if ($m.Length -gt 400) { $m = $m.Substring(0, 400) }
      $out += @{ id = ($e.LogName.Substring(0,3) + [string]$e.RecordId); at = [int64](($e.TimeCreated.ToUniversalTime() - [datetime]'1970-01-01').TotalSeconds); log = [string]$e.LogName; src = [string]$e.ProviderName; eid = [int]$e.Id; lvl = [int]$e.Level; msg = $m }
    }
  } catch {}
  return ,$out
}
`

// scanEvents: ok=false khi PowerShell không chạy được (lần sau đọc lại từ mốc cũ).
func scanEvents(since int64) ([]SysEvent, bool) {
	b, err := runPS(eventsPS+"\nWrite-Output (ConvertTo-MamJson (Get-MamEvents "+strconv.FormatInt(since, 10)+"))", 3*time.Minute)
	if err != nil {
		return nil, false
	}
	return toEvents(b), true
}
