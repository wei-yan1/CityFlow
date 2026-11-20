param(
    [switch]$Build,
    [switch]$SkipDocker,
    [ValidateRange(1, 8)]
    [int]$PaymentInstances = 1
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location -LiteralPath $root

$logDir = Join-Path $root 'logs\microservices'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

# Scale the queue consumer and database-claimed payout worker together. Keep
# reconciliation scans on the first instance until they have distributed
# claims of their own. Keep 8094 for the first instance and avoid 8095.
$paymentPorts = @()
for ($i = 0; $i -lt $PaymentInstances; $i++) {
    if ($i -eq 0) {
        $paymentPorts += 8094
    } else {
        $paymentPorts += 8096 + ($i - 1)
    }
}
$paymentEndpoints = $paymentPorts | ForEach-Object { "127.0.0.1:$_" }
$paymentEndpointList = ($paymentEndpoints -join ',')

function Wait-Port([int]$Port, [int]$TimeoutSeconds = 30) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue) {
            return
        }
        Start-Sleep -Milliseconds 500
    }
    throw "port $Port did not start listening"
}

function Wait-Gateway([System.Diagnostics.Process]$Process, [int]$TimeoutSeconds = 90) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if ($Process.HasExited) {
            throw "gateway exited during startup preflight (exit $($Process.ExitCode)); see logs\microservices\gateway.err.log"
        }
        if (Get-NetTCPConnection -LocalPort 8087 -State Listen -ErrorAction SilentlyContinue) {
            return
        }
        Start-Sleep -Milliseconds 500
    }
    throw 'gateway did not start listening; see logs\microservices\gateway.err.log'
}

function Stop-CityFlowProcess([string]$Name, [string]$ExpectedPath) {
    Get-Process -Name $Name -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $ExpectedPath } |
        Stop-Process -Force
}

if (-not $SkipDocker) {
    Write-Host 'Starting Docker infrastructure...'
    docker compose -f (Join-Path $root 'deploy\docker-compose.yml') up -d
    if ($LASTEXITCODE -ne 0) { throw 'docker compose up failed' }
}

Write-Host 'Ensuring service databases and accounts...'
& (Join-Path $root 'scripts\db\init-service-databases.ps1')

$bin = Join-Path $root 'bin'
New-Item -ItemType Directory -Force -Path $bin | Out-Null

Stop-CityFlowProcess 'identity' (Join-Path $bin 'identity.exe')
Stop-CityFlowProcess 'task' (Join-Path $bin 'task.exe')
Stop-CityFlowProcess 'discovery' (Join-Path $bin 'discovery.exe')
Stop-CityFlowProcess 'payment' (Join-Path $bin 'payment.exe')
Stop-CityFlowProcess 'tracking' (Join-Path $bin 'tracking.exe')
Stop-CityFlowProcess 'reaction' (Join-Path $bin 'reaction.exe')
Stop-CityFlowProcess 'gateway' (Join-Path $bin 'gateway.exe')
Start-Sleep -Seconds 2

if ($Build -or -not (Test-Path -LiteralPath (Join-Path $bin 'gateway.exe'))) {
    Write-Host 'Building CityFlow binaries...'
    go build -o (Join-Path $bin 'identity.exe') .\app\identity\cmd\rpc
    if ($LASTEXITCODE -ne 0) { throw 'identity build failed' }
    go build -o (Join-Path $bin 'task.exe') .\app\task\cmd\rpc
    if ($LASTEXITCODE -ne 0) { throw 'task build failed' }
    go build -o (Join-Path $bin 'discovery.exe') .\app\discovery\cmd\rpc
    if ($LASTEXITCODE -ne 0) { throw 'discovery build failed' }
    go build -o (Join-Path $bin 'payment.exe') .\app\payment\cmd\rpc
    if ($LASTEXITCODE -ne 0) { throw 'payment build failed' }
    go build -o (Join-Path $bin 'tracking.exe') .\app\tracking\cmd\rpc
    if ($LASTEXITCODE -ne 0) { throw 'tracking build failed' }
    go build -o (Join-Path $bin 'reaction.exe') .\app\reaction\cmd\rpc
    if ($LASTEXITCODE -ne 0) { throw 'reaction build failed' }
    go build -o (Join-Path $bin 'gateway.exe') .\app\gateway\cmd\api
    if ($LASTEXITCODE -ne 0) { throw 'gateway build failed' }
}

$identityOut = Join-Path $logDir 'identity.out.log'
$identityErr = Join-Path $logDir 'identity.err.log'
$taskOut = Join-Path $logDir 'task.out.log'
$taskErr = Join-Path $logDir 'task.err.log'
$discoveryOut = Join-Path $logDir 'discovery.out.log'
$discoveryErr = Join-Path $logDir 'discovery.err.log'
$trackingOut = Join-Path $logDir 'tracking.out.log'
$trackingErr = Join-Path $logDir 'tracking.err.log'
$reactionOut = Join-Path $logDir 'reaction.out.log'
$reactionErr = Join-Path $logDir 'reaction.err.log'
$gatewayOut = Join-Path $logDir 'gateway.out.log'
$gatewayErr = Join-Path $logDir 'gateway.err.log'

$identity = Start-Process -FilePath (Join-Path $bin 'identity.exe') `
    -ArgumentList '-f', 'app/identity/cmd/rpc/etc/identity.yaml' `
    -WorkingDirectory $root `
    -RedirectStandardOutput $identityOut `
    -RedirectStandardError $identityErr `
    -WindowStyle Hidden -PassThru

$task = Start-Process -FilePath (Join-Path $bin 'task.exe') `
    -ArgumentList '-f', 'app/task/cmd/rpc/etc/task.yaml' `
    -WorkingDirectory $root `
    -RedirectStandardOutput $taskOut `
    -RedirectStandardError $taskErr `
    -WindowStyle Hidden -PassThru `
    -Environment @{ TASK_PAYMENT_RPC_ENDPOINTS = $paymentEndpointList }

$discovery = Start-Process -FilePath (Join-Path $bin 'discovery.exe') `
    -ArgumentList '-f', 'app/discovery/cmd/rpc/etc/discovery.yaml' `
    -WorkingDirectory $root `
    -RedirectStandardOutput $discoveryOut `
    -RedirectStandardError $discoveryErr `
    -WindowStyle Hidden -PassThru

$paymentProcesses = @()
for ($i = 0; $i -lt $PaymentInstances; $i++) {
    $paymentPort = $paymentPorts[$i]
    $paymentOut = Join-Path $logDir ("payment-{0}.out.log" -f ($i + 1))
    $paymentErr = Join-Path $logDir ("payment-{0}.err.log" -f ($i + 1))
    $paymentProcess = Start-Process -FilePath (Join-Path $bin 'payment.exe') `
        -ArgumentList '-f', 'app/payment/cmd/rpc/etc/payment.yaml' `
        -WorkingDirectory $root `
        -RedirectStandardOutput $paymentOut `
        -RedirectStandardError $paymentErr `
        -WindowStyle Hidden -PassThru `
        -Environment @{
            PAYMENT_LISTEN_ON = "127.0.0.1:$paymentPort"
            PAYMENT_SETTLEMENT_WORKER_ENABLED = 'true'
            PAYMENT_RECONCILIATION_WORKER_ENABLED = $(if ($i -eq 0) { 'true' } else { 'false' })
        }
    $paymentProcesses += $paymentProcess
    # Wait for the first instance so its migrations finish before the extras
    # start against the same schema.
    if ($i -eq 0) {
        Wait-Port $paymentPort
    }
}

$tracking = Start-Process -FilePath (Join-Path $bin 'tracking.exe') `
    -ArgumentList '-f', 'app/tracking/cmd/rpc/etc/tracking.yaml' `
    -WorkingDirectory $root `
    -RedirectStandardOutput $trackingOut `
    -RedirectStandardError $trackingErr `
    -WindowStyle Hidden -PassThru

$reaction = Start-Process -FilePath (Join-Path $bin 'reaction.exe') `
    -ArgumentList '-f', 'app/reaction/cmd/rpc/etc/reaction.yaml' `
    -WorkingDirectory $root `
    -RedirectStandardOutput $reactionOut `
    -RedirectStandardError $reactionErr `
    -WindowStyle Hidden -PassThru

Wait-Port 8090
Wait-Port 8091
Wait-Port 8092
$paymentPorts | ForEach-Object { Wait-Port $_ }
Wait-Port 8095
Wait-Port 8093
Wait-Port 8097

# Gateway binds only after every domain service is listening; its own startup
# preflight then waits for all health RPCs before opening the public port.
$gateway = Start-Process -FilePath (Join-Path $bin 'gateway.exe') `
    -ArgumentList '-f', 'app/gateway/cmd/api/etc/gateway-api.yaml' `
    -WorkingDirectory $root `
    -RedirectStandardOutput $gatewayOut `
    -RedirectStandardError $gatewayErr `
    -WindowStyle Hidden -PassThru `
    -Environment @{ GATEWAY_PAYMENT_RPC_ENDPOINTS = $paymentEndpointList }

Wait-Gateway $gateway

$deadline = (Get-Date).AddSeconds(30)
$ready = $false
while ((Get-Date) -lt $deadline) {
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:8087/readyz' -TimeoutSec 3
        if ($response.StatusCode -eq 200) { $ready = $true; break }
    } catch {
        Start-Sleep -Milliseconds 500
    }
}
if (-not $ready) {
    throw 'Gateway did not become ready; see logs\microservices\gateway.err.log'
}

@{
    identity         = $identity.Id
    task             = $task.Id
    discovery        = $discovery.Id
    payment          = @($paymentProcesses | ForEach-Object { $_.Id })
    paymentInstances = $PaymentInstances
    paymentPorts     = @($paymentPorts)
    tracking         = $tracking.Id
    reaction         = $reaction.Id
    gateway          = $gateway.Id
} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $logDir 'pids.json') -Encoding utf8

Write-Host ''
Write-Host 'CityFlow microservices are ready:'
Write-Host '  Gateway  http://localhost:8087'
Write-Host '  Identity 127.0.0.1:8090 (gRPC internal)'
Write-Host '  Task     127.0.0.1:8091 (gRPC internal)'
Write-Host '  Discovery 127.0.0.1:8092 (gRPC internal)'
Write-Host ("  Payment   {0} instance(s): {1} (gRPC internal)" -f $PaymentInstances, ($paymentEndpoints -join ', '))
Write-Host '  Tracking  127.0.0.1:8095 gRPC / 8093 WSS (internal)'
Write-Host '  Reaction  127.0.0.1:8097 (gRPC internal)'
Write-Host ''
Write-Host 'Demo accounts: user@cityflow.local / merchant@cityflow.local / rider@cityflow.local / admin@cityflow.local'
Write-Host 'Password: cityflow123'
