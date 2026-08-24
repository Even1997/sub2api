CREATE TABLE IF NOT EXISTS reward_draw_wallets (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    ticket_balance BIGINT NOT NULL DEFAULT 0 CHECK (ticket_balance >= 0),
    consumption_remainder_units BIGINT NOT NULL DEFAULT 0
        CHECK (consumption_remainder_units >= 0 AND consumption_remainder_units < 1000000),
    lifetime_consumption_units BIGINT NOT NULL DEFAULT 0 CHECK (lifetime_consumption_units >= 0),
    lifetime_tickets BIGINT NOT NULL DEFAULT 0 CHECK (lifetime_tickets >= 0),
    lifetime_draws BIGINT NOT NULL DEFAULT 0 CHECK (lifetime_draws >= 0),
    wool_redeemed BIGINT NOT NULL DEFAULT 0 CHECK (wool_redeemed >= 0),
    reward_units_redeemed BIGINT NOT NULL DEFAULT 0 CHECK (reward_units_redeemed >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS reward_draw_ledger (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_type VARCHAR(32) NOT NULL,
    source_key VARCHAR(256) NOT NULL,
    ticket_delta BIGINT NOT NULL DEFAULT 0,
    ticket_after BIGINT NOT NULL DEFAULT 0 CHECK (ticket_after >= 0),
    consumption_units BIGINT NOT NULL DEFAULT 0 CHECK (consumption_units >= 0),
    wool_delta BIGINT NOT NULL DEFAULT 0,
    reward_units BIGINT NOT NULL DEFAULT 0 CHECK (reward_units >= 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_reward_draw_ledger_source UNIQUE (user_id, source_type, source_key)
);

CREATE INDEX IF NOT EXISTS idx_reward_draw_ledger_user_created
    ON reward_draw_ledger(user_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS reward_draw_batches (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_key VARCHAR(64) NOT NULL,
    draw_count SMALLINT NOT NULL CHECK (draw_count BETWEEN 1 AND 100),
    pool_version INTEGER NOT NULL,
    total_wool BIGINT NOT NULL DEFAULT 0 CHECK (total_wool >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_reward_draw_batch_request UNIQUE (user_id, request_key)
);

CREATE INDEX IF NOT EXISTS idx_reward_draw_batches_user_created
    ON reward_draw_batches(user_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS reward_draw_redemptions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_count INTEGER NOT NULL CHECK (card_count > 0),
    wool BIGINT NOT NULL CHECK (wool > 0),
    reward_units BIGINT NOT NULL CHECK (reward_units > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reward_draw_redemptions_user_created
    ON reward_draw_redemptions(user_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS reward_draw_cards (
    id BIGSERIAL PRIMARY KEY,
    batch_id BIGINT NOT NULL REFERENCES reward_draw_batches(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    position SMALLINT NOT NULL CHECK (position >= 0),
    breed_id VARCHAR(32) NOT NULL,
    breed_name VARCHAR(64) NOT NULL,
    profession_id VARCHAR(32) NOT NULL,
    profession_name VARCHAR(64) NOT NULL,
    treasure_id VARCHAR(32) NOT NULL,
    treasure_name VARCHAR(64) NOT NULL,
    recipe_id VARCHAR(32) NOT NULL DEFAULT '',
    title VARCHAR(96) NOT NULL,
    wool_value BIGINT NOT NULL CHECK (wool_value > 0),
    redemption_id BIGINT REFERENCES reward_draw_redemptions(id),
    redeemed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_reward_draw_card_position UNIQUE (batch_id, position)
);

CREATE INDEX IF NOT EXISTS idx_reward_draw_cards_user_available
    ON reward_draw_cards(user_id, created_at DESC, id DESC)
    WHERE redeemed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_reward_draw_cards_user_combination
    ON reward_draw_cards(user_id, breed_id, profession_id, treasure_id);

-- Start sheep-bell accounting at 2026-08-10 00:00:00 Asia/Shanghai.
-- Round each settled usage row exactly as the runtime accrual path does.
INSERT INTO reward_draw_wallets (
    user_id,
    ticket_balance,
    consumption_remainder_units,
    lifetime_consumption_units,
    lifetime_tickets
)
SELECT
    user_id,
    consumption_units / 1000000,
    consumption_units % 1000000,
    consumption_units,
    consumption_units / 1000000
FROM (
    SELECT
        user_id,
        SUM(ROUND(GREATEST(actual_cost, 0) * 1000000)::BIGINT)::BIGINT AS consumption_units
    FROM usage_logs
    WHERE actual_cost > 0
      AND created_at >= TIMESTAMPTZ '2026-08-10 00:00:00+08'
    GROUP BY user_id
) opening_usage
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO reward_draw_ledger (
    user_id,
    source_type,
    source_key,
    ticket_delta,
    ticket_after,
    consumption_units,
    metadata
)
SELECT
    user_id,
    'migration',
    'reward-draw-cutoff-20260810-v1',
    ticket_balance,
    ticket_balance,
    lifetime_consumption_units,
    '{"description":"opening sheep bells from usage since 2026-08-10 00:00 Asia/Shanghai"}'::jsonb
FROM reward_draw_wallets
WHERE lifetime_consumption_units > 0
ON CONFLICT (user_id, source_type, source_key) DO NOTHING;
