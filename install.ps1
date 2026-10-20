# OpenScion Windows PowerShell Installer
# Usage: irm https://raw.githubusercontent.com/Lakshya-2440/OpenScion/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$Repo = "Lakshya-2440/OpenScion"
$BinaryName = "scion.exe"
$AliasName = "scn.exe"

Write-Host @"
   ___                   ____       _             
  / _ \ _ __   ___ _ __ / ___|  ___(_) ___  _ __  
 | | | | '_ \ / _ \ '_ \\___ \ / __| |/ _ \| '_ \ 
 | |_| | |_) |  __/ | | |___) | (__| | (_) | | | |
  \___/| .__/ \___|_| |_|____/ \___|_|\___/|_| |_|
       |_|                                        
Agent-Scale Terminal Version Control System (Windows)
--------------------------------------------------------
"@ -ForegroundColor Cyan

# 1. Architecture Detection
$Arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$Target = "windows_$Arch"
Write-Host "Detected target platform: $Target" -ForegroundColor DarkCyan

# 2. Determine Install Directory
$InstallDir = "$env:USERPROFILE\.local\bin"
if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
}

$TempDir = [System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), [System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Force -Path $TempDir | Out-Null

$Installed = $false

# 3. Attempt downloading release binary
$ReleaseUrl = "https://github.com/$Repo/releases/latest/download/scion-$Target.zip"
Write-Host "Attempting prebuilt release download..." -ForegroundColor DarkGray

try {
    $ZipPath = Join-Path $TempDir "scion.zip"
    Invoke-WebRequest -Uri $ReleaseUrl -OutFile $ZipPath -UseBasicParsing -TimeoutSec 10
    Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force
    Copy-Item (Join-Path $TempDir "scion.exe") (Join-Path $InstallDir $BinaryName) -Force
    Copy-Item (Join-Path $InstallDir $BinaryName) (Join-Path $InstallDir $AliasName) -Force
    $Installed = $true
    Write-Host "✔ Downloaded and installed release binary." -ForegroundColor Green
} catch {
    # 4. Fallback: Compile from source if Go is present
    Write-Host "Release archive not available directly. Checking for Go compiler..." -ForegroundColor Yellow
    if (Get-Command go -ErrorAction SilentlyContinue) {
        Write-Host "Compiling from source with Go..." -ForegroundColor Cyan
        $SrcDir = Join-Path $TempDir "src"
        git clone --depth 1 "https://github.com/$Repo.git" $SrcDir
        Push-Location $SrcDir
        go build -ldflags="-s -w" -o (Join-Path $InstallDir $BinaryName) ./cmd/scion
        Copy-Item (Join-Path $InstallDir $BinaryName) (Join-Path $InstallDir $AliasName) -Force
        Pop-Location
        $Installed = $true
        Write-Host "✔ Compiled and installed scion.exe and scn.exe successfully." -ForegroundColor Green
    } else {
        Write-Host "Error: Neither release binary nor Go compiler found." -ForegroundColor Red
        Write-Host "Install Go from https://go.dev/dl/ and re-run this script."
        Remove-Item -Recurse -Force $TempDir -ErrorAction SilentlyContinue
        exit 1
    }
}

Remove-Item -Recurse -Force $TempDir -ErrorAction SilentlyContinue

# 5. Ensure PATH contains install directory
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    $env:Path = "$env:Path;$InstallDir"
    Write-Host "✔ Added $InstallDir to user PATH." -ForegroundColor Green
}

# 6. Configure AI Coding Agents if Claude Code detected
$ClaudeDir = Join-Path $HOME ".claude"
if (Test-Path $ClaudeDir) {
    $ClaudeGlobal = Join-Path $ClaudeDir "CLAUDE.md"
    $RuleText = @"

## OpenScion Version Control (scn)
When working in any project with a `.scion\` directory:
- Use `scn status` (instead of `git status`)
- Use `scn diff` (instead of `git diff`)
- Use `scn commit -m "<message>" --agent <model>` (instead of `git commit`)
- Use `scn log` (instead of `git log`)
- Use `scn pull` and `scn push` for synchronization
"@
    if (-not (Test-Path $ClaudeGlobal) -or (Get-Content $ClaudeGlobal -Raw) -notmatch "OpenScion") {
        Add-Content -Path $ClaudeGlobal -Value $RuleText
        Write-Host "✔ Auto-configured AI coding agents (Claude Code) for OpenScion." -ForegroundColor Green
    }
}

Write-Host @"

OpenScion installed successfully!
  • Binary: $InstallDir\$BinaryName
  • Alias:  $InstallDir\$AliasName

Get Started:
  scion init   # Initializes repo & auto-generates AGENTS.md for AI agents
  scion status # View working tree
  scion commit -m "..." # Commit changes
"@ -ForegroundColor Green
