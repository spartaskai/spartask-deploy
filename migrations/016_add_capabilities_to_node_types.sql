-- 1. Add capabilities JSONB column to def_node_types
ALTER TABLE def_node_types 
ADD COLUMN IF NOT EXISTS capabilities JSONB NOT NULL DEFAULT '{}'::jsonb;

-- 2. Update START_NODE to add process initiation capability
UPDATE def_node_types
SET capabilities = '{"can_start_pipeline": true}'::jsonb
WHERE code = 'START_NODE';

-- 3. Update API_LISTENER to add process initiation capability
UPDATE def_node_types
SET capabilities = '{"can_start_pipeline": true}'::jsonb
WHERE code = 'API_LISTENER';

-- 4. In case there are existing nodes using TRIGGER_NODE, convert them to START_NODE
-- This prevents foreign key violations when we delete TRIGGER_NODE
UPDATE def_nodes
SET type = 'START_NODE'
WHERE type = 'TRIGGER_NODE';

-- 5. Delete TRIGGER_NODE type from def_node_types registry
DELETE FROM def_node_types
WHERE code = 'TRIGGER_NODE';
