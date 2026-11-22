-- Coupons moved to the Promotion service. The Payment database kept the old
-- coupon tables after the split; Payment now reaches Promotion through the
-- CouponGate RPC and never reads these tables. Drop the child table first so
-- the old foreign key cannot block the drop.
DROP TABLE IF EXISTS coupon_claims;
DROP TABLE IF EXISTS coupons;
