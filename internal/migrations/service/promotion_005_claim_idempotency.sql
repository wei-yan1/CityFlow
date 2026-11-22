-- 领券幂等：同一用户对同一张券的同一个请求只允许落一条 claim。
-- NULL 表示调用方没有提供幂等键（兼容脚本/直连 RPC），MySQL 唯一索引允许多个 NULL。
ALTER TABLE coupon_claims
    ADD COLUMN idempotency_key VARCHAR(64) NULL AFTER claim_no,
    ADD UNIQUE KEY coupon_claims_idempotency (coupon_id, user_id, idempotency_key);
