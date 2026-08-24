ALTER TABLE reward_draw_batches
    ADD COLUMN IF NOT EXISTS pool_id VARCHAR(16) NOT NULL DEFAULT 'legacy'
        CHECK (pool_id IN ('legacy', 'breed', 'profession', 'treasure'));

CREATE TABLE IF NOT EXISTS reward_draw_components (
    id BIGSERIAL PRIMARY KEY,
    batch_id BIGINT NOT NULL REFERENCES reward_draw_batches(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    position SMALLINT NOT NULL CHECK (position >= 0),
    pool_id VARCHAR(16) NOT NULL CHECK (pool_id IN ('breed', 'profession', 'treasure')),
    tier_id VARCHAR(32) NOT NULL,
    name VARCHAR(64) NOT NULL,
    rarity VARCHAR(16) NOT NULL,
    wool_value BIGINT NOT NULL CHECK (wool_value > 0),
    is_jackpot BOOLEAN NOT NULL DEFAULT FALSE,
    combined_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_reward_draw_component_position UNIQUE (batch_id, position)
);

CREATE INDEX IF NOT EXISTS idx_reward_draw_components_user_available
    ON reward_draw_components(user_id, pool_id, created_at DESC, id DESC)
    WHERE combined_at IS NULL;

ALTER TABLE reward_draw_cards
    ALTER COLUMN batch_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS breed_component_id BIGINT REFERENCES reward_draw_components(id),
    ADD COLUMN IF NOT EXISTS profession_component_id BIGINT REFERENCES reward_draw_components(id),
    ADD COLUMN IF NOT EXISTS treasure_component_id BIGINT REFERENCES reward_draw_components(id),
    ADD COLUMN IF NOT EXISTS is_jackpot BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX IF NOT EXISTS uq_reward_draw_cards_breed_component
    ON reward_draw_cards(breed_component_id) WHERE breed_component_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_reward_draw_cards_profession_component
    ON reward_draw_cards(profession_component_id) WHERE profession_component_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_reward_draw_cards_treasure_component
    ON reward_draw_cards(treasure_component_id) WHERE treasure_component_id IS NOT NULL;
