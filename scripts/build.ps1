<#
.SYNOPSIS
    Gotiler build script
#>

param (
    [Parameter(Position = 0)]
    [ValidateSet("linux-amd64", "linux-arm64", "windows-amd64", "all")]
    [string]$Target = "all",

    [Parameter()]
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"

function Write-Info {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Blue
}

function Write-Success {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Green
}

function Write-Warn {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Yellow
}

function Write-ErrorMsg {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Red
}

Write-Success "Gotiler build script"

Write-Host " => Target: " -NoNewline -ForegroundColor Blue
Write-Host $Target -ForegroundColor Cyan

$env:DOCKER_BUILDKIT = "1"

$GitCommit = "unknown"
try {
    $GitCommit = (git rev-list -1 HEAD 2>$null).Trim()
    if (-not $GitCommit) {
        $GitCommit = "unknown"
    }
} catch {
    $GitCommit = "unknown"
}

# -------------------------------------------------------------------
# 1. Prepare Build Target
# -------------------------------------------------------------------

$DockerTarget = switch ($Target) {
    "all"           { "final" }
    "linux-amd64"   { "linux-amd64-builder" }
    "linux-arm64"   { "linux-arm64-builder" }
    "windows-amd64" { "windows-amd64-builder" }
}

Write-Host " => Removing old build artifacts... " -NoNewline -ForegroundColor Blue

if ($Target -eq "all") {
    if (Test-Path "./build") {
        Remove-Item -Recurse -Force "./build"
    }
} else {
    if (Test-Path "./build/$Target") {
        Remove-Item -Recurse -Force "./build/$Target"
    }
    if (Test-Path "./build/tests/$Target") {
        Remove-Item -Recurse -Force "./build/tests/$Target"
    }
}

Write-Host "done" -ForegroundColor Green

# -------------------------------------------------------------------
# 2. Build Process
# -------------------------------------------------------------------

Write-Info " => Building..."

$BuildArgs = @()
if ($Version) { $BuildArgs = @("--build-arg", "VERSION=$Version") }
$BuildArgs += @("--build-arg", "GIT_COMMIT=$GitCommit")

if ($Target -eq "all") {

    docker build @BuildArgs `
        -t gotiler:build `
        --target final `
        --output ./build .

    if ($LASTEXITCODE -ne 0) {
        throw "Docker build failed."
    }

} else {

    docker build @BuildArgs `
        -t "gotiler:build-$Target" `
        --target $DockerTarget .

    if ($LASTEXITCODE -ne 0) {
        throw "Docker build failed."
    }

    $TmpExtract = Join-Path `
        ([System.IO.Path]::GetTempPath()) `
        ([Guid]::NewGuid().ToString())

    New-Item -ItemType Directory -Force -Path $TmpExtract | Out-Null

    $ContainerId = $null

    try {

        $ContainerId = docker create "gotiler:build-$Target"

        if (-not $ContainerId) {
            throw "Failed to create temporary container."
        }

        docker cp "$($ContainerId):/artifacts/." "$TmpExtract"

        if ($LASTEXITCODE -ne 0) {
            throw "docker cp failed."
        }

        # Copy target folder (executable + share)
        $TargetSrc = Join-Path $TmpExtract $Target
        New-Item -ItemType Directory -Force -Path "./build/$Target" | Out-Null
        Copy-Item -Path "$TargetSrc/*" -Destination "./build/$Target/" -Recurse -Force

        # Copy tests
        $TestsSrc = Join-Path $TmpExtract "tests/$Target"
        New-Item -ItemType Directory -Force -Path "./build/tests/$Target" | Out-Null
        Copy-Item -Path "$TestsSrc/*" -Destination "./build/tests/$Target/" -Recurse -Force

    } finally {

        if ($ContainerId) {
            docker rm -f $ContainerId *> $null
        }

        if (Test-Path $TmpExtract) {
            Remove-Item -Recurse -Force $TmpExtract
        }
    }
}

# -------------------------------------------------------------------
# 3. Test Runner
# -------------------------------------------------------------------

function Run-Tests {
    param(
        [string]$Arch
    )

    $RunningOnWindows = $IsWindows

    if ($null -eq $RunningOnWindows) {
        $RunningOnWindows = ([System.Environment]::OSVersion.Platform -eq "Win32NT")
    }

    if ($RunningOnWindows) {
        $HostArch = "amd64"
    } else {
        try {
            $HostArch = (uname -m)
            switch -Regex ($HostArch) {
                "^(x86_64|amd64)$"  { $HostArch = "amd64" }
                "^(aarch64|arm64)$" { $HostArch = "arm64" }
            }
        } catch {
            $HostArch = "unknown"
        }
    }

    $CanRun = switch ($Arch) {
        "windows-amd64" { $RunningOnWindows -and $HostArch -eq "amd64" }
        "linux-amd64"   { (-not $RunningOnWindows) -and $HostArch -eq "amd64" }
        "linux-arm64"   { (-not $RunningOnWindows) -and $HostArch -eq "arm64" }
        default         { $false }
    }

    if (-not $CanRun) {
        Write-Warn "    [SKIP] $Arch tests not runnable on host ($HostArch)"
        return
    }

    $TestDir = "./build/tests/$Arch"

    if (-not (Test-Path $TestDir)) {
        Write-Warn "    [SKIP] No tests found for $Arch"
        return
    }

    # the coordinate conversion tests need the PROJ database shipped with the build
    $env:PROJ_DATA = (Resolve-Path "./build/$Arch/share").Path
    $TestRoot = (Resolve-Path $TestDir).Path
    $RepoRoot = (Resolve-Path "$PSScriptRoot/..").Path

    # test binaries mirror the package layout: run each one from its package
    # directory, like go test does, so tests find their testdata
    $Files = Get-ChildItem -Path $TestRoot -File -Recurse -Include "*.test", "*.test.exe"

    foreach ($File in $Files) {

        $PkgDir = $File.DirectoryName.Substring($TestRoot.Length).TrimStart("\", "/").Replace("\", "/")
        Write-Host "    => Running: ./$PkgDir" -ForegroundColor Cyan

        Push-Location (Join-Path $RepoRoot $PkgDir)
        try {
            & $File.FullName "-test.v"
        } finally {
            Pop-Location
        }

        if ($LASTEXITCODE -ne 0) {
            Write-ErrorMsg "    [FAIL] ./$PkgDir"
            exit $LASTEXITCODE
        }

        Write-Host "    [PASS] ./$PkgDir" -ForegroundColor Green
    }
}

# -------------------------------------------------------------------
# 4. Run Tests
# -------------------------------------------------------------------

Write-Info " => Running tests..."

if ($Target -eq "all") {
    foreach ($Arch in @("linux-amd64", "linux-arm64", "windows-amd64")) {
        Run-Tests $Arch
    }
} else {
    Run-Tests $Target
}

# -------------------------------------------------------------------
# 5. Cleanup
# -------------------------------------------------------------------

if ((Test-Path "./build/tests") -and (-not $env:CI)) {
    Write-Info " => Cleaning test artifacts..."
    Remove-Item -Recurse -Force "./build/tests"
}

Write-Success "=> Build and test complete."
