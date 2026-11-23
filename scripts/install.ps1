# Install the latest jelly-dev release (Windows, PowerShell 5.1+).
#   irm https://raw.githubusercontent.com/Eutima/jelly-dev/master/scripts/install.ps1 | iex
# Environment: JELLY_DEV_VERSION (e.g. v0.2.0, default latest), INSTALL_DIR (default %LOCALAPPDATA%\Programs\jelly-dev),
#              JD_SKIP_GIT_CONFIG=1 (do not set jd as git's diff and merge tool),
#              JD_SKIP_DEPS=1 (do not check or install dependencies),
#              JD_DEPS (groups to handle, comma separated; default basics,terminal,runtimes,lsp),
#              JD_LANGS (languages to install language servers and formatters for: python, html, css, json, yaml, docker, compose, javascript, angular, bash,
#              go, rust, c, cpp, java, all or none; default all),
#              JD_DRY_RUN=1 (print what would be installed),
#              JD_BIN (path of an installed jd.exe, for the pip package that runs this script itself on its first start:
#              jd is not downloaded; the dependencies and git are handled and the user PATH gets the tool folders).
# Without administrator rights nothing asks for them: winget installs for the current user (--scope user); a tool whose installer needs
# administrator rights is reported with the command to run from an elevated shell.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$ProgressPreference = 'SilentlyContinue'

$repo = 'Eutima/jelly-dev'

# Add-ToUserPath puts a folder on the user PATH (and on the PATH of this session) unless it is there already.
function Add-ToUserPath([string]$dir) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $known = @($userPath -split ';' | ForEach-Object { $_.TrimEnd('\') })
    if ($known -notcontains $dir.TrimEnd('\')) {
        if ($env:JD_DRY_RUN) { Write-Host "[dry-run] add $dir to your user PATH"; return }
        $newPath = (@($userPath -split ';') + $dir | Where-Object { $_ }) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host "Added $dir to your user PATH."
    }
    if (@($env:Path -split ';' | ForEach-Object { $_.TrimEnd('\') }) -notcontains $dir.TrimEnd('\')) { $env:Path = "$env:Path;$dir" }
}

# Test-Admin tells whether this PowerShell runs with administrator rights.
function Test-Admin {
    $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

# Assert-JdRuns stops with an explanation when Windows does not let jd.exe start ("Access denied" when it is run): that is security software
# or a policy that blocks the new, unsigned program, not a damaged download (the SHA-256 sum was checked before the file was copied).
function Assert-JdRuns([string]$exe) {
    if ($env:JD_DRY_RUN) { return }
    $reason = ''
    try {
        & $exe --version | Out-Null
        if ($LASTEXITCODE -eq 0) { return }
        $reason = "exit code $LASTEXITCODE"
    } catch { $reason = ($_.Exception.Message -split "`r?`n")[0] } # the first line: the error text of PowerShell is long
    if (-not $reason) { $reason = 'unknown error' }
    $quoted = $exe.Replace("'", "''") # the path goes into a single quoted PowerShell string below
    $message = @"

$exe was installed, but Windows does not let it run ($reason).
Security software or a policy is blocking the new program. Fix it, then run this installer again. Check:
  - A notice "Attack surface reduction" (German: "Verringerung der Angriffsflaeche", rule "Use advanced protection against ransomware") means Microsoft
    Defender does not know this new, unsigned program yet. In PowerShell as administrator:
      Add-MpPreference -AttackSurfaceReductionOnlyExclusions '$quoted'
    On a managed PC (Intune, Group Policy) the administrator has to add that exclusion.
  - Windows Security > Virus & threat protection > Protection history: a blocked or quarantined jd.exe is listed there; allow or restore it,
    that is all.
  - Other antivirus or AppLocker / Windows Defender Application Control (company PCs): ask your administrator to allow $exe,
    or set INSTALL_DIR to a folder the policy allows.
"@
    Write-Host $message -ForegroundColor Yellow
    throw "jd.exe cannot be started. If Microsoft Defender blocked it, run this in PowerShell as administrator: Add-MpPreference -AttackSurfaceReductionOnlyExclusions '$quoted' (more above), then run jd --version"
}

$installDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\jelly-dev' }

if (-not $env:JD_BIN) {
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture $($env:PROCESSOR_ARCHITECTURE)" }
}

$tag = $env:JELLY_DEV_VERSION
if (-not $tag) {
    $request = [Net.WebRequest]::Create("https://github.com/$repo/releases/latest")
    $request.AllowAutoRedirect = $false
    $response = $request.GetResponse()
    try { $tag = ($response.Headers['Location']).Split('/')[-1] } finally { $response.Close() }
}
if ($tag -notmatch '^v\d+\.\d+\.\d+$') { throw "Could not determine the latest release (got '$tag')" }
$version = $tag.TrimStart('v')

$archive = "jelly-dev_${version}_windows_${arch}.zip"
$baseUrl = "https://github.com/$repo/releases/download/$tag"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    Write-Host "Downloading jelly-dev $tag for windows/$arch"
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
    Unblock-File (Join-Path $installDir 'jd.exe') -ErrorAction SilentlyContinue # no "downloaded from the internet" mark
    Remove-Item (Join-Path $installDir 'jelly-dev.exe') -Force -ErrorAction SilentlyContinue # old command name
}
finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

Add-ToUserPath $installDir
Assert-JdRuns (Join-Path $installDir 'jd.exe')
}
# ---- dependencies: git, Windows Terminal, runtimes and language servers for the IDE ----------------------------
$script:problems = @()

function Write-Problem([string]$text) {
    Write-Warning $text
    $script:problems += $text
}

function Invoke-Step([string]$text, [scriptblock]$action) {
    if ($env:JD_DRY_RUN) { Write-Host "[dry-run] $text"; return $true }
    $global:LASTEXITCODE = 0
    try { & $action; return ($global:LASTEXITCODE -eq 0) } catch { return $false }
}

function Update-SessionPath {
    $env:Path = [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' + [Environment]::GetEnvironmentVariable('Path', 'User')
}

function Test-Group([string]$name) {
    $groups = if ($env:JD_DEPS) { $env:JD_DEPS } else { 'basics,terminal,runtimes,lsp' }
    return (",$groups," -like "*,$name,*")
}

# Ensure-Winget installs a package with winget unless one of the commands exists already.
function Ensure-Winget([string]$label, [string]$id, [string[]]$commands) {
    foreach ($command in $commands) {
        if (Get-Command $command -ErrorAction SilentlyContinue) { Write-Host "ok       $label ($command)"; return }
    }
    if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
        Write-Problem "$label is missing and winget was not found (install the 'App Installer' from the Microsoft Store)"
        return
    }
    # without administrator rights the package goes to the user's folders; nothing asks for elevation
    $scope = if (Test-Admin) { @() } else { @('--scope', 'user') }
    Write-Host "install  $label (winget $id$(if ($scope) { ' --scope user' }))"
    $done = Invoke-Step "winget install --id $id $scope" { winget install --id $id -e --silent @scope --accept-package-agreements --accept-source-agreements }
    if (-not $done) {
        $hint = if ($scope) { "; its installer may need administrator rights: run from an elevated PowerShell: winget install --id $id -e" } else { '' }
        Write-Problem "$label could not be installed (winget install --id $id)$hint"
    } else { Update-SessionPath }
}

function Install-Npm([string]$label, [string]$command, [string[]]$packages) {
    if (Get-Command $command -ErrorAction SilentlyContinue) { Write-Host "ok       $label (npm)"; return }
    if (-not (Get-Command npm -ErrorAction SilentlyContinue)) { Write-Problem "$label needs npm, which is missing"; return }
    Write-Host "install  $label (npm)"
    if (-not (Invoke-Step "npm install -g $packages" { npm install -g @packages })) { Write-Problem "$label could not be installed (npm install -g $packages)" }
}

function Install-Download([string]$label, [string]$url, [scriptblock]$finish) {
    Write-Host "install  $label (download)"
    if ($env:JD_DRY_RUN) { Write-Host "[dry-run] download $url"; return }
    try { & $finish } catch { Write-Problem "$label could not be installed from ${url}: $($_.Exception.Message)" }
}

# Test-Lang tells whether the tools of a language are wanted: JD_LANGS lists the languages (python, html, css, json, yaml, docker, compose,
# javascript, angular, bash, go, java), "none" means no language, "all" every one; without it every language is wanted.
function Test-Lang([string[]]$ids) {
    if (-not $env:JD_LANGS) { return $true }
    $wanted = ($env:JD_LANGS -replace '[\s;]+', ',').ToLower().Split(',')
    if ($wanted -contains 'all') { return $true }
    foreach ($id in $ids) { if ($wanted -contains $id) { return $true } }
    return $false
}

# Test-RustComponent tells whether the toolchain has a component: rustup keeps a proxy for it in .cargo\bin either way.
function Test-RustComponent([string]$name) {
    if (-not (Get-Command rustup -ErrorAction SilentlyContinue)) { return $false }
    try { & rustup which $name 2>$null | Out-Null; return ($LASTEXITCODE -eq 0) } catch { return $false }
}

function Install-Dependencies {
    Write-Host ''
    Write-Host 'Checking the tools the IDE needs (skip with JD_SKIP_DEPS=1, choose groups with JD_DEPS)'
    if (Test-Group 'basics') { Ensure-Winget 'git' 'Git.Git' @('git') }
    if (Test-Group 'terminal') { Ensure-Winget 'Windows Terminal' 'Microsoft.WindowsTerminal' @('wt') }
    # the language servers and formatters need Go, Node.js, Python and Java: choosing them installs the runtimes as well
    if ((Test-Group 'runtimes') -or (Test-Group 'lsp')) {
        if (Test-Lang @('go')) { Ensure-Winget 'Go' 'GoLang.Go' @('go') }
        if (Test-Lang @('python', 'html', 'css', 'json', 'yaml', 'docker', 'compose', 'javascript', 'angular', 'bash')) { Ensure-Winget 'Node.js and npm' 'OpenJS.NodeJS.LTS' @('npm') }
        if (Test-Lang @('java')) { Ensure-Winget 'Java JDK' 'EclipseAdoptium.Temurin.21.JDK' @('javac') }
        if (Test-Lang @('c', 'cpp')) { Ensure-Winget 'LLVM (clangd and clang-format for C and C++)' 'LLVM.LLVM' @('clangd') }
        if (Test-Lang @('rust')) { Ensure-Winget 'Rustup (the Rust toolchain)' 'Rustlang.Rustup' @('rustup') }
        if (Test-Lang @('python', 'java')) { Ensure-Winget 'Python (runs black and starts the Java language server)' 'Python.Python.3.12' @('python', 'py') }
    }
    if (Test-Group 'lsp') {
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        if (Test-Lang @('go')) {
        if (Get-Command gopls -ErrorAction SilentlyContinue) { Write-Host 'ok       gopls (Go)' }
        elseif (-not (Get-Command go -ErrorAction SilentlyContinue)) { Write-Problem 'gopls needs Go, which is missing' }
        else {
            Write-Host 'install  gopls (go install)'
            $env:GOBIN = $installDir
            if (-not (Invoke-Step 'go install golang.org/x/tools/gopls@latest' { go install golang.org/x/tools/gopls@latest })) { Write-Problem 'gopls could not be installed (go install golang.org/x/tools/gopls@latest)' }
        }
        }
        if (Test-Lang @('python')) { Install-Npm 'pyright (Python)' 'pyright-langserver' @('pyright') }
        if (Test-Lang @('javascript', 'angular')) { Install-Npm 'typescript-language-server (JavaScript, TypeScript)' 'typescript-language-server' @('typescript', 'typescript-language-server') }
        if (Test-Lang @('html', 'css', 'json')) { Install-Npm 'HTML, CSS and JSON language servers' 'vscode-html-language-server' @('vscode-langservers-extracted') }
        if (Test-Lang @('yaml')) { Install-Npm 'YAML language server' 'yaml-language-server' @('yaml-language-server') }
        if (Test-Lang @('docker')) { Install-Npm 'Dockerfile language server' 'docker-langserver' @('dockerfile-language-server-nodejs') }
        if (Test-Lang @('compose')) { Install-Npm 'Docker Compose language server' 'docker-compose-langserver' @('@microsoft/compose-language-service') }
        if (Test-Lang @('bash')) { Install-Npm 'Bash language server' 'bash-language-server' @('bash-language-server') }
        if (Test-Lang @('angular')) { Install-Npm 'Angular language server' 'ngserver' @('@angular/language-server', '@angular/language-service', 'typescript') }
        if (Test-Lang @('html', 'css', 'json', 'yaml', 'javascript', 'angular')) { Install-Npm 'prettier (HTML, CSS, JavaScript, TypeScript, JSON formatter)' 'prettier' @('prettier') }
        if (Test-Lang @('rust')) {
            if ((Test-RustComponent 'rust-analyzer') -and (Test-RustComponent 'rustfmt')) { Write-Host 'ok       rust-analyzer and rustfmt' }
            elseif (-not (Get-Command rustup -ErrorAction SilentlyContinue)) { Write-Problem 'rust-analyzer and rustfmt need rustup, which is missing' }
            else {
                Write-Host 'install  rust-analyzer and rustfmt (rustup)'
                if (-not (Invoke-Step 'rustup component add rust-analyzer rustfmt' { rustup component add rust-analyzer rustfmt })) { Write-Problem 'rust-analyzer and rustfmt could not be installed (rustup component add rust-analyzer rustfmt)' }
            }
        }
        if (Test-Lang @('python')) {
        if (Get-Command black -ErrorAction SilentlyContinue) { Write-Host 'ok       black (Python formatter)' }
        else {
            $python = @('python', 'py') | Where-Object { Get-Command $_ -ErrorAction SilentlyContinue } | Select-Object -First 1
            if (-not $python) { Write-Problem 'black (Python formatter) needs Python, which is missing' }
            else {
                Write-Host 'install  black (pip)'
                if (-not (Invoke-Step 'python -m pip install --user black' { & $python -m pip install --user black })) { Write-Problem 'black could not be installed (python -m pip install --user black)' }
            }
        }
        }
        if (Test-Lang @('java')) {
        if (Get-Command jdtls -ErrorAction SilentlyContinue) { Write-Host 'ok       jdtls (Java)' }
        elseif (-not (Get-Command java -ErrorAction SilentlyContinue)) { Write-Problem 'jdtls (Java language server) needs a JDK, which is missing' }
        else {
            $url = 'https://download.eclipse.org/jdtls/milestones/1.63.0/jdt-language-server-1.63.0-202610191322.tar.gz'
            $sha256 = 'b1f2abe0b3cccb462cf64fb4a5587c7d09df60cf8be7748b152cbd89d018bdc4' # pinned: raise both together
            Install-Download 'jdtls' $url {
                $target = Join-Path $env:LOCALAPPDATA 'jdtls'
                New-Item -ItemType Directory -Path $target -Force | Out-Null
                $archive = Join-Path ([IO.Path]::GetTempPath()) 'jdtls.tar.gz'
                Invoke-WebRequest $url -OutFile $archive
                if ((Get-FileHash $archive -Algorithm SHA256).Hash.ToLower() -ne $sha256) {
                    Remove-Item $archive -Force
                    throw 'checksum mismatch: the download is not the pinned jdtls release'
                }
                tar -xzf $archive -C $target
                Remove-Item $archive -Force
                Set-Content -Path (Join-Path $installDir 'jdtls.cmd') -Value "@python `"$target\bin\jdtls`" %*" -Encoding ASCII
            }
        }
        }
    }
    if ($script:problems.Count -gt 0) {
        Write-Warning ("some tools are missing, jd itself works without them:`n  - " + ($script:problems -join "`n  - "))
    } else {
        Write-Host 'All dependencies are in place.'
    }
}

if (-not $env:JD_SKIP_DEPS) { Install-Dependencies }

# The folders the tools were installed into: npm and go install put the language servers there, rustup its toolchain; with the pip
# package the folder of jd.exe as well. They go on the user PATH in every case (new terminals pick them up).
$toolFolders = @((Join-Path $env:APPDATA 'npm'), (Join-Path $env:USERPROFILE 'go\bin'), (Join-Path $env:USERPROFILE '.cargo\bin'))
if ($env:JD_BIN) { $toolFolders += (Split-Path -Parent $env:JD_BIN) }
foreach ($folder in $toolFolders) { if (Test-Path $folder) { Add-ToUserPath $folder } }

# Make jd the global default diff and merge tool of git (opt out with JD_SKIP_GIT_CONFIG=1).
if (-not $env:JD_SKIP_GIT_CONFIG) {
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $jdExe = if ($env:JD_BIN) { $env:JD_BIN } else { Join-Path $installDir 'jd.exe' }
        & $jdExe install-git --apply | Out-Null
        if ($LASTEXITCODE -ne 0) { throw 'Could not configure git; run: jd install-git --apply' }
        Write-Host 'Configured git: jd is now the global diff.tool and merge.tool (git difftool / git mergetool)'
    } else {
        Write-Host 'note: git not found; later run: jd install-git --apply'
    }
}
if (-not $env:JD_BIN) {
    # jd must not run this script again on its first start (the pip package does that); a dry run leaves no mark
    # (a failed mark only means a second setup run on the first start, never a failed install)
    if (-not $env:JD_DRY_RUN) { try { & (Join-Path $installDir 'jd.exe') setup --mark-done 2>&1 | Out-Null } catch { } }
    Write-Host "Installed $installDir\jd.exe (open new terminals to pick up PATH; run: jd --version)"
}
