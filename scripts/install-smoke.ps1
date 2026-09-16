# The Windows half of install-smoke.sh. Builds squirrel.exe from this checkout,
# zips it the way goreleaser does, then runs install.ps1 against that zip
# twice, refuses a bad checksum, checks that `irm ... | iex` leaves the session
# alone and that a relative install folder goes on the user PATH absolute, and
# runs setup for real into a throwaway profile folder so the PowerShell lines
# in tools.md are exercised, including the install lines: setup --install-tools
# downloads every tool's release and each one's check has to pass afterwards.
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$installer = Join-Path $root "install.ps1"
$version = "0.0.0-smoke"
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  "AMD64" { "amd64" }
  "ARM64" { "arm64" }
  default { throw "unsupported smoke architecture: $env:PROCESSOR_ARCHITECTURE" }
}
$work = Join-Path ([System.IO.Path]::GetTempPath()) "squirrel-install-smoke-$PID"
$savedProfile = $env:USERPROFILE

function Assert([bool]$Condition, [string]$Message) {
  if (-not $Condition) {
    throw "install smoke: $Message"
  }
}

function Set-InstallerEnvironment([string]$Dist, [string]$Bin) {
  $env:SQUIRREL_INSTALL_ARCHIVE = Join-Path $Dist $archiveName
  $env:SQUIRREL_INSTALL_CHECKSUMS = Join-Path $Dist "checksums.txt"
  $env:SQUIRREL_INSTALL_VERSION = $version
  $env:SQUIRREL_INSTALL_DIR = $Bin
  $env:SQUIRREL_INSTALL_SKIP_PATH = "1"
}

function Invoke-Installer([string]$Dist, [string]$Bin) {
  Set-InstallerEnvironment $Dist $Bin
  # The installer's output goes to the host, not into this function's
  # return value, so the caller compares one exit code and not a list.
  & powershell -NoProfile -ExecutionPolicy Bypass -File $installer | Out-Host
  return $LASTEXITCODE
}

# The README line is `irm ... | iex`, which runs the installer's text in the
# terminal's own scope. This runs it the same way in a fresh session and fails
# on anything the session gained. LASTEXITCODE is PowerShell's own, set
# whenever a native program runs, and the installer runs squirrel.exe.
function Invoke-InstallerThroughSession([string]$Dist, [string]$Bin) {
  Set-InstallerEnvironment $Dist $Bin
  $probe = Join-Path $work "session-probe.ps1"
  Set-Content -Path $probe -Encoding ascii -Value @'
param([string]$Installer)
$ErrorActionPreference = "Stop"
$before = @(Get-Variable | ForEach-Object Name) + @(Get-Command -CommandType Function | ForEach-Object Name)
Get-Content -Raw $Installer | Invoke-Expression
$after = @(Get-Variable | ForEach-Object Name) + @(Get-Command -CommandType Function | ForEach-Object Name)
$leaked = @($after | Where-Object { $_ -notin $before -and $_ -notin @("before", "LASTEXITCODE") })
if ($leaked.Count) {
  throw "install.ps1 left these in the session: $($leaked -join ', ')"
}
Write-Output "install.ps1 through iex left nothing in the session"
'@
  & powershell -NoProfile -ExecutionPolicy Bypass -File $probe $installer | Out-Host
  return $LASTEXITCODE
}

# The registry write that puts the PATH back says nothing to Explorer. A
# user-variable write through .NET sends the same environment-change broadcast
# the installer does, so a terminal opened afterwards sees the restored PATH
# and not the folder this smoke deleted. The variable itself goes right away.
function Publish-EnvironmentChange {
  [Environment]::SetEnvironmentVariable("SQUIRREL_INSTALL_SMOKE", "1", "User")
  [Environment]::SetEnvironmentVariable("SQUIRREL_INSTALL_SMOKE", $null, "User")
}

# The installer puts its folder on the user PATH, so a relative
# SQUIRREL_INSTALL_DIR has to go on as an absolute path. This is the one install
# here that writes the PATH, and it puts the PATH back afterwards.
function Assert-RelativeInstallDirGoesOnPathAbsolute([string]$Dist) {
  $environmentKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("Environment", $true)
  $savedKind = $environmentKey.GetValueKind("Path")
  $savedPath = [string]$environmentKey.GetValue("Path", "", [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
  Push-Location $work
  try {
    Set-InstallerEnvironment $Dist "relative-bin"
    Remove-Item Env:SQUIRREL_INSTALL_SKIP_PATH
    & powershell -NoProfile -ExecutionPolicy Bypass -File $installer | Out-Host
    Assert ($LASTEXITCODE -eq 0) "install into a relative folder failed"
    $absolute = Join-Path (Get-Location).Path "relative-bin"
    Assert (Test-Path (Join-Path $absolute "squirrel.exe")) "squirrel.exe did not land in $absolute"
    $userPath = @([string]$environmentKey.GetValue("Path", "") -split ";")
    Assert ($absolute -in $userPath) "the user PATH did not get $absolute"
    Assert ("relative-bin" -notin $userPath) "the user PATH got the relative folder as written"
    Write-Output "a relative SQUIRREL_INSTALL_DIR went on the user PATH as $absolute"
  } finally {
    Pop-Location
    $environmentKey.SetValue("Path", $savedPath, $savedKind)
    Publish-EnvironmentChange
    $environmentKey.Dispose()
  }
}

try {
  New-Item -ItemType Directory -Force $work | Out-Null
  $dist = Join-Path $work "dist"
  $build = Join-Path $work "build"
  $profileHome = Join-Path $work "home"
  $bin = Join-Path $work "bin"
  New-Item -ItemType Directory -Force $dist, $build, (Join-Path $profileHome ".claude"), (Join-Path $profileHome ".codex") | Out-Null

  Push-Location $root
  try {
    & go build -buildvcs=false -ldflags "-s -w -X main.version=$version" -o (Join-Path $build "squirrel.exe") ./cmd/squirrel
    Assert ($LASTEXITCODE -eq 0) "go build failed"
  } finally {
    Pop-Location
  }
  $archiveName = "squirrel_${version}_windows_${arch}.zip"
  $archive = Join-Path $dist $archiveName
  Compress-Archive -LiteralPath (Join-Path $build "squirrel.exe") -DestinationPath $archive
  $hash = (Get-FileHash -Algorithm SHA256 $archive).Hash.ToLowerInvariant()
  Set-Content -Path (Join-Path $dist "checksums.txt") -Value "$hash  $archiveName" -Encoding ascii

  # A fresh profile folder, so the setup run at the end of install.ps1 finds
  # the empty harness folders above and, with no terminal, changes nothing.
  $env:USERPROFILE = $profileHome
  Assert ((Invoke-Installer $dist $bin) -eq 0) "first install failed"
  Assert ((Invoke-Installer $dist $bin) -eq 0) "second install over the first failed"
  $installed = Join-Path $bin "squirrel.exe"
  $reported = (& $installed --version | Out-String).Trim()
  Assert ($reported -eq $version) "installed squirrel reports '$reported', want '$version'"
  Assert ((Invoke-InstallerThroughSession $dist (Join-Path $work "session-bin")) -eq 0) "install.ps1 changed the session it ran in"
  Assert-RelativeInstallDirGoesOnPathAbsolute $dist
  Assert (-not (Test-Path (Join-Path $profileHome ".claude\skills\squirrel"))) "install.ps1 changed the profile without a terminal"

  $report = & $installed setup --harness claude,codex --yes | Out-String
  Write-Output $report
  Assert ($LASTEXITCODE -eq 0) "squirrel setup exited with $LASTEXITCODE"
  foreach ($path in @(".claude\skills\squirrel\SKILL.md", ".claude\CLAUDE.md", ".codex\skills\squirrel\SKILL.md", ".codex\AGENTS.md", ".squirrel\config.json")) {
    Assert (Test-Path (Join-Path $profileHome $path)) "setup did not write $path"
  }
  Assert ((Get-Content (Join-Path $profileHome ".claude\CLAUDE.md") -Raw).Contains("<!-- squirrel:start -->")) "the letter is not in CLAUDE.md"
  Assert ($report.Contains("git and gh")) "the report does not name git and gh"
  if ($env:GH_TOKEN) {
    # gh is on the runner and GH_TOKEN makes gh auth status pass, so the
    # Windows check line, Get-Command through PowerShell, is what says ok.
    Assert ($report.Contains("ok git and gh")) "the Windows check line for git and gh did not pass"
  }
  Assert ($report -match "irm https://raw\.githubusercontent\.com/janiorvalle/roast/[^\r\n]*install\.ps1") "the report does not show the Windows install line for roast"

  # Every installer registers its folder on the user PATH in the registry.
  # Setup reads that PATH back before each line it runs, so the check after
  # an install finds the tool with nothing on this process's PATH.
  $programs = Join-Path $env:LOCALAPPDATA "Programs"
  $report = & $installed setup --harness claude,codex --yes --install-tools | Out-String
  Write-Output $report
  Assert ($LASTEXITCODE -eq 0) "squirrel setup --install-tools exited with $LASTEXITCODE"
  foreach ($tool in @("roast", "TruffleHog", "tokenomnom")) {
    Assert ($report -match "(?m)^\s+ok $tool\b") "setup did not report $tool ok after installing it"
  }
  foreach ($executable in @("roast\roast.exe", "trufflehog\trufflehog.exe", "tokenomnom\tokenomnom.exe", "tokenomnom\nomnom.exe")) {
    Assert (Test-Path (Join-Path $programs $executable)) "$executable is not where its installer puts it"
  }
  foreach ($skill in @("roast", "tokenomnom")) {
    Assert (Test-Path (Join-Path $profileHome ".claude\skills\$skill\SKILL.md")) "setup did not install the $skill skill after installing the tool"
  }
  $writtenTruffleInstaller = Join-Path $profileHome ".squirrel\scripts\install-trufflehog.ps1"
  Assert (Test-Path $writtenTruffleInstaller) "setup did not write the embedded TruffleHog installer under the profile"

  # The TruffleHog installer ships in the binary, so the copy setup wrote is
  # what refuses a bad checksum here, against a tarball that never gets opened.
  $badTruffleDist = Join-Path $work "bad-trufflehog"
  New-Item -ItemType Directory -Force $badTruffleDist | Out-Null
  $truffleArchiveName = "trufflehog_0.0.0-smoke_windows_${arch}.tar.gz"
  Set-Content -Path (Join-Path $badTruffleDist "not-trufflehog.txt") -Value "not a release" -Encoding ascii
  & tar -czf (Join-Path $badTruffleDist $truffleArchiveName) -C $badTruffleDist "not-trufflehog.txt"
  Assert ($LASTEXITCODE -eq 0) "tar could not build the bad TruffleHog archive"
  Set-Content -Path (Join-Path $badTruffleDist "trufflehog_0.0.0-smoke_checksums.txt") -Value ("0" * 64 + "  $truffleArchiveName") -Encoding ascii
  $badTruffleBin = Join-Path $work "bad-trufflehog-bin"
  $env:TRUFFLEHOG_INSTALL_BASE_URL = $badTruffleDist
  $env:TRUFFLEHOG_INSTALL_VERSION = "0.0.0-smoke"
  $env:TRUFFLEHOG_INSTALL_DIR = $badTruffleBin
  $env:TRUFFLEHOG_INSTALL_SKIP_PATH = "1"
  & powershell -NoProfile -ExecutionPolicy Bypass -File $writtenTruffleInstaller | Out-Host
  Assert ($LASTEXITCODE -ne 0) "the TruffleHog installer accepted a checksum mismatch"
  Assert (-not (Test-Path (Join-Path $badTruffleBin "trufflehog.exe"))) "the TruffleHog checksum failure left a binary behind"

  $badDist = Join-Path $work "bad-dist"
  New-Item -ItemType Directory -Force $badDist | Out-Null
  Copy-Item $archive (Join-Path $badDist $archiveName)
  Set-Content -Path (Join-Path $badDist "checksums.txt") -Value ("0" * 64 + "  $archiveName") -Encoding ascii
  $badBin = Join-Path $work "bad-bin"
  Assert ((Invoke-Installer $badDist $badBin) -ne 0) "installer accepted a checksum mismatch"
  Assert (-not (Test-Path (Join-Path $badBin "squirrel.exe"))) "checksum failure left a binary behind"
  Write-Output "install smoke passed"
} finally {
  $env:USERPROFILE = $savedProfile
  Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
# The last native command above is the installer refusing the bad checksum,
# and Windows PowerShell would hand its exit code to whoever ran this script.
exit 0
