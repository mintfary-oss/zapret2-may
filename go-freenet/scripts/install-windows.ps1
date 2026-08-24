# FreeNet Windows auto-installer — PowerShell
#
# One-liner (run PowerShell as Administrator):
#   irm https://github.com/mintfary-oss/zapret2-may/releases/latest/download/install-windows.ps1 | iex
#
# Or download and run:
#   Set-ExecutionPolicy RemoteSigned -Scope CurrentUser
#   .\install-windows.ps1
#
# What it does:
#   1. Downloads freenet-windows-bundle.zip (freenet.exe + WinDivert.dll + WinDivert64.sys)
#   2. Installs all files to C:\Program Files\FreeNet\
#   3. Registers FreeNet as a Windows service (auto-start)
#   4. WinDivert intercepts ALL application traffic automatically (no proxy config needed)
#   5. Opens http://localhost:8080 in the default browser

param(
    [string]$InstallDir  = "C:\Program Files\FreeNet",
    [string]$WebAddr     = ":8080",
    [string]$SocksAddr   = "127.0.0.1:1080",
    [switch]$Uninstall   = $false
)

$ErrorActionPreference = "Stop"
$GithubRepo  = "mintfary-oss/zapret2-may"
$BundleUrl   = "https://github.com/$GithubRepo/releases/latest/download/freenet-windows-bundle.zip"
$BinaryPath  = Join-Path $InstallDir "freenet.exe"
$ServiceName = "FreeNet"

# ── Helpers ──────────────────────────────────────────────────────────────────

function Write-Step { param($msg) Write-Host "`n━━ $msg " -ForegroundColor Cyan }
function Write-Ok   { param($msg) Write-Host "[+] $msg" -ForegroundColor Green }
function Write-Warn { param($msg) Write-Host "[!] $msg" -ForegroundColor Yellow }
function Write-Err  { param($msg) Write-Error "[✗] $msg" }

# ── Admin check ──────────────────────────────────────────────────────────────

$currentPrincipal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Err "This script must be run as Administrator. Right-click PowerShell → 'Run as administrator'."
}

# ── Uninstall ─────────────────────────────────────────────────────────────────

if ($Uninstall) {
    Write-Step "Uninstalling FreeNet"

    $svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    if ($svc) {
        Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
        & sc.exe delete $ServiceName | Out-Null
        Write-Ok "Service '$ServiceName' removed"
    } else {
        Write-Warn "Service '$ServiceName' not found"
    }

    if (Test-Path $InstallDir) {
        Remove-Item -Recurse -Force $InstallDir
        Write-Ok "Removed $InstallDir"
    }

    Write-Ok "FreeNet uninstalled."
    exit 0
}

# ── Download bundle ───────────────────────────────────────────────────────────

Write-Step "Downloading FreeNet (bundle with WinDivert)"
Write-Ok "Source: $BundleUrl"

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}

$tmpZip = Join-Path $env:TEMP "freenet-bundle.zip"
$tmpDir = Join-Path $env:TEMP "freenet-bundle"
try {
    Invoke-WebRequest -Uri $BundleUrl -OutFile $tmpZip -UseBasicParsing
    Write-Ok "Downloaded → $tmpZip"
} catch {
    Write-Err "Download failed: $_"
}

# ── Extract bundle ────────────────────────────────────────────────────────────

Write-Step "Extracting bundle"

# Stop existing service before replacing files.
$svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($svc -and $svc.Status -eq "Running") {
    Stop-Service -Name $ServiceName -Force
    Start-Sleep -Seconds 2
}

if (Test-Path $tmpDir) { Remove-Item -Recurse -Force $tmpDir }
Expand-Archive -Path $tmpZip -DestinationPath $tmpDir -Force
Remove-Item -Force $tmpZip

# Copy all bundle files (freenet.exe, WinDivert.dll, WinDivert64.sys).
Get-ChildItem -Path $tmpDir -File | ForEach-Object {
    Copy-Item -Force $_.FullName $InstallDir
    Write-Ok "Installed → $(Join-Path $InstallDir $_.Name)"
}
Remove-Item -Recurse -Force $tmpDir

# ── Write default config ──────────────────────────────────────────────────────

Write-Step "Writing configuration"
$ConfigPath = Join-Path $InstallDir "config.yaml"
if (-not (Test-Path $ConfigPath)) {
    @"
proxy:
  listen_addr: "$SocksAddr"
  transparent_addr: ""

bypass:
  strategy: "auto"
  split_pos: 2
  fake_ttl: 8

hostlist:
  enabled: false
  auto_update: true
  url: "https://antifilter.download/list/domains.lst"
"@ | Set-Content -Encoding UTF8 $ConfigPath
    Write-Ok "Config written → $ConfigPath"
} else {
    Write-Warn "Config already exists, keeping existing"
}

# ── Windows service ───────────────────────────────────────────────────────────

Write-Step "Installing Windows service"

# Remove old service definition if present.
$oldSvc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($oldSvc) {
    Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
    & sc.exe delete $ServiceName | Out-Null
    Start-Sleep -Seconds 1
}

# Use freenet.exe -install to self-register.
& $BinaryPath -install -config $ConfigPath -web $WebAddr
if ($LASTEXITCODE -ne 0) {
    # Fallback: register via sc.exe directly.
    Write-Warn "freenet -install failed, registering via sc.exe"
    & sc.exe create $ServiceName binPath= "`"$BinaryPath`" -config `"$ConfigPath`" -web $WebAddr" start= auto DisplayName= "FreeNet DPI Bypass" | Out-Null
    & sc.exe description $ServiceName "FreeNet — обход DPI блокировок (RKN/TSPU)" | Out-Null
    & sc.exe start $ServiceName | Out-Null
}

Write-Ok "Service '$ServiceName' installed and started"

# ── Add to PATH ───────────────────────────────────────────────────────────────

$machinePath = [Environment]::GetEnvironmentVariable("PATH", "Machine")
if ($machinePath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("PATH", "$machinePath;$InstallDir", "Machine")
    Write-Ok "Added $InstallDir to system PATH"
}

# ── Firewall rule (optional, for LAN access) ──────────────────────────────────

$ruleName = "FreeNet Web UI"
$existingRule = Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue
if (-not $existingRule) {
    New-NetFirewallRule `
        -DisplayName $ruleName `
        -Direction Inbound `
        -Protocol TCP `
        -LocalPort 8080 `
        -Action Allow `
        -Profile Private `
        -ErrorAction SilentlyContinue | Out-Null
    Write-Ok "Firewall rule added (port 8080, private network)"
}

# ── Done ──────────────────────────────────────────────────────────────────────

Write-Host ""
Write-Host "  ✓ FreeNet установлен и запущен!" -ForegroundColor Green
Write-Host ""
Write-Host "  Веб-интерфейс  → http://localhost:8080" -ForegroundColor Cyan
Write-Host "  SOCKS5 прокси  → $SocksAddr"
Write-Host ""
Write-Host "  Остановить   :  Stop-Service FreeNet"
Write-Host "  Запустить    :  Start-Service FreeNet"
Write-Host "  Удалить      :  .\install-windows.ps1 -Uninstall"
Write-Host ""

# Open browser.
Start-Process "http://localhost:8080"
