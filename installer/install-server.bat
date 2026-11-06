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
set /p P="  Port [7070]: "
if "%P%"=="" set P=7070
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install-server.ps1" -Lang %LANGCODE% -Port %P%
echo.
pause
