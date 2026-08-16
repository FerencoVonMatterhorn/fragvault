# Defines the GPU render VM, the way cloud-init/hosting.yaml.tftpl defines the
# hosting VM. Run by the azurerm_virtual_machine_run_command in
# vm_gpu_render.tf, which embeds this file verbatim -- editing it changes the
# run command's source and re-runs it on the next apply.
#
# It must stay idempotent: the run command re-runs on every edit to this file,
# and a re-run must not undo a working machine.
#
# KEEP THIS FILE PURE ASCII -- no em dashes, no smart quotes, nothing above
# U+007F. Run Command writes these bytes to disk and lets Windows PowerShell 5.1
# open the file, and a file with no BOM is decoded as Windows-1252 rather than
# UTF-8. An em dash then arrives as three CP1252 characters ending in 0x94,
# which is a right double quotation mark and which PowerShell honours as a
# string delimiter -- so a dash inside a double-quoted string silently ends the
# string and the parse dies somewhere else entirely with MissingEndCurlyBrace.
# A `precondition` in vm_gpu_render.tf fails the plan if this slips.
#
# What it deliberately does NOT do:
#   - log Steam in, or download CS2. Steam Guard is far easier to clear by hand
#     once than to automate, and the login is machine-bound anyway. Done once
#     over RDP, then captured into the golden image.
#   - set the auto-logon password. That is a secret and has no business in
#     extension settings, which are readable from the portal. Autologon64.exe
#     is downloaded below; run it by hand once and it stores the password in
#     LSA secrets rather than the registry.
#
# Everything here is public and secret-free by design.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'  # a visible progress bar makes Invoke-WebRequest crawl

# Pinned, not "latest". A clip that renders wrong should be attributable to a
# version, and HLAE tracks CS2 updates closely enough that floating would make
# every render a different experiment.
#
# 2.191.1 is the newest release GitHub marks as latest; the 2.192.x tags exist
# but are pre-releases. The asset is named after the version with underscores,
# hlae_2_191_1.zip, and there is no hlae.zip -- asking for one is a 404 that
# arrives as a bare "Invoke-WebRequest : Not Found". Derived from $HlaeVersion
# so bumping still means editing one line.
$HlaeVersion = '2.191.1'
$HlaeAsset   = "hlae_$($HlaeVersion -replace '\.', '_').zip"
$HlaeUrl     = "https://github.com/advancedfx/advancedfx/releases/download/v$HlaeVersion/$HlaeAsset"

$Root    = 'C:\fragvault'
$LogDir  = Join-Path $Root 'logs'
$HlaeDir = 'C:\hlae'

New-Item -ItemType Directory -Force -Path $Root, $LogDir, (Join-Path $Root 'demos'), (Join-Path $Root 'out') | Out-Null

Start-Transcript -Path (Join-Path $LogDir 'bootstrap.log') -Append | Out-Null

function Write-Step($msg) { Write-Host "=== $msg" }

# choco is a native command, so a failed package does NOT trip
# $ErrorActionPreference. A sysinternals checksum failure once sailed straight
# past it and the script carried on building a half-installed box, reporting
# nothing until something later fell over. Check the exit code explicitly.
# 3010 is "installed, reboot pending" and is a success.
function Invoke-Choco {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Packages)
    choco install -y --no-progress @Packages
    if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne 3010) {
        throw "choco install $($Packages -join ' ') failed with exit code $LASTEXITCODE"
    }
}

# TLS 1.2 -- Windows Server 2022 defaults are fine, but the Chocolatey
# bootstrapper and GitHub both fail obscurely if a stale default wins.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# --- GPU ---------------------------------------------------------------------
# The AMD driver extension is ordered before this one, so the MI25 should
# already be present. Report rather than fail: a missing GPU is worth seeing in
# the log, but it does not stop the rest of the install being useful, and the
# driver install sometimes needs the reboot that follows.
Write-Step 'Checking for the AMD GPU'
$gpu = Get-CimInstance Win32_VideoController | Where-Object { $_.Name -match 'Radeon|MI25' }
if ($gpu) {
    Write-Host "GPU present: $($gpu.Name) driver $($gpu.DriverVersion)"
} else {
    Write-Warning 'No AMD GPU found yet. If this persists after a reboot, the AmdGpuDriverWindows extension did not apply -- check `az vm extension list`.'
}

# --- Package manager ---------------------------------------------------------
Write-Step 'Installing Chocolatey'
if (-not (Get-Command choco.exe -ErrorAction SilentlyContinue)) {
    Set-ExecutionPolicy Bypass -Scope Process -Force
    Invoke-Expression ((New-Object Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))
    $env:Path = [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' + [Environment]::GetEnvironmentVariable('Path', 'User')
}

Write-Step 'Installing ffmpeg and 7zip'
# ffmpeg is the encoder HLAE pipes raw frames into -- mirv_streams does not
# encode anything itself.
Invoke-Choco ffmpeg 7zip

# --- Autologon ---------------------------------------------------------------
# One 100 KB binary instead of the chocolatey sysinternals package, which pulls
# the whole 191 MB suite. Microsoft republishes SysinternalsSuite.zip in place
# without changing its URL, so the package's pinned sha256 goes stale and the
# install dies on a checksum mismatch -- which is exactly what it did here.
# Autologon is the only thing in that suite this box needs, and fetching it
# straight from Microsoft's own host has no stale checksum to trip over.
#
# Unpinned by nature: live.sysinternals.com always serves current. Acceptable
# for a tool that is run by hand once and touches nothing a render depends on.
Write-Step 'Installing Autologon'
$autologon = Join-Path $Root 'Autologon64.exe'
if (-not (Test-Path $autologon)) {
    Invoke-WebRequest -Uri 'https://live.sysinternals.com/Autologon64.exe' -OutFile $autologon -UseBasicParsing
}

# --- Steam -------------------------------------------------------------------
Write-Step 'Installing the Steam client'
# The full client, not just steamcmd: CS2 refuses to launch without a running,
# logged-in Steam client, whatever steamcmd has downloaded.
Invoke-Choco steam

# CS2 refuses to launch without a running Steam client, and nobody is sitting at
# this box to start one. Without this, every boot -- including every restore
# from the golden image -- comes up unable to render until a human logs in and
# starts Steam by hand, which is exactly the manual step the image exists to
# remove. Found the hard way: after the first autologon reboot there was no Run
# entry anywhere and no Steam process.
#
# HKLM rather than the render account's own Run key because this script runs as
# SYSTEM and that user's hive is not loaded at bootstrap time. There is one
# interactive account on this box, so machine-wide is equivalent. -silent starts
# it to the tray without opening the client window.
Write-Step 'Making Steam start with the console session'
$steamExe = 'C:\Program Files (x86)\Steam\steam.exe'
Set-ItemProperty -Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run' `
    -Name 'FragVaultSteam' -Value ('"' + $steamExe + '" -silent') -Type String -Force

# --- HLAE --------------------------------------------------------------------
Write-Step "Installing HLAE $HlaeVersion"
$hlaeMarker = Join-Path $HlaeDir ".version-$HlaeVersion"
if (-not (Test-Path $hlaeMarker)) {
    $zip = Join-Path $env:TEMP "hlae-$HlaeVersion.zip"
    Invoke-WebRequest -Uri $HlaeUrl -OutFile $zip -UseBasicParsing
    if (Test-Path $HlaeDir) { Remove-Item -Recurse -Force $HlaeDir }
    Expand-Archive -Path $zip -DestinationPath $HlaeDir -Force
    Remove-Item $zip -Force
    New-Item -ItemType File -Path $hlaeMarker -Force | Out-Null
} else {
    Write-Host "HLAE $HlaeVersion already installed"
}

# The Source 2 hook is in x64\, not at the root. The root holds the Source 1
# AfxHookSource.dll next to an AfxHookSource2_changelog.xml, which makes a
# root-level check look correct and fail anyway. CS2 is 64-bit; x64 is the one
# that matters.
#
# Fatal rather than a warning: Write-Warning does not fail a run, so a missing
# hook used to exit 0 and hand back a box that cannot record anything -- the
# failure would surface much later, as a render that produces no video.
$hook = Join-Path $HlaeDir 'x64\AfxHookSource2.dll'
if (-not (Test-Path $hook)) {
    throw "AfxHookSource2.dll not found at $hook. HLAE $HlaeVersion extracted but the Source 2 hook is missing, so this box cannot record CS2. Check whether the release layout changed."
}
Write-Host "Source 2 hook present: $hook"

# --- Console session hygiene -------------------------------------------------
# This box has no monitor, and rendering happens in the auto-logon console
# session. Anything that blanks, locks or reboots that session kills a render
# in progress.
Write-Step 'Disabling screen saver, sleep and automatic reboots'

powercfg /setactive SCHEME_MIN
powercfg /change monitor-timeout-ac 0
powercfg /change standby-timeout-ac 0
powercfg /change hibernate-timeout-ac 0

# No lock screen: a locked session has no GPU-backed desktop to render into.
$winlogon = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon'
Set-ItemProperty -Path $winlogon -Name 'ScreenSaverGracePeriod' -Value '0' -Type String -Force

$personalization = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\Personalization'
New-Item -Path $personalization -Force | Out-Null
Set-ItemProperty -Path $personalization -Name 'NoLockScreen' -Value 1 -Type DWord -Force

# Windows Update may install, but must never reboot on its own.
$au = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate\AU'
New-Item -Path $au -Force | Out-Null
Set-ItemProperty -Path $au -Name 'NoAutoRebootWithLoggedOnUsers' -Value 1 -Type DWord -Force

# IE Enhanced Security Configuration blocks the Steam client's embedded
# browser, which is where the login form lives. Server-only setting.
Write-Step 'Disabling IE Enhanced Security Configuration'
$escAdmin = 'HKLM:\SOFTWARE\Microsoft\Active Setup\Installed Components\{A509B1A7-37EF-4b3f-8CFC-4F3A74704073}'
$escUser  = 'HKLM:\SOFTWARE\Microsoft\Active Setup\Installed Components\{A509B1A8-37EF-4b3f-8CFC-4F3A74704073}'
foreach ($key in @($escAdmin, $escUser)) {
    if (Test-Path $key) { Set-ItemProperty -Path $key -Name 'IsInstalled' -Value 0 -Type DWord -Force }
}

# --- Firewall ----------------------------------------------------------------
# The render agent will listen here. The agent itself is a later step; the rule
# is harmless now and means the machine doesn't need touching again for it.
# Scoped to the VNet -- the NSG already blocks everything but RDP from outside,
# and this keeps the box closed even if that ever loosens.
Write-Step 'Opening the render agent port to the VNet'
if (-not (Get-NetFirewallRule -Name 'FragVaultRenderAgent' -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -Name 'FragVaultRenderAgent' -DisplayName 'FragVault render agent' `
        -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8090 `
        -RemoteAddress '10.10.0.0/16' | Out-Null
}

Write-Step 'Bootstrap complete'
Write-Host @'

Remaining manual steps (once, over RDP -- see docs/adr-003-render-vm.md):
  1. Log the Steam client in with the DEDICATED render account. It must not be
     the gc-sidecar's account: the sidecar holds gamesPlayed([730]) permanently
     and Steam allows one game session per account, so sharing it breaks match
     discovery.
  2. Install CS2 and let it finish. It is ~56 GB, not the ~35 GB often quoted.
  3. Run C:\fragvault\Autologon64.exe to enable auto-logon. It stores the
     password in LSA secrets rather than the registry, which is why this is
     done by hand and not from this script.
  4. Render one clip by hand to prove the box works, then capture the golden
     image with scripts/capture-golden-image.sh.

Always launch CS2 through HLAE with -insecure. HLAE injects a DLL, and running
with VAC active risks a ban on the account.

'@

Stop-Transcript | Out-Null
