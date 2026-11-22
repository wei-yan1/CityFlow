# Creates one MySQL instance with service-owned databases and accounts.
# Credentials are read from the git-ignored .env; nothing is hardcoded here.
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)

$dbHost = if ($env:CITYFLOW_DB_HOST) { $env:CITYFLOW_DB_HOST } else { '127.0.0.1' }
$dbPort = if ($env:CITYFLOW_DB_PORT) { $env:CITYFLOW_DB_PORT } else { '3307' }
$dbRootUser = if ($env:CITYFLOW_DB_ROOT_USER) { $env:CITYFLOW_DB_ROOT_USER } else { 'root' }
$dbRootPassword = $env:CITYFLOW_DB_ROOT_PASSWORD

$envFile = Join-Path $root '.env'
if (-not (Test-Path -LiteralPath $envFile)) {
  throw ".env not found at $envFile; copy configs/local.env.example to .env and fill the database passwords"
}

# The .env DSNs are the single source of truth for service accounts, so the
# credentials created here always match what each service reads at startup.
$dsnByKey = @{}
foreach ($line in Get-Content -LiteralPath $envFile) {
  if ($line -match '^\s*([A-Za-z0-9_]+)\s*=\s*(.*)$') {
    $dsnByKey[$Matches[1]] = $Matches[2].Trim().Trim('"')
  }
}

# The compose file owns the MySQL root password. Read it from deploy/.env when
# the caller did not export CITYFLOW_DB_ROOT_PASSWORD, so a fresh checkout can
# run start-local.ps1 without a hidden extra step.
if (-not $dbRootPassword) {
  $dbRootPassword = $dsnByKey['CITYFLOW_DB_ROOT_PASSWORD']
  if (-not $dbRootPassword) { $dbRootPassword = $dsnByKey['MYSQL_ROOT_PASSWORD'] }
}
if (-not $dbRootPassword) {
  $deployEnv = Join-Path $root 'deploy\.env'
  if (Test-Path -LiteralPath $deployEnv) {
    foreach ($line in Get-Content -LiteralPath $deployEnv) {
      if ($line -match '^\s*MYSQL_ROOT_PASSWORD\s*=\s*(.*)$') {
        $dbRootPassword = $Matches[1].Trim().Trim('"')
        break
      }
    }
  }
}
if (-not $dbRootPassword) {
  throw 'MySQL root password not configured; set CITYFLOW_DB_ROOT_PASSWORD or define MYSQL_ROOT_PASSWORD in deploy/.env'
}

$services = @(
  @{ Key = 'IDENTITY_DATABASE_DSN';     Db = 'cityflow_identity' },
  @{ Key = 'TASK_DATABASE_DSN';         Db = 'cityflow_task' },
  @{ Key = 'DISCOVERY_DATABASE_DSN';    Db = 'cityflow_discovery' },
  @{ Key = 'PAYMENT_DATABASE_DSN';      Db = 'cityflow_payment' },
  @{ Key = 'PROMOTION_DATABASE_DSN';    Db = 'cityflow_promotion' },
  @{ Key = 'NOTIFICATION_DATABASE_DSN'; Db = 'cityflow_notification' },
  @{ Key = 'TRACKING_DATABASE_DSN';     Db = 'cityflow_tracking' }
)

$mysql = Get-Command mysql.exe -ErrorAction SilentlyContinue
if (-not $mysql) {
  $candidate = 'C:\Program Files\MySQL\MySQL Server 8.0\bin\mysql.exe'
  if (Test-Path -LiteralPath $candidate) { $mysql = Get-Item -LiteralPath $candidate }
}
if (-not $mysql) { throw 'mysql.exe not found; install the MySQL client or add it to PATH' }
$mysqlPath = if ($mysql.Source) { $mysql.Source } else { $mysql.FullName }

$clientArgs = @('-h', $dbHost, '-P', $dbPort, "-u$dbRootUser", '--default-character-set=utf8mb4')
if ($dbRootPassword) { $env:MYSQL_PWD = $dbRootPassword } else { Remove-Item Env:MYSQL_PWD -ErrorAction SilentlyContinue }

$sql = New-Object System.Text.StringBuilder
foreach ($service in $services) {
  [void]$sql.AppendLine("CREATE DATABASE IF NOT EXISTS $($service.Db) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;")
}

foreach ($service in $services) {
  $dsn = $dsnByKey[$service.Key]
  if (-not $dsn) { throw "$($service.Key) is missing in .env" }
  if ($dsn -notmatch '^([^:]+):([^@]*)@tcp\(') {
    throw "$($service.Key) must be a go-zero MySQL DSN: user:password@tcp(host:port)/db"
  }
  $user = $Matches[1].Trim()
  $pass = $Matches[2]
  if ($user -eq '' -or $pass -eq '') { throw "$($service.Key) has an empty user or password" }
  $userSql = $user -replace "'", "''"
  $passSql = $pass -replace "'", "''"
  [void]$sql.AppendLine("CREATE USER IF NOT EXISTS '$userSql'@'%' IDENTIFIED BY '$passSql';")
  [void]$sql.AppendLine("ALTER USER '$userSql'@'%' IDENTIFIED BY '$passSql';")
  [void]$sql.AppendLine("GRANT ALL PRIVILEGES ON $($service.Db).* TO '$userSql'@'%';")
}

[void]$sql.AppendLine(@'
-- Task owns rider performance profiles. Older local databases created a
-- read-only compatibility view over the legacy schema; remove that view before
-- Task's migration creates the real table. A real table is left untouched.
SET @rider_profile_type = (
  SELECT TABLE_TYPE FROM information_schema.TABLES
  WHERE TABLE_SCHEMA='cityflow_task' AND TABLE_NAME='rider_profiles'
);
SET @drop_rider_profile_view = IF(@rider_profile_type='VIEW',
  'DROP VIEW cityflow_task.rider_profiles', 'SET @cityflow_noop = 1');
PREPARE cityflow_drop_rider_profile_view FROM @drop_rider_profile_view;
EXECUTE cityflow_drop_rider_profile_view;
DEALLOCATE PREPARE cityflow_drop_rider_profile_view;

-- Payment and trusted-place facts are owned by Payment and Discovery. They are
-- not exposed as cross-schema views in Task. Remove any view left by older
-- local databases; a real Table of the same name is left untouched.
SET @payment_orders_type = (
  SELECT TABLE_TYPE FROM information_schema.TABLES
  WHERE TABLE_SCHEMA='cityflow_task' AND TABLE_NAME='payment_orders'
);
SET @drop_payment_orders_view = IF(@payment_orders_type='VIEW',
  'DROP VIEW cityflow_task.payment_orders', 'SET @cityflow_noop = 1');
PREPARE cityflow_drop_payment_orders_view FROM @drop_payment_orders_view;
EXECUTE cityflow_drop_payment_orders_view;
DEALLOCATE PREPARE cityflow_drop_payment_orders_view;

SET @trusted_places_type = (
  SELECT TABLE_TYPE FROM information_schema.TABLES
  WHERE TABLE_SCHEMA='cityflow_task' AND TABLE_NAME='trusted_places'
);
SET @drop_trusted_places_view = IF(@trusted_places_type='VIEW',
  'DROP VIEW cityflow_task.trusted_places', 'SET @cityflow_noop = 1');
PREPARE cityflow_drop_trusted_places_view FROM @drop_trusted_places_view;
EXECUTE cityflow_drop_trusted_places_view;
DEALLOCATE PREPARE cityflow_drop_trusted_places_view;

FLUSH PRIVILEGES;
'@)

$sql.ToString() | & $mysqlPath @clientArgs 2>&1
if ($LASTEXITCODE -ne 0) { throw 'failed to initialize service databases' }
Remove-Item Env:MYSQL_PWD -ErrorAction SilentlyContinue
Write-Host "Service databases and accounts are ready on ${dbHost}:${dbPort}."

