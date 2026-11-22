-- The monthly hot list reads one highest-liked review per merchant. The old
-- place index orders by created_at, which forces a filesort for every card.
-- Keep the public ranking query on a single covering range scan instead.
SET @has_merchant_hot_review_index = (
    SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'place_reviews'
      AND INDEX_NAME = 'place_reviews_merchant_hot'
);
SET @add_merchant_hot_review_index = IF(@has_merchant_hot_review_index = 0,
    'ALTER TABLE place_reviews ADD KEY place_reviews_merchant_hot (place_source, place_id, like_count DESC, created_at DESC, id DESC)',
    'SET @cityflow_noop = 1');
PREPARE cityflow_add_merchant_hot_review_index FROM @add_merchant_hot_review_index;
EXECUTE cityflow_add_merchant_hot_review_index;
DEALLOCATE PREPARE cityflow_add_merchant_hot_review_index;
