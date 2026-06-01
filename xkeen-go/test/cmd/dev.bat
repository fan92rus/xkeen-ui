@echo off
REM dev.bat — Run xkeen-go locally with fake Xray data for frontend development.
REM
REM Starts 3 processes in separate windows:
REM   1. Fake Xray metrics server on :11111
REM   2. Go backend on :8089
REM   3. Vite dev server on :5173 (hot-reload)
REM
REM Usage: test\cmd\dev.bat
REM Open: http://localhost:5173

cd /d "%~dp0..\.."

if not exist "C:\tmp\xkeen-dev" mkdir "C:\tmp\xkeen-dev"
if not exist "C:\tmp\xkeen-dev\xkeen-ui" mkdir "C:\tmp\xkeen-dev\xkeen-ui"
if not exist "C:\tmp\xkeen-dev\xkeen-ui\backups" mkdir "C:\tmp\xkeen-dev\xkeen-ui\backups"

echo ==> Starting fake Xray on :11111 ...
start "FakeXray" /min cmd /c "go run ./test/cmd/fakexray"

timeout /t 2 /nobreak >nul

echo ==> Starting Go backend on :8089 ...
start "GoBackend" /min cmd /c "go run . -config test/cmd/devconfig.json"

timeout /t 3 /nobreak >nul

echo ==> Starting Vite dev server on :5173 ...
cd web
start "ViteDev" cmd /c "npx vite --host"

echo.
echo ==> Ready! Open http://localhost:5173
echo     Login: admin / admin
echo     Close the console windows to stop.
echo.
