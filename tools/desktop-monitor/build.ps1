param([string]$Python = "python", [string]$Output = "$PSScriptRoot/dist")
$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    & $Python -m PyInstaller --noconfirm --clean --windowed --onedir --name Sub2Monitor --collect-all webview --collect-all pythonnet --collect-all clr_loader --add-data "ui;ui" --distpath $Output --workpath "$PSScriptRoot/build" monitor.py
    if ($LASTEXITCODE -ne 0) { throw "PyInstaller build failed" }
    Copy-Item -LiteralPath "$PSScriptRoot/.env.example" -Destination "$Output/Sub2Monitor/.env.example"
    Copy-Item -LiteralPath "$PSScriptRoot/README.md" -Destination "$Output/Sub2Monitor/README.md"
} finally { Pop-Location }
