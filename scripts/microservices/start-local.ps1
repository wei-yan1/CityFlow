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

# Scale the queue consumer and database-claimed payout worker together. Local
# reconciliation stays on the first instance; when PAYMENT_XXL_JOB_ENABLED is
# set, Payment disables it and the scheduler owns the singleton scans. 8094 is
# the first instance; extra instances start at 8100 because 8098/8099 are
# reserved for internal services.
$paymentPorts = @()
for ($i = 0; $i -lt $PaymentInstances; $i++) {
    if ($i -eq 0) {
        $paymentPorts += 8094
    } else {
        $paymentPorts += 8100 + ($i - 1)
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

function Start-RpcService([string]$Name, [string]$ConfigPath) {
    $out = Join-Path $logDir "$Name.out.log"
    $err = Join-Path $logDir "$Name.err.log"
    return Start-Process -FilePath (Join-Path $bin "$Name.exe") `
        -ArgumentList '-f', $ConfigPath `
        -WorkingDirectory $root `
        -RedirectStandardOutput $out `
        -RedirectStandardError $err `
        -WindowStyle Hidden -PassThru
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

foreach ($name in 'identity', 'task', 'discovery', 'reaction', 'payment', 'tracking', 'promotion', 'notification', 'gateway') {
    Stop-CityFlowProcess $name (Join-Path $bin "$name.exe")
}
Start-Sleep -Seconds 2

if ($Build -or -not (Test-Path -LiteralPath (Join-Path $bin 'gateway.exe')) -or -not (Test-Path -LiteralPath (Join-Path $bin 'reaction.exe'))) {
    Write-Host 'Building CityFlow binaries...'
    $targets = [ordered]@{
        identity     = '.\app\identity\cmd\rpc'
        task         = '.\app\task\cmd\rpc'
        discovery    = '.\app\discovery\cmd\rpc'
        reaction     = '.\app\reaction\cmd\rpc'
        payment      = '.\app\payment\cmd\rpc'
        tracking     = '.\app\tracking\cmd\rpc'
        promotion    = '.\app\promotion\cmd\rpc'
        notification = '.\app\notification\cmd\rpc'
        gateway      = '.\app\gateway\cmd\api'
    }
    foreach ($entry in $targets.GetEnumerator()) {
        go build -o (Join-Path $bin "$($entry.Key).exe") $entry.Value
        if ($LASTEXITCODE -ne 0) { throw "$($entry.Key) build failed" }
    }
}

$identity = Start-RpcService 'identity' 'app/identity/cmd/rpc/etc/identity.yaml'
$task = Start-Process -FilePath (Join-Path $bin 'task.exe') `
    -ArgumentList '-f', 'app/task/cmd/rpc/etc/task.yaml' `
    -WorkingDirectory $root `
    -RedirectStandardOutput (Join-Path $logDir 'task.out.log') `
    -RedirectStandardError (Join-Path $logDir 'task.err.log') `
    -WindowStyle Hidden -PassThru `
    -Environment @{ TASK_PAYMENT_RPC_ENDPOINTS = $paymentEndpointList }
$discovery = Start-RpcService 'discovery' 'app/discovery/cmd/rpc/etc/discovery.yaml'
$reaction = Start-RpcService 'reaction' 'app/reaction/cmd/rpc/etc/reaction.yaml'
$promotion = Start-RpcService 'promotion' 'app/promotion/cmd/rpc/etc/promotion.yaml'

$paymentProcesses = @()
for ($i = 0; $i -lt $PaymentInstances; $i++) {
    $paymentPort = $paymentPorts[$i]
    $paymentOut = Join-Path $logDir ("payment-{0}.out.log" -f ($i + 1))
    $paymentErr = Join-Path $logDir ("payment-{0}.err.log" -f ($i + 1))
    $paymentEnv = @{
        PAYMENT_LISTEN_ON = "127.0.0.1:$paymentPort"
        PAYMENT_SETTLEMENT_WORKER_ENABLED = 'true'
        PAYMENT_RECONCILIATION_WORKER_ENABLED = $(if ($i -eq 0) { 'true' } else { 'false' })
    }
    if ($env:PAYMENT_XXL_JOB_ENABLED -match '^(1|true|yes|on)$') {
        $paymentEnv['PAYMENT_RECONCILIATION_WORKER_ENABLED'] = 'false'
        $paymentEnv['PAYMENT_XXL_JOB_EXECUTOR_PORT'] = "$(10000 + $i)"
    }
    $paymentProcess = Start-Process -FilePath (Join-Path $bin 'payment.exe') `
        -ArgumentList '-f', 'app/payment/cmd/rpc/etc/payment.yaml' `
        -WorkingDirectory $root `
        -RedirectStandardOutput $paymentOut `
        -RedirectStandardError $paymentErr `
        -WindowStyle Hidden -PassThru `
        -Environment $paymentEnv
    $paymentProcesses += $paymentProcess
    if ($i -eq 0) {
        Wait-Port $paymentPort
    }
}

$notification = Start-RpcService 'notification' 'app/notification/cmd/rpc/etc/notification.yaml'
$tracking = Start-RpcService 'tracking' 'app/tracking/cmd/rpc/etc/tracking.yaml'

Wait-Port 8090
Wait-Port 8091
Wait-Port 8092
Wait-Port 8097
$paymentPorts | ForEach-Object { Wait-Port $_ }
Wait-Port 8098
Wait-Port 8099
Wait-Port 8095
Wait-Port 8093

# Gateway binds only after every domain service is listening; its own startup
# preflight then waits for all health RPCs before opening the public port.
$gateway = Start-Process -FilePath (Join-Path $bin 'gateway.exe') `
    -ArgumentList '-f', 'app/gateway/cmd/api/etc/gateway-api.yaml' `
    -WorkingDirectory $root `
    -RedirectStandardOutput (Join-Path $logDir 'gateway.out.log') `
    -RedirectStandardError (Join-Path $logDir 'gateway.err.log') `
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
    reaction         = $reaction.Id
    payment          = @($paymentProcesses | ForEach-Object { $_.Id })
    paymentInstances = $PaymentInstances
    paymentPorts     = @($paymentPorts)
    promotion        = $promotion.Id
    notification     = $notification.Id
    tracking         = $tracking.Id
    gateway          = $gateway.Id
} | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $logDir 'pids.json') -Encoding utf8

Write-Host ''
Write-Host 'CityFlow microservices are ready:'
Write-Host '  Gateway      http://localhost:8087'
Write-Host '  Identity     127.0.0.1:8090 (gRPC internal)'
Write-Host '  Task         127.0.0.1:8091 (gRPC internal)'
Write-Host '  Discovery    127.0.0.1:8092 (gRPC internal)'
Write-Host '  Reaction     127.0.0.1:8097 (gRPC internal)'
Write-Host ("  Payment      {0} instance(s): {1} (gRPC internal)" -f $PaymentInstances, ($paymentEndpoints -join ', '))
Write-Host '  Promotion    127.0.0.1:8098 (gRPC internal)'
Write-Host '  Notification 127.0.0.1:8099 (gRPC internal)'
Write-Host '  Tracking     127.0.0.1:8095 gRPC / 8093 WSS (internal)'
Write-Host ''
Write-Host 'Demo accounts: user@cityflow.local / merchant@cityflow.local / rider@cityflow.local / admin@cityflow.local'
Write-Host 'Password: cityflow123'

