# 支付与骑手结算

> 状态：已实现（2026-11-14）。当前支付与结算代码分布在 Gateway、Payment、Task 和 Identity 进程；历史脚手架文档已删除。

## 1. 钱怎么走

CityFlow 不自建资金池，也不让平台账户替用户保管货款。链路是：

1. 用户在小程序/H5 内向**平台自己的支付宝或微信商户号**付款；
2. 付款成功后系统只记账，不立即动这笔钱；
3. 骑手接单、履约；
4. 任务 `COMPLETED` 后，平台从商户号余额中向骑手的支付宝账号或微信零钱发起一笔**转账/佣金**；
5. 转账成功，结算单落 `PAID`。

合规定性要先说清楚：

- **平台自营**（平台与用户成立配送服务合同，骑手是平台一方或劳务提供者）：平台收款后再向骑手支付报酬，属于代付/结算，是当前代码假设的模式。
- **纯撮合平台代收再转给第三方骑手**：在没有支付业务许可的情况下，容易被认定为“二清”（二次清算）。正式上线前应按实际业务形态做合规评估，或改为持牌机构的分账产品。

## 2. 代码分层

| 位置 | 职责 |
|---|---|
| `app/payment/domain/payment/model.go` | 支付单/结算/收款账户/回调流水模型，状态常量与状态机，`SplitAmount` |
| `app/payment/domain/payment/gateway.go` | `Gateway` 端口：`Precreate / Query / ParseNotify / Transfer / Refund`；另有可选的 `Closer / RefundQuerier / PayoutQuerier` 能力接口 |
| `app/payment/domain/payment/id.go` | UUID、`CF`/`ST` 单号（Crockford Base32，26 字符，满足微信 ≤32） |
| `app/payment/application/payment/payment_service.go` | 支付用例、接单/改价闸门、取消退款、支付/退款/结算三类 worker |
| `app/payment/application/payment/task_view.go` | 消费方本地任务视图与 Task 状态常量，不 import Task domain |
| `app/payment/repository/payment.go` | 支付仓储接口（`type Page = pagination.Page`） |
| `app/payment/internal/adapter/gateway/mock.go` | 无凭证演示通道 |
| `app/payment/internal/adapter/gateway/alipay.go` | 支付宝 openapi v2：预下单、查单、关单、异步通知验签、退款/退款查询、单笔转账/转账查询 |
| `app/payment/internal/adapter/gateway/wechat.go` | 微信支付 API v3：Native 下单、查单、关单、回调验签 + AES-256-GCM、退款/退款查询、商家转账/转账查询 |
| `app/payment/internal/adapter/mysql/*.go` | Payment 自有 MySQL 仓储；状态迁移全部 CAS |
| `app/gateway/cmd/api/internal/handler/payments.go` | 支付/结算/收款账户 REST 适配；只做协议转换与错误映射 |
| `app/payment/cmd/rpc/internal/logic/*.go` | 支付、退款、结算、回调 RPC logic；优惠券 RPC 已迁至 Promotion |
| `internal/migrations/service/payment_001_payment.sql` | 支付单、结算、回调、提现账户表 |
| `internal/migrations/service/payment_002_settlement_fee.sql` | 结算平台费字段 |
| `internal/migrations/service/payment_003_refund_failed.sql` | `REFUND_FAILED` 状态约束 |
| `internal/migrations/service/payment_004..007*.sql` | inbox、inbox recovery、保留策略、对账索引 |

## 3. 端到端流程（对应代码）

### 3.1 创建支付

`POST /api/v1/tasks/{taskId}/payment`

1. `CreateCharge` 先取任务，校验 `CreatedBy == payer`、任务为 `OPEN`、`quoted_amount_fen > 0`；
2. **金额只从服务端任务报价取**，客户端没有任何金额入参；
3. `existingCharge` 保证一个任务同时只有一个活跃支付单：
   - 已有 `PAID` / `REFUND_PENDING`：直接返回 `already_paid=true`；
   - 已有 `REFUND_FAILED`：直接拒绝，避免用户为同一任务重复付款；
   - 已有同渠道、同金额、未过期的 `PENDING`：复用原二维码；
   - 金额变了、渠道变了或已过期：先调渠道 `Close` 关单，成功后再关本地单并新建；
   - 渠道关单失败时不新建二维码，避免旧码仍可支付、新旧两笔同时被扫；
4. `newOrder` 生成 `CF...` 商户单号，金额/渠道/有效期落库；
   - MySQL `CreatePaymentOrder` 依赖 `payment_orders.active_task_id` 生成列与唯一索引，在 Payment 本地库内保证一任务一活跃单；不再通过 `SELECT ... FOR UPDATE` 锁 Task 表；
   - 幂等键命中但任务、金额、付款人或渠道不一致时返回 `409`，不会把别人的支付单交给调用方；
5. 调 `gateway.Precreate`，把二维码内容写入 `pay_payload`、`pay_url`、`expires_at`；
6. 返回 `ChargeResult{payment, qr_code, pay_url, expires_at, already_paid}`。

真实渠道凭证缺失时，`Configured()` 为 false，接口返回 `503`（`payment.ErrNotConfigured`），不会伪造成功。

### 3.2 支付结果进入系统

两条路，最终都落到同一状态机：

- **异步回调**：`POST /api/v1/payment-notify/{provider}`（无鉴权，engine 级路由）。
  - 支付宝：表单参数 RSA2 验签；返回纯文本 `success`。
  - 微信：`Wechatpay-*` 头验签 + `AEAD_AES_256_GCM` 解密；按 `out_bill_no` / `out_refund_no` / `out_trade_no` 分别识别转账、退款、收款通知；返回 `{"code":"SUCCESS"}`。
  - 验签失败或金额不一致：支付单不变，返回失败 ack，渠道会重试。
- **主动查单**：`POST /api/v1/payments/{id}/sync`，调 `Query`；`PAID` 落库、`CLOSED` 关闭、`REFUNDED` 落退款。用于用户手动刷新、回调丢失补偿和对账。
- **定时对账**：worker 每 4 个 tick 调 `ReconcileOnce`，补偿过期未关的 `PENDING` 和长期 `REFUND_PENDING`（见 3.6）。

处理回调时：

1. 网关验签/解密后才产出 `Notification`；
2. service 校验 `Notification.AmountFen == PaymentOrder.AmountFen`，且单号能对应到本地支付单；
3. `MarkPaymentPaid` 用 `WHERE status='PENDING'` CAS；重复回调返回既有记录；
4. 退款通知走 `MarkPaymentRefunded` / `MarkPaymentRefundFailed`，同样只做状态迁移；
5. 写 `payment_notifies` 流水并累加 `notify_count`；
6. 重复回调不重复加钱。

### 3.3 接单闸门

`TaskService.Claim` 在仓储 CAS 之前先读任务并调 `PaymentGate.EnsureClaimable`：

- 任务有报价但支付单不是 `PAID` → `409 请先完成支付后再接单`；
- 无报价的任务不要求支付，按普通任务处理；
- 已支付任务正常抢单。

### 3.4 改价闸门

`TaskService.Update` 调 `EnsureEditable`：

- 已有 `PENDING` 或 `PAID` 支付单时，`quoted_amount_fen` 不允许变化 → `409`；
- 未支付时可以改价；改价后旧二维码会在下一次创建支付时被关闭并重建。

### 3.5 完成与结算

`TaskService.UpdateStatus` 把任务迁到 `COMPLETED` 后，同步调 `SettlementTrigger.EnsureSettlement`：

1. 任务必须有 `quoted_amount_fen`、`worker_id`；
2. 必须有 `PAID` 支付单；
3. 幂等键固定为 `task:<taskID>`，重复调用返回同一结算单；
4. `SplitAmount(amount, feeBPS)`：平台费 = `amount × feeBPS / 10000`，默认 `0`，骑手拿全额；
   - 平台费写入 `rider_settlements.fee_fen`，骑手实收写入 `amount_fen`，两者相加等于任务结算金额；
5. 结算金额 ≤ 0 不建单。

结算落库为 `PENDING`，由 worker 异步执行。

### 3.6 Worker 打款

`PaymentService.RunWorker` 每 `CITYFLOW_PAYMENT_SETTLE_INTERVAL_SECONDS`（默认 30 秒）执行一次 `SettleOnce`：

1. `ClaimDueSettlements(now, 10, 2m)` 抢一批到期结算单；
   - MySQL 用 `FOR UPDATE SKIP LOCKED`，多实例不重复打款；
   - 抢占后状态变 `PROCESSING` 并加 2 分钟租约；
2. 读取骑手收款账户；账户缺失或渠道不一致则退避重试；
   - **MOCK 渠道例外**：演示通道不需要收款账户；
3. 第二次及以后尝试先调 `QueryPayout`：渠道已成功则直接落 `PAID`，仍在处理则保持当前租约、不重复转账；
4. 调 `gateway.Transfer`；
5. `SUCCESS` → `MarkSettlementPaid`（写 `provider_payout_id` 与 `paid_at`）；
6. `FAILED` 或渠道仍在处理 → 指数退避 `1/2/4/...` 分钟，最多 8 次；8 次后如果最后一次打款可能已发出（超时/处理中），**不能直接标 FAILED**：
   - 先调 `QueryPayout`：确认成功则落 `PAID`，确认失败才落 `FAILED`；
   - 查询报错或渠道仍在处理 → 落 `UNKNOWN`，停止自动重试，等人工核对；
   - 只有“未发起过转账”的失败（如骑手没填收款账户）才直接终态 `FAILED`；
7. 每次重试用同一个 `out_bill_no`，渠道侧天然幂等。

worker 每 4 个 tick 还会调一次 `ReconcileOnce`：

- 扫 1 分钟前创建、仍是 `PENDING` 的支付单，主动查单；渠道已支付则落 `PAID`（校验金额），已关闭则关本地单；
- 已过期且渠道仍是 `PENDING`：先调渠道 `Close`，成功后再关本地单；失败则保留 `PENDING` 等下一轮，绝不在旧码仍可支付时新建二维码；
- 扫 `REFUND_PENDING` 超过 1 分钟的退款单：先 `QueryRefund`，查不到再以同一退款号重提；`SUCCESS` 落 `REFUNDED`，渠道明确失败落 `REFUND_FAILED`。

worker 每 20 个 tick 还会扫一遍 `COMPLETED` 任务，补建崩溃期间漏掉的结算单。

多实例部署不要在每个 Payment 进程都跑这段对账。启用 XXL-JOB 的 `cityflow-payment` 执行器后，进程会强制关闭本地 `RunReconciliationWorker`，由 `payment.reconcile` 定时触发同一套 `ReconcileOnce + reconcile`；`payment.inbox.purge` 和 `payment.notify.purge` 是独立的保留清理 handler，不再和本地对账 worker 绑在一起。结算打款和 inbox recovery 仍保留每实例运行，因为它们用 `SKIP LOCKED` + 租约并发领取。

### 3.7 取消退款

`TaskService.Cancel` 成功把任务置为 `CANCELLED` 并在同一事务写 `task_outbox` 的 `task.cancelled.v1` 后：

- Payment 消费事件并调用 `RefundCancelledTask`；任务创建者作为退款申请人，`cancel_reason` 写入 `payment_orders.refund_reason`；
- 只有 `PAID` / `REFUND_PENDING` / `REFUND_FAILED` 可退；
- 结算已 `PROCESSING` / `PAID` / `UNKNOWN` 时拒绝退款（`409 骑手结算已开始或状态待核对`）；
- 结算还是 `PENDING` 时，先用 CAS 把结算单改成 `FAILED`（取消打款），再发起退款；如果 worker 已抢到该结算单，CAS 失败，退款被拒绝；
- 退款号固定为 `RF-<支付单号>`，重试幂等；
- 渠道返回 `PROCESSING` 时支付单停在 `REFUND_PENDING`，由 worker 查询/重提；渠道明确失败落 `REFUND_FAILED`，不会静默当作成功；
- 慢渠道不会拖住用户的取消请求。

## 4. 状态机

### 支付单

```text
PENDING ──PAID──> PAID ──> REFUND_PENDING ──> REFUNDED
   │                              │
   ├──> FAILED                    └──> REFUND_FAILED ──> REFUND_PENDING（重试）
   └──> CLOSED                          │
                                         └──> REFUNDED
```

`PAID` 不能再 `CLOSED`；`FAILED` 不能复活；`REFUND_FAILED` 只能重试退款或直接补记为 `REFUNDED`；仓储层每次迁移都带 `WHERE status=...`。

### 结算单

```text
PENDING ──> PROCESSING ──> PAID
                │
                ├──> PENDING（退避重试）
                ├──> FAILED（渠道确认失败/未发起转账）
                └──> UNKNOWN（结果无法确认，需人工核对）
FAILED ──> PENDING（补偿重试）
```

`PAID` 不可逆；`UNKNOWN` 不会被 worker 自动领取，也不能退款，必须人工核对后改成 `PAID` 或 `FAILED`。

## 5. 幂等与唯一约束

| 对象 | 键 |
|---|---|
| 商户订单号 | `CF` + 24 位 Crockford Base32 |
| 结算单号 | `ST` + 24 位 |
| 退款号 | `RF-<支付单号>` |
| 支付幂等 | 唯一 `(payer_id, idempotency_key)`、唯一 `(provider, provider_order_no)` |
| 活跃支付单 | 同一 `task_id` 只允许一个 `PENDING`；MySQL 在 `CreatePaymentOrder` 事务内锁 `tasks` 行后检查 |
| 支付流水号 | 唯一 `(provider, provider_trade_id)` |
| 结算幂等 | 唯一 `task_id`、唯一 `idempotency_key`、唯一 `(provider, provider_bill_no)` |
| 打款流水号 | 唯一 `(provider, provider_payout_id)` |

## 6. 无凭证演示（MOCK）

默认 `CITYFLOW_PAYMENT_PROVIDER=MOCK`，无需任何商户信息即可跑完整条链路：

```powershell
# 1) 用户登录，创建带报价的任务
$user = (Invoke-RestMethod -Method Post http://localhost:8087/api/v1/auth/login -ContentType 'application/json' -Body '{"email":"user@cityflow.local","password":"cityflow123"}').access_token
$task = Invoke-RestMethod -Method Post http://localhost:8087/api/v1/tasks -Headers @{Authorization="Bearer $user"} -ContentType 'application/json' -Body '{"title":"代取快递","type":"DELIVERY","pickup":"快递点","destination":"宿舍","quoted_amount_fen":1000}'

# 2) 创建支付，拿二维码内容
$charge = Invoke-RestMethod -Method Post "http://localhost:8087/api/v1/tasks/$($task.id)/payment" -Headers @{Authorization="Bearer $user"} -ContentType 'application/json' -Body '{"provider":"MOCK"}'

# 3) 骑手此时接单会被拒（409）
$rider = (Invoke-RestMethod -Method Post http://localhost:8087/api/v1/auth/login -ContentType 'application/json' -Body '{"email":"rider@cityflow.local","password":"cityflow123"}').access_token
Invoke-RestMethod -Method Post "http://localhost:8087/api/v1/tasks/$($task.id)/claim" -Headers @{Authorization="Bearer $rider"}

# 4) 模拟支付成功（与真实回调走同一状态机）
Invoke-RestMethod -Method Post "http://localhost:8087/api/v1/payments/$($charge.payment.id)/mock-pay" -Headers @{Authorization="Bearer $user"}

# 5) 接单并推进到完成
Invoke-RestMethod -Method Post "http://localhost:8087/api/v1/tasks/$($task.id)/claim" -Headers @{Authorization="Bearer $rider"}
foreach ($s in 'ARRIVED','PROCESSING','COMPLETED') {
  Invoke-RestMethod -Method Post "http://localhost:8087/api/v1/tasks/$($task.id)/status" -Headers @{Authorization="Bearer $rider"} -ContentType 'application/json' -Body (ConvertTo-Json @{status=$s})
}

# 6) 查看结算（worker 默认 30 秒内打款；演示可把间隔调成 5 秒）
Invoke-RestMethod "http://localhost:8087/api/v1/tasks/$($task.id)/payment" -Headers @{Authorization="Bearer $user"}
Invoke-RestMethod "http://localhost:8087/api/v1/riders/me/settlements" -Headers @{Authorization="Bearer $rider"}
```

MOCK 的二维码是 `cityflow-mock://pay/<orderNo>`，前端可直接把它渲染成二维码，也可以只展示“模拟支付”按钮。

当前 Web 端已接入支付单生成、支付串复制、MOCK 模拟支付和“查询支付状态”；二维码图片组件、骑手收款账户表单仍未接入。

## 7. 配置

| 变量 | 默认 | 说明 |
|---|---|---|
| `CITYFLOW_PAYMENT_PROVIDER` | `MOCK` | `MOCK` / `ALIPAY` / `WECHAT`，仅决定未指定 provider 时的默认渠道 |
| `CITYFLOW_PAYMENT_FEE_BPS` | `0` | 平台费率，万分比；`0` 表示骑手拿全额 |
| `CITYFLOW_PAYMENT_EXPIRE_MINUTES` | `30` | 支付单有效期 |
| `CITYFLOW_PAYMENT_ALLOW_MOCK_PAY` | `true` | 是否开放 `/mock-pay`；生产务必设为 `false` |
| `CITYFLOW_PAYMENT_NOTIFY_BASE_URL` | 空 | 用它拼 `<base>/api/v1/payment-notify/alipay|wechat` |
| `CITYFLOW_PAYMENT_SETTLE_INTERVAL_SECONDS` | `30` | 结算 worker 轮询间隔 |
| `ALIPAY_APP_ID` / `ALIPAY_PRIVATE_KEY(_FILE)` / `ALIPAY_PUBLIC_KEY(_FILE)` | 空 | 支付宝应用凭证；`_FILE` 优先 |
| `ALIPAY_GATEWAY_URL` | 沙箱 | 默认 `https://openapi-sandbox.dl.alipaydev.com/gateway.do` |
| `ALIPAY_NOTIFY_URL` | 空 | 不填则用 notify base 推导 |
| `WECHAT_PAY_MCH_ID` / `_APP_ID` / `_SERIAL_NO` / `_API_V3_KEY` | 空 | 微信商户与 API v3 凭证 |
| `WECHAT_PAY_PRIVATE_KEY_FILE` / `_PLATFORM_PUBLIC_KEY_FILE` | 空 | 商户私钥、微信平台公钥 |
| `WECHAT_PAY_TRANSFER_SCENE_ID` | `1000` | 商家转账场景 ID，必须在商户平台确认 |

## 8. 真实渠道要点

### 支付宝

- 收款：`alipay.trade.precreate`（当面付），返回 `qr_code`；
- 查单：`alipay.trade.query`；
- 退款：`alipay.trade.refund`，`out_request_no` 固定；
- 付款给骑手：`alipay.fund.trans.uni.transfer`，`product_code=TRANS_ACCOUNT_NO_PWD`、`biz_scene=DIRECT_TRANSFER`；
- 沙箱地址对转账能力与线上一致，但没有营业执照无法开通正式产品；
- 若要“先冻结、完成后分账”，需签约「商家分账」，并在 `alipay.trade.royalty.relation.bind` 绑定收入方；2020-03-20 后自助签约默认分账上限 30%，个人接收方退款不退分账。当前实现选择“先收款、完成后单笔转账”，不依赖分账协议。

### 微信支付

- 收款：`POST /v3/pay/transactions/native`，返回 `code_url`；
- 回调：API v3 签名 + AES-256-GCM；应答必须返回 `{"code":"SUCCESS"}`；
- 付款给骑手：`POST /v3/fund-app/mch-transfer/transfer-bills`（商家转账），`transfer_scene_id` 使用“佣金报酬”场景；
- 商家转账不支持小微商户和个体工商户；收款人需在微信内确认收款；单笔上限 2 万、单用户单日 2 万、单日额度默认 5 万（可申请提高）；
- 分账产品（`/v3/profitsharing/orders`）默认单笔分账比例上限 30%，同一分账单最多 50 次、每次最多 50 方，分给个人零钱后不能分账回退。当前实现没有用分账，而是用商家转账，因此不受 30% 比例限制，但受转账额度限制。

### 两者共同

- 回调地址必须是公网 HTTPS，且能通过渠道的连通性校验；
- 所有回调都要验签后再信字段，金额必须与本地支付单一致；
- 主动查单是必备兜底，不能只依赖回调；
- 生产环境要关掉 `ALLOW_MOCK_PAY`；当前已实现按单主动查单/关单/退款查询，但**还没有按日拉取渠道账单做逐笔对账**。

## 9. 仍未覆盖（上线前必须补）

以下是这轮没有做的资金安全和合规项，不要因为当前测试全绿就当成已上线：

1. **日终账单对账**：下载支付宝/微信账单并按渠道流水号逐笔匹配，统计长短款；当前只有按单查询，渠道漏单/重复结算发现不了。
2. **总账/复式记账**：现在只有业务单据（支付单、结算单、回调流水）和 `fee_fen`，没有“支付清算户 / 平台收入 / 骑手应付”分户账和日终余额，无法试算平衡。
3. **异常单处理台**：`REFUND_FAILED`、`UNKNOWN` 结算、金额不一致、长期 `PROCESSING`、晚到支付目前只落状态和日志；没有后台列表、人工改成 `PAID/FAILED` 的接口和冲正流程。
4. **晚到支付/重复支付**：已用渠道关单把概率压到很低，但关单与支付竞态仍可能产生“本地已关、渠道已收”；需要异常单 + 自动原路退款或人工退款。
5. **部分退款/多次退款**：当前只支持全额退款，退款金额固定为支付单金额；没有手续费承担、部分退款累计校验。
6. **收款账户合规**：没有实名/四要素校验、渠道限额预检、黑名单/风控；真实转账被渠道拒绝时只能退避重试。
7. **回调防重放**：只验签名和商户号，没有时间窗、事件 ID 去重、IP allowlist 和限流；状态 CAS 能防重复加钱，但挡不住无效请求量。
8. **监控告警**：金额不一致、退款失败、结算长时间 `PROCESSING`、对账差异没有 metrics/告警，只有 `log.Printf`。
9. **生产密钥管理**：商户私钥/APIv3 Key 从环境变量或文件读取，没有 KMS、轮换和最小权限部署说明。

## 10. 骑手注册付费（个人经营码最小实现）

这条链路和任务支付/骑手结算是**两套独立流程**：它不创建 `payment_orders`，不进入 `PaymentService`，也不产生 `rider_settlements`。目的只是用一笔可人工核验的 1 分钱，证明骑手完成注册付费，然后由管理员开通账号。

### 10.1 钱怎么走

1. 骑手在 `/rider/register` 扫描平台所有者本人的**支付宝个人经营收款码**，支付至少 `0.01` 元；
2. 付款说明填写邮箱前缀或手机号后四位，方便管理员在支付宝账单里搜索；
3. 骑手在注册页填写资料，并提交支付宝交易号或付款备注；
4. 管理员在平台后台“骑手注册”视图核对支付宝账单，确认金额和凭证后点“核验并开通”；
5. 通过后 Identity 在自己的事务中创建 `users` 的 RIDER 账号、标记申请已通过，并写 `rider.approved.v1` outbox；Task 消费该事件创建 `rider_profiles` 的 OFFLINE 资料。重复审批不重复创建账号；Task 投影失败可从事件重试，不回滚已开通的账号。驳回只改状态，不创建账号。

**资金直接进入个人经营码，平台不经手、不托管、不自动退款。** 个人经营码没有支付回调，所以不存在“支付成功自动注册”；管理员人工核验就是当前最小实现的安全边界。

### 10.2 代码位置

| 位置 | 职责 |
|---|---|
| `app/identity/domain/rider/registration.go` | `Registration`、`RegistrationRequest`、`PENDING/APPROVED/REJECTED`、最低 1 分钱常量 |
| `app/identity/repository/repository.go` | `RiderRegistrationRepository` 接口 |
| `app/identity/application/riderreg/rider_registration_service.go` | 提交校验、密码哈希、列表、通过/驳回、`rider.approved.v1` outbox |
| `app/gateway/cmd/api/internal/handler/accounts.go` | 公开提交与管理员审核 REST 适配 |
| `app/identity/internal/adapter/mysql/registration_approval.go` | MySQL 审核事务 + `SELECT ... FOR UPDATE`；`registrations.go` 负责提交/列表 |
| `internal/migrations/service/identity_002_rider_registrations.sql` | `rider_registrations` 表、金额/状态/车辆约束、唯一支付凭证 |
| `internal/migrations/service/identity_003_outbox.sql` | Identity outbox |
| `web/rider/register.html` + `web/assets/rider/register.js` | 展示经营码、注册表单、成功提示 |
| `web/assets/platform/platform.js` | 后台“骑手注册”列表、字段渲染、通过/驳回操作 |

### 10.3 提交校验

公开接口 `POST /api/v1/rider-registrations` 不接受金额以外的任何“已支付”标记：

- `email` 必须是可解析邮箱；`password` 至少 8 位；`display_name` 必填；
- `vehicle_type` 只允许 `WALK/BICYCLE/E_BIKE/CAR/OTHER`；
- `skills` 至少一项；`service_area`、`payment_note` 有长度上限；
- `max_distance_meters` 1–100000；`max_concurrent_tasks` 1–20；
- `paid_amount_fen >= 1`，缺失或 0 直接 `400`；
- `payment_reference` 必填且全局唯一；相同凭证不能重复提交；
- pending 状态的邮箱/手机号不可重复；被驳回后可以用新的支付凭证重新申请；
- 密码用 `security.HashPassword` 哈希后落库，`Registration` 的 `password_hash` 字段 `json:"-"`，响应不返回哈希。

### 10.4 审核事务

`ApproveRiderRegistrationAccount` 在 MySQL 事务中：

1. `SELECT ... FOR UPDATE` 锁定该注册记录；
2. 只允许 `PENDING`；已通过/已驳回返回 `409`；
3. 再次确认 `paid_amount_fen >= 1`；
4. 创建 `users`：`role=RIDER`、`status=ACTIVE`、邮箱/手机号来自申请、密码哈希来自申请；
5. 更新注册记录为 `APPROVED`，写 `reviewed_by`、`review_note`、`reviewed_at`、`user_id`；
6. 同事务写 `rider.approved.v1` outbox，携带申请中的车辆/服务类型/区域/距离/并行任务；Task 消费后创建 `rider_profiles`（`status=OFFLINE`）。

账号创建和审批状态在同一事务内；Task 资料由事件投影，投影失败可重试，不回滚已开通账号。不会出现“申请未通过但账号已建”的半成品。


### 10.5 状态机

```text
PENDING ──核验通过──> APPROVED（创建 RIDER 账号 + OFFLINE 资料）
   │
   └──驳回──> REJECTED（不创建账号，可用新凭证重新提交）
```

审核通过后不能再重复通过；已驳回也不能直接改成通过，必须重新提交申请。

### 10.6 已知限制

1. **没有自动回调**：个人经营码不提供支付回调，无法自动确认到账；
2. **核验靠人工**：管理员必须在支付宝账单中按金额 + 交易号/备注比对，系统不替你判断真伪；
3. **不支持自动退款**：重复支付、多付、误付需要所有者在支付宝里人工退款；
4. **没有截图上传**：当前只收文本凭证，避免引入对象存储和图片审核；
5. **没有限流/邮箱验证**：公开接口可被反复调用，个人项目先接受这个成本；
6. 注册页展示的是裁剪后的经营码图（不含个人姓名行）；原图不要直接公开。

### 10.7 测试

```powershell
go test ./app/identity/application/riderreg   # 最低 1 分钱、重复邮箱、通过开通、驳回、outbox
go test ./app/...                             # Gateway 路由与公开提交/审核适配
go test ./app/identity/...                    # Identity MySQL 适配器与审批事务
```

MySQL 集成测试会实际应用 `identity_002_rider_registrations.sql` 和 outbox 迁移，验证账号开通与事件写入，并清理测试数据。

## 11. 测试

```powershell
go test ./...            # 领域、Gateway 签名/解密、application、HTTP 全链路
go vet ./...
go build ./...
```

集成测试需要真实 MySQL 时，通过 `CITYFLOW_TEST_MYSQL_DSN` 指向可丢弃测试库；迁移按 Payment 自己的 `payment_001..007*.sql` 应用，不依赖旧单体迁移。

覆盖点：金额字符串精确转换、支付宝 RSA2 请求签名与响应验签、微信 API v3 请求签名、回调验签 + AES-GCM 解密、微信退款通知识别、金额不一致拒绝、回调重放幂等、接单闸门、取消退款取消 PENDING 结算、结算抢占与打款、打款前回查、打款结果不确定转 `UNKNOWN`、支付超时关单与补单、退款查询/重提、平台费落库、单任务单活跃支付单、MySQL `SKIP LOCKED` 抢占。



