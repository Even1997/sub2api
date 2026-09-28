CREATE TABLE IF NOT EXISTS openai_oauth_reauth_worker_control (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_seen_at TIMESTAMPTZ,
    last_worker_id TEXT NOT NULL DEFAULT ''
);
INSERT INTO openai_oauth_reauth_worker_control (id) VALUES (1)
ON CONFLICT (id) DO NOTHING;

-- Only structured metadata is retained. Never copy credentials or error bodies.
CREATE TABLE IF NOT EXISTS openai_oauth_reauth_events (
    id BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('task', 'paused', 'resumed')),
    task_id BIGINT REFERENCES openai_oauth_reauth_tasks(id) ON DELETE CASCADE,
    account_id BIGINT,
    worker_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    stage TEXT NOT NULL DEFAULT '',
    attempt INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE OR REPLACE FUNCTION record_openai_oauth_reauth_task_event() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND
       (NEW.status, NEW.stage, NEW.worker_id, NEW.attempt) IS NOT DISTINCT FROM
       (OLD.status, OLD.stage, OLD.worker_id, OLD.attempt) THEN
        RETURN NEW;
    END IF;
    INSERT INTO openai_oauth_reauth_events (kind, task_id, account_id, worker_id, status, stage, attempt)
    VALUES ('task', NEW.id, NEW.account_id, COALESCE(NEW.worker_id, ''), NEW.status, NEW.stage, NEW.attempt);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS openai_oauth_reauth_task_event ON openai_oauth_reauth_tasks;
CREATE TRIGGER openai_oauth_reauth_task_event
AFTER INSERT OR UPDATE ON openai_oauth_reauth_tasks
FOR EACH ROW EXECUTE FUNCTION record_openai_oauth_reauth_task_event();

CREATE OR REPLACE FUNCTION trim_openai_oauth_reauth_events() RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM openai_oauth_reauth_events
    WHERE id < (
        SELECT id FROM openai_oauth_reauth_events
        ORDER BY id DESC OFFSET 999 LIMIT 1
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS openai_oauth_reauth_events_retention ON openai_oauth_reauth_events;
CREATE TRIGGER openai_oauth_reauth_events_retention
AFTER INSERT ON openai_oauth_reauth_events
FOR EACH ROW EXECUTE FUNCTION trim_openai_oauth_reauth_events();
