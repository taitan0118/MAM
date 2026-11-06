@echo off
rem Cai MAM Console: chep vao may va tao bieu tuong tren man hinh va menu Start (khong can quyen Administrator).
set "D=%LocalAppData%\MAM Console"
mkdir "%D%" 2>nul
copy /y "%~dp0mam-console.exe" "%D%\" >nul || (echo Khong chep duoc mam-console.exe & pause & exit /b 1)
powershell -NoProfile -Command "$w=New-Object -ComObject WScript.Shell; foreach($p in @([Environment]::GetFolderPath('Desktop'),[Environment]::GetFolderPath('Programs'))){$s=$w.CreateShortcut($p+'\MAM Console.lnk');$s.TargetPath='%D%\mam-console.exe';$s.IconLocation='%D%\mam-console.exe,0';$s.Description='MAM Console';$s.Save()}"
echo Da cai MAM Console. Mo bang bieu tuong "MAM Console" tren man hinh.
pause
