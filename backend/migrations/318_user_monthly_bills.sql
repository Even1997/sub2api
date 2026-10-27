-- Billing snapshots never modify wallet balances. Historical completeness cannot
-- be inferred from the oldest remaining usage log after retention cleanup.
CREATE TABLE monthly_bill_coverage (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    complete_from TIMESTAMPTZ NOT NULL
);
INSERT INTO monthly_bill_coverage VALUES (TRUE, NOW());

CREATE TABLE monthly_bill_missing_months (
    user_id BIGINT NOT NULL,
    month TEXT NOT NULL,
    PRIMARY KEY (user_id, month)
);
CREATE FUNCTION mark_monthly_bill_missing_usage() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO monthly_bill_missing_months(user_id, month)
    VALUES (OLD.user_id, to_char(OLD.created_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM'))
    ON CONFLICT DO NOTHING;
    RETURN OLD;
END;
$$;
CREATE TRIGGER monthly_bill_usage_deleted BEFORE DELETE ON usage_logs
    FOR EACH ROW EXECUTE FUNCTION mark_monthly_bill_missing_usage();

CREATE FUNCTION mark_monthly_bill_truncated_usage() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE monthly_bill_coverage SET complete_from=GREATEST(complete_from, clock_timestamp());
    RETURN NULL;
END;
$$;
CREATE TRIGGER monthly_bill_usage_truncated BEFORE TRUNCATE ON usage_logs
    EXECUTE FUNCTION mark_monthly_bill_truncated_usage();

CREATE INDEX usage_billing_events_unsettled_user_time
    ON usage_billing_events ((payload->'command'->>'UserID'), observed_at)
    WHERE state IN ('pending','applied','blocked');

CREATE TABLE user_monthly_bills (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    month TEXT NOT NULL CHECK (month ~ '^\d{4}-(0[1-9]|1[0-2])$'),
    version INTEGER NOT NULL CHECK (version > 0),
    generated_at TIMESTAMPTZ NOT NULL,
    snapshot JSONB NOT NULL,
    PRIMARY KEY (user_id, month, version)
);
