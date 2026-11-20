param(
    [string]$ManagementUrl = '',
    [string[]]$Queues = @('payment.task-events', 'task.rider-events'),
    [switch]$Json,
    [switch]$FailOnDlq
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Set-Location -LiteralPath $root

function Read-DotEnv([string]$Path) {
    $values = @{}
    if (-not (Test-Path -LiteralPath $Path)) { return $values }
    foreach ($line in Get-Content -LiteralPath $Path) {
        if ($line -match '^\s*#') { continue }
        if ($line -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') {
            $value = $Matches[2].Trim()
            if ($value.Length -ge 2 -and (
                ($value.StartsWith('"') -and $value.EndsWith('"')) -or
                ($value.StartsWith("'") -and $value.EndsWith("'")))) {
                $value = $value.Substring(1, $value.Length - 2)
            }
            $values[$Matches[1]] = $value
        }
    }
    return $values
}

function Get-BasicAuth([string]$AmqpUrl) {
    try {
        $uri = [uri]$AmqpUrl
        if (-not $uri.UserInfo) { return $null }
        $pair = [uri]::UnescapeDataString($uri.UserInfo)
        return 'Basic ' + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($pair))
    } catch {
        return $null
    }
}

function Get-ApiQueue([string]$BaseUrl, [string]$Vhost, [string]$Queue, [string]$Auth) {
    $encodedVhost = [uri]::EscapeDataString($Vhost)
    $encodedQueue = [uri]::EscapeDataString($Queue)
    $headers = @{}
    if ($Auth) { $headers['Authorization'] = $Auth }
    $url = "$($BaseUrl.TrimEnd('/'))/api/queues/$encodedVhost/$encodedQueue"
    return Invoke-RestMethod -Uri $url -Headers $headers -TimeoutSec 5
}

function Get-Rate($Stats, [string]$Name) {
    if ($null -eq $Stats) { return 0.0 }
    $detail = $Stats.$Name
    if ($null -eq $detail) { return 0.0 }
    $rate = $detail.rate
    if ($null -eq $rate) { return 0.0 }
    return [double]$rate
}

$envValues = Read-DotEnv (Join-Path $root '.env')
$baseUrl = if ($ManagementUrl) { $ManagementUrl } elseif ($envValues['RABBITMQ_MANAGEMENT_URL']) { $envValues['RABBITMQ_MANAGEMENT_URL'] } else { 'http://127.0.0.1:15673' }
$amqpUrl = $envValues['RABBITMQ_URL']
$auth = if ($amqpUrl) { Get-BasicAuth $amqpUrl } else { $null }
$vhost = '/'
if ($amqpUrl) {
    try {
        $path = ([uri]$amqpUrl).AbsolutePath.Trim('/')
        if ($path) { $vhost = [uri]::UnescapeDataString($path) }
    } catch { }
}

$rows = @()
foreach ($queue in $Queues) {
    foreach ($name in @($queue, "$queue.dlq")) {
        try {
            $info = Get-ApiQueue $baseUrl $vhost $name $auth
            $ready = [int64]($info.messages_ready -as [int64])
            $unacked = [int64]($info.messages_unacknowledged -as [int64])
            $consumers = [int64]($info.consumers -as [int64])
            $publishRate = Get-Rate $info.message_stats publish_details
            $deliverRate = Get-Rate $info.message_stats deliver_get_details
            $estimatedOldest = $null
            if ($deliverRate -gt 0 -and $ready -gt 0) {
                $estimatedOldest = [math]::Round($ready / $deliverRate, 1)
            }
            $rows += [pscustomobject]@{
                Queue = $name
                Ready = $ready
                Unacked = $unacked
                Consumers = $consumers
                'Incoming/s' = [math]::Round($publishRate, 1)
                'Delivered/s' = [math]::Round($deliverRate, 1)
                'Est oldest (s)' = $estimatedOldest
            }
        } catch {
            $rows += [pscustomobject]@{
                Queue = $name
                Ready = 'ERROR'
                Unacked = ''
                Consumers = ''
                'Incoming/s' = ''
                'Delivered/s' = ''
                'Est oldest (s)' = $_.Exception.Message
            }
        }
    }
}

if ($Json) {
    $rows | ConvertTo-Json -Depth 4
} else {
    $rows | Format-Table -AutoSize
}

if ($FailOnDlq) {
    $dlqReady = ($rows | Where-Object { $_.Queue -like '*.dlq' -and $_.Ready -is [int64] } | Measure-Object -Property Ready -Sum).Sum
    if ($dlqReady -gt 0) { exit 2 }
}
