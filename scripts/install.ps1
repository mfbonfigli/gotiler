# Installs gotiler from its GitHub releases on 64-bit Windows, in PowerShell:
#
#   irm https://github.com/mfbonfigli/gotiler/releases/latest/download/install.ps1 | iex
#
# It downloads the release archive, checks it against the release's
# SHA256SUMS, extracts it into the install folder and adds that folder to your
# user PATH, without administrator rights. Running it again replaces the
# installed version but keeps the files you added to its share folder, such
# as PROJ grids. It needs only Windows PowerShell 5.1 or PowerShell 7. On
# ARM64 it installs the x86_64 build, which Windows 11 runs through emulation.
#
# Settings, as environment variables ($env:GOTILER_VERSION = 'v3.1.0'):
#   GOTILER_VERSION       release to install; default: the latest one
#   GOTILER_INSTALL_DIR   install folder; default: %LOCALAPPDATA%\Programs\gotiler
#   GOTILER_RELEASES_URL  releases page, for tests;
#                         default: https://github.com/mfbonfigli/gotiler/releases
#
# Everything runs in one script block, which PowerShell parses whole before
# running it, so a partly downloaded script runs nothing. Errors are thrown
# rather than passed to exit, which would close the window running irm | iex.

& {
    $ErrorActionPreference = 'Stop'
    # the progress bar slows Windows PowerShell downloads down several times
    $ProgressPreference = 'SilentlyContinue'

    # the file that marks a folder as installed by this script, which it may replace
    $installMarker = '.gotiler-install'

    function Stop-Install([string]$message) {
        throw "gotiler install: $message"
    }

    function Test-SamePath([string]$a, [string]$b) {
        $a.TrimEnd('\') -eq $b.TrimEnd('\')
    }

    if ($PSVersionTable.PSVersion.Major -lt 5) {
        Stop-Install 'needs Windows PowerShell 5.1 or PowerShell 7'
    }
    if ($PSVersionTable.PSVersion.Major -ge 6 -and -not $IsWindows) {
        Stop-Install 'this script is for Windows: on Linux and macOS, use install.sh'
    }
    $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    if ($arch -eq 'ARM64') {
        Write-Host 'Installing the x86_64 build, which Windows 11 on ARM runs through emulation.'
    } elseif ($arch -ne 'AMD64') {
        Stop-Install "gotiler has no release for $arch Windows"
    }

    $releasesUrl = 'https://github.com/mfbonfigli/gotiler/releases'
    if ($env:GOTILER_RELEASES_URL) {
        $releasesUrl = $env:GOTILER_RELEASES_URL.TrimEnd('/')
    }
    if ($env:GOTILER_INSTALL_DIR) {
        $installDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($env:GOTILER_INSTALL_DIR)
    } else {
        $installDir = Join-Path $env:LOCALAPPDATA 'Programs\gotiler'
    }
    $installDir = $installDir.TrimEnd('\')

    # stop before any download if installing would overwrite files this script did not install
    if ((Test-Path -LiteralPath $installDir) -and
        (Get-ChildItem -LiteralPath $installDir -Force | Select-Object -First 1) -and
        -not (Test-Path -LiteralPath (Join-Path $installDir $installMarker))) {
        Stop-Install "$installDir holds files this script did not install: set GOTILER_INSTALL_DIR to another folder"
    }

    if ($env:GOTILER_VERSION) {
        $tag = $env:GOTILER_VERSION
        if (-not $tag.StartsWith('v')) {
            $tag = "v$tag"
        }
    } else {
        try {
            $response = Invoke-WebRequest -Uri "$releasesUrl/latest" -Method Head -UseBasicParsing
        } catch {
            Stop-Install "could not reach $releasesUrl/latest: $($_.Exception.Message)"
        }
        # the page the redirect ended on: Windows PowerShell and PowerShell 7 keep it in different places
        $finalUri = $response.BaseResponse.ResponseUri
        if (-not $finalUri) {
            $finalUri = $response.BaseResponse.RequestMessage.RequestUri
        }
        $tag = $finalUri.AbsolutePath.TrimEnd('/').Split('/')[-1]
        if ($tag -notmatch '^v\d') {
            Stop-Install "found no release at $releasesUrl/latest"
        }
    }

    $archive = "gotiler-windows-amd64-$tag.zip"
    $tmpDir = Join-Path ([IO.Path]::GetTempPath()) "gotiler-install-$([Guid]::NewGuid())"
    $staged = "$installDir.new"
    $old = "$installDir.old"
    New-Item -ItemType Directory -Path $tmpDir | Out-Null
    try {
        Write-Host "Downloading gotiler $tag for windows-amd64..."
        $archivePath = Join-Path $tmpDir $archive
        $sumsPath = Join-Path $tmpDir 'SHA256SUMS'
        try {
            Invoke-WebRequest -Uri "$releasesUrl/download/$tag/$archive" -OutFile $archivePath -UseBasicParsing
        } catch {
            Stop-Install "could not download $archive from release ${tag}: $($_.Exception.Message)"
        }
        try {
            Invoke-WebRequest -Uri "$releasesUrl/download/$tag/SHA256SUMS" -OutFile $sumsPath -UseBasicParsing
        } catch {
            Stop-Install "could not download the SHA256SUMS of release ${tag}: $($_.Exception.Message)"
        }

        $expected = $null
        foreach ($line in Get-Content -LiteralPath $sumsPath) {
            if ($line -match '^([0-9a-fA-F]{64}) [ *](.+)$' -and $Matches[2] -eq $archive) {
                $expected = $Matches[1].ToLowerInvariant()
            }
        }
        if (-not $expected) {
            Stop-Install "the SHA256SUMS of release $tag does not list $archive"
        }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
        if ($actual -ne $expected) {
            Stop-Install "$archive does not match the release's SHA256SUMS (got $actual, expected $expected)"
        }
        Write-Host 'Checksum verified.'

        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $installDir) | Out-Null
        if (Test-Path -LiteralPath $staged) {
            Remove-Item -LiteralPath $staged -Recurse -Force
        }
        Expand-Archive -LiteralPath $archivePath -DestinationPath $staged
        if (-not (Test-Path -LiteralPath (Join-Path $staged 'gotiler.exe'))) {
            Stop-Install "$archive does not hold gotiler.exe"
        }
        Set-Content -LiteralPath (Join-Path $staged $installMarker) -Value "version=$tag"

        # keep the files added to the installed share folder, such as PROJ grids
        $oldShare = Join-Path $installDir 'share'
        if (Test-Path -LiteralPath $oldShare) {
            foreach ($entry in Get-ChildItem -LiteralPath $oldShare -Force) {
                $target = Join-Path (Join-Path $staged 'share') $entry.Name
                if (-not (Test-Path -LiteralPath $target)) {
                    Copy-Item -LiteralPath $entry.FullName -Destination $target -Recurse
                }
            }
        }

        if (Test-Path -LiteralPath $old) {
            Remove-Item -LiteralPath $old -Recurse -Force
        }
        if (Test-Path -LiteralPath $installDir) {
            try {
                Move-Item -LiteralPath $installDir -Destination $old
            } catch {
                Stop-Install "could not replace $installDir, which is in use: close any running gotiler and try again"
            }
        }
        try {
            Move-Item -LiteralPath $staged -Destination $installDir
        } catch {
            if (Test-Path -LiteralPath $old) {
                Move-Item -LiteralPath $old -Destination $installDir
            }
            throw
        }
        if (Test-Path -LiteralPath $old) {
            Remove-Item -LiteralPath $old -Recurse -Force
        }
    } finally {
        Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
        if (Test-Path -LiteralPath $staged) {
            Remove-Item -LiteralPath $staged -Recurse -Force -ErrorAction SilentlyContinue
        }
    }

    $exe = Join-Path $installDir 'gotiler.exe'
    # gotiler prints its banner on stderr, which Windows PowerShell turns into errors
    $ErrorActionPreference = 'Continue'
    $version = & $exe version 2>$null
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = 'Stop'
    if ($exitCode -ne 0) {
        Stop-Install "the installed $exe does not run"
    }
    Write-Host "Installed gotiler $version in $installDir."

    # add the install folder to the user PATH, reading and writing the raw
    # registry value: [Environment]::SetEnvironmentVariable would expand the
    # %VARIABLES% of the other entries for good
    $environmentKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    try {
        $userPath = [string]$environmentKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        $entries = @($userPath -split ';' | Where-Object { $_ -ne '' })
        if (-not ($entries | Where-Object { Test-SamePath $_ $installDir })) {
            $environmentKey.SetValue('Path', (($entries + $installDir) -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
            # setting a user variable tells running programs, such as Explorer,
            # that the environment changed, so new terminals see the new PATH
            [Environment]::SetEnvironmentVariable('GOTILER_INSTALL_REFRESH', '1', 'User')
            [Environment]::SetEnvironmentVariable('GOTILER_INSTALL_REFRESH', '', 'User')
            Write-Host "Added $installDir to your user PATH."
        }
    } finally {
        $environmentKey.Close()
    }
    if (-not (($env:Path -split ';') | Where-Object { Test-SamePath $_ $installDir })) {
        $env:Path = "$env:Path;$installDir"
    }
    Write-Host 'Run gotiler from this window or any new terminal.'
}
