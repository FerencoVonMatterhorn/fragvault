# Defines the GPU render VM, the way cloud-init/hosting.yaml.tftpl defines the
# hosting VM. Run by the CustomScriptExtension in vm_gpu_render.tf, which
# embeds this file verbatim — editing it changes the extension settings and
# re-runs it on the next apply.
#
# It must stay idempotent: the extension re-runs on every settings change, and
# a re-run must not undo a working machine.
#
# What it deliberately does NOT do:
#   - log Steam in, or download CS2. Steam Guard is far easier to clear by hand
#     once than to automate, and the login is machine-bound anyway. Done once
#     over RDP, then captured into the golden image.
#   - set the auto-logon password. That is a secret and has no business in
#     extension settings, which are readable from the portal. Autologon.exe is
#     installed below; run it by hand once and it stores the password in LSA
#     secrets rather than the registry.
#
# Everything here is public and secret-free by design.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'  # a visible progress bar makes Invoke-WebRequest crawl

# Pinned, not "latest". A clip that renders wrong should be attributable to a
# version, and HLAE tracks CS2 updates closely enough that floating would make
# every render a different experiment.
$HlaeVersion = '2.187.0'
$HlaeUrl     = "https://github.com/advancedfx/advancedfx/releases/download/v$HlaeVersion/hlae.zip"

$Root    = 'C:\fragvault'
$LogDir  = Join-Path $Root 'logs'
$HlaeDir = 'C:\hlae'

New-Item -ItemType Directory -Force -Path $Root, $LogDir, (Join-Path $Root 'demos'), (Join-Path $Root 'out') | Out-Null

Start-Transcript -Path (Join-Path $LogDir 'bootstrap.log') -Append | Out-Null

function Write-Step($msg) { Write-Host "=== $msg" }

# TLS 1.2 — Windows Server 2022 defaults are fine, but the Chocolatey
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
    Write-Warning 'No AMD GPU found yet. If this persists after a reboot, the AmdGpuDriverWindows extension did not apply — check `az vm extension list`.'
}

# --- Package manager ---------------------------------------------------------
Write-Step 'Installing Chocolatey'
if (-not (Get-Command choco.exe -ErrorAction SilentlyContinue)) {
    Set-ExecutionPolicy Bypass -Scope Process -Force
    Invoke-Expression ((New-Object Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))
    $env:Path = [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' + [Environment]::GetEnvironmentVariable('Path', 'User')
}

Write-Step 'Installing ffmpeg, 7zip, sysinternals'
# ffmpeg is the encoder HLAE pipes raw frames into — mirv_streams does not
# encode anything itself. sysinternals is here for Autologon.exe.
choco install -y --no-progress ffmpeg 7zip sysinternals

# --- Steam -------------------------------------------------------------------
Write-Step 'Installing the Steam client'
# The full client, not just steamcmd: CS2 refuses to launch without a running,
# logged-in Steam client, whatever steamcmd has downloaded.
choco install -y --no-progress steam

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

if (-not (Test-Path (Join-Path $HlaeDir 'AfxHookSource2.dll'))) {
    Write-Warning "AfxHookSource2.dll not found in $HlaeDir — the release layout may have changed. CS2 recording needs the Source 2 hook, not AfxHookSource.dll."
}

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
# Scoped to the VNet — the NSG already blocks everything but RDP from outside,
# and this keeps the box closed even if that ever loosens.
Write-Step 'Opening the render agent port to the VNet'
if (-not (Get-NetFirewallRule -Name 'FragVaultRenderAgent' -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -Name 'FragVaultRenderAgent' -DisplayName 'FragVault render agent' `
        -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8090 `
        -RemoteAddress '10.10.0.0/16' | Out-Null
}

Write-Step 'Bootstrap complete'
Write-Host @'

Remaining manual steps (once, over RDP — see docs/adr-003-render-vm.md):
  1. Log the Steam client in with the DEDICATED render account. It must not be
     the gc-sidecar's account: the sidecar holds gamesPlayed([730]) permanently
     and Steam allows one game session per account, so sharing it breaks match
     discovery.
  2. Install CS2 (~35 GB) and let it finish.
  3. Run `Autologon.exe` (installed with sysinternals) to enable auto-logon.
  4. Render one clip by hand to prove the box works, then capture the golden
     image with scripts/capture-golden-image.sh.

Always launch CS2 through HLAE with -insecure. HLAE injects a DLL, and running
with VAC active risks a ban on the account.

'@

Stop-Transcript | Out-Null
