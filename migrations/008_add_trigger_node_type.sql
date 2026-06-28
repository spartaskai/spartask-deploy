-- Seed the new TRIGGER_NODE node type in the registry
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES (
    'TRIGGER_NODE', 
    'Trigger Node', 
    'TRIGGER', 
    'Süreci dış kaynaklardan veya API üzerinden gelen eventlerle başlatan başlangıç düğümü.', 
    '{"type": "object", "additionalProperties": true, "description": "Varsayılan başlangıç payload değişkenleri"}', 
    '{"type": "object"}', 
    '{"type": "object"}', 
    'IN_PROCESS',
    TRUE
) ON CONFLICT (code) DO NOTHING;
