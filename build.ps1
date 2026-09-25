<#
.SYNOPSIS
  Builds Claude Profile Manager on Windows.

.DESCRIPTION
  Produces dist\claude-profile-manager.exe (GUI, no console window) and
  dist\cpm.exe (console CLI). The GUI needs cgo, so a 64-bit GCC must be on
  PATH — e.g. MSYS2 (pacman -S mingw-w64-ucrt-x86_64-gcc) or TDM-GCC.

.EXAMPLE
  .\build.ps1            # build both binaries
  .\build.ps1 -Package   # also create an installer-ready .exe with icon via `fyne package`
  .\build.ps1 -Test      # run unit tests first
#>
param(
    [switch]$Package,
    [switch]$Test,
    [string]$Version = ""
)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go is not installed or not on PATH. Get it from https://go.dev/dl/"
}
if (-not $Version) {
    $Version = (git describe --tags --always --dirty 2>$null)
    if (-not $Version) { $Version = "dev" }
}
$ldflags = "-s -w -X claude-profile-manager/internal/cli.Version=$Version"

Write-Host "==> go mod tidy"
go mod tidy
if ($LASTEXITCODE) { throw "go mod tidy failed" }

if ($Test) {
    Write-Host "==> go test"
    go test ./internal/...
    if ($LASTEXITCODE) { throw "tests failed" }
}

New-Item -ItemType Directory -Force dist | Out-Null

Write-Host "==> cpm.exe (console CLI)"
$env:CGO_ENABLED = "0"
go build -ldflags $ldflags -o dist\cpm.exe .\cmd\cpm
if ($LASTEXITCODE) { throw "building cpm.exe failed" }

if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
    throw "gcc not found. The Fyne GUI needs cgo: install MSYS2 and run 'pacman -S mingw-w64-ucrt-x86_64-gcc', then add C:\msys64\ucrt64\bin to PATH."
}
$env:CGO_ENABLED = "1"

if ($Package) {
    if (-not (Get-Command fyne -ErrorAction SilentlyContinue)) {
        Write-Host "==> installing fyne CLI"
        go install fyne.io/tools/cmd/fyne@latest
    }
    Write-Host "==> fyne package (embeds icon and version info)"
    $appVersion = ($Version -replace '^v', '') -replace '-.*$', ''
    if ($appVersion -notmatch '^\d+\.\d+\.\d+$') { $appVersion = "0.1.0" }
    fyne package --release --target windows --app-version $appVersion --name "Claude Profile Manager"
    if ($LASTEXITCODE) { throw "fyne package failed" }
    Move-Item -Force "Claude Profile Manager.exe" dist\claude-profile-manager.exe
} else {
    Write-Host "==> claude-profile-manager.exe (GUI)"
    go build -ldflags "$ldflags -H windowsgui" -o dist\claude-profile-manager.exe .
    if ($LASTEXITCODE) { throw "building the GUI failed" }
}

Write-Host ""
Write-Host "Done:" -ForegroundColor Green
Get-ChildItem dist\*.exe | ForEach-Object { Write-Host ("  " + $_.FullName) }
