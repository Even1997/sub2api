-- Reward Draw v3: raw components target ~1% and fixed recipes target ~1.5%.
-- Finished cards keep their original value; only component inventory is rebalanced.
UPDATE reward_draw_components
SET wool_value = CASE tier_id
    WHEN 'code' THEN 7
    WHEN 'reasoning' THEN 9
    WHEN 'speed' THEN 11
    WHEN 'inspiration' THEN 13
    WHEN 'wisdom' THEN 20
    WHEN 'fortune' THEN 40
    WHEN 'miracle' THEN 100000
    ELSE wool_value
END
WHERE tier_id IN ('code', 'reasoning', 'speed', 'inspiration', 'wisdom', 'fortune', 'miracle');

UPDATE reward_draw_batches AS batch
SET total_wool = component_totals.total_wool
FROM (
    SELECT batch_id, SUM(wool_value) AS total_wool
    FROM reward_draw_components
    GROUP BY batch_id
) AS component_totals
WHERE batch.id = component_totals.batch_id;
