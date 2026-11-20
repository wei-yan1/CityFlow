# CityFlow

CityFlow 是一个 Go 编写的多任务即时服务调度平台基础版。当前包含用户、商家、骑手和平台角色，支持 JWT 登录、账号与角色资料 CRUD、任务创建/编辑/取消、骑手抢单和状态流转，以及高德/Leaflet 地图选点、地点搜索、骑行路线预览、骑手位置快照与按任务授权的 WebSocket 推送。

> 项目仍在开发阶段，表结构和业务逻辑允许大幅增删，不维护旧数据/旧接口兼容；数据所有权、隐私 DTO、事务边界、容量和验证门槛以代码和迁移为准。

## 项目结构

```text
CityFlow/
├─ app/                      按官方 goctl 结构组织的 go-zero 服务
│  ├─ gateway/cmd/api/       公开 REST API Gateway（8087），含 gateway.api 和页面/静态资源 handler
│  ├─ identity/cmd/rpc/      账号/JWT/刷新会话 zRPC（8090），含 identity.proto
│  ├─ task/cmd/rpc/          任务履约闭环 zRPC（8091），含 task.proto
│  ├─ discovery/cmd/rpc/     商家/地点/地图/评价 zRPC（8092）
│  ├─ tracking/cmd/rpc/      位置快照/GEO/轨迹/WSS zRPC（8095，WSS 8093）
│  └─ payment/cmd/rpc/       支付/退款/结算/优惠券 zRPC（8094），含对账 worker
├─ configs/                  环境变量示例
├─ deploy/                   Docker Compose 等本地基础设施
├─ internal/
│  ├─ application/           用例服务与仓储接口
│  ├─ domain/                用户、任务、位置等领域模型
│  ├─ infrastructure/        内存、MySQL、缓存和事件适配器
│  ├─ platform/              认证上下文、gRPC 元数据与通用 HTTP 约定
│  └─ migrations/service/    服务自有 schema 的版本化迁移
├─ scripts/                  开发检查脚本和微服务启停脚本
│  └─ db/                    服务库/账号初始化脚本
├─ web/                      由 Gateway 内嵌提供的页面与静态资源
│  ├─ auth/                  登录页面
│  ├─ user/                  用户端页面
│  ├─ rider/                 骑手端页面
│  ├─ merchant/              商家端页面
│  ├─ platform/              平台端页面
│  └─ assets/                按角色划分脚本与共享资源
├─ go.mod
└─ README.md
```

Go 业务代码按“领域 + 技术职责”分布在 `internal/`：应用服务、仓储接口、领域模型和基础设施实现分别归类；`web/` 由 Gateway 的页面 handler 通过 `go:embed` 直接提供，不再有独立页面进程。`app/` 下每个服务采用 goctl 生成的官方目录形态：API 服务有 `gateway.go`、`etc/gateway-api.yaml`、`internal/{config,handler,logic,svc,types}` 和 `.api` 契约；RPC 服务有 `<service>.go`、`etc/<service>.yaml`、`.proto`、pb/client、`internal/{config,logic,server,svc}`。

## 快速启动

需要 Go 1.26 或兼容版本。程序启动时会读取 `.env` 中的本地 MySQL、Redis 和 JWT 配置；参考 `configs/local.env.example` 创建该文件，实际密钥不要提交。显式设置的环境变量优先，包括显式空值。默认运行方式是 Gateway + 五个领域微服务，不再启动独立页面进程。

使用 Docker 基础设施时，一次启动全部依赖：

```powershell
docker compose -f deploy/docker-compose.yml up -d
```

Compose 会启动 MySQL、Redis、PostGIS、RabbitMQ。默认只绑定本机端口：MySQL `3307`、Redis `6380`、PostGIS `5434`、RabbitMQ `5673`，管理端 `15673`；这样不会和宿主机的 MySQL80（3306）、Memurai（6379）及 Windows RabbitMQ（5672）抢端口。

然后一键构建并启动 Gateway、Identity、Task、Discovery、Payment、Tracking 和 Reaction。Gateway 直接提供网页和静态资源：

```powershell
.\scripts\microservices\start-local.ps1 -Build -SkipDocker
```

需要横向扩 Payment 的 MQ 消费和结算出款能力时，用 `-PaymentInstances` 启动多个 Payment 进程。它们共享同一个 `payment.task-events` 队列、Payment 数据库和结算行租约；第一个实例使用 `8094`，额外实例从 `8096` 开始分配端口；Gateway 和 Task 会收到全部 Payment endpoints。每个实例都运行顺序 MQ consumer 和数据库领取型结算 worker；只有第一个实例运行待支付/退款对账及漏结算扫描：

```powershell
.\scripts\microservices\start-local.ps1 -SkipDocker -PaymentInstances 2
```

`PaymentInstances` 取值 1 到 8。结算 worker 通过 `FOR UPDATE SKIP LOCKED` 和两分钟租约领取互不重叠的批次；每实例仍顺序处理自己领取的结算单。对账扫描暂由首实例单独执行，首实例停机时该扫描会暂停，其他 Payment 实例仍可消费消息及执行结算。多实例前先确认数据库连接池和支付渠道限流能承受相应并发；consumer 的 prefetch 为 `2`，限制每个 consumer 的未确认消息数，不代表单实例有两个并行 handler。

MQ 可靠性已加入 Publisher Confirm、Payment Inbox 崩溃恢复、队列健康和清理。快速检查：

```powershell
.\scripts\mq\mq-status.ps1
.\scripts\mq\e2e-payment.ps1 -Count 1000 -RealSettlements -Cleanup
```

`mq-status.ps1` 输出 ready/unacked/consumers/最老消息年龄估算和 DLQ；`e2e-payment.ps1` 会发真实 `task.completed.v1`、等待 `payment_inbox` 全部 `PROCESSED`，再检查队列是否清空。

启动脚本创建五个有持久表的服务库及账号，各领域服务仅应用自己的 `internal/migrations/service/` 迁移；Tracking 不使用 MySQL，位置和骑手轨迹只在 Redis。默认启动包含七个 go-zero 进程，不复制旧库数据，`8089` 不监听。Identity 默认创建演示账号（可通过 `CITYFLOW_SEED_DEMO=false` 关闭）。Payment 在自己的进程中执行支付对账和骑手结算，并通过 Task RPC 查询已完成任务。Reaction 以 Redis 处理点赞/收藏热路径，Stream 批量落 `reaction_records`。支付环境变量示例见 [`configs/local.env.example`](configs/local.env.example)。

国内地图配置见 [`configs/map.local.example.json`](configs/map.local.example.json)：复制为 `configs/map.local.json`，填入高德 Web端 JS API Key、安全码及 Web服务 Key，重启即可启用。未配置时使用 Leaflet/OSM。Discovery 当前运行路径只装配高德在线 provider；`maps.Local`/`maps.Hybrid` 与 PostGIS 桂林图层代码保留，但 local/hybrid 尚未接入，配置值不会改变实际 provider。

服务地址：<http://localhost:8087>。页面入口为 `/login`、`/rider/register`、`/user`、`/rider`、`/merchant` 和 `/admin`。

## 微服务运行模式（官方 go-zero 结构）

当前默认运行方式是 Gateway（REST）+ Identity / Task / Discovery / Payment / Tracking / Reaction（zRPC）七个独立 go-zero 进程。页面、`/assets`、`/uploads` 由 Gateway 内嵌的 `web/` 资源直接提供；Gateway 的 `/api` 未匹配路由直接 404，不存在旧接口回退，也不再有 `8089` 进程。浏览器只访问 `8087`：

| 进程 | 端口 | 契约/职责 |
|---|---|---|
| Gateway | `8087` | `gateway.api`；公开 REST 入口、本地验签 + 短 TTL 状态缓存、request ID/访问日志/限流/统一错误、登录/刷新 Cookie、任务/商家/支付/位置路由、显式 WSS 代理；启动前必须等五个 zRPC 健康 |
| Identity | `8090` | `identity.proto`；zRPC 登录、刷新、退出、`Me`、`Verify`、注册、找回密码、用户资料、平台用户管理、骑手注册审核 |
| Task | `8091` | `task.proto`；zRPC 任务列表、创建、抢单、状态推进、任务详情、骑手履约资料、骑手评价/打赏举报、骑手评论/回复 |
| Discovery | `8092` | `discovery.proto`；商家自助 CRUD/媒体、平台商家、地图搜索/附近/路线/行政区、地点详情、地点与任务商家评价、地点评论/回复 |
| Payment | `8094` | `payment.proto`；支付单、Mock/Sync/Refund/Notify、骑手结算、提现账户、优惠券与领取 |
| Tracking | `8095` / `8093` | `tracking.proto`；骑手位置上报/快照/历史/附近查询；`8093` 为只监听本机的 WSS 流，Gateway 代理 `/api/v1/tasks/{id}/location/stream` |
| Reaction | `8097` | `reaction.proto`；点赞/收藏热路径、状态查询、我的收藏；Redis + Stream 批量落 `reaction_records`，投影消息通知属主服务 |

目录采用 goctl 官方单服务结构，并按主流单仓库多服务方式放在 `app/<service>/cmd/api` 或 `cmd/rpc` 下。API 服务根入口是 `gateway.go`，RPC 服务根入口是 `identity.go` / `task.go` / `discovery.go` / `payment.go` / `tracking.go` / `reaction.go`，不再使用手写的 `services/<name>/main.go`。`gateway.api`、`identity.proto`、`task.proto`、`discovery.proto`、`payment.proto`、`tracking.proto`、`reaction.proto` 分别是各服务的契约来源。

一键启动（会先拉起 Docker 依赖、构建并启动全部 go-zero 进程）：

```powershell
.\scripts\microservices\start-local.ps1 -Build
```

依赖已在运行时可以跳过 Compose，只重启本地进程：

```powershell
.\scripts\microservices\start-local.ps1 -SkipDocker
```

启动顺序是硬约束：脚本先等待 Identity/Task/Discovery/Payment/Tracking/Reaction 的 gRPC 端口和 Tracking WSS 端口全部监听，再启动 Gateway；Gateway 在绑定 `8087` 前还会对 Identity/Task/Discovery/Payment/Tracking 五个核心服务执行健康 RPC 预检，任一核心服务不可用就直接退出。Reaction 不可用只降级点赞展示，不阻断评价读取。

停止本地服务但保留 Docker 基础设施：

```powershell
.\scripts\microservices\stop-local.ps1
```

Gateway 的 `GET /readyz` 会通过 gRPC 健康检查同时检查 Identity、Task、Discovery、Payment 和 Tracking。服务间使用静态 endpoints 直连，不依赖 etcd；各 zRPC 服务端会再次校验 Bearer token 和账号状态，不把“经过 Gateway”当作授权。Tracking 的 WSS 握手从 `Sec-WebSocket-Protocol: bearer.<token>` 读取令牌，并在订阅前通过 Task 校验任务可见性。

Gateway 的保护路由本地验签，并在 5 秒 TTL 的进程内缓存中查询账号 active 状态；缓存 miss 才调用 Identity `Verify`，不会让 Identity 成为每个请求的串行依赖。登录/刷新/退出由 Gateway 设置 HttpOnly 刷新 Cookie。REST 使用 `RequestTimeoutSeconds`（默认 15 秒）和 1 MiB 请求体上限，WSS 路由单独注册、不套短超时；request ID、访问日志、限流和统一错误映射都在边缘层完成。`GET /api/v1/tasks/{id}/location/stream` 已经实测经 Gateway 返回 `101` 并正常心跳。

### 数据库与账号

同一个 MySQL 实例内已经按服务拆库，不要求先部署五台数据库服务器：

| 数据库 | 账号 | 当前用途 |
|---|---|---|
| `cityflow_identity` | `identity_dev` | `users`、`password_reset_tokens`、`rider_registrations` |
| `cityflow_task` | `task_dev` | `tasks`、`task_events`、`task_incidents`、`rider_profiles`、`rider_feedback` |
| `cityflow_discovery` | `discovery_dev` | `merchant_profiles`、`merchant_media`、`place_reviews`、`place_review_comments`、`trusted_places` |
| `cityflow_reaction` | `reaction_dev` | `reaction_records` |
| `cityflow_payment` | `payment_dev` | `payment_orders`、`rider_settlements`、`payment_notifies`、`rider_payout_accounts`、`coupons`、`coupon_claims` |

`identity_dev` 不能访问 `cityflow_task`，`task_dev` 不能访问 `cityflow_identity`；Task 的 `rider_profiles`、`rider_feedback` 由 Task 自建并独占写入。Payment、Discovery 也各自拥有表结构和写事务；Task 不再直接写支付单、可信地点或轨迹表。Tracking 不再保存原始位置；位置和骑手轨迹只写 Redis。每个服务只应用 `internal/migrations/service/` 下自己的迁移；`scripts/db/` 只负责创建服务库和账号，不再创建兼容视图或复制旧数据。

演示密码统一为 `cityflow123`：

| 角色 | 邮箱 |
|---|---|
| 用户 | `user@cityflow.local` |
| 商家 | `merchant@cityflow.local` |
| 骑手 | `rider@cityflow.local` |
| 管理员 | `admin@cityflow.local` |

## 支付链路（默认 MOCK，可直接演示）

带 `quoted_amount_fen` 的任务必须先付款才能被骑手接单。默认 `CITYFLOW_PAYMENT_PROVIDER=MOCK`，不需要任何支付宝/微信凭证：

1. 用户 `POST /api/v1/tasks/{id}/payment` → 返回二维码内容与支付单；
2. 用户 `POST /api/v1/payments/{id}/mock-pay` → 订单变 `PAID`；
3. 骑手接单并推进到 `COMPLETED` → Payment 自动创建结算单；
4. 骑手 `GET /api/v1/riders/me/settlements` 查看结算单。当前服务只创建 `PENDING` 结算，不自动向渠道打款；真实渠道的出款 worker 仍需单独接入。

真实渠道：在 `.env` 填 `ALIPAY_*` 或 `WECHAT_PAY_*`，把 `CITYFLOW_PAYMENT_NOTIFY_BASE_URL` 指向公网 HTTPS，渠道回调地址为 `/api/v1/payment-notify/alipay|wechat`。未填凭证时接口返回 503，不会假成功。生产环境务必设置 `CITYFLOW_PAYMENT_ALLOW_MOCK_PAY=false`。

真实渠道的退款、对账和结算策略需要在上线前按渠道文档补齐；当前默认 MOCK 只用于本地演示。

## 骑手付费注册（个人经营码，最小实现）

骑手先扫描个人支付宝经营码支付至少 `0.01` 元，再在 `/rider/register` 提交交易号/付款备注。个人经营码没有支付回调，后台管理员在支付宝账单核验后，通过“骑手注册”视图开通账号；Identity 在自己的事务里创建 `RIDER` 账号并标记申请已通过，Task 通过 zRPC 单独创建 `OFFLINE` 骑手履约资料。跨服务失败会补偿回滚：申请回到 `PENDING`，本次创建的账号删除，已写入的 Task 资料归档。资金直接进入经营码，平台不经手，也不支持自动退款。

公开接口为 `POST /api/v1/rider-registrations`；管理员列表与审核接口在 `/api/v1/platform/rider-registrations` 下。

## 当前技术

- go-zero：Gateway（REST）+ Identity / Task / Discovery / Payment / Tracking / Reaction（zRPC）七个独立服务；服务间调用是 zRPC，Gateway 只做公开 REST 入口、页面/静态资源服务和 WSS 代理。
- GORM + MySQL 8 / InnoDB：用户、商家、骑手、任务和任务事件持久化
- go-redis：可选的 JWT 账号状态短时缓存；Redis 不可用时查询自动回源 MySQL
- Repository 接口：隔离内存与 MySQL 存储
- JWT HS256：角色认证
- 高德 JS API / Leaflet：地点搜索、骑行路线与位置展示；Discovery 当前走高德在线服务，本地 PostGIS/混合 provider 代码保留但未接入运行路径；复用 Fleet-Ops 地图追踪组件，Redis GEO 保存近期位置，WebSocket 按任务权限推送，Redis 按骑手保存轨迹并由任务事件时间窗查询，OSRM 骑行道路匹配用于展示
- 消息队列、对象存储：在对应业务能力形成后再引入；PostGIS 桂林图层代码保留，当前未接入 Discovery 的 provider 装配

HTTP 服务配置了请求超时、优雅退出、异常恢复、请求 ID、存活/就绪探针和可调数据库连接池。MySQL 迁移与演示数据初始化使用数据库 advisory lock，避免多个应用实例同时启动时重复初始化。数据库表结构继续由版本化 SQL 迁移维护，GORM 负责记录映射、查询、事务和更新，不在启动时自动改表。应用实例本身不保存登录会话状态；多实例部署需使用同一 `CITYFLOW_JWT_SECRET` 并共享 MySQL。

Redis 既用于近期位置快照/GEO 和按骑手追加的轨迹 ZSET，也缓存账号是否启用；账号状态缓存默认有效期为 5 秒，状态变更时尝试删除缓存；账号状态缓存故障时回源 MySQL；已配置 Redis 时，位置读写故障返回 503，不切换到进程内数据。位置事件目前使用进程内总线，多实例实时广播仍需 Redis Pub/Sub 等共享通道。单个 MySQL 实例仍是可用性瓶颈。真正的多可用区容灾需要在部署层配置 MySQL 主从/托管高可用、备份恢复与负载均衡；Compose 中的单节点 Redis 也不构成生产级高可用。

## 检查

```powershell
./scripts/check.ps1
```

手动执行时可运行 `go test -race ./...`、`go vet ./...` 和 `docker compose -f deploy/docker-compose.yml config --quiet`。

演示密码和默认 JWT 密钥只适用于本地开发，部署前请替换密钥并关闭演示账号。

地图、定位和实时订阅由 Discovery / Tracking 服务负责；本地默认使用 Leaflet/OSM，配置高德 Key 后切换为在线地图。
