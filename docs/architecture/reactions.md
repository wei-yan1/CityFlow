# 点赞与收藏

更新日期：2026-11-21。

## 服务边界

`reaction` 是独立微服务（`app/reaction`，gRPC `8097`），是点赞/收藏状态与计数的唯一写入方。它不使用 MySQL，全部热状态在 Redis：

| 目标类型 | 正文归属 | 计数投影 |
|---|---|---|
| `PLACE` | Discovery | 无本地列，计数由 Reaction 实时返回 |
| `PLACE_REVIEW` | Discovery | `place_reviews.like_count` |
| `PLACE_COMMENT` | Discovery | `place_review_comments.like_count` |
| `MERCHANT` | Discovery | 不走 Reaction；Discovery 用 `merchant_like` 月度事实、Redis 热榜和 `merchant_hot_snapshot` 维护 |
| `RIDER_REVIEW` | Task | `rider_feedback.like_count` |
| `RIDER_COMMENT` | Task | `rider_feedback_comments.like_count` |

Gateway 的 `reactions.go` 对内容目标只调用 Reaction zRPC；`MERCHANT` 点赞和热榜由 Gateway 直接调用 Discovery 的专用 RPC，不再进入通用 Reaction。内容服务不再接收点赞写请求，也不再保留 `reaction_records` 表。

## Redis 键

- 点赞用户集合：`likes:set:biz:{bizId}`，member 是 `userId`。
- 收藏用户集合：`likes:set:fav:{bizId}`。
- 待投影计数：`likes:times:type:{TYPE}`，member 是 `bizId`，score 是当前**绝对点赞数**。
- 用户索引：`likes:user:{userID}:{kind}:{targetType}`，用于“我的点赞/收藏”分页。

同一 `bizId` 在 ZSET 中只有一条；重复点赞只把 score 改成最新总数，不累加。

## 写入与读取

点赞：`SADD likes:set:biz:{bizId} userId`；取消：`SREM`。写入用 Lua 原子完成集合变更、`SCARD` 取当前总数、`ZADD likes:times:type:{TYPE} bizId 总数`，并同步维护用户索引。取消到 0 也要 `ZADD score=0`，让投影把旧计数归零。

状态查询走 pipeline：`SCARD likes:set:biz:{id}` 取当前计数、`SISMEMBER` 取当前用户是否点赞。列表接口按目标 ID 批量查询，避免逐条 RTT。

## 定时批量投影

Reaction 的 `RunProjector` 默认每 20 秒执行一次，对每种投影类型：

```text
ZPOPMIN likes:times:type:{TYPE} 30
```

取走即删除，拼成一批消息：

```json
[{"bizId":"101","likedTimes":12},{"bizId":"205","likedTimes":3}]
```

发到 exchange `like.record.topic`，routing key 为 `{TYPE}.times.changed`。消息体只有绝对计数，消费方直接覆盖，不做加减。发布失败时 requeue 尚未确认的条目，避免丢投影。

Discovery 消费 `PLACE_REVIEW.times.changed`、`PLACE_COMMENT.times.changed`；Task 消费 `RIDER_REVIEW.times.changed`、`RIDER_COMMENT.times.changed`。消费端按数组批量 `UPDATE ... SET like_count=? WHERE id=?`，重复投递幂等。

## 接口

```text
POST /api/v1/reactions
GET  /api/v1/reactions/state?target_type=&kind=&target_ids=
GET  /api/v1/reactions/mine?target_type=&kind=&cursor=&limit=

POST /api/v1/merchants/{merchant_id}/likes   # Discovery 专用
GET  /api/v1/merchants/hot?category=&limit=   # Discovery 专用
```

通用 Reaction 只接受 `PLACE`、`PLACE_REVIEW`、`PLACE_COMMENT`、`RIDER_REVIEW`、`RIDER_COMMENT`；传 `MERCHANT` 会返回 `invalid target_type`。商家点赞由 Discovery 校验商家存在性并写月度 `merchant_like`，同一用户同一商家同一自然月只保留一条。商家资源的写入口是 `POST /api/v1/merchants/{merchant_id}/likes`，请求体 `{"active":true|false}`，返回 `active/count/month`；当前用户批量状态仍通过 `GET /api/v1/reactions/state?target_type=MERCHANT&kind=LIKE&target_ids=...` 读取，Gateway 只做目标映射。

请求体：

```json
{"target_type":"PLACE_REVIEW","target_id":"...","kind":"LIKE","active":true}
```

身份从 Bearer JWT 读取，不接受请求体里的 `user_id`。`SetReaction`、`ListMine` 必须登录；`GetReactionState` 允许匿名，匿名时 `liked=false`。

## 不变量

1. 点赞状态和计数只写 Reaction Redis；内容服务只写 `like_count` 投影。
2. MQ 消息是绝对计数，消费端覆盖而非累加；同一批可重复投递。
3. 内容列表展示：有本地投影的类型读 `like_count`，Reaction 只覆盖当前用户的 `liked`；`PLACE` 直接读 Reaction 计数，`MERCHANT` 直接读 Discovery 热榜/月度事实。
4. 目标存在性与可见性由正文归属服务校验；Reaction 只校验类型、ID 长度和 kind。
5. Redis 清空会丢失点赞状态，这是当前设计的既定边界；内容表里的 `like_count` 只作为展示投影，不作为恢复源。
