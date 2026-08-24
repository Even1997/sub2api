ALTER TABLE reward_draw_components
    ADD COLUMN IF NOT EXISTS redemption_id BIGINT REFERENCES reward_draw_redemptions(id),
    ADD COLUMN IF NOT EXISTS redeemed_at TIMESTAMPTZ;

DROP INDEX IF EXISTS idx_reward_draw_components_user_available;
CREATE INDEX idx_reward_draw_components_user_available
    ON reward_draw_components(user_id, pool_id, created_at DESC, id DESC)
    WHERE combined_at IS NULL AND redeemed_at IS NULL;

ALTER TABLE reward_draw_redemptions
    ADD COLUMN IF NOT EXISTS component_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE reward_draw_redemptions
    DROP CONSTRAINT IF EXISTS reward_draw_redemptions_card_count_check;
ALTER TABLE reward_draw_redemptions
    ADD CONSTRAINT reward_draw_redemptions_card_count_nonnegative CHECK (card_count >= 0),
    ADD CONSTRAINT reward_draw_redemptions_component_count_nonnegative CHECK (component_count >= 0),
    ADD CONSTRAINT reward_draw_redemptions_item_count_positive CHECK (card_count + component_count > 0);
