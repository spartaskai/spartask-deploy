-- Create the Node Types registry table
CREATE TABLE IF NOT EXISTS def_node_types (
    code VARCHAR(100) PRIMARY KEY,        -- Unique identifier: 'EMAIL_SENDER', 'AI_SUMMARIZER', 'WEBHOOK'
    name VARCHAR(255) NOT NULL,           -- Human-readable name: 'Send Email'
    category VARCHAR(100) NOT NULL,       -- Grouping: 'COMMUNICATION', 'AI', 'INTEGRATION', 'UTILITY'
    description TEXT,                      -- Brief details about what the node does
    
    -- JSON Schemas for validation and UI generation
    config_schema JSONB NOT NULL,         -- Defines configuration options
    input_schema JSONB NOT NULL,          -- Defines required input payload
    output_schema JSONB NOT NULL,         -- Defines produced outputs
    
    -- Execution strategy parameters
    execution_strategy VARCHAR(50) NOT NULL DEFAULT 'IN_PROCESS', -- 'IN_PROCESS', 'DISTRIBUTED_NATS', 'WEBHOOK'
    execution_target TEXT,                -- Target URL if WEBHOOK, or NATS queue group if DISTRIBUTED_NATS
    
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Populate with initial types (optional default types)
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES 
(
    'UI_RENDERER', 
    'Form Executor', 
    'UI', 
    'Generates a dynamic schema for a react form.', 
    '{"type": "object", "properties": {"form_id": {"type": "string", "format": "uuid"}}, "required": ["form_id"]}', 
    '{"type": "object"}', 
    '{"type": "object", "properties": {"form_id": {"type": "string"}, "schema": {"type": "object"}}}', 
    'IN_PROCESS',
    TRUE
),
(
    'EVENT_BUS', 
    'Event Bus', 
    'INTEGRATION', 
    'Routes events through NATS message broker.', 
    '{"type": "object"}', 
    '{"type": "object"}', 
    '{"type": "object"}', 
    'IN_PROCESS',
    FALSE
),
(
    'START_NODE', 
    'Start Node', 
    'UTILITY', 
    'Initial starting point of the workflow pipeline.', 
    '{"type": "object"}', 
    '{"type": "object"}', 
    '{"type": "object"}', 
    'IN_PROCESS',
    FALSE
)
ON CONFLICT (code) DO NOTHING;

-- Modify def_nodes to create a foreign key link to our registry
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 
        FROM information_schema.table_constraints 
        WHERE constraint_name = 'fk_node_type' AND table_name = 'def_nodes'
    ) THEN
        ALTER TABLE def_nodes 
        ADD CONSTRAINT fk_node_type 
        FOREIGN KEY (type) REFERENCES def_node_types(code);
    END IF;
END $$;
