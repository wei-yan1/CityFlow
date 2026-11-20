# 桂林地图数据导入脚本（OpenStreetMap -> PostGIS）
#
# 用法：在项目根目录执行
#   pwsh scripts/map/import-guilin.ps1
#
# 依赖：Docker Desktop（含 postgis/postgis:16-3.4 镜像）。
# 数据：download.openstreetmap.fr 的广西省级提取，ODbL 许可。

param(
    [string]$Container = 'cityflow-postgis',
    [int]$Port = 5434,
    [string]$Database = 'cityflow_map',
    [string]$User = 'cityflow',
    [string]$Password = 'cityflow_map_dev',
    [string]$SourceUrl = 'https://download.openstreetmap.fr/extracts/asia/china/guangxi-latest.osm.pbf'
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$dataDir = Join-Path $root 'data\osm'
$pbf = Join-Path $dataDir 'guangxi-latest.osm.pbf'
$schema = Join-Path $PSScriptRoot 'guilin_schema.sql'

New-Item -ItemType Directory -Force -Path $dataDir | Out-Null

# 1. 确保 PostGIS 容器在运行
$running = docker ps --filter "name=$Container" --format '{{.Names}}'
if (-not $running) {
    $exists = docker ps -a --filter "name=$Container" --format '{{.Names}}'
    if ($exists) {
        docker start $Container | Out-Null
    } else {
        docker run -d --name $Container `
            -e "POSTGRES_DB=$Database" -e "POSTGRES_USER=$User" -e "POSTGRES_PASSWORD=$Password" `
            -p "${Port}:5432" -v cityflow_pg_data:/var/lib/postgresql/data `
            postgis/postgis:16-3.4 | Out-Null
    }
    Start-Sleep -Seconds 8
}
docker exec $Container pg_isready -U $User -d $Database | Out-Null

# 2. 下载广西提取（约 60 MB）
if (-not (Test-Path -LiteralPath $pbf)) {
    Write-Host "下载 $SourceUrl ..."
    Invoke-WebRequest -Uri $SourceUrl -OutFile $pbf -UserAgent 'CityFlow-map/1.0' -TimeoutSec 900
}

# 3. 工具链：osm2pgsql 与 osmium 只装在容器内，容器重建后需重跑本节
docker exec $Container bash -c "command -v osm2pgsql >/dev/null && command -v osmium >/dev/null" 2>$null
if ($LASTEXITCODE -ne 0) {
    docker exec $Container bash -c "rm -f /etc/apt/sources.list.d/pgdg.list; apt-get update >/dev/null 2>&1; DEBIAN_FRONTEND=noninteractive apt-get install -y -qq osm2pgsql osmium-tool >/dev/null 2>&1"
}

# 4. 导入 OSM（保留 hstore 标签，几何使用 WGS84）
docker cp $pbf "${Container}:/data/guangxi.osm.pbf"
docker exec $Container bash -c "PGPASSWORD=$Password psql -U $User -h 127.0.0.1 -d $Database -c 'CREATE EXTENSION IF NOT EXISTS hstore;' -c 'DROP TABLE IF EXISTS planet_osm_point, planet_osm_line, planet_osm_polygon, planet_osm_roads;'"
docker exec $Container bash -c "PGPASSWORD=$Password osm2pgsql -d $Database -U $User -H 127.0.0.1 -P 5432 --latlong --hstore --slim --drop --cache 1024 --number-processes 2 /data/guangxi.osm.pbf"

# 5. 裁剪桂林 bbox，供离线瓦片使用
docker exec $Container bash -c "osmium extract -b 109.30,24.10,111.35,26.45 --set-bounds -o /data/guilin.osm.pbf /data/guangxi.osm.pbf"
docker cp "${Container}:/data/guilin.osm.pbf" (Join-Path $dataDir 'guilin.osm.pbf')

# 6. 生成桂林业务图层（POI / 路网 / 行政边界）
docker cp $schema "${Container}:/data/guilin_schema.sql"
docker exec $Container bash -c "PGPASSWORD=$Password psql -U $User -h 127.0.0.1 -d $Database -f /data/guilin_schema.sql"

# 7. 结果统计
$sql = "SELECT 'poi' AS layer, count(*) FROM guilin_poi UNION ALL SELECT 'road', count(*) FROM guilin_road UNION ALL SELECT 'admin', count(*) FROM guilin_admin;"
$sql | docker exec -i $Container psql -U $User -h 127.0.0.1 -d $Database
Write-Host "完成：PostGIS 端口 $Port，数据库 $Database"
