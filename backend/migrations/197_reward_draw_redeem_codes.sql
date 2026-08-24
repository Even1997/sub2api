ALTER TABLE reward_draw_redemptions
    ADD COLUMN IF NOT EXISTS redeem_code_id BIGINT REFERENCES redeem_codes(id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_reward_draw_redemptions_redeem_code
    ON reward_draw_redemptions(redeem_code_id)
    WHERE redeem_code_id IS NOT NULL;
