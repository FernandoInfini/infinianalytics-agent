# Installs (or upgrades) the InfiniAnalytics agent on Windows as a service.
#
# The dashboard's "Añadir servidor" dialog prints the exact line to run in an
# elevated PowerShell (Run as administrator):
#   & ([scriptblock]::Create((irm '<download>/install.ps1'))) -Code 'XXXX-XXXX-XXXX' -Url 'https://api.analytics.infini.es'
#
# Host vitals are always sent. Add -Disks off and/or -Docker off to leave those
# modules out, and -Set 'KEY=VALUE','KEY=VALUE' for any other adjustable
# setting; they are saved in agent.env and kept on upgrades. -Reset first puts
# every adjustable setting not given back to its default, so a line with it
# describes the whole configuration. -MachineId is for servers cloned from one
# image (enroll only).
#
# Without -Code it only upgrades the binary, applies any settings given and
# restarts an already enrolled agent - the dashboard's "Actualizar agente" and
# "Configurar agente" lines.
param(
    [string]$Code = "",
    [string]$Url = "https://api.analytics.infini.es",
    [ValidateSet("", "on", "off")][string]$Disks = "",
    [ValidateSet("", "on", "off")][string]$Docker = "",
    [string[]]$Set = @(),
    [switch]$Reset,
    [string]$MachineId = "",
    [string]$DownloadUrl = "https://github.com/InfiniWorkspace/infinianalytics-agent/releases/latest/download"
)
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run this from an elevated PowerShell (Run as administrator)."
}

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$asset = "infinianalytics-agent-windows-$arch.exe"
$dir = Join-Path $env:ProgramFiles "InfiniAnalytics Agent"
$exe = Join-Path $dir "infinianalytics-agent.exe"
New-Item -ItemType Directory -Force -Path $dir | Out-Null

Write-Host "Downloading $asset..."
$tmp = "$exe.download"
Invoke-WebRequest -UseBasicParsing -Uri "$DownloadUrl/$asset" -OutFile $tmp
try {
    $sums = (Invoke-WebRequest -UseBasicParsing -Uri "$DownloadUrl/SHA256SUMS").Content
    $line = ($sums -split "`n") | Where-Object { $_ -match " $([regex]::Escape($asset))\s*$" } | Select-Object -First 1
    if ($line) {
        $expected = ($line -split "\s+")[0].ToLower()
        $actual = (Get-FileHash -Algorithm SHA256 $tmp).Hash.ToLower()
        if ($expected -ne $actual) { Remove-Item $tmp; throw "Checksum mismatch for $asset" }
    }
} catch [System.Net.WebException] { }

# A running service holds the exe open.
$svc = Get-Service -Name "infinianalytics-agent" -ErrorAction SilentlyContinue
if ($svc -and $svc.Status -eq "Running") { Stop-Service -Name "infinianalytics-agent" -Force }
Move-Item -Force $tmp $exe

if ($Code) {
    $enroll = @("enroll", $Code, "--url", $Url)
    if ($MachineId) { $enroll += "--machine-id=$MachineId" }
    & $exe @enroll
    if ($LASTEXITCODE -ne 0) { throw "Enrollment failed" }
}
$extra = @()
if ($Disks) { $extra += "--disks=$Disks" }
if ($Docker) { $extra += "--docker=$Docker" }
foreach ($kv in $Set) { $extra += "--set=$kv" }
if ($Reset) { $extra += "--reset" }
& $exe install @extra
if ($LASTEXITCODE -ne 0) { throw "Service installation failed" }
& $exe status
Write-Host "Done. The agent runs as the 'InfiniAnalytics Agent' service."
