# Tests scripts/install.ps1 against scripts/test-release-server.py, which
# serves BaseUrl/good, a release holding the archives and install.ps1 built
# by this run, and BaseUrl/bad, the same release with wrong checksums.
#
# Each scenario runs the one-line install, irm .../latest/download/install.ps1
# | iex, in a new Shell process (powershell for Windows PowerShell 5.1, pwsh
# for PowerShell 7). Installs go under a temporary LOCALAPPDATA. The installer
# edits the user PATH in the registry, which the test restores at the end.
#
# Usage: scripts/test-install.ps1 -BaseUrl URL -Tag TAG [-Shell powershell|pwsh]
#   e.g. scripts/test-install.ps1 -BaseUrl http://127.0.0.1:8000 -Tag v0.0.0-ci

param(
    [Parameter(Mandatory = $true)] [string]$BaseUrl,
    [Parameter(Mandatory = $true)] [string]$Tag,
    [string]$Shell = 'powershell'
)

$ErrorActionPreference = 'Stop'

$temp = Join-Path ([IO.Path]::GetTempPath()) "gotiler-install-test-$([Guid]::NewGuid())"
New-Item -ItemType Directory -Path $temp | Out-Null
$env:LOCALAPPDATA = $temp
Remove-Item Env:GOTILER_VERSION, Env:GOTILER_INSTALL_DIR -ErrorAction SilentlyContinue
$installDir = Join-Path $temp 'Programs\gotiler'
$exe = Join-Path $installDir 'gotiler.exe'

$environmentKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$savedPath = $environmentKey.GetValue('Path', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
if ($null -ne $savedPath) {
    $savedPathKind = $environmentKey.GetValueKind('Path')
}

function Fail([string]$message) {
    throw "FAIL: $message"
}

# Invoke-Installer runs the one-line install from ReleasesUrl in a new Shell
# process and returns its exit code, printing its output
function Invoke-Installer([string]$releasesUrl) {
    $env:GOTILER_RELEASES_URL = $releasesUrl
    # Windows PowerShell turns the stderr lines of native commands into errors
    $ErrorActionPreference = 'Continue'
    $script:output = & $Shell -NoProfile -NonInteractive -Command "irm '$releasesUrl/latest/download/install.ps1' | iex" 2>&1 | Out-String
    $exitCode = $LASTEXITCODE
    Write-Host $script:output
    return $exitCode
}

function Test-GotilerRuns {
    $ErrorActionPreference = 'Continue'
    $version = & $exe version 2>$null
    return ($LASTEXITCODE -eq 0 -and $version)
}

function Get-UserPathEntries {
    $value = [string]$environmentKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    return @($value -split ';' | Where-Object { $_ -eq $installDir })
}

try {
    Write-Host "==> Installing the latest release with $Shell"
    if ((Invoke-Installer "$BaseUrl/good") -ne 0) { Fail 'the install failed' }
    if (-not (Test-GotilerRuns)) { Fail "$exe does not run" }
    if ((Get-UserPathEntries).Count -ne 1) { Fail "the user PATH does not list $installDir" }
    if ($environmentKey.GetValueKind('Path') -ne [Microsoft.Win32.RegistryValueKind]::ExpandString) {
        Fail 'the user PATH is no longer an expandable string'
    }

    Write-Host '==> Reinstalling a pinned release keeps the files added to share'
    Set-Content -LiteralPath (Join-Path $installDir 'share\test_grid.tif') -Value 'grid'
    $env:GOTILER_VERSION = $Tag
    if ((Invoke-Installer "$BaseUrl/good") -ne 0) { Fail 'the reinstall failed' }
    Remove-Item Env:GOTILER_VERSION
    if (-not (Test-Path -LiteralPath (Join-Path $installDir 'share\test_grid.tif'))) { Fail 'the reinstall dropped share\test_grid.tif' }
    if ((Test-Path -LiteralPath "$installDir.new") -or (Test-Path -LiteralPath "$installDir.old")) { Fail 'the reinstall left temporary folders' }
    if ((Get-UserPathEntries).Count -ne 1) { Fail "the reinstall listed $installDir twice in the user PATH" }
    if (-not (Test-GotilerRuns)) { Fail 'the reinstalled gotiler does not run' }

    Write-Host '==> A wrong checksum leaves the install untouched'
    if ((Invoke-Installer "$BaseUrl/bad") -eq 0) { Fail 'the installer accepted a wrong checksum' }
    if ($script:output -notmatch 'does not match') { Fail 'no checksum error' }
    if (-not (Test-Path -LiteralPath (Join-Path $installDir 'share\test_grid.tif'))) { Fail "the failed install changed $installDir" }
    if (-not (Test-GotilerRuns)) { Fail 'the failed install broke the previous one' }

    Write-Host '==> Folders the installer did not create are left alone'
    $other = Join-Path $temp 'other'
    New-Item -ItemType Directory -Path $other | Out-Null
    Set-Content -LiteralPath (Join-Path $other 'notes.txt') -Value 'keep'
    $env:GOTILER_INSTALL_DIR = $other
    if ((Invoke-Installer "$BaseUrl/good") -eq 0) { Fail 'the installer replaced a folder it did not create' }
    Remove-Item Env:GOTILER_INSTALL_DIR
    if (-not (Test-Path -LiteralPath (Join-Path $other 'notes.txt'))) { Fail "the installer changed $other" }

    Write-Host 'All install tests passed.'
} finally {
    if ($null -ne $savedPath) {
        $environmentKey.SetValue('Path', $savedPath, $savedPathKind)
    } else {
        $environmentKey.DeleteValue('Path', $false)
    }
    $environmentKey.Close()
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
