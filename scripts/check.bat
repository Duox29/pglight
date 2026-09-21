@echo off
REM Quality gate for pglight (see AGENTS.md section 1). Fails fast. Mirrors scripts/check.sh.
setlocal
cd /d "%~dp0\.."

echo == gofmt ==
gofmt -l main.go startup.go startup_test.go internal/ > "%TEMP%\pglight-gofmt.txt" 2>&1
for %%S in ("%TEMP%\pglight-gofmt.txt") do if not %%~zS==0 (
  echo gofmt dirty:
  type "%TEMP%\pglight-gofmt.txt"
  exit /b 1
)

echo == go vet ==
go vet ./... || exit /b 1

echo == go test ==
go test ./... || exit /b 1

echo == go build ==
go build -o "%TEMP%\pglight-check.exe" . || exit /b 1

echo == tsc ==
pushd web
call npx tsc --noEmit || (popd & exit /b 1)

echo == eslint ==
call npx eslint src || (popd & exit /b 1)
popd

echo ALL CHECKS PASSED
