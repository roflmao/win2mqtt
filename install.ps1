<#
.SYNOPSIS
    Installs win2mqtt as a per-user Scheduled Task that starts hidden at logon.

.DESCRIPTION
    Copies win2mqtt.exe (and win2mqtt.yaml, if not already installed) from the
    directory containing this script to %LOCALAPPDATA%\win2mqtt, registers a
    Scheduled Task named "win2mqtt" that runs at logon of the current user, and
    starts it. No administrator rights are required.

.PARAMETER Uninstall
    Stops and removes the Scheduled Task, kills any running win2mqtt.exe and
    deletes the install directory (including the config file and log).

.EXAMPLE
    Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
    .\install.ps1

.EXAMPLE
    .\install.ps1 -Uninstall
#>
[CmdletBinding()]
param(
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'

$TaskName   = 'win2mqtt'
$InstallDir = Join-Path $env:LOCALAPPDATA 'win2mqtt'
$ExeName    = 'win2mqtt.exe'
$ConfigName = 'win2mqtt.yaml'
$ScriptDir  = Split-Path -Parent $MyInvocation.MyCommand.Path

function Write-Info($Message) { Write-Host "[install] $Message" }

function Stop-ExistingTask {
    $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($task) {
        Write-Info "Stopping existing task"
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    }
    Get-Process -Name ($ExeName -replace '\.exe$', '') -ErrorAction SilentlyContinue |
        Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 500
}

if ($Uninstall) {
    Stop-ExistingTask
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
        Write-Info "Removed scheduled task $TaskName"
    }
    if (Test-Path $InstallDir) {
        Remove-Item -Recurse -Force $InstallDir
        Write-Info "Removed $InstallDir"
    }
    Write-Info "Uninstall complete"
    exit 0
}

$sourceExe = Join-Path $ScriptDir $ExeName
if (-not (Test-Path $sourceExe)) {
    throw "$ExeName not found next to install.ps1 ($ScriptDir). Download it from the releases page and rename it to $ExeName."
}

$sourceConfig = Join-Path $ScriptDir $ConfigName
$targetConfig = Join-Path $InstallDir $ConfigName
if (-not (Test-Path $sourceConfig) -and -not (Test-Path $targetConfig)) {
    throw "$ConfigName not found next to install.ps1. Copy $ConfigName.example to $ConfigName and fill in your broker settings."
}

Stop-ExistingTask

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item -Force $sourceExe (Join-Path $InstallDir $ExeName)
Write-Info "Installed $ExeName to $InstallDir"

if (Test-Path $targetConfig) {
    Write-Info "Keeping existing $targetConfig"
} else {
    Copy-Item $sourceConfig $targetConfig
    Write-Info "Installed $ConfigName"
}

$action = New-ScheduledTaskAction -Execute (Join-Path $InstallDir $ExeName) -WorkingDirectory $InstallDir
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$settings = New-ScheduledTaskSettingsSet `
    -Hidden `
    -ExecutionTimeLimit ([TimeSpan]::Zero) `
    -RestartCount 3 `
    -RestartInterval (New-TimeSpan -Minutes 1) `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -MultipleInstances IgnoreNew
$principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited

Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings -Principal $principal -Force | Out-Null
Write-Info "Registered scheduled task $TaskName (runs at logon)"

Start-ScheduledTask -TaskName $TaskName
Write-Info "Started $TaskName"
Write-Info "Log file: $(Join-Path $InstallDir 'win2mqtt.log')"
