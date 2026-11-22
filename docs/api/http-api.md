# HTTP API

本地地址：`http://localhost:8087`。页面入口为 `/login`、`/rider/register`、`/user`、`/rider`、`/merchant` 和 `/admin`。

除存活检查、就绪检查和登录外，API 使用：

```http
Authorization: Bearer <access_token>
```

## 认证与健康检查

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/api/v1/auth/login` | 公开 | 邮箱、密码登录，返回 24 小时有效的 HS256 JWT 和 HttpOnly 刷新 Cookie；刷新会话有效 7 天 |
| `POST` | `/api/v1/auth/refresh` | 刷新 Cookie | 原子轮换刷新凭证并签发新访问令牌 |
| `POST` | `/api/v1/auth/logout` | 刷新 Cookie | 撤销当前刷新会话 |
| `GET` | `/api/v1/auth/me` | 登录 | 返回当前账号 |
| `GET` | `/livez` | 公开 | 进程存活，不依赖数据库 |
| `GET` | `/readyz` | 公开 | 检查数据库连接，可供负载均衡器摘除未就绪实例 |
| `GET` | `/healthz` | 公开 | 与就绪检查相同，保留兼容入口 |

## 账号 CRUD

平台管理员可管理账号。账号密码只接受创建请求，响应不会包含密码或哈希；删除账号采用停用，保留历史任务和审计关系。

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/api/v1/users/me` | 获取当前账号资料 |
| `PUT` | `/api/v1/users/me` | 更新自己的姓名、邮箱、电话和头像链接 |
| `GET` | `/api/v1/platform/users?status=ACTIVE&limit=20&offset=0` | 平台分页查询账号，可按状态筛选 |
| `POST` | `/api/v1/platform/users` | 创建账号，字段：`email`、`password`、`name`、`phone`、`role` |
| `GET` | `/api/v1/platform/users/{userId}` | 获取账号 |
| `PUT` | `/api/v1/platform/users/{userId}` | 更新账号资料 |
| `DELETE` | `/api/v1/platform/users/{userId}` | 停用账号；不能停用当前管理员自己 |

新建商家或骑手账号后，再创建对应资料。账号和业务资料分开管理，避免将登录凭据混入业务表。

## 商家与骑手资料

平台可以查询、新建、修改和归档资料；商家通过 `/me` 新增、查询、修改和归档自己的门店，骑手通过 `/me` 修改自己的资料。归档会隐藏资料，不删除关联账号或任务历史。

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` / `GET` / `PUT` / `DELETE` | `/api/v1/merchants/me` | 商家 | 新增、查询、修改、归档自己的门店；归档后可再次 POST 建立资料 |
| `GET` / `PUT` / `DELETE` | `/api/v1/riders/me` | 骑手 | 查询、修改、归档当前骑手资料 |
| `GET` | `/api/v1/platform/merchants?status=OPEN&limit=20&offset=0` | 管理员 | 分页查询门店，可按营业状态筛选 |
| `POST` | `/api/v1/platform/merchants` | 管理员 | 创建门店资料，需传 `user_id` 和门店字段 |
| `GET` / `PUT` / `DELETE` | `/api/v1/platform/merchants/{userId}` | 管理员 | 查询、修改或归档门店 |
| `GET` | `/api/v1/platform/riders?status=AVAILABLE&limit=20&offset=0` | 管理员 | 分页查询骑手，可按工作状态筛选 |
| `POST` | `/api/v1/platform/riders` | 管理员 | 创建骑手资料，需传 `user_id` 和骑手字段 |
| `GET` / `PUT` / `DELETE` | `/api/v1/platform/riders/{userId}` | 管理员 | 查询、修改或归档骑手资料 |

商家资料包含名称、地址、坐标、营业状态、商家分区和每小时容量。商家分区使用 `service_types` 单值，只允许 `FOOD`、`SUPPLIES`、`OTHER`；骑手资料仍使用自己的 `skills` 服务类型。骑手资料包含展示名、车辆、服务类型、服务区域、最大距离和并行任务上限。

### 商家点赞与月度热榜

商家点赞以月为单位去重，由 Discovery 专用事实表落地；资源接口只做商家目标映射，不进入通用 Reaction：

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/api/v1/merchants/{merchant_id}/likes` | 登录 | body 为 `{"active":true/false}`；同一用户同一商家当月只计一次，返回 `active/count/month` |
| `GET` | `/api/v1/reactions/state?target_type=MERCHANT&kind=LIKE&target_ids=...` | 登录 | 查询当前用户当月点赞状态与总赞数 |
| `GET` | `/api/v1/merchants/hot?category=FOOD&limit=10` | 公开 | 返回指定分区的热榜，`category` 必填，`limit` 默认 10，最多 50；每项含 `avatar_url`、`review_count`、`rating`、`top_review` |
| `GET` | `/api/v1/merchants/hot?category=FOOD&month=202610&limit=10` | 公开 | 查询历史月份快照；`month` 为 `YYYYMM`，为空或当前月时走实时榜，过去月份只读 `merchant_hot_snapshot` |

`top_review` 是该商家点赞最高的评价，包含 `id`、公开作者标签、评分、正文、点赞数和日期；无评价时省略。热榜的头像、评价汇总和第一热评由 Discovery 批量读取 `merchant_media` 与 `place_reviews`，不从每个卡片单独请求。

事实表 `merchant_like` 以 `(merchant_id, user_id, like_month)` 唯一；Redis 使用 `hot:merchant:{yyyyMM}:{category}`，member 是商家 ID，score 是当月点赞总数。Redis 只负责榜单和商家详情短缓存，MySQL 是权威；Redis 缺失时按分类从事实表重建。`merchant_hot_snapshot` 以 `(month, category, rank_no)` 为主键，保存每月冻结榜单；`month` 为空或等于当前月时走实时榜，过去月份只读快照，不再查询 Redis 或当前点赞数。快照保留已归档商家，profile 已不存在时以“已下线商家”占位并保留当月名次与点赞数。当前月是实时统计，历史月是历史快照，两者在页面上用月份选择器和标签区分。非法月份格式或未来月份返回 `400`，快照存储不可用返回 `503`。

商家端 `POST /merchants/me` 请求体与修改资料一致，所属账号从 JWT 获取，不接受 `user_id`；活动门店已存在返回 409。归档后再次新增复用原门店 ID、替换资料并恢复有效状态，MySQL 与内存行为一致。新增成功返回 201，归档成功 204。所有 `/merchants/me` 操作仅向商家角色开放。

商家地图选点/地点搜索在编辑草稿中生效，保存后才写入 WGS84 坐标。对外地图只展示有效账号下、未归档且营业状态为 OPEN 的门店，暂停/关闭/归档时隐藏。用户、骑手、平台地图每约 30 秒重新获取商家投影；点击“刷新地图”会同时请求最新商家资料，无需等待缓存周期。这里是应用内商家标记，不会修改高德自身 POI。

## 骑手付费注册

这是独立的骑手入驻流程：骑手先扫描个人支付宝经营码支付至少 `0.01` 元，再提交交易号/付款备注，管理员人工核验后开通账号。个人经营码没有支付回调，因此没有“支付成功自动注册”。资金直接进入经营码，平台不经手，也不支持自动退款。

公开注册页为 `/rider/register`，登录页有入口。公开提交接口位于认证中间件之前，不需要 Bearer Token。

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/api/v1/rider-registrations` | 公开 | 提交付费注册申请；返回 `201` 和申请记录，不返回密码哈希 |
| `GET` | `/api/v1/platform/rider-registrations?status=PENDING&limit=20&offset=0` | 管理员 | 分页查询注册申请；`status` 可为空或 `PENDING/APPROVED/REJECTED` |
| `POST` | `/api/v1/platform/rider-registrations/{id}/approve` | 管理员 | 核验通过；创建 `RIDER` 账号与 `OFFLINE` 骑手资料，body `{"note":"..."}` |
| `POST` | `/api/v1/platform/rider-registrations/{id}/reject` | 管理员 | 驳回；只改状态，不创建账号，body `{"note":"..."}` |

公开提交请求体：

```json
{
  "email": "rider@example.com",
  "password": "至少-8-位",
  "display_name": "骑手姓名",
  "phone": "13900000000",
  "vehicle_type": "E_BIKE",
  "skills": ["DELIVERY", "PICKUP"],
  "service_area": "桂林电子科技大学周边",
  "max_distance_meters": 5000,
  "max_concurrent_tasks": 3,
  "paid_amount_fen": 1,
  "payment_reference": "2026111422001234567890",
  "payment_note": "支付宝付款"
}
```

校验规则：邮箱可解析、密码至少 8 位、姓名必填、车辆类型在白名单内、服务类型至少一项、最大距离 1–100000、并行任务 1–20、`paid_amount_fen >= 1`、`payment_reference` 1–128 字且全局唯一。Pending 状态下邮箱/手机号不能重复；被驳回后可用新凭证重新申请。`paid_amount_fen` 缺失或为 0 返回 `400`，不会创建申请。

审核通过返回的申请记录包含 `status=APPROVED` 和 `user_id`；账号密码沿用申请时提交的密码。审核通过后新骑手可直接用邮箱/密码登录 `/rider`。重复通过、重复驳回返回 `409`；驳回后再通过也返回 `409`，需要重新提交申请。

示例：

```powershell
$body = @{
  email='rider@example.com'; password='rider-pass-123'; display_name='测试骑手'
  vehicle_type='E_BIKE'; skills=@('DELIVERY'); service_area='校园'
  max_distance_meters=5000; max_concurrent_tasks=3
  paid_amount_fen=1; payment_reference='ALI-20261114-001'; payment_note='支付宝付款'
} | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8087/api/v1/rider-registrations -ContentType 'application/json' -Body $body
```

## 任务 CRUD 与履约

任务列表默认每页 50 条，`limit` 最大 200，`offset` 从 0 开始。用户只查自己的任务；骑手查询开放任务和自己已接的任务；管理员可以查全量任务。

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/api/v1/tasks?status=OPEN&limit=50&offset=0` | 登录 | 按角色返回可见任务 |
| `POST` | `/api/v1/tasks` | 用户/管理员 | 创建任务 |
| `GET` | `/api/v1/tasks/{taskId}` | 登录 | 查询有权查看的任务及状态事件 |
| `PUT` / `PATCH` | `/api/v1/tasks/{taskId}` | 创建者/管理员 | 只允许编辑开放任务 |
| `DELETE` | `/api/v1/tasks/{taskId}` | 创建者/管理员 | 业务取消，body 可选 `{"reason":"..."}`，保留任务、取消原因和状态事件 |
| `POST` | `/api/v1/tasks/{taskId}/claim` | 骑手 | 抢单；数据库条件更新保证只有一个成功者 |
| `POST` | `/api/v1/tasks/{taskId}/status` | 指派骑手/管理员 | 按状态机推进任务 |
| `GET` | `/api/v1/coupons/task/{taskId}` | 任务创建者/管理员 | 按任务报价返回可用券及抵扣后实付；任务已取消或无报价时返回空列表 |

任务状态：

```text
OPEN → CLAIMED → ARRIVED → PROCESSING → COMPLETED
  └──────────────→ CANCELLED
        CLAIMED ──→ CANCELLED
        ARRIVED ──→ CANCELLED
```

更新任务内容不会改变状态。删除语义是取消，而非物理删除；任务状态转移和事件记录在同一事务中提交。

## 分页响应

列表响应形如：

```json
{"data": [], "limit": 20, "offset": 0}
```

平台资源和任务列表均支持 `limit`、`offset`。`limit` 范围为 1–200，`offset` 范围为 0–1,000,000；超出范围返回 `400`。

## 错误响应

```json
{"error":"错误说明"}
```

常见状态码：`400` 参数错误，`401` 登录失效或账号停用，`403` 角色/资源权限不足，`404` 不存在或已归档，`409` 重复数据或状态冲突，`500` 服务内部错误，`503` 数据库未就绪。响应头 `X-Request-ID` 可用于日志定位。

## 地图与位置（2026-11-08）

| 方法 | 路径 | 权限与用途 |
|---|---|---|
| GET | `/api/v1/map/config` | 登录后获取提供商、JS API 配置、瓦片地址、署名与搜索/路线开关，不返回后端 Web 服务 Key |
| GET | `/api/v1/map/search?keywords=人民广场&city=上海` | 登录后搜索地点；关键词 2–100 字，城市可选、最多 50 字；返回 GCJ02 地点（数据源可为高德或本地 PostGIS） |
| POST | `/api/v1/map/riding` | 登录后规划骑行/电动车/驾车路线；输入/输出均为明确标识的 GCJ02 |
| GET | `/api/v1/map/merchants?limit=100&offset=0` | 登录后分页获取营业中商家的地图投影，不返回账号或后台字段 |
| POST | `/api/v1/riders/me/location` | 骑手上报 `latitude`、`longitude`、`accuracy`、ISO8601 `observed_at` |
| GET | `/api/v1/tasks/{taskId}/location` | 活动任务的创建用户、关联骑手或管理员获取当前位置快照 |
| GET | `/api/v1/tasks/{taskId}/history?after=0&limit=100&display=true` | 该任务创建用户、关联骑手、管理员查看骑手历史；任务结束后仍可查询 |
| GET / WebSocket | `/api/v1/tasks/{taskId}/location/stream` | 同一任务权限下订阅位置更新 |
| GET | `/api/v1/platform/rider-locations?latitude=31.2&longitude=121.4&radius_meters=5000&limit=50` | 仅管理员查询附近近期骑手；半径最大 50000 米，最多 100 条 |

任务创建/更新可附带 `pickup_location` 和 `destination_location`，格式为 `{"latitude":31.2,"longitude":121.4}`，不填写时为无坐标任务。任务和骑手位置均为 WGS84，不能把高德 GCJ02 原样存入这些字段。

骑行请求示例：`{"coordinate_system":"GCJ02","origin":{"latitude":31.23,"longitude":121.47},"destination":{"latitude":31.24,"longitude":121.48}}`。响应包含 `provider`（`amap` 或 `local`）、`coordinate_system=GCJ02`、`mode=bicycling`、`coordinates` 点数组、`distance_meters`、`duration_seconds` 和 `instructions`。搜索响应包含 `provider`、`coordinate_system` 与 `data` 地点数组。未配置服务返回 503；数据源连接、权限、配额或路线失败返回 502；参数错误返回 400。

位置上报最短间隔 10 秒，超频返回 429 和 `Retry-After`；旧定位返回 409；Redis 故障返回 503。`task_id` 只是实时标注，不再用于解析历史归属；即使任务已完成，带旧 `task_id` 的上报仍会更新骑手快照和待归档轨迹，不会返回 404。当前位置快照和待归档轨迹先原子写入 Redis，RabbitMQ 归档器再异步写入 MySQL；上报成功不代表 MySQL 已经落库。

历史接口返回 `data` 原始 WGS84 记录、`coordinate_system=WGS84`、`next_cursor`、`has_more`，每页 1–500 点，默认 100；非法游标/页大小返回 400。每点有 `id`、`task_id`、`rider_id`、经纬度、`accuracy`、`observed_at`、`updated_at` 和 `version`。服务端从 `task_events` 的 `CLAIMED/COMPLETED/CANCELLED` 推导任务时间窗，再合并查询 MySQL 归档和 Redis 待归档点；`task_id` 不决定归属。历史读取继续遵循任务权限，其他用户和无关骑手不能读取；任务结束后仍可查询，MySQL 归档当前永久保留，Redis 待归档成员在对应点提交成功后删除。

`display=true` 额外返回 `display`，包含 `segments`、`rejected_points`、`gaps`、`notice`、`attribution`；各段 `coordinates` 为 WGS84，`mode=road_matched` 是道路匹配结果并带置信度，`mode=observed` 是降级定位连线，应以虚线区分；段间不强行连线。匹配失败不影响原始记录查询；MySQL 或 Redis 任一路读取失败时降级到另一路，两路都不可用返回 503。

WebSocket 在握手 subprotocol 中使用 `cityflow` 和 `bearer.<JWT>`，保留同源检查；消息形如 `{"type":"rider.location_changed","task_id":"...","position":{"rider_id":"...","latitude":31.2,"longitude":121.4,"version":123,"observed_at":"...","updated_at":"...","accuracy":10}}`。快照过期时发送 `rider.location_unavailable`。任务结束、令牌过期或权限失效时连接关闭。

实现细节和当前限制见 [地图与位置架构](../architecture/map-tracking.md)。

## 评价匿名与审计（2026-11-19）

| 方法 | 路径 | 权限与用途 |
|---|---|---|
| `GET` | `/api/v1/places/{source}/{id}` | 公开；评价只返回 `author_label`、评分、内容、日期、点赞、`mine`，不含 `user_id`、`task_id`、精确时间 |
| `POST` | `/api/v1/places/{source}/{id}/reviews` | 登录用户；body 增加 `anonymous`，创建后不可改 |
| `GET` | `/api/v1/merchants/me/reviews?limit=20&offset=0` | MERCHANT；只看标签、评分、内容、日期、点赞，不含身份/任务 |
| `GET` | `/api/v1/platform/reviews?source=&place_id=&limit=20&offset=0` | ADMIN；默认脱敏列表，含 `user_id_masked`、`task_id`、精确时间 |
| `POST` | `/api/v1/platform/reviews/{reviewId}/reveal` | ADMIN；body `{"reason":"至少 10 个字"}`；写审计后返回 `user{id,name,phone}` |

匿名评价每条生成独立随机标签 `匿名用户·xxxxxx`；实名评价生成稳定伪名 `用户·xxxxxx`。骑手公开主页的评价和打赏强制显示“匿名用户”，不返回 `user_id`、`task_id`、骑手 ID 或精确时间。`review_identity_audit` 记录每次 reveal，`review_id` 不设外键，删除评价不删除审计。完整规则见 [评价匿名与身份可见性](../architecture/review-anonymity.md)。

## 支付与骑手结算（2026-11-14）

任务是先付款后接单：带 `quoted_amount_fen` 的任务在支付成功前不能抢单；没有报价的旧任务不受影响。金额只由服务端任务报价决定，客户端不能传价格。

| 方法 | 路径 | 权限与用途 |
|---|---|---|
| `POST` | `/api/v1/tasks/{taskId}/payment` | USER/ADMIN；创建或复用支付单。body `{"provider":"MOCK","idempotency_key":""}`，`provider` 省略时用配置的默认渠道 |
| `GET` | `/api/v1/tasks/{taskId}/payment` | 任务创建者、接单骑手、管理员；返回 `payment` 与 `settlement` |
| `POST` | `/api/v1/payments/{paymentId}/mock-pay` | USER/ADMIN；模拟渠道回调，条件：`ALLOW_MOCK_PAY=true`、支付单渠道为 `MOCK`、付款人匹配 |
| `POST` | `/api/v1/payments/{paymentId}/sync` | USER/ADMIN；主动向渠道查单，金额一致且已支付才落 `PAID`，渠道关闭则关本地单，渠道已退款则落 `REFUNDED` |
| `POST` | `/api/v1/payments/{paymentId}/refund` | USER/ADMIN；body `{"reason":""}`；结算 `PROCESSING`/`PAID`/`UNKNOWN` 时 409，结算 `PENDING` 时先取消结算再退款 |
| `GET` | `/api/v1/riders/me/settlements?limit=50&offset=0` | RIDER；自己的结算单列表 |
| `GET` | `/api/v1/riders/me/payout-account` | RIDER；未设置时返回空对象 |
| `PUT` | `/api/v1/riders/me/payout-account` | RIDER；body `{"provider":"ALIPAY","account":"","account_name":""}`，`provider` 可为 `ALIPAY`/`WECHAT`/`BANK` |
| `POST` | `/api/v1/payment-notify/{provider}` | 公开；渠道异步回调，`provider` 为 `alipay`/`wechat`/`mock`，handler 内部统一转大写 |

创建支付响应（201）：

```json
{
  "payment": {
    "id": "...", "task_id": "...", "payer_id": "...", "amount_fen": 1000, "currency": "CNY",
    "status": "PENDING", "provider": "MOCK", "provider_order_no": "CF...",
    "pay_url": "cityflow-mock://pay/CF...", "expires_at": "2026-11-14T10:30:00Z"
  },
  "qr_code": "cityflow-mock://pay/CF...",
  "pay_url": "cityflow-mock://pay/CF...",
  "expires_at": "2026-11-14T10:30:00Z",
  "already_paid": false
}
```

说明：

- 同一任务同时只有一个活跃支付单；同渠道、同金额、未过期时复用二维码，金额/渠道变化或过期时先调渠道关单、成功后再新建；已支付返回 `already_paid=true`；`REFUND_FAILED` 状态直接返回 `409`，避免二次付款。
- 真实渠道未配置凭证时返回 `503`，不会伪造成功；MOCK 不需要任何凭证。
- 支付宝回调应答为纯文本 `success`，微信为 `{"code":"SUCCESS","message":"成功"}`；验签失败或金额不一致返回失败应答并保留支付单状态，渠道会重试。微信回调同时识别收款、退款和转账通知。
- 支付状态包含 `PENDING/PAID/FAILED/CLOSED/REFUND_PENDING/REFUND_FAILED/REFUNDED`；退款渠道处理中会停在 `REFUND_PENDING`，由后台 worker 查询/重提，不会永久卡住。
- 任务 `COMPLETED` 后自动创建结算单（幂等键 `task:<taskId>`），worker 按配置间隔打款；平台费写入结算单 `fee_fen`，骑手实收为 `amount_fen`。结算状态为 `PENDING/PROCESSING/PAID/FAILED/UNKNOWN`；`UNKNOWN` 表示打款结果无法确认，不会再自动重试，也不会允许退款。有报价但未支付的任务接单返回 `409`，已产生支付单后改价返回 `409`。

完整链路、状态机、幂等键与真实渠道限额见 [支付与骑手结算](../architecture/payment.md)。



