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
set /p D="  Also delete ALL data? type Y for yes, Enter for no: "
if /i "%D%"=="Y" (
  powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0uninstall-server.ps1" -Lang %LANGCODE% -PurgeData
) else (
  powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0uninstall-server.ps1" -Lang %LANGCODE%
)
echo.
pause
