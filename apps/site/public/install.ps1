# Windows PowerShell 5.1 and PowerShell 7; no administrator rights required.
function Install-Excalibur {
    $ErrorActionPreference = 'Stop'
    if ($env:OS -ne 'Windows_NT') { throw 'Use install.sh on Linux or macOS.' }
    $architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
    switch ($architecture) {
        'x64' { $arch = 'amd64' }
        'arm64' { $arch = 'arm64' }
        default { throw "Unsupported Windows architecture: $architecture" }
    }
    $base = $env:EXCALIBUR_BASE_URL
    if (-not $base) {
        $base = 'https://github.com/CristhianRudolf7/excalibur-agent/releases'
        if ($env:EXCALIBUR_VERSION) {
            if ($env:EXCALIBUR_VERSION -notmatch '^[A-Za-z0-9._-]+$') { throw 'Invalid EXCALIBUR_VERSION.' }
            $base += '/download/' + $env:EXCALIBUR_VERSION
        } else { $base += '/latest/download' }
    }
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $archive = "excalibur_windows_$arch.zip"
    $work = Join-Path ([IO.Path]::GetTempPath()) ('excalibur-install-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $work | Out-Null
    try {
        Invoke-WebRequest -UseBasicParsing "$base/$archive" -OutFile (Join-Path $work $archive)
        Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile (Join-Path $work 'checksums.txt')
        $lines = @(Get-Content (Join-Path $work 'checksums.txt') | Where-Object { $_ -match ('^[a-fA-F0-9]{64}  ' + [regex]::Escape($archive) + '$') })
        if ($lines.Count -ne 1) { throw 'Archive is missing or duplicated in checksums.txt.' }
        $expected = $lines[0].Substring(0, 64)
        $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $work $archive)).Hash
        if ($actual -ne $expected) { throw 'SHA-256 verification failed; nothing was installed.' }
        Expand-Archive -LiteralPath (Join-Path $work $archive) -DestinationPath (Join-Path $work 'unpacked')
        $source = Join-Path $work 'unpacked/excalibur.exe'
        if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw 'Binary is missing from the archive.' }
        $destination = Join-Path $env:LOCALAPPDATA 'Excalibur/bin'
        New-Item -ItemType Directory -Force -Path $destination | Out-Null
        $binary = Join-Path $destination 'excalibur.exe'
        if (Test-Path -LiteralPath $binary) {
            & $binary uninstall
            if ($LASTEXITCODE -ne 0) { throw 'Could not stop the previous installation.' }
            $old = "$binary.old"
            Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
            Move-Item -LiteralPath $binary -Destination $old -Force
        }
        Copy-Item -LiteralPath $source -Destination $binary -Force
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (@($userPath -split ';' | Where-Object { $_.TrimEnd('\') -ieq $destination.TrimEnd('\') }).Count -eq 0) {
            [Environment]::SetEnvironmentVariable('Path', ($destination + ';' + $userPath).TrimEnd(';'), 'User')
        }
        $env:Path = $destination + ';' + $env:Path
        & $binary install --autostart
        if ($LASTEXITCODE -ne 0) { throw 'Service installation failed.' }
        $url = (& $binary open | Out-String).Trim()
        if ($LASTEXITCODE -ne 0) { throw 'Could not read the application URL.' }
        $ready = $false
        for ($attempt = 0; $attempt -lt 60; $attempt++) {
            try {
                $health = Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 "$url/healthz"
                if ($health.StatusCode -eq 200) { $ready = $true; break }
            } catch { Start-Sleep -Seconds 1 }
        }
        if (-not $ready) { throw 'Daemon health check timed out; inspect the daemon log.' }
        Write-Host "Excalibur is ready. Open $url"
        Write-Host 'Restart your shell to use excalibur from PATH.'
    } finally { Remove-Item -LiteralPath $work -Recurse -Force }
}
Install-Excalibur
