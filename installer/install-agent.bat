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
set /p L="  Language / Ngon ngu / yuyan [2]: "
set LANGCODE=en
if "%L%"=="1" set LANGCODE=vi
if "%L%"=="3" set LANGCODE=zh
echo.
set /p S="  Server address, e.g. 192.168.1.10:7070 : "
if "%S%"=="" ( echo No address given. & pause & exit /b 1 )
set /p K="  Enrollment key (Enter if none): "
if "%K%"=="" (
  "%~dp0mam-agent.exe" install -server %S% -lang %LANGCODE%
) else (
  "%~dp0mam-agent.exe" install -server %S% -key %K% -lang %LANGCODE%
)
echo.
pause
