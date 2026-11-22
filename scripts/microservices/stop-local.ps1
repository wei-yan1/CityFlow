param()

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$bin = Join-Path $root 'bin'

foreach ($name in @('gateway', 'notification', 'promotion', 'tracking', 'payment', 'discovery', 'task', 'identity', 'reaction')) {
    $path = Join-Path $bin ($name + '.exe')
    Get-Process -Name $name -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $path } |
        Stop-Process -Force
}

Write-Host 'CityFlow microservices stopped. Docker infrastructure was left running.'
