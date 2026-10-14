# install.ps1 - fetch the arxi-tui and arxi core binaries for this Windows machine,
# verify them, and install them side by side. The Windows twin of install.sh: same
# artifact names, same SHA256SUMS check, same environment variables.
#
# The two programs ship as a pair from ONE release. arxi-tui finds the core beside
# its own executable, and the pair is only guaranteed to fit when both come from the
# same tag, so this script never installs one without the other.
#
# Usage (PowerShell 5.1 or 7+):
#   irm https://raw.githubusercontent.com/michitrader/arxi-tui/master/install.ps1 | iex
#   # or, pinning a version / install dir:
#   $env:ARXI_VERSION = "v0.1.0"; $env:ARXI_BIN_DIR = "$HOME\bin"; .\install.ps1
#
# Environment (same meaning as in install.sh):
#   ARXI_VERSION        tag to install; empty = latest release
#   ARXI_BIN_DIR        install folder; default %LOCALAPPDATA%\Programs\arxi
#   ARXI_DOWNLOAD_BASE  replaces the whole ".../releases/download/<version>" prefix
#                       (a mirror, or a local file server, to test without a release)
#   ARXI_NO_PATH        set to 1 to leave the user PATH alone
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'   # Invoke-WebRequest is far slower with the bar on 5.1

$Repo = 'michitrader/arxi-tui'
$Name = 'arxi-tui'
$Core = 'arxi'

function Fail([string]$msg) {
    [Console]::Error.WriteLine("install.ps1: $msg")
    exit 1
}

# ---------------------------------------------------------------------------
# Platform. build-release.sh publishes windows/amd64 only; refuse anything else
# rather than download a binary that will not start.
# ---------------------------------------------------------------------------
function Get-Arch {
    # PROCESSOR_ARCHITEW6432 is set when a 32-bit PowerShell runs on 64-bit Windows.
    $a = $env:PROCESSOR_ARCHITEW6432
    if (-not $a) { $a = $env:PROCESSOR_ARCHITECTURE }
    switch -Regex ($a) {
        '^(AMD64|x86_64)$' { return 'amd64' }
        '^ARM64$'          { return 'arm64' }
        default            { return 'unsupported' }
    }
}

$Arch = Get-Arch
if ($Arch -ne 'amd64') {
    Fail "unsupported CPU '$Arch': releases are built for windows/amd64 only. Build from source with: go install ./cmd/$Name"
}
$Label = 'windows'
$Ext = '.exe'

# TLS 1.2 is not the default on Windows PowerShell 5.1; GitHub requires it.
try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch { }

# ---------------------------------------------------------------------------
# Version. "latest" follows the releases/latest redirect and reads the tag off the
# end: no API token, no JSON.
# ---------------------------------------------------------------------------
$Version = $env:ARXI_VERSION
if (-not $Version) { $Version = 'latest' }
$Base = $env:ARXI_DOWNLOAD_BASE

if (-not $Base -and $Version -eq 'latest') {
    try {
        $req = [Net.HttpWebRequest]::Create("https://github.com/$Repo/releases/latest")
        $req.AllowAutoRedirect = $false
        $resp = $req.GetResponse()
        $loc = $resp.Headers['Location']
        $resp.Close()
    } catch [Net.WebException] {
        # A 302 is returned as a response; some runtimes surface it as an exception.
        $loc = $null
        if ($_.Exception.Response) { $loc = $_.Exception.Response.Headers['Location'] }
    }
    if (-not $loc -or $loc -notmatch '/tag/([^/?#]+)$') {
        Fail 'could not resolve the latest release - set ARXI_VERSION explicitly'
    }
    $Version = $Matches[1]
}
if (-not $Base) { $Base = "https://github.com/$Repo/releases/download/$Version" }

$Artifact = "${Name}_${Version}_${Label}_${Arch}${Ext}"
$CoreArtifact = "${Core}_${Version}_${Label}_${Arch}${Ext}"

Write-Host 'arxi-tui installer'
Write-Host "  version:  $Version"
Write-Host "  platform: $Label/$Arch"
Write-Host "  artifact: $Artifact + $CoreArtifact"

$Tmp = Join-Path ([IO.Path]::GetTempPath()) ("arxi-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Tmp | Out-Null

try {
    Write-Host 'downloading...'
    foreach ($f in @($Artifact, $CoreArtifact, 'SHA256SUMS')) {
        $url = "$Base/$f"
        try {
            if ($url -match '^[A-Za-z]:\\' -or $url -match '^file:') { Copy-Item -LiteralPath ($url -replace '^file:///?', '') -Destination (Join-Path $Tmp $f) }
            else { Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile (Join-Path $Tmp $f) }
        } catch {
            Fail "could not download $url ($($_.Exception.Message))"
        }
    }

    # -----------------------------------------------------------------------
    # Verify. A tampered or truncated download must not be installed. Only the
    # lines of the two artifacts are consulted, so a SHA256SUMS that covers six
    # platforms does not fail on the five that were not downloaded.
    # -----------------------------------------------------------------------
    Write-Host 'verifying checksums...'
    $sums = Get-Content -LiteralPath (Join-Path $Tmp 'SHA256SUMS')
    foreach ($a in @($Artifact, $CoreArtifact)) {
        $line = $sums | Where-Object { $_ -match ("^[0-9a-fA-F]{64}\s+\*?" + [regex]::Escape($a) + '$') } | Select-Object -First 1
        if (-not $line) { Fail "no checksum for $a in SHA256SUMS" }
        $want = ($line -split '\s+')[0].ToLower()
        $got = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $Tmp $a)).Hash.ToLower()
        if ($want -ne $got) { Fail "checksum mismatch for $a (want $want, got $got)" }
    }

    # -----------------------------------------------------------------------
    # Install. Default is a per-user folder: no admin rights needed.
    # -----------------------------------------------------------------------
    $BinDir = $env:ARXI_BIN_DIR
    if (-not $BinDir) { $BinDir = Join-Path $env:LOCALAPPDATA 'Programs\arxi' }
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

    # A running .exe cannot be overwritten on Windows, but it can be renamed: move the
    # old one aside so an upgrade while arxi-tui is open still works.
    function Put([string]$src, [string]$dest) {
        if (Test-Path -LiteralPath $dest) {
            $old = "$dest.old"
            Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
            try { Move-Item -LiteralPath $dest -Destination $old -Force } catch { Fail "cannot replace $dest (is arxi-tui running? close it and try again)" }
        }
        Copy-Item -LiteralPath $src -Destination $dest
    }
    $Dest = Join-Path $BinDir "$Name$Ext"
    $CoreDest = Join-Path $BinDir "$Core$Ext"
    Put (Join-Path $Tmp $Artifact) $Dest
    Put (Join-Path $Tmp $CoreArtifact) $CoreDest
    Write-Host "installed $Dest"
    Write-Host "installed $CoreDest"

    # First-run check: run both and show what they report. This is what catches a
    # blocked executable (SmartScreen / antivirus) now, not at the first chat.
    foreach ($exe in @($Dest, $CoreDest)) {
        $flag = if ($exe -eq $Dest) { '-version' } else { '--version' }
        try { Write-Host ("  " + ((& $exe $flag) -join ' ')) }
        catch { Write-Warning "$exe did not start ($($_.Exception.Message)); an antivirus or Windows SmartScreen may have blocked it" }
    }

    # PATH: user scope only, and only when the folder is not already there.
    $onPath = ($env:Path -split ';') -contains $BinDir
    if (-not $onPath) {
        if ($env:ARXI_NO_PATH -eq '1') {
            Write-Host "note: $BinDir is not on your PATH - add it, e.g. setx PATH `"%PATH%;$BinDir`""
        } else {
            $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
            if (-not (($userPath -split ';') -contains $BinDir)) {
                [Environment]::SetEnvironmentVariable('Path', ($(if ($userPath) { "$userPath;" } else { '' }) + $BinDir), 'User')
            }
            Write-Host "added $BinDir to your user PATH - open a new terminal for it to take effect"
        }
    }
    Write-Host "run: $Name -version"
}
finally {
    Remove-Item -LiteralPath $Tmp -Recurse -Force -ErrorAction SilentlyContinue
}
