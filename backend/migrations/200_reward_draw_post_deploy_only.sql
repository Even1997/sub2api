WITH opening AS MATERIALIZED (
    SELECT user_id, consumption_units
    FROM reward_draw_ledger
    WHERE source_type = 'migration'
      AND source_key = 'compute-cottage-opening-v1'
),
before_reset AS MATERIALIZED (
    SELECT
        w.user_id,
        w.ticket_balance AS old_ticket_balance,
        GREATEST(w.lifetime_consumption_units - o.consumption_units, 0) AS post_deploy_units,
        w.lifetime_draws
    FROM reward_draw_wallets w
    JOIN opening o ON o.user_id = w.user_id
),
reset_wallets AS (
    UPDATE reward_draw_wallets w
    SET ticket_balance = GREATEST((b.post_deploy_units / 1000000) - b.lifetime_draws, 0),
        consumption_remainder_units = b.post_deploy_units % 1000000,
        lifetime_consumption_units = b.post_deploy_units,
        lifetime_tickets = b.post_deploy_units / 1000000,
        updated_at = NOW()
    FROM before_reset b
    WHERE w.user_id = b.user_id
    RETURNING w.user_id, b.old_ticket_balance, w.ticket_balance AS new_ticket_balance
),
remove_opening AS (
    DELETE FROM reward_draw_ledger l
    USING opening o
    WHERE l.user_id = o.user_id
      AND l.source_type = 'migration'
      AND l.source_key = 'compute-cottage-opening-v1'
    RETURNING l.user_id
)
INSERT INTO reward_draw_ledger (
    user_id,
    source_type,
    source_key,
    ticket_delta,
    ticket_after,
    metadata
)
SELECT
    r.user_id,
    'migration',
    'reward-draw-post-deploy-only-v1',
    r.new_ticket_balance - r.old_ticket_balance,
    r.new_ticket_balance,
    '{"description":"removed pre-deployment consumption from sheep bells"}'::jsonb
FROM reset_wallets r
WHERE EXISTS (SELECT 1 FROM remove_opening d WHERE d.user_id = r.user_id)
ON CONFLICT (user_id, source_type, source_key) DO NOTHING;
