# MAM · thư viện PowerShell dùng chung (chạy được từ PowerShell 2.0 trên Windows 7)
function ConvertTo-MamJson($v) {
  if ($v -eq $null) { return 'null' }
  if ($v -is [string]) { $s = (($v -replace '\\','\\' -replace '"','\"') -replace '[\x00-\x1f]',' '); return '"' + [regex]::Replace($s, '[^\x00-\x7e]', { param($m) '\u' + ([int][char]$m.Value).ToString('x4') }) + '"' }  # ASCII + \uXXXX: không phụ thuộc bảng mã đầu ra
  if ($v -is [bool]) { if ($v) { return 'true' } else { return 'false' } }
  if ($v -is [int] -or $v -is [long] -or $v -is [double] -or $v -is [decimal] -or $v -is [single] -or $v -is [uint32] -or $v -is [uint64] -or $v -is [int16] -or $v -is [uint16] -or $v -is [byte]) {
    return [string]::Format([Globalization.CultureInfo]::InvariantCulture, '{0}', $v)
  }
  if ($v -is [System.Collections.IDictionary]) {
    $p = @(); foreach ($k in $v.Keys) { $p += ('"' + $k + '":' + (ConvertTo-MamJson $v[$k])) }
    return '{' + ($p -join ',') + '}'
  }
  if ($v -is [System.Collections.IEnumerable]) {
    $p = @(); foreach ($i in $v) { $p += (ConvertTo-MamJson $i) }
    return '[' + ($p -join ',') + ']'
  }
  return ConvertTo-MamJson ([string]$v)
}

function Get-MamTemp {
  try {
    $t = @(Get-WmiObject -Namespace 'root\wmi' -Class MSAcpi_ThermalZoneTemperature -ErrorAction Stop)
    # Vùng nào ra số không thể có ở máy đang chạy (dưới 20°C hoặc trên 110°C) là BIOS khai báo rác, bỏ.
    $max = $null
    foreach ($z in $t) { $c = ($z.CurrentTemperature / 10) - 273.15; if ($c -ge 20 -and $c -le 110 -and ($max -eq $null -or $c -gt $max)) { $max = $c } }
    if ($max -ne $null) { return [math]::Round($max, 1) }
  } catch { }
  return $null
}

function Get-MamHw {
  $os = Get-WmiObject Win32_OperatingSystem
  $cs = Get-WmiObject Win32_ComputerSystem
  $cpu = @(Get-WmiObject Win32_Processor)
  $gpu = @(Get-WmiObject Win32_VideoController)
  $bd = Get-WmiObject Win32_BaseBoard
  $bios = Get-WmiObject Win32_BIOS
  $fail = $false
  try { foreach ($x in @(Get-WmiObject -Namespace 'root\wmi' -Class MSStorageDriver_FailurePredictStatus -ErrorAction Stop)) { if ($x.PredictFailure) { $fail = $true } } } catch { }
  $disks = @()
  foreach ($d in @(Get-WmiObject Win32_DiskDrive)) {
    $disks += @{ model = [string]$d.Model; sizeGB = [math]::Round($d.Size / 1GB, 0); type = [string]$d.MediaType; health = [string]$d.Status; tempC = $null; wear = $null; hours = $null; serial = ([string]$d.SerialNumber).Trim(); pnp = ([string]$d.PNPDeviceID) }
  }
  try {
    if (Get-Command Get-PhysicalDisk -ErrorAction SilentlyContinue) {
      # Ghép theo số sê-ri (hoặc tên), KHÔNG theo thứ tự: thứ tự Win32_DiskDrive và Get-PhysicalDisk có thể ngược nhau,
      # ghép theo thứ tự thì SSD bị ghi thành HDD và ngược lại. Không khớp được thì để nguyên, không đoán.
      $pds = @(Get-PhysicalDisk)
      for ($i = 0; $i -lt $disks.Count; $i++) {
        $sn = [string]$disks[$i].serial
        $pd = $null
        if ($sn) { $pd = @($pds | Where-Object { ([string]$_.SerialNumber).Trim() -eq $sn })[0] }
        if (-not $pd) { $pd = @($pds | Where-Object { ([string]$_.FriendlyName).Trim() -eq ([string]$disks[$i].model).Trim() })[0] }
        if (-not $pd) { continue }
        $disks[$i].health = [string]$pd.HealthStatus; $disks[$i].type = [string]$pd.MediaType
        try { $rc = $pd | Get-StorageReliabilityCounter -ErrorAction Stop
          $disks[$i].tempC = $rc.Temperature; $disks[$i].wear = $rc.Wear; $disks[$i].hours = $rc.PowerOnHours } catch { }
      }
    }
  } catch { }
  # Chỉ số S.M.A.R.T thật của ổ: Windows chỉ báo Unhealthy khi ổ gần chết, SSD mòn 63% vẫn ra Healthy và Wear = 0.
  # Ghép theo PNPDeviceID (InstanceName của WMI = PNPDeviceID + '_0'), chắc hơn ghép theo số sê-ri.
  try {
    $sd = @{}; $sf = @{}
    try { foreach ($x in @(Get-WmiObject -Namespace 'root\wmi' -Class MSStorageDriver_FailurePredictData -ErrorAction Stop)) { $sd[([string]$x.InstanceName).ToUpper()] = $x.VendorSpecific } } catch { }
    try { foreach ($x in @(Get-WmiObject -Namespace 'root\wmi' -Class MSStorageDriver_FailurePredictStatus -ErrorAction Stop)) { $sf[([string]$x.InstanceName).ToUpper()] = [bool]$x.PredictFailure } } catch { }
    for ($i = 0; $i -lt $disks.Count; $i++) {
      $key = ([string]$disks[$i].pnp).ToUpper() + '_0'
      $b = $sd[$key]
      if ($sf.ContainsKey($key)) { $disks[$i].predictFail = $sf[$key] }
      if (-not $b) { continue }
      $at = @{}; $va = @{}
      for ($j = 2; $j + 11 -lt $b.Count; $j += 12) {
        $id = [int]$b[$j]
        if ($id -eq 0) { continue }
        $raw = [int64]$b[$j + 5] + [int64]$b[$j + 6] * 256 + [int64]$b[$j + 7] * 65536 + [int64]$b[$j + 8] * 16777216
        $at[$id] = $raw; $va[$id] = [int]$b[$j + 3]
      }
      $disks[$i].smart = $true
      if ($at.ContainsKey(9)) { $disks[$i].hours = [int64]$at[9] }
      if ($at.ContainsKey(194)) { $disks[$i].tempC = [int]([int64]$at[194] -band 255) }
      foreach ($m in @(@(5, 'realloc'), @(197, 'pending'), @(198, 'offline'), @(187, 'uncorrect'))) {
        if ($at.ContainsKey($m[0])) { $disks[$i][$m[1]] = [int64]$at[$m[0]] }
      }
      # % sức khỏe: mỗi chỉ số SMART có giá trị chuẩn hóa 100 = như mới, tụt dần khi ổ yếu.
      # Lấy giá trị thấp nhất trong các chỉ số về lỗi và tuổi thọ; bỏ giá trị 0 hoặc >100 (ổ không hỗ trợ).
      $hp = $null
      foreach ($id in 5, 10, 177, 187, 196, 197, 198, 231) {
        if ($va.ContainsKey($id) -and $va[$id] -ge 1 -and $va[$id] -le 100 -and ($hp -eq $null -or $va[$id] -lt $hp)) { $hp = $va[$id] }
      }
      if ($hp -ne $null) { $disks[$i].healthPct = $hp }
      # Tuổi thọ SSD còn lại: chỉ số E7 (231) hoặc B1 (177); HDD không có, để trống chứ không đoán.
      if (([string]$disks[$i].type) -match 'SSD') {
        foreach ($id in 231, 177) {
          if ($va.ContainsKey($id) -and $va[$id] -ge 0 -and $va[$id] -le 100) { $disks[$i].lifeLeft = $va[$id]; break }
        }
      }
    }
  } catch { }
  # Loại RAM (DDR3/DDR4/DDR5) để Kiểm kê hiện rõ; Windows 7 không có SMBIOSMemoryType thì dùng MemoryType
  $ramType = ''
  $ramSticks = 0
  $ramSlots = 0
  try {
    $mt = @{ 20 = 'DDR'; 21 = 'DDR2'; 24 = 'DDR3'; 26 = 'DDR4'; 34 = 'DDR5' }
    $ts = @()
    $mems = @(Get-WmiObject Win32_PhysicalMemory)
    $ramSticks = $mems.Count
    # Tổng số khe của bo mạch; máy ảo hoặc BIOS khai thiếu thì để 0, Console chỉ hiện số thanh
    try { foreach ($a in @(Get-WmiObject Win32_PhysicalMemoryArray)) { $ramSlots += [int]$a.MemoryDevices } } catch { }
    if ($ramSlots -lt $ramSticks) { $ramSlots = 0 }
    foreach ($m in $mems) {
      $k = 0
      try { $k = [int]$m.SMBIOSMemoryType } catch { }
      if (-not $mt[$k]) { try { $k = [int]$m.MemoryType } catch { } }
      if ($mt[$k] -and $ts -notcontains $mt[$k]) { $ts += $mt[$k] }
    }
    $ramType = ($ts -join '/')
  } catch { }
  $macs = @(); $ips = @()
  foreach ($n in @(Get-WmiObject Win32_NetworkAdapterConfiguration | Where-Object { $_.IPEnabled })) {
    if ($n.MACAddress) { $macs += [string]$n.MACAddress }
    foreach ($ip in @($n.IPAddress)) { if ($ip -and $ip -notmatch ':') { $ips += [string]$ip } }
  }
  $cores = 0; $threads = 0
  foreach ($c in $cpu) { $cores += [int]$c.NumberOfCores; $threads += [int]$c.NumberOfLogicalProcessors }
  $av = $null; $avUp = $null
  try {
    if (Get-Command Get-MpComputerStatus -ErrorAction SilentlyContinue) {
      $mp = Get-MpComputerStatus -ErrorAction Stop
      if ($mp.AntivirusEnabled) { $av = 'Windows Defender: đang bật' } else { $av = 'Windows Defender: đang TẮT' }
      if ($mp.AntivirusSignatureLastUpdated) { $avUp = $mp.AntivirusSignatureLastUpdated.ToString('yyyy-MM-ddTHH:mm:ss') }
    }
  } catch { }
  $patch = $null
  try { $hf = Get-HotFix -ErrorAction Stop | Sort-Object InstalledOn -Descending | Select-Object -First 1; if ($hf -and $hf.InstalledOn) { $patch = $hf.InstalledOn.ToString('yyyy-MM-dd') } } catch { }
  $apps = @()
  try {
    $seen = @{}
    foreach ($k in @('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*', 'HKLM:\SOFTWARE\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*')) {
      foreach ($a in @(Get-ItemProperty -Path $k -ErrorAction SilentlyContinue)) {
        if (-not $a.DisplayName -or $a.SystemComponent -eq 1) { continue }
        $key = ([string]$a.DisplayName) + '|' + ([string]$a.DisplayVersion)
        if ($seen.ContainsKey($key)) { continue }
        $seen[$key] = 1
        $apps += @{ n = ([string]$a.DisplayName); v = ([string]$a.DisplayVersion); p = ([string]$a.Publisher) }
      }
    }
    $apps = @($apps | Sort-Object { $_.n } | Select-Object -First 400)
  } catch { }
  $reboot = $false
  try { if ((Test-Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired') -or (Test-Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending')) { $reboot = $true } } catch { }
  return @{
    cpu = ([string]$cpu[0].Name).Trim(); cores = $cores; threads = $threads
    ramGB = [math]::Round($cs.TotalPhysicalMemory / 1GB, 1); ramType = $ramType; ramSticks = $ramSticks; ramSlots = $ramSlots
    gpu = ($gpu | ForEach-Object { [string]$_.Name }) -join '; '
    board = (([string]$bd.Manufacturer) + ' ' + ([string]$bd.Product)).Trim()
    bios = ([string]$bios.SMBIOSBIOSVersion); serial = ([string]$bios.SerialNumber)
    model = (([string]$cs.Manufacturer) + ' ' + ([string]$cs.Model)).Trim()
    os = [string]$os.Caption; osBuild = [string]$os.Version; arch = [string]$os.OSArchitecture
    domain = [string]$cs.Domain
    smartFail = $fail; disks = $disks; macs = $macs; ips = $ips
    av = $av; avUpdated = $avUp; lastPatch = $patch; apps = $apps; rebootPending = $reboot
  }
}
