-- Docker otomatik oluşturduğu için CREATE DATABASE komutuna gerek yok, 
-- veritabanı adını docker-compose içinde vereceğiz.
-- Sadece tabloları oluşturuyoruz.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- DURUM/STATÜ ENUM TANIMLARI
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'instance_status') THEN
        CREATE TYPE instance_status AS ENUM ('RUNNING', 'COMPLETED', 'FAILED', 'SUSPENDED');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'queue_status') THEN
        CREATE TYPE queue_status AS ENUM ('QUEUED', 'LOCKED', 'COMPLETED', 'FAILED');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'execution_status') THEN
        CREATE TYPE execution_status AS ENUM ('PENDING', 'SUCCESS', 'FAILURE');
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS def_pipelines (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    version INT DEFAULT 1,
    is_active BOOLEAN DEFAULT TRUE,
    ui_layout JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS def_nodes (
    id UUID PRIMARY KEY,
    pipeline_definition_id UUID NOT NULL REFERENCES def_pipelines(id) ON DELETE CASCADE,
    type VARCHAR(100) NOT NULL,
    name VARCHAR(255) NOT NULL,
    configuration JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS def_node_subscriptions (
    id UUID PRIMARY KEY,
    pipeline_node_id UUID NOT NULL REFERENCES def_nodes(id) ON DELETE CASCADE,
    event_name VARCHAR(255) NOT NULL,
    filter_rule JSONB
);

CREATE TABLE IF NOT EXISTS def_node_publications (
    id UUID PRIMARY KEY,
    pipeline_node_id UUID NOT NULL REFERENCES def_nodes(id) ON DELETE CASCADE,
    event_name VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS run_instances (
    id UUID PRIMARY KEY,
    pipeline_definition_id UUID NOT NULL REFERENCES def_pipelines(id) ON DELETE CASCADE,
    status instance_status NOT NULL,
    global_context JSONB,
    started_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS run_events (
    id UUID PRIMARY KEY,
    pipeline_instance_id UUID NOT NULL REFERENCES run_instances(id) ON DELETE CASCADE,
    event_name VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL,
    producer_node_id UUID,
    emitted_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS run_queue (
    id UUID PRIMARY KEY,
    pipeline_instance_id UUID NOT NULL REFERENCES run_instances(id) ON DELETE CASCADE,
    pipeline_node_id UUID NOT NULL REFERENCES def_nodes(id) ON DELETE CASCADE,
    trigger_event_id UUID NOT NULL REFERENCES run_events(id) ON DELETE CASCADE,
    status queue_status DEFAULT 'QUEUED',
    retry_count INT DEFAULT 0,
    next_try_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS run_executions (
    id UUID PRIMARY KEY,
    pipeline_instance_id UUID NOT NULL REFERENCES run_instances(id) ON DELETE CASCADE,
    pipeline_node_id UUID NOT NULL REFERENCES def_nodes(id) ON DELETE CASCADE,
    triggered_by_event_id UUID,
    status execution_status NOT NULL,
    input_snapshot JSONB,
    output_snapshot JSONB,
    error_message TEXT,
    started_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    finished_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_queue_status ON run_queue (status, next_try_at);
CREATE INDEX IF NOT EXISTS idx_event_name ON def_node_subscriptions (event_name);