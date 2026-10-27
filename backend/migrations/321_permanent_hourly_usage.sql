-- Hourly financial history is independent of request-log retention.
-- No cascading foreign keys to keys/groups/logs: deleting them must not erase spend.
ALTER TABLE usage_logs ADD COLUMN hourly_summary_recorded BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX usage_logs_hourly_pending ON usage_logs(user_id,created_at,id) WHERE NOT hourly_summary_recorded;
CREATE TABLE user_usage_hourly (
    hour TIMESTAMPTZ NOT NULL,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    model TEXT NOT NULL,
    requested_model TEXT NOT NULL,
    billing_type SMALLINT NOT NULL,
    billing_mode TEXT NOT NULL,
    request_type SMALLINT NOT NULL,
    stream BOOLEAN NOT NULL,
    openai_ws_mode BOOLEAN NOT NULL,
    native_compaction_v2 BOOLEAN NOT NULL,
    requests BIGINT NOT NULL,
    input_tokens BIGINT NOT NULL,
    output_tokens BIGINT NOT NULL,
    cache_creation_tokens BIGINT NOT NULL,
    cache_read_tokens BIGINT NOT NULL,
    image_count BIGINT NOT NULL,
    video_count BIGINT NOT NULL,
    video_seconds BIGINT NOT NULL,
    total_cost NUMERIC NOT NULL,
    actual_cost NUMERIC NOT NULL,
    PRIMARY KEY (hour,user_id,api_key_id,group_id,model,requested_model,billing_type,billing_mode,request_type,stream,openai_ws_mode,native_compaction_v2)
);
CREATE INDEX user_usage_hourly_user_time ON user_usage_hourly(user_id,hour);

CREATE FUNCTION apply_user_usage_hourly(r usage_logs, delta BIGINT) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO user_usage_hourly (hour,user_id,api_key_id,group_id,model,requested_model,billing_type,billing_mode,request_type,stream,openai_ws_mode,native_compaction_v2,requests,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,image_count,video_count,video_seconds,total_cost,actual_cost)
    VALUES (date_trunc('hour', r.created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
        r.user_id,
        COALESCE(r.api_key_id,0),
        COALESCE(r.group_id,0),
        COALESCE(r.model,''),
        COALESCE(NULLIF(TRIM(r.requested_model),''),r.model,''),
        COALESCE(r.billing_type,0),
        COALESCE(NULLIF(r.billing_mode,''),CASE WHEN r.image_count>0 THEN 'image' ELSE 'token' END),
        COALESCE(r.request_type,0),
        COALESCE(r.stream,FALSE),
        COALESCE(r.openai_ws_mode,FALSE),
        COALESCE(r.native_compaction_v2,FALSE),
        delta,
        COALESCE(r.input_tokens,0)::bigint * delta,
        COALESCE(r.output_tokens,0)::bigint * delta,
        COALESCE(r.cache_creation_tokens,0)::bigint * delta,
        COALESCE(r.cache_read_tokens,0)::bigint * delta,
        COALESCE(r.image_count,0)::bigint * delta,
        COALESCE(r.video_count,0)::bigint * delta,
        COALESCE(r.video_duration_seconds,0)::bigint * COALESCE(r.video_count,0) * delta,
        COALESCE(r.total_cost,0) * delta,
        COALESCE(r.actual_cost,0) * delta)
    ON CONFLICT (hour,user_id,api_key_id,group_id,model,requested_model,billing_type,billing_mode,request_type,stream,openai_ws_mode,native_compaction_v2) DO UPDATE SET
        requests = user_usage_hourly.requests + EXCLUDED.requests,
        input_tokens = user_usage_hourly.input_tokens + EXCLUDED.input_tokens,
        output_tokens = user_usage_hourly.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = user_usage_hourly.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = user_usage_hourly.cache_read_tokens + EXCLUDED.cache_read_tokens,
        image_count = user_usage_hourly.image_count + EXCLUDED.image_count,
        video_count = user_usage_hourly.video_count + EXCLUDED.video_count,
        video_seconds = user_usage_hourly.video_seconds + EXCLUDED.video_seconds,
        total_cost = user_usage_hourly.total_cost + EXCLUDED.total_cost,
        actual_cost = user_usage_hourly.actual_cost + EXCLUDED.actual_cost;
END;
$$;

-- BEFORE only marks inserts. AFTER adds totals, so ON CONFLICT DO NOTHING
-- cannot count a rejected insert. All effects share the billing transaction.
CREATE FUNCTION maintain_user_usage_hourly() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF TG_WHEN='BEFORE' THEN
            NEW.hourly_summary_recorded := TRUE;
        ELSE
            PERFORM apply_user_usage_hourly(NEW,1);
        END IF;
        RETURN NEW;
    ELSIF TG_OP='DELETE' THEN
        IF NOT OLD.hourly_summary_recorded THEN
            PERFORM apply_user_usage_hourly(OLD,1);
        END IF;
        RETURN OLD;
    END IF;
    -- Moving a log between time partitions fires DELETE+INSERT rather than
    -- AFTER UPDATE. Keep the original billing timestamp immutable to prevent
    -- a retention delete from being mistaken for a correction.
    IF OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Usage billing timestamp is immutable';
    END IF;
    IF NOT OLD.hourly_summary_recorded THEN
        PERFORM apply_user_usage_hourly(NEW,1);
    ELSIF ROW(OLD.created_at,OLD.user_id,OLD.api_key_id,OLD.group_id,OLD.model,OLD.requested_model,OLD.billing_type,OLD.billing_mode,OLD.request_type,OLD.stream,OLD.openai_ws_mode,OLD.native_compaction_v2,OLD.video_duration_seconds,OLD.input_tokens,OLD.output_tokens,OLD.cache_creation_tokens,OLD.cache_read_tokens,OLD.image_count,OLD.video_count,OLD.total_cost,OLD.actual_cost) IS DISTINCT FROM ROW(NEW.created_at,NEW.user_id,NEW.api_key_id,NEW.group_id,NEW.model,NEW.requested_model,NEW.billing_type,NEW.billing_mode,NEW.request_type,NEW.stream,NEW.openai_ws_mode,NEW.native_compaction_v2,NEW.video_duration_seconds,NEW.input_tokens,NEW.output_tokens,NEW.cache_creation_tokens,NEW.cache_read_tokens,NEW.image_count,NEW.video_count,NEW.total_cost,NEW.actual_cost) THEN
        PERFORM apply_user_usage_hourly(OLD,-1);
        PERFORM apply_user_usage_hourly(NEW,1);
    END IF;
    NEW.hourly_summary_recorded := TRUE;
    RETURN NEW;
END;
$$;
CREATE TRIGGER user_usage_hourly_before BEFORE INSERT OR UPDATE OR DELETE ON usage_logs
    FOR EACH ROW EXECUTE FUNCTION maintain_user_usage_hourly();
CREATE TRIGGER user_usage_hourly_insert AFTER INSERT ON usage_logs
    FOR EACH ROW EXECUTE FUNCTION maintain_user_usage_hourly();

-- Row locks make each bounded batch retryable and safe across multiple instances.
CREATE FUNCTION backfill_user_usage_hourly(batch_size INTEGER DEFAULT 1000) RETURNS BIGINT LANGUAGE plpgsql AS $$
DECLARE affected BIGINT;
BEGIN
    WITH pending AS (
        SELECT tableoid,ctid FROM usage_logs WHERE NOT hourly_summary_recorded
        ORDER BY user_id,created_at,id LIMIT batch_size FOR UPDATE SKIP LOCKED
    )
    UPDATE usage_logs u SET hourly_summary_recorded=TRUE
    FROM pending p WHERE u.tableoid=p.tableoid AND u.ctid=p.ctid;
    GET DIAGNOSTICS affected=ROW_COUNT;
    RETURN affected;
END;
$$;

CREATE FUNCTION guard_hourly_usage_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE pending BOOLEAN;
BEGIN
    EXECUTE format('SELECT EXISTS (SELECT 1 FROM %s WHERE NOT hourly_summary_recorded)',TG_RELID::regclass) INTO pending;
    IF pending THEN RAISE EXCEPTION 'Hourly usage backfill pending; cannot truncate request history'; END IF;
    RETURN NULL;
END;
$$;
DROP TRIGGER monthly_bill_usage_deleted ON usage_logs;
DROP TRIGGER monthly_bill_usage_truncated ON usage_logs;
CREATE TRIGGER user_usage_hourly_truncate BEFORE TRUNCATE ON usage_logs
    EXECUTE FUNCTION guard_hourly_usage_truncate();
-- Statement triggers are not inherited by partitions. Existing partitions can
-- still contain legacy rows, so protect direct TRUNCATE as well.
DO $$
DECLARE partition_name REGCLASS;
BEGIN
    FOR partition_name IN SELECT inhrelid::regclass FROM pg_inherits WHERE inhparent='usage_logs'::regclass LOOP
        EXECUTE format('CREATE TRIGGER user_usage_hourly_truncate BEFORE TRUNCATE ON %s EXECUTE FUNCTION guard_hourly_usage_truncate()',partition_name);
    END LOOP;
END;
$$;
-- Preserve the existing conservative coverage boundary and missing-month facts.
