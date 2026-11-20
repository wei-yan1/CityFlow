param(
    [int]$Count = 1000,
    [ValidateRange(1, 8)][int]$PaymentInstances = 1,
    [switch]$StartServices,
    [switch]$FailOnDlq,
    [switch]$RealSettlements,
    [switch]$Cleanup
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location -LiteralPath $root
$env:PATH = "C:\Program Files\Go\bin;$env:PATH"

function Assert-Port([int]$Port, [string]$Name) {
    if (-not (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue)) {
        throw "$Name is not listening on port $Port; start infrastructure/services first"
    }
}

if ($StartServices) {
    & (Join-Path $root 'scripts\microservices\start-local.ps1') -Build -PaymentInstances $PaymentInstances -SkipDocker
}
Assert-Port 3307 'MySQL'
Assert-Port 6380 'Redis'
Assert-Port 5673 'RabbitMQ AMQP'
Assert-Port 15673 'RabbitMQ Management'
Assert-Port 8094 'Payment'

Write-Host 'Queue before load:'
& (Join-Path $root 'scripts\mq\mq-status.ps1')

Write-Host "Publishing $Count synthetic task.completed.v1 events..."
$loadArgs = @('-count', $Count, '-json')
if ($RealSettlements) { $loadArgs += '-seed-orders' }
if ($Cleanup) { $loadArgs += '-cleanup' }
& go run ./tools/mqload @loadArgs
if ($LASTEXITCODE -ne 0) { throw "mqload failed with exit code $LASTEXITCODE" }

Write-Host 'Queue after load:'
& (Join-Path $root 'scripts\mq\mq-status.ps1')
if ($FailOnDlq) {
    & (Join-Path $root 'scripts\mq\mq-status.ps1') -FailOnDlq
}

if ($PaymentInstances -eq 1 -and (Test-Path -LiteralPath (Join-Path $root 'logs\microservices\payment-1.err.log'))) {
    Write-Host 'Latest Payment database/sql pool samples:'
    Select-String -Path (Join-Path $root 'logs\microservices\payment-1.err.log') -Pattern 'payment db pool' |
        Select-Object -Last 4 |
        ForEach-Object { $_.Line }
}
