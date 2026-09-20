@echo off
REM Kill the pglight process listening on %PORT% (default 8080)
REM and rerun a fresh build in the background. Mirrors scripts/rerun.sh.
REM Usage: scripts\rerun.bat
REM        set PORT=9001 & scripts\rerun.bat
setlocal
cd /d "%~dp0\.."
if "%PORT%"=="" set PORT=8080

set PID=
for /f "tokens=5" %%a in ('netstat -ano ^| findstr ":%PORT% " ^| findstr LISTENING') do if not defined PID set PID=%%a

if defined PID (
  echo Stopping pid %PID% on :%PORT%...
  taskkill /F /PID %PID% >nul 2>&1
  timeout /t 2 /nobreak >nul
) else (
  echo Nothing listening on :%PORT%.
)

go run . || exit /b 1

echo Starting pglight on :%PORT% ...
start "pglight" /MIN cmd /c ""%TEMP%\pglight.exe" > "%TEMP%\pglight.log" 2>&1"
echo pglight rerun on :%PORT%. Log: %TEMP%\pglight.log
