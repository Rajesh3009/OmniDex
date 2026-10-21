<#
.SYNOPSIS
    Build script for OmniDex supporting Windows, Linux, and macOS cross-compilation.

.DESCRIPTION
    Compiles OmniDex with CGO_ENABLED=0 and stripped symbols (-s -w).
    Outputs binaries to the 'dist' folder.

.PARAMETER Target
    Target platform: 'current', 'windows', 'linux', 'mac', 'all'. Default is 'current'.

.EXAMPLE
    .\build.ps1
    .\build.ps1 -Target all
    .\build.ps1 -Target linux
#>

param(
    [ValidateSet("current", "windows", "linux", "mac", "all")]
    [string]$Target = "current",

    [string]$OutputDir = "dist"
)

$ErrorActionPreference = "Stop"

# Ensure we operate in the script's root directory
if ($PSScriptRoot) {
    Set-Location $PSScriptRoot
}

$env:GO111MODULE = "on"

# Ensure Output directory exists
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir | Out-Null
}

$targets = @(
    @{ OS = "windows"; Arch = "amd64"; Ext = ".exe"; Platform = "windows" },
    @{ OS = "windows"; Arch = "arm64"; Ext = ".exe"; Platform = "windows" },
    @{ OS = "linux";   Arch = "amd64"; Ext = "";     Platform = "linux" },
    @{ OS = "linux";   Arch = "arm64"; Ext = "";     Platform = "linux" },
    @{ OS = "darwin";  Arch = "amd64"; Ext = "";     Platform = "mac" },
    @{ OS = "darwin";  Arch = "arm64"; Ext = "";     Platform = "mac" }
)

# Filter targets based on parameter
if ($Target -eq "current") {
    $currentOS = (go env GOOS).Trim()
    $currentArch = (go env GOARCH).Trim()
    if (-not $currentOS) {
        $currentOS = if ($IsWindows -or ($env:OS -like "*Windows*")) { "windows" } elseif ($IsLinux) { "linux" } elseif ($IsMacOS) { "darwin" } else { "windows" }
    }
    if (-not $currentArch) {
        $currentArch = if ([System.Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
    }
    $ext = if ($currentOS -eq "windows") { ".exe" } else { "" }
    $selectedTargets = @(@{ OS = $currentOS; Arch = $currentArch; Ext = $ext; Platform = "current" })
} elseif ($Target -ne "all") {
    $selectedTargets = $targets | Where-Object { $_.Platform -eq $Target }
} else {
    $selectedTargets = $targets
}

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "  OmniDex Multi-Platform Build System" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "Output Directory : $OutputDir"
Write-Host "Selected Target  : $Target"
Write-Host ""

$env:CGO_ENABLED = "0"
$ldflags = "-s -w"

$successCount = 0
$failCount = 0

foreach ($t in $selectedTargets) {
    $outName = "omnidex-$($t.OS)-$($t.Arch)$($t.Ext)"
    $outPath = Join-Path $OutputDir $outName
    
    Write-Host -NoNewline "Building for $($t.OS)/$($t.Arch)... " -ForegroundColor Yellow

    $env:GOOS = $t.OS
    $env:GOARCH = $t.Arch

    try {
        go build -ldflags $ldflags -o $outPath .
        if ($LASTEXITCODE -eq 0 -and (Test-Path $outPath)) {
            $fileSizeMB = [math]::Round((Get-Item $outPath).Length / 1MB, 2)
            Write-Host "SUCCESS ($fileSizeMB MB) -> $outName" -ForegroundColor Green
            $successCount++
        } else {
            Write-Host "FAILED" -ForegroundColor Red
            $failCount++
        }
    } catch {
        Write-Host "ERROR: $_" -ForegroundColor Red
        $failCount++
    }
}

# If current or windows was built, also copy/create standard root omnidex.exe for quick local run
if ($Target -in @("all", "windows", "current")) {
    $primaryWin = Join-Path $OutputDir "omnidex-windows-amd64.exe"
    if (Test-Path $primaryWin) {
        Copy-Item -Path $primaryWin -Destination (Join-Path $OutputDir "omnidex.exe") -Force
        Copy-Item -Path $primaryWin -Destination "omnidex.exe" -Force
        Write-Host "`nCreated local 'omnidex.exe' in root and '$OutputDir/'" -ForegroundColor Gray
    }
}

Write-Host "`n------------------------------------------"
if ($failCount -eq 0) {
    Write-Host "Build finished successfully! ($successCount binaries created)" -ForegroundColor Green
} else {
    Write-Host "Build completed with warnings: $successCount succeeded, $failCount failed." -ForegroundColor Magenta
}
