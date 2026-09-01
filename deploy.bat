@echo off
setlocal EnableExtensions DisableDelayedExpansion
chcp 65001 >nul

if /I "%~1"=="-h" goto :usage
if /I "%~1"=="--help" goto :usage

where curl.exe >nul 2>nul || (echo エラー: curl.exe が見つかりません。& exit /b 1)
where tar.exe >nul 2>nul || (echo エラー: tar.exe が見つかりません。& exit /b 1)
where certutil.exe >nul 2>nul || (echo エラー: certutil.exe が見つかりません。& exit /b 1)

set "PROJECT_DIR=%~1"
if not defined PROJECT_DIR set "PROJECT_DIR=%~dp0"
for %%I in ("%PROJECT_DIR%") do set "PROJECT_DIR=%%~fI"
if not exist "%PROJECT_DIR%\" (echo エラー: プロジェクトのフォルダーが見つかりません: %PROJECT_DIR%& exit /b 1)
if not exist "%PROJECT_DIR%\sorahost.json" (echo エラー: sorahost.json が見つかりません。ビルド済みプロジェクトのルートで実行してください。& exit /b 1)

echo.
echo === SORAHOSTへデプロイ ===
echo 対象: %PROJECT_DIR%
echo.
if not defined SORAHOST_ENDPOINT (
  echo PteWorkerのコンソールに表示された「エンドポイント」を貼り付けてください。
  set /p "SORAHOST_ENDPOINT=エンドポイント: "
)
if not defined SORAHOST_TOKEN (
  echo.
  echo PteWorkerのコンソールに表示された「デプロイトークン」を貼り付けてください。
  for /f "usebackq delims=" %%T in (`powershell.exe -NoProfile -Command "$s=Read-Host 'デプロイトークン（入力内容は表示されません）' -AsSecureString; $p=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($s); try {[Runtime.InteropServices.Marshal]::PtrToStringBSTR($p)} finally {[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($p)}"`) do set "SORAHOST_TOKEN=%%T"
)
if not defined SORAHOST_ENDPOINT (echo エラー: エンドポイントが入力されていません。& exit /b 1)
if not defined SORAHOST_TOKEN (echo エラー: デプロイトークンが入力されていません。& exit /b 1)
set "VALIDATED_ENDPOINT="
for /f "usebackq delims=" %%U in (`powershell.exe -NoProfile -Command "try {$u=[Uri]$env:SORAHOST_ENDPOINT; if (-not $u.IsAbsoluteUri -or @('http','https') -notcontains $u.Scheme -or -not $u.Host) {exit 1}; $u.AbsoluteUri.TrimEnd('/')} catch {exit 1}"`) do set "VALIDATED_ENDPOINT=%%U"
if not defined VALIDATED_ENDPOINT (echo エラー: エンドポイントは http:// または https:// で始まる正しいURLを入力してください。& exit /b 1)
set "SORAHOST_ENDPOINT=%VALIDATED_ENDPOINT%"

set "TEMP_DIR=%TEMP%\sorahost-%RANDOM%-%RANDOM%"
mkdir "%TEMP_DIR%" >nul 2>nul || (echo error: could not create a temporary directory.& exit /b 1)
set "ARTIFACT=%TEMP_DIR%\artifact.tar.gz"
set "RESPONSE=%TEMP_DIR%\response.json"

echo.
echo [1/3] デプロイするファイルをまとめています...
tar.exe -czf "%ARTIFACT%" -C "%PROJECT_DIR%" --exclude=.git --exclude=.env --exclude=.env.* --exclude=.npmrc --exclude=.netrc --exclude=.DS_Store .
if errorlevel 1 goto :package_error

set "DIGEST="
for /f "usebackq tokens=*" %%H in (`certutil.exe -hashfile "%ARTIFACT%" SHA256 ^| findstr /R /I "^[0-9a-f][0-9a-f]*$"`) do if not defined DIGEST set "DIGEST=%%H"
set "DIGEST=%DIGEST: =%"
if not defined DIGEST goto :digest_error

echo [2/3] サーバーへアップロードしています...
curl.exe --silent --show-error --fail-with-body --output "%RESPONSE%" --request POST --header "Authorization: Bearer %SORAHOST_TOKEN%" --header "Content-Type: application/gzip" --header "X-Artifact-Sha256: %DIGEST%" --data-binary "@%ARTIFACT%" --max-time 1800 "%SORAHOST_ENDPOINT%/deploy"
if errorlevel 1 goto :upload_error

echo [3/3] 公開が完了しました。
echo.
echo デプロイ成功！ サイトが新しい内容に切り替わりました。
call :cleanup
exit /b 0

:usage
echo SORAHOST デプロイスクリプト（Windows）
echo.
echo 使い方:
echo   1. デプロイしたいプロジェクトへ deploy.bat を置きます
echo   2. deploy.bat をダブルクリック、またはコマンドプロンプトで実行します
echo   3. 画面の案内に従ってエンドポイントとトークンを貼り付けます
echo.
echo 別のフォルダーを指定する場合: deploy.bat C:\path\to\project
exit /b 0

:package_error
echo error: could not create the deployment artifact.>&2
call :cleanup
exit /b 1

:digest_error
echo error: could not compute the artifact SHA-256 digest.>&2
call :cleanup
exit /b 1

:upload_error
echo error: deployment failed.>&2
if exist "%RESPONSE%" type "%RESPONSE%" >&2
echo See https://github.com/Sorahost/deploy-cli>&2
call :cleanup
exit /b 1

:cleanup
if defined TEMP_DIR if exist "%TEMP_DIR%\" rmdir /s /q "%TEMP_DIR%"
exit /b 0
