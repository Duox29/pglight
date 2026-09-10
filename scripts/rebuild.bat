@echo off
REM Full pipeline: build frontend (Vite dist) + restart backend from source.
REM Mirrors scripts/rebuild.sh. Pure cmd.exe, no Git Bash needed.
REM Usage: scripts\rebuild.bat
REM        set PORT=9001 & scripts\rebuild.bat
setlocal
cd /d "%~dp0\.."
if "%PORT%"=="" set PORT=8080

echo == frontend (npm) ==
if exist web\node_modules goto :build
echo node_modules missing, installing...
pushd web
call npm install || (popd & exit /b 1)
popd

:build
pushd web
call npm run build || (popd & exit /b 1)
popd

echo == backend: go run main.go (from source, no binary) ==
echo == restart :%PORT% ==
set PID=
for /f "tokens=5" %%a in ('netstat -ano ^| findstr ":%PORT% " ^| findstr LISTENING') do if not defined PID set PID=%%a

if defined PID (
  echo Stopping pid %PID%...
  taskkill /F /PID %PID% >nul 2>&1
  timeout /t 2 /nobreak >nul
) else (
  echo Nothing listening on :%PORT%.
)

echo Starting pglight (go run main.go) on :%PORT% ...
start "pglight" /MIN cmd /c "go run main.go > "%TEMP%\pglight.log" 2>&1"
echo pglight (go run main.go) on :%PORT%. Log: %TEMP%\pglight.log
