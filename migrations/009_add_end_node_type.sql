-- Seed the new END_NODE node type in the registry
INSERT INTO def_node_types (code, name, category, description, config_schema, input_schema, output_schema, execution_strategy, is_active)
VALUES (
    'END_NODE', 
    'End Node', 
    'SYSTEM', 
    'Süreci başarıyla veya başarısızlıkla sonlandıran bitiş düğümü.', 
    '{"type": "object", "properties": {"end_status": {"type": "string", "enum": ["COMPLETED", "FAILED"], "default": "COMPLETED"}}, "required": ["end_status"]}', 
    '{"type": "object"}', 
    '{"type": "object"}', 
    'IN_PROCESS',
    TRUE
) ON CONFLICT (code) DO NOTHING;
