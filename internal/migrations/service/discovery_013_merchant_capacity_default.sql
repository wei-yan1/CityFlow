-- 商家每小时容量必须为正数；历史数据里误存 0 的记录按最小可服务容量 1 修正。
ALTER TABLE merchant_profiles
    MODIFY COLUMN capacity_per_hour INT UNSIGNED NOT NULL DEFAULT 1;

UPDATE merchant_profiles
SET capacity_per_hour = 1
WHERE capacity_per_hour < 1;
