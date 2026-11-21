# Install the latest jelly-diff release (Windows, PowerShell 5.1+).
#   irm https://raw.githubusercontent.com/Eutima/jelly-diff/master/scripts/install.ps1 | iex
# Environment: JELLY_DIFF_VERSION (e.g. v0.2.0, default latest), INSTALL_DIR (default %LOCALAPPDATA%\Programs\jelly-diff),
#              JD_SKIP_GIT_CONFIG=1 (do not set jd as git's diff and merge tool).
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$ProgressPreference = 'SilentlyContinue'

$repo = 'Eutima/jelly-diff'
$installDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\jelly-diff' }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture $($env:PROCESSOR_ARCHITECTURE)" }
}

$tag = $env:JELLY_DIFF_VERSION
if (-not $tag) {
    $request = [Net.WebRequest]::Create("https://github.com/$repo/releases/latest")
    $request.AllowAutoRedirect = $false
    $response = $request.GetResponse()
    try { $tag = ($response.Headers['Location']).Split('/')[-1] } finally { $response.Close() }
}
if ($tag -notmatch '^v\d+\.\d+\.\d+$') { throw "Could not determine the latest release (got '$tag')" }
$version = $tag.TrimStart('v')

$archive = "jelly-diff_${version}_windows_${arch}.zip"
$baseUrl = "https://github.com/$repo/releases/download/$tag"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    Write-Host "Downloading jelly-diff $tag for windows/$arch"
    Invoke-WebRequest "$baseUrl/$archive" -OutFile (Join-Path $tmp $archive)
    Invoke-WebRequest "$baseUrl/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match "\s$([regex]::Escape($archive))$" } | Select-Object -First 1
    if (-not $line) { throw "No checksum listed for $archive" }
    $expected = ($line -split '\s+')[0]
    $actual = (Get-FileHash (Join-Path $tmp $archive) -Algorithm SHA256).Hash
    if ($expected -ne $actual) { throw "Checksum mismatch for $archive" }

    Expand-Archive (Join-Path $tmp $archive) -DestinationPath $tmp -Force
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    Copy-Item (Join-Path $tmp 'jd.exe') (Join-Path $installDir 'jd.exe') -Force
    Remove-Item (Join-Path $installDir 'jelly-diff.exe') -Force -ErrorAction SilentlyContinue # old command name
}
finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (@($userPath -split ';') -notcontains $installDir) {
    $newPath = (@($userPath -split ';') + $installDir | Where-Object { $_ }) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    Write-Host "Added $installDir to your user PATH."
}
# Make jd the global default diff and merge tool of git (opt out with JD_SKIP_GIT_CONFIG=1).
if (-not $env:JD_SKIP_GIT_CONFIG) {
    if (Get-Command git -ErrorAction SilentlyContinue) {
        & (Join-Path $installDir 'jd.exe') install-git --apply | Out-Null
        if ($LASTEXITCODE -ne 0) { throw 'Could not configure git; run: jd install-git --apply' }
        Write-Host 'Configured git: jd is now the global diff.tool and merge.tool (git difftool / git mergetool)'
    } else {
        Write-Host 'note: git not found; later run: jd install-git --apply'
    }
}
# Make jd available in this session right away (other open terminals need a restart).
if (@($env:Path -split ';') -notcontains $installDir) { $env:Path = "$env:Path;$installDir" }
Write-Host "Installed $installDir\jd.exe (open new terminals to pick up PATH; run: jd --version)"
