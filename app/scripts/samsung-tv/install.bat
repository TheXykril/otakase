@echo off
rem Installs Otakase on a Samsung TV over Wi-Fi. See README.txt first.
rem   install.bat [TV-IP]
rem Set SAMSUNG_PROFILE to a Tizen Studio security profile that holds your
rem Samsung certificate to re-sign the app before installing.
setlocal EnableDelayedExpansion
set "DIR=%~dp0"
if "%TIZEN_SDK%"=="" set "TIZEN_SDK=C:\tizen-studio"
set "SDB=%TIZEN_SDK%\tools\sdb.exe"
set "TIZEN=%TIZEN_SDK%\tools\ide\bin\tizen.bat"
if not exist "%SDB%" (
  where sdb >nul 2>nul && (set "SDB=sdb") || (
    echo sdb was not found. Install Tizen Studio ^(see README.txt^) or set TIZEN_SDK.
    exit /b 1
  )
)

set "IP=%~1"
if "%IP%"=="" set /p "IP=TV IP address (TV Settings > General > Network > Network Status): "
if "%IP%"=="" ( echo No IP address given. & exit /b 1 )
"%SDB%" connect %IP% >nul
set "SERIAL=%IP%:26101"
"%SDB%" -s %SERIAL% get-state 2>nul | findstr /c:"device" >nul || (
  echo Could not reach the TV at %IP%.
  echo Turn on Developer Mode with this computer's IP address and restart the TV ^(README.txt^).
  exit /b 1
)

set "VERSION="
for /f "tokens=2 delims=:" %%v in ('"%SDB%" -s %SERIAL% capability ^| findstr /b "platform_version"') do set "VERSION=%%v"
for /f "tokens=1,2 delims=." %%a in ("%VERSION%") do ( set "MAJOR=%%a" & set "MINOR=%%b" )
if "%MAJOR%"=="" ( echo Could not read the TV's Tizen version. & exit /b 1 )
if %MAJOR% LSS 6 (
  echo This TV runs Tizen %VERSION%. Otakase needs a 2021 or newer Samsung TV ^(Tizen 6.0+^).
  exit /b 1
)
set "GROUP=6.5"
if %MAJOR% GEQ 10 set "GROUP=10.0"
if %MAJOR%==6 if "%MINOR%"=="0" set "GROUP=6.0"
set "TPK=%DIR%otakase-samsung-tv-tizen%GROUP%.tpk"
echo TV runs Tizen %VERSION%, installing otakase-samsung-tv-tizen%GROUP%.tpk

if not "%SAMSUNG_PROFILE%"=="" (
  if not exist "%TIZEN%" ( echo Re-signing needs the tizen command from Tizen Studio. & exit /b 1 )
  set "WORK=%TEMP%\otakase-tv"
  if not exist "!WORK!" mkdir "!WORK!"
  copy /y "%TPK%" "!WORK!\" >nul
  set "TPK=!WORK!\otakase-samsung-tv-tizen%GROUP%.tpk"
  call "%TIZEN%" package -t tpk -s "%SAMSUNG_PROFILE%" -- "!TPK!" || exit /b 1
)

set "LOG=%TEMP%\otakase-tv-install.log"
if exist "%TIZEN%" (
  call "%TIZEN%" install -n "%TPK%" -s %SERIAL% > "%LOG%" 2>&1
) else (
  "%SDB%" -s %SERIAL% install "%TPK%" > "%LOG%" 2>&1
)
type "%LOG%"
findstr /i "fail error" "%LOG%" >nul && (
  echo.
  echo Install failed. If the message mentions a certificate or signature,
  echo create a Samsung certificate for this TV and run again with
  echo   set SAMSUNG_PROFILE=^<profile name^>
  echo   install.bat %IP%
  echo ^(README.txt, step 3^).
  exit /b 1
)
echo Done. Open Otakase from Apps on the TV.
