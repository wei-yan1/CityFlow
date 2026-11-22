-- Seed shared redeem codes for the three demo coupons. They are real outputs
-- of the 10-character codec, not hand-written strings:
--   WELCOME5 -> 1KSM000001
--   RIDE20   -> 484G000002
--   CAMPUS3  -> 81GW000003
INSERT IGNORE INTO coupon_redeem_code
    (id, coupon_id, batch_no, code, seq, max_redeem, used_count, status, created_by, note, starts_at, ends_at)
VALUES
    ('00000000-0000-4000-8000-000000000201','00000000-0000-4000-8000-000000000101','CAMPUS-2026','1KSM000001',1,200,0,'ACTIVE','system','新客券','2026-01-01 00:00:00','2027-12-31 23:59:59'),
    ('00000000-0000-4000-8000-000000000202','00000000-0000-4000-8000-000000000102','CAMPUS-2026','484G000002',2,100,0,'ACTIVE','system','折扣券','2026-01-01 00:00:00','2027-12-31 23:59:59'),
    ('00000000-0000-4000-8000-000000000203','00000000-0000-4000-8000-000000000103','CAMPUS-2026','81GW000003',3,300,0,'ACTIVE','system','校园券','2026-01-01 00:00:00','2027-12-31 23:59:59');

UPDATE promotion_redeem_code_sequence
SET next_value = GREATEST(next_value, 4)
WHERE name = 'coupon_redeem_code';
