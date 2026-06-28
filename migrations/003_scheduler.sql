DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'scheduler_status') THEN
        CREATE TYPE scheduler_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS scheduled_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger_at TIMESTAMP NOT NULL,
    event_name VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    status scheduler_status NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMP DEFAULT NOW(),
    processed_at TIMESTAMP,
    fail_reason TEXT
);

CREATE INDEX idx_scheduled_events_trigger_status ON scheduled_events(trigger_at, status);
