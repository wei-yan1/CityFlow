# Creates one MySQL instance with service-owned databases and accounts.
# Development credentials only; production must override them.
$ErrorActionPreference = 'Stop'
$root = 'C:\Users\MR\Desktop\CityFlow'

$sql = @'
CREATE DATABASE IF NOT EXISTS cityflow_identity DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS cityflow_task DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS cityflow_discovery DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS cityflow_payment DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE IF NOT EXISTS cityflow_reaction DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

CREATE USER IF NOT EXISTS 'identity_dev'@'%' IDENTIFIED BY 'CHANGE_ME_IDENTITY_DB_PASSWORD';
CREATE USER IF NOT EXISTS 'task_dev'@'%' IDENTIFIED BY 'CHANGE_ME_TASK_DB_PASSWORD';
CREATE USER IF NOT EXISTS 'discovery_dev'@'%' IDENTIFIED BY 'CHANGE_ME_DISCOVERY_DB_PASSWORD';
CREATE USER IF NOT EXISTS 'payment_dev'@'%' IDENTIFIED BY 'CHANGE_ME_PAYMENT_DB_PASSWORD';
CREATE USER IF NOT EXISTS 'reaction_dev'@'%' IDENTIFIED BY 'CHANGE_ME_REACTION_DB_PASSWORD';

GRANT ALL PRIVILEGES ON cityflow_identity.* TO 'identity_dev'@'%';
GRANT ALL PRIVILEGES ON cityflow_task.* TO 'task_dev'@'%';
GRANT ALL PRIVILEGES ON cityflow_discovery.* TO 'discovery_dev'@'%';
GRANT ALL PRIVILEGES ON cityflow_payment.* TO 'payment_dev'@'%';
GRANT ALL PRIVILEGES ON cityflow_reaction.* TO 'reaction_dev'@'%';

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
'@

$sql | docker exec -i cityflow-mysql mysql -uroot -pCHANGE_ME_MYSQL_ROOT_PASSWORD 2>&1
if ($LASTEXITCODE -ne 0) { throw 'failed to initialize service databases' }
Write-Host 'Service databases and accounts are ready.'
