@echo off
setlocal
rem ASCII only on purpose: the real work is done by the .ps1 next to this file.
>nul 2>&1 net session
if errorlevel 1 (
  powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
  exit /b
)
cd /d "%~dp0"
echo.
echo  1 = Tieng Viet   2 = English   3 = Chinese (zhongwen)
set /p L="  Language / Ngon ngu / yuyan [1]: "
set LANGCODE=vi
if "%L%"=="2" set LANGCODE=en
if "%L%"=="3" set LANGCODE=zh
echo.
echo  For Windows 7 / 8 / Server 2008 R2 (PowerShell agent).
set /p S="  Server address, e.g. 192.168.1.10:7070 : "
if "%S%"=="" ( echo No address given. & pause & exit /b 1 )
set /p K="  Enrollment key (Enter if none): "
if "%S:~0,4%"=="http" (set SRV=%S%) else (set SRV=http://%S%)
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0MAM-Agent.ps1" -Install -Server %SRV% -Key "%K%" -Lang %LANGCODE%
echo.
pause
