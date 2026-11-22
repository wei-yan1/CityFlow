# 本地开发

## 前置条件

- Go 1.26 或兼容版本
- Docker Desktop（需要本地 MySQL、Redis、PostGIS、RabbitMQ 时使用）
- PowerShell 7、curl 或其他 HTTP 调试工具
- 需要生成或修改 go-zero 契约时安装 goctl 1.7.x（当前已验证 `1.7.3`）

## 微服务模式（默认）

CityFlow 默认运行 Gateway（go-zero REST/WSS）和 Identity、Discovery、Reaction、Task、Payment、Promotion、Tracking、Notification（go-zero zRPC）九个进程。Gateway 直接提供页面和静态资源；先启动依赖，再一键构建并启动全部进程：

```powershell
.\scripts\microservices\start-local.ps1 -Build
```

访问 <http://localhost:8087/login>。Gateway 是浏览器入口，未匹配的 API 直接返回 404。启动脚本先等各 gRPC 端口（`8090`、`8091`、`8092`、`8094`、`8095`、`8096`、`8097`、`8098`、`8099`）和 Tracking WSS（`8093`）就绪；全部通过后才监听 `8087`。停止本地进程但保留 Docker：

```powershell
.\scripts\microservices\stop-local.ps1
```

服务目录按 goctl 官方结构组织：`app/gateway/cmd/api`、`app/identity/cmd/rpc`、`app/discovery/cmd/rpc`、`app/reaction/cmd/rpc`、`app/task/cmd/rpc`、`app/payment/cmd/rpc`、`app/promotion/cmd/rpc`、`app/tracking/cmd/rpc`、`app/notification/cmd/rpc`。修改 REST 契约先改 `gateway.api` 并运行 `goctl api validate --api app/gateway/cmd/api/gateway.api`；修改 RPC 契约先改对应 `.proto` 再重新生成 pb/client/server。不要再新增 `services/<name>/main.go`。

Gateway 的边缘配置在 `app/gateway/cmd/api/etc/gateway-api.yaml`：

- `JWTSecret` 留空时从 `CITYFLOW_JWT_SECRET` 读取，必须和所有后端进程使用同一密钥；本地缺省使用开发 secret 并打印告警。
- `AuthStatusCacheTTLSeconds` 默认 5 秒；缓存 active/inactive，不缓存 Identity 不可用错误。
- `RateLimitRPS`/`RateLimitBurst` 为进程级令牌桶；`/healthz`、`/readyz` 不参与限流。
- `RequestTimeoutSeconds` 默认 15 秒，只作用于 REST 路由；WSS 路由单独注册，不套短超时。
- `Timeout: 0` 关闭 HTTP server 级写超时，避免长连接被提前切断；REST 超时由路由级 `rest.WithTimeout` 控制。
- `Middlewares.Log: false`、`Middlewares.Recover: false`，统一使用 Gateway 自己的 AccessLog 和 Recover。
- `TrackingStreamURL` 默认 `http://127.0.0.1:8093`，只由 Gateway 反代 WSS；`UploadDir` 为空时依次看 `CITYFLOW_UPLOAD_DIR`、`uploads`。

## Docker 基础设施模式

启动全部依赖：

```powershell
docker compose -f deploy/docker-compose.yml up -d
```

Compose 启动 MySQL、Redis、PostGIS、RabbitMQ；默认端口：MySQL `3307`、Redis `6380`、PostGIS `5434`、RabbitMQ `5673`、管理端 `15673`。

> 如果宿主机已有 MySQL 占用 `3307`，Docker 的 MySQL 端口映射无法生效。此时服务实际连接的是宿主机 MySQL；`init-service-databases.ps1` 直接连 `127.0.0.1:3307` 建库建账号，因此两条路径都能工作。可用 `CITYFLOW_DB_HOST`、`CITYFLOW_DB_PORT`、`CITYFLOW_DB_ROOT_USER`、`CITYFLOW_DB_ROOT_PASSWORD` 覆盖初始化目标。

启动完整微服务（依赖已拉起时）：

```powershell
.\scripts\microservices\start-local.ps1 -Build -SkipDocker
```

各服务配置在各自 `etc/*.yaml`，Gateway 通过 `gateway-api.yaml` 指向九个 gRPC endpoints 和 Tracking WSS。`REDIS_ADDR`、`CITYFLOW_JWT_SECRET`、`RABBITMQ_URL` 等共享环境变量按需覆盖。

七个有 MySQL 的服务（Identity、Task、Discovery、Payment、Promotion、Notification、Tracking）各自读取专用 DSN；Reaction 不使用 MySQL DSN：

```dotenv
IDENTITY_DATABASE_DSN=identity_dev:identity_dev_pw_2026@tcp(127.0.0.1:3307)/cityflow_identity?parseTime=true&charset=utf8mb4&loc=UTC
TASK_DATABASE_DSN=task_dev:task_dev_pw_2026@tcp(127.0.0.1:3307)/cityflow_task?parseTime=true&charset=utf8mb4&loc=UTC
DISCOVERY_DATABASE_DSN=discovery_dev:discovery_dev_pw_2026@tcp(127.0.0.1:3307)/cityflow_discovery?parseTime=true&charset=utf8mb4&loc=UTC
PAYMENT_DATABASE_DSN=payment_dev:payment_dev_pw_2026@tcp(127.0.0.1:3307)/cityflow_payment?parseTime=true&charset=utf8mb4&loc=UTC
PROMOTION_DATABASE_DSN=promotion_dev:promotion_dev_pw_2026@tcp(127.0.0.1:3307)/cityflow_promotion?parseTime=true&charset=utf8mb4&loc=UTC
NOTIFICATION_DATABASE_DSN=notification_dev:notification_dev_pw_2026@tcp(127.0.0.1:3307)/cityflow_notification?parseTime=true&charset=utf8mb4&loc=UTC
TRACKING_DATABASE_DSN=tracking_dev:tracking_dev_pw_2026@tcp(127.0.0.1:3307)/cityflow_tracking?parseTime=true&charset=utf8mb4&loc=UTC
```

`start-local.ps1` 会先执行 `scripts/db/init-service-databases.ps1` 创建八个持久化服务库和账号，再启动服务；Reaction 只依赖 Redis/RabbitMQ，不建库。每个服务只应用自己的 `service/<service>*.sql`；不再复制旧 `cityflow` 数据，也没有兼容视图。`CITYFLOW_SEED_DEMO=false` 可关闭 Identity 的演示账号初始化。

Redis 通过 `REDIS_ADDR` 可选启用：Identity 用于刷新会话，Tracking 用于位置快照/GEO/待归档轨迹，Reaction 用于点赞状态/计数/投影队列，Gateway/Task 用于账号状态缓存；不设置或 Redis 暂时不可用时，鉴权直接读取 MySQL。缓存默认 5 秒过期，可用 `AUTH_STATUS_CACHE_TTL_SECONDS` 调整。Reaction 是独立进程，启动端口 `8097`，只依赖 Redis 与 RabbitMQ。

RabbitMQ 用于跨服务事件：Task outbox relay 发布 `task.created.v1`/`task.completed.v1`/`task.cancelled.v1`；Payment 发布支付结果；Tracking 归档器发布 `rider.track.batch.v1` 到 `tracking.track-archive`，消费者写 MySQL 后删除 Redis 成员；Notification 消费任务事件。`MQ_URL` 为空时事件链路停用，RPC 主链路仍可工作；Tracking 的 MQ URL 使用 `RABBITMQ_URL`。

Gateway 的 WSS 路由是 `GET /api/v1/tasks/{id}/location/stream`，浏览器握手时在 `Sec-WebSocket-Protocol` 中发送 `cityflow` 和 `bearer.<JWT>`。Gateway 只把请求重写为 `/internal/tasks/...` 后反代到 Tracking `8093`；Tracking 本地验签、通过 Identity 校验账号 active，并调用 Task `CanTrack` 校验任务归属后订阅骑手。不要绕过 Gateway 直接连 `8093` 验收。

骑手位置 REST 路由也由 Gateway 暴露：`POST /api/v1/riders/me/location` 每 10 秒最多一次，`task_id` 仅作实时标注；`GET /api/v1/tasks/{id}/location` 快照、`GET /api/v1/tasks/{id}/history?after=&limit=` 历史、`GET /api/v1/platform/rider-locations` 管理员附近。Tracking 在 Redis 中保存快照、GEO 和按骑手追加的待归档 ZSET，归档器经 RabbitMQ 写入 `cityflow_tracking.rider_track_points`，消费者提交成功后才删除 Redis 成员；历史从 Task 的 `task_events` 时间窗合并查询 MySQL 归档和 Redis 待归档点，任务结束后仍可查询。Redis 不可用时内存模式重启即清空；没有 MySQL 或 RabbitMQ 时只保留 Redis 待归档数据，不启用异步归档。

商家账号登录 `/merchant` 可自行新增、编辑和归档门店。地图点击选点或搜索地点后保存，对外地图只显示营业中的门店；暂停/关闭/归档会隐藏。高德 POI 搜索结果只用于选位置，不会自动成为平台商家。

商家联调脚本 `node scripts/merchant-map-smoke.cjs` 使用 `CITYFLOW_TEST_URL` 指向独立的可丢弃演示服务。MySQL 归档重建测试可通过 `CITYFLOW_TEST_MYSQL_DSN` 指定专用测试库执行。

数据库连接池可通过 `DATABASE_MAX_OPEN_CONNS`、`DATABASE_MAX_IDLE_CONNS`、`DATABASE_CONN_MAX_IDLE_SECONDS` 和 `DATABASE_CONN_MAX_LIFETIME_SECONDS` 调整；REST 超时、限流和 WSS 路由在 `gateway-api.yaml` 配置。默认值见 [`configs/local.env.example`](../../configs/local.env.example)。

`/livez` 只检查进程是否存活，`/readyz` 会检查数据库是否可用。多个 API 实例可以共享同一个 MySQL、Redis 和 JWT 密钥，但当前 Compose 只启动单节点 MySQL、Redis、PostGIS 和 RabbitMQ，尚未提供主备切换或多可用区容灾。内存模式仅适用于单实例开发与测试。

## 演示账号

密码统一为 `cityflow123`：

| 角色 | 邮箱 |
|---|---|
| 用户 | `user@cityflow.local` |
| 商家 | `merchant@cityflow.local` |
| 骑手 | `rider@cityflow.local` |
| 管理员 | `admin@cityflow.local` |

## 检查

```powershell
go test ./...
go test -race ./...
go vet ./...
go build ./...
docker compose -f deploy/docker-compose.yml config --quiet
```

Docker 容器未启动时仍可运行内存模式测试；真实 MySQL 连接需要可访问的 MySQL 实例。

Reaction 的 Redis 生命周期测试默认跳过；需要真实 Redis 时指定地址，测试会使用 DB 15 并在结束后清空该测试库：

```powershell
$env:CITYFLOW_TEST_REDIS_ADDR='127.0.0.1:6380'
go test -count=1 ./app/reaction/internal/adapter/redis
```

## 日常改动顺序

1. 先更新对应的产品、架构或 API 文档。
2. 在对应服务的 `domain` / `application` / `internal/adapter` / `cmd` 文件中实现；共享能力才放 `internal/platform`。
3. 为权限、状态流转和并发行为补充必要测试。
4. 运行检查后再合并到下一阶段。

